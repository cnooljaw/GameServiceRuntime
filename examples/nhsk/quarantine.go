package nhsk

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	applyDiagnosticExportResultCommand gsr.CommandID = 0x0410f009
	reportBattleQuarantinedCommand     gsr.CommandID = 0x0410f015
	listQuarantinedAdminCommand        gsr.CommandID = 0x0410f016
	retryDiagnosticAdminCommand        gsr.CommandID = 0x0410f017
	releaseQuarantinedAdminCommand     gsr.CommandID = 0x0410f018
	captureBattleDiagnosticCommand     gsr.CommandID = 0x0410f01a
)

var (
	errInvalidDiagnosticArtifact = errors.New("nhsk: invalid diagnostic artifact")
	errDiagnosticQueueFull       = errors.New("nhsk: diagnostic queue full")
	// ErrDiagnosticReceiptMismatch indicates that a release receipt does not bind the retained entry.
	ErrDiagnosticReceiptMismatch = errors.New("nhsk: diagnostic receipt mismatch")
)

// BattleCommandRecord is one immutable input observed immediately before a Battle handler.
type BattleCommandRecord struct {
	Sequence   uint64
	RecordedAt time.Time
	Source     gsr.ServiceRef
	CommandID  gsr.CommandID
	Payload    json.RawMessage
}

// BattleDiagnosticEvidence is the last stable Battle view and complete ordered input history.
type BattleDiagnosticEvidence struct {
	FailedAt           time.Time
	FailedCommand      gsr.CommandID
	CommandSequence    uint64
	LastStableSnapshot NHSKBattleSnapshot
	Commands           []BattleCommandRecord
	RandomSeed         int64
	RandomSeedKnown    bool
	ClockAtFailure     time.Time
	Stack              string
}

// DiagnosticArtifact is an immutable exporter input for one exact failed ServiceRef.
type DiagnosticArtifact struct {
	Host                 gsr.ServiceRef
	BattleID             game.BattleID
	Ref                  gsr.ServiceRef
	ConnectionGeneration ConnectionGeneration
	Evidence             BattleDiagnosticEvidence
	RuntimeInspection    gsr.RuntimeInspection
}

// DiagnosticExporter atomically publishes evidence and returns its release receipt.
type DiagnosticExporter interface {
	ExportDiagnostic(context.Context, DiagnosticArtifact) (DiagnosticReceipt, error)
}

// DiagnosticSubmitter accepts evidence without blocking a Host Mailbox.
type DiagnosticSubmitter interface {
	SubmitDiagnostic(DiagnosticArtifact) error
}

// DiagnosticRuntime is the narrow process capability used by fixed diagnostic workers.
type DiagnosticRuntime interface {
	game.CommandRuntime
	Inspect() gsr.RuntimeInspection
}

// DiagnosticRunnerConfig bounds diagnostic filesystem work.
type DiagnosticRunnerConfig struct {
	QueueSize int
	Workers   int
}

// DiagnosticRunner owns fixed exporter workers outside every Service handler.
type DiagnosticRunner struct {
	runtime   DiagnosticRuntime
	exporter  DiagnosticExporter
	queue     chan DiagnosticArtifact
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
	lifecycle sync.RWMutex
}

type diagnosticExportResult struct {
	BattleID game.BattleID
	Ref      gsr.ServiceRef
	Receipt  DiagnosticReceipt
	Error    string
}

// NewDiagnosticRunner starts fixed, bounded diagnostic workers.
func NewDiagnosticRunner(runtime DiagnosticRuntime, exporter DiagnosticExporter, config DiagnosticRunnerConfig) (*DiagnosticRunner, error) {
	if runtime == nil || exporter == nil {
		return nil, errInvalidDiagnosticArtifact
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 16
	}
	if config.Workers <= 0 {
		config.Workers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := &DiagnosticRunner{runtime: runtime, exporter: exporter, queue: make(chan DiagnosticArtifact, config.QueueSize), ctx: ctx, cancel: cancel}
	runner.wg.Add(config.Workers)
	for index := 0; index < config.Workers; index++ {
		go runner.worker()
	}
	return runner, nil
}

// SubmitDiagnostic copies and enqueues evidence without blocking a Host Mailbox.
func (runner *DiagnosticRunner) SubmitDiagnostic(artifact DiagnosticArtifact) error {
	if runner == nil || artifact.BattleID == 0 || artifact.Ref.ID == 0 || artifact.Host.ID == 0 {
		return errInvalidDiagnosticArtifact
	}
	runner.lifecycle.RLock()
	defer runner.lifecycle.RUnlock()
	artifact.Evidence.LastStableSnapshot = cloneBattleSnapshot(artifact.Evidence.LastStableSnapshot)
	artifact.Evidence.Commands = cloneCommandRecords(artifact.Evidence.Commands)
	select {
	case <-runner.ctx.Done():
		return context.Canceled
	case runner.queue <- artifact:
		return nil
	default:
		return errDiagnosticQueueFull
	}
}

// Close cancels queued work and waits for active workers to return.
func (runner *DiagnosticRunner) Close() error {
	if runner == nil {
		return nil
	}
	runner.closeOnce.Do(func() {
		runner.lifecycle.Lock()
		defer runner.lifecycle.Unlock()
		runner.cancel()
		runner.wg.Wait()
	})
	return nil
}

func (runner *DiagnosticRunner) worker() {
	defer runner.wg.Done()
	for {
		select {
		case <-runner.ctx.Done():
			return
		case artifact := <-runner.queue:
			if runner.ctx.Err() != nil {
				return
			}
			artifact.RuntimeInspection = runner.runtime.Inspect()
			receipt, err := runner.exporter.ExportDiagnostic(runner.ctx, artifact)
			result := diagnosticExportResult{BattleID: artifact.BattleID, Ref: artifact.Ref, Receipt: receipt}
			if err != nil {
				result.Error = err.Error()
			}
			_ = runner.runtime.Send(artifact.Host, applyDiagnosticExportResultCommand, result)
		}
	}
}

type retryDiagnosticRequest struct {
	BattleID game.BattleID
	Ref      gsr.ServiceRef
}

type releaseQuarantinedRequest struct{ Receipt DiagnosticReceipt }

// QuarantineAdmin exposes node-local diagnostic operations. Its private
// CommandIDs are deliberately absent from Cluster codecs and Legacy routing.
type QuarantineAdmin struct {
	runtime game.CommandRuntime
	host    gsr.ServiceRef
	root    string
}

// NewQuarantineAdmin binds node-local operations to one process Host.
func NewQuarantineAdmin(runtime game.CommandRuntime, host gsr.ServiceRef, diagnosticRoot string) *QuarantineAdmin {
	return &QuarantineAdmin{runtime: runtime, host: host, root: diagnosticRoot}
}

// ListQuarantined returns independent retained-entry projections.
func (admin *QuarantineAdmin) ListQuarantined(ctx context.Context) ([]QuarantinedBattleSnapshot, error) {
	if admin == nil || admin.runtime == nil || admin.host.ID == 0 {
		return nil, errInvalidHostConfig
	}
	value, err := admin.runtime.Call(ctx, admin.host, listQuarantinedAdminCommand, nil)
	if err != nil {
		return nil, err
	}
	entries, ok := value.([]QuarantinedBattleSnapshot)
	if !ok {
		return nil, gsr.ErrInvalidClusterEnvelope
	}
	return entries, nil
}

// RetryDiagnostic explicitly resubmits one exact retained entry.
func (admin *QuarantineAdmin) RetryDiagnostic(ctx context.Context, battleID game.BattleID, ref gsr.ServiceRef) error {
	value, err := admin.runtime.Call(ctx, admin.host, retryDiagnosticAdminCommand, retryDiagnosticRequest{BattleID: battleID, Ref: ref})
	return adminResultError(value, err)
}

// ReleaseQuarantinedBattle releases only the exact entry bound by a published receipt.
func (admin *QuarantineAdmin) ReleaseQuarantinedBattle(ctx context.Context, receipt DiagnosticReceipt) error {
	value, err := admin.runtime.Call(ctx, admin.host, releaseQuarantinedAdminCommand, releaseQuarantinedRequest{Receipt: receipt})
	return adminResultError(value, err)
}

// CleanupDiagnosticMaterial removes a published directory independently from Battle release.
func (admin *QuarantineAdmin) CleanupDiagnosticMaterial(receipt DiagnosticReceipt) error {
	if admin == nil || strings.TrimSpace(admin.root) == "" {
		return errInvalidDiagnosticArtifact
	}
	return (LocalDiagnosticExporter{Root: admin.root}).CleanupDiagnostic(receipt)
}

func adminResultError(value any, err error) error {
	if err != nil {
		return err
	}
	result, ok := value.(HostCommandResult)
	if !ok {
		return gsr.ErrInvalidClusterEnvelope
	}
	if result.Accepted {
		return nil
	}
	if result.Rejection == ErrDiagnosticReceiptMismatch.Error() {
		return ErrDiagnosticReceiptMismatch
	}
	return errors.New(result.Rejection)
}

type battleQuarantineBoundaryConfig struct {
	Host                 gsr.ServiceRef
	Factory              gsr.ServiceRef
	ConnectionGeneration ConnectionGeneration
}

type battleQuarantineReport struct {
	BattleID             game.BattleID
	Ref                  gsr.ServiceRef
	ConnectionGeneration ConnectionGeneration
	Evidence             BattleDiagnosticEvidence
}

type battleQuarantineBoundary struct {
	inner      *NHSKBattleService
	config     battleQuarantineBoundaryConfig
	service    gsr.ServiceContext
	sequence   uint64
	records    []BattleCommandRecord
	lastStable NHSKBattleSnapshot
}

func newBattleQuarantineBoundary(inner *NHSKBattleService, config battleQuarantineBoundaryConfig) (*battleQuarantineBoundary, error) {
	if inner == nil || config.Host.Node == "" || config.Host.ID == 0 {
		return nil, errInvalidBattleConfig
	}
	return &battleQuarantineBoundary{inner: inner, config: config, lastStable: inner.snapshot()}, nil
}

func (boundary *battleQuarantineBoundary) Init(ctx gsr.ServiceContext) error {
	if ctx == nil {
		return errInvalidBattleConfig
	}
	boundary.service = ctx
	return boundary.inner.Init(ctx)
}

func (boundary *battleQuarantineBoundary) Handle(ctx gsr.CommandContext, command gsr.Command) (err error) {
	if command.ID == captureBattleDiagnosticCommand {
		return boundary.inner.reply(ctx, boundary.evidence(command.ID, "diagnostic capture"))
	}
	boundary.sequence++
	payload, encodeErr := json.Marshal(command.Payload)
	if encodeErr != nil {
		payload = []byte(fmt.Sprintf(`{"encoding_error":%q,"payload_type":%q}`, encodeErr.Error(), fmt.Sprintf("%T", command.Payload)))
	}
	boundary.records = append(boundary.records, BattleCommandRecord{Sequence: boundary.sequence, RecordedAt: boundary.service.Now(), Source: ctx.Source(), CommandID: command.ID, Payload: append([]byte(nil), payload...)})
	defer func() {
		if recovered := recover(); recovered != nil {
			boundary.report(command.ID, fmt.Sprintf("panic: %v\n%s", recovered, debug.Stack()))
			panic(recovered)
		}
	}()
	err = boundary.inner.Handle(ctx, command)
	if err == nil {
		if invariantErr := boundary.inner.validateDiagnosticInvariants(); invariantErr != nil {
			boundary.report(command.ID, fmt.Sprintf("invariant: %v\n%s", invariantErr, debug.Stack()))
			panic(invariantErr)
		}
		boundary.lastStable = boundary.inner.snapshot()
	}
	return err
}

func (boundary *battleQuarantineBoundary) report(command gsr.CommandID, stack string) {
	evidence := boundary.evidence(command, stack)
	report := battleQuarantineReport{BattleID: boundary.inner.id, Ref: boundary.service.Self(), ConnectionGeneration: boundary.config.ConnectionGeneration, Evidence: evidence}
	for _, target := range []gsr.ServiceRef{boundary.config.Host, boundary.config.Factory} {
		if target.ID == 0 {
			continue
		}
		if err := boundary.service.Send(target, reportBattleQuarantinedCommand, cloneQuarantineReport(report)); err != nil {
			boundary.service.Metrics().Inc("nhsk_quarantine_report_errors_total")
			boundary.service.Logger().Error("NHSK quarantine report delivery failed", "battle_id", boundary.inner.id, "target", target, "error", err)
		}
	}
}

func (boundary *battleQuarantineBoundary) evidence(command gsr.CommandID, stack string) BattleDiagnosticEvidence {
	return BattleDiagnosticEvidence{
		FailedAt: boundary.service.Now(), FailedCommand: command, CommandSequence: boundary.sequence,
		LastStableSnapshot: cloneBattleSnapshot(boundary.lastStable), Commands: cloneCommandRecords(boundary.records),
		RandomSeed: boundary.inner.randomSeed, RandomSeedKnown: boundary.inner.randomSeedKnown,
		ClockAtFailure: boundary.inner.clock.Now(), Stack: stack,
	}
}

func (boundary *battleQuarantineBoundary) Stop(ctx context.Context) error {
	return boundary.inner.Stop(ctx)
}
func (boundary *battleQuarantineBoundary) Close() error {
	err := boundary.inner.Close()
	boundary.service = nil
	return err
}

func (battle *NHSKBattleService) validateDiagnosticInvariants() error {
	if battle.id == 0 || battle.activeSeat < 0 || battle.activeSeat >= len(battle.bySeat) {
		return errors.New("invalid Battle identity or active seat")
	}
	seen := make(map[game.PlayerID]struct{}, len(battle.players))
	for seat, playerID := range battle.bySeat {
		if playerID == "" {
			continue
		}
		player, exists := battle.players[playerID]
		if !exists || int(player.SeatID) != seat {
			return errors.New("seat index disagrees with player state")
		}
		if _, duplicate := seen[playerID]; duplicate {
			return errors.New("player occupies multiple seats")
		}
		seen[playerID] = struct{}{}
	}
	if battle.phase == NHSKBattlePlaying && !battle.hasFourPlayers() {
		return errors.New("playing Battle does not have four active players")
	}
	return nil
}

func cloneBattleSnapshot(snapshot NHSKBattleSnapshot) NHSKBattleSnapshot {
	snapshot.Players = append([]BattlePlayer(nil), snapshot.Players...)
	hands := make(map[game.PlayerID][]byte, len(snapshot.Hands))
	for player, cards := range snapshot.Hands {
		hands[player] = append([]byte(nil), cards...)
	}
	snapshot.Hands = hands
	auto := make(map[game.PlayerID]bool, len(snapshot.Auto))
	for player, enabled := range snapshot.Auto {
		auto[player] = enabled
	}
	snapshot.Auto = auto
	return snapshot
}

func cloneCommandRecords(records []BattleCommandRecord) []BattleCommandRecord {
	cloned := make([]BattleCommandRecord, len(records))
	copy(cloned, records)
	for index := range cloned {
		cloned[index].Payload = append([]byte(nil), records[index].Payload...)
	}
	return cloned
}

func cloneQuarantineReport(report battleQuarantineReport) battleQuarantineReport {
	report.Evidence.LastStableSnapshot = cloneBattleSnapshot(report.Evidence.LastStableSnapshot)
	report.Evidence.Commands = cloneCommandRecords(report.Evidence.Commands)
	return report
}

// LocalDiagnosticExporter publishes an fsync'd directory below Root using rename.
type LocalDiagnosticExporter struct{ Root string }

// ExportDiagnostic writes complete material, atomically publishes it, then returns a receipt.
func (exporter LocalDiagnosticExporter) ExportDiagnostic(ctx context.Context, artifact DiagnosticArtifact) (DiagnosticReceipt, error) {
	if strings.TrimSpace(exporter.Root) == "" || !validDiagnosticArtifact(artifact) {
		return DiagnosticReceipt{}, errInvalidDiagnosticArtifact
	}
	if err := ctx.Err(); err != nil {
		return DiagnosticReceipt{}, err
	}
	files, digest, err := diagnosticFiles(artifact)
	if err != nil {
		return DiagnosticReceipt{}, err
	}
	if err := os.MkdirAll(exporter.Root, 0o750); err != nil {
		return DiagnosticReceipt{}, err
	}
	temporary, err := os.MkdirTemp(exporter.Root, ".tmp-")
	if err != nil {
		return DiagnosticReceipt{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(temporary)
		}
	}()
	for _, file := range files {
		if err := writeAndSync(filepath.Join(temporary, file.name), file.data); err != nil {
			return DiagnosticReceipt{}, err
		}
	}
	receipt := DiagnosticReceipt{
		ReceiptID: digest, BattleID: artifact.BattleID, Ref: artifact.Ref, Digest: digest,
		CreatedAt: artifact.Evidence.FailedAt,
	}
	node := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(string(artifact.Ref.Node))
	directory := filepath.Join(exporter.Root, fmt.Sprintf("battle-%d-ref-%s-%d-%s", artifact.BattleID, node, artifact.Ref.ID, digest[:16]))
	receipt.Directory = directory
	receiptBytes, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return DiagnosticReceipt{}, err
	}
	receiptBytes = append(receiptBytes, '\n')
	if err := writeAndSync(filepath.Join(temporary, "receipt.json"), receiptBytes); err != nil {
		return DiagnosticReceipt{}, err
	}
	if err := syncDirectory(temporary); err != nil {
		return DiagnosticReceipt{}, err
	}
	if err := os.Rename(temporary, directory); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return DiagnosticReceipt{}, err
		}
		if _, statErr := os.Stat(filepath.Join(directory, "receipt.json")); statErr != nil {
			return DiagnosticReceipt{}, err
		}
	} else {
		cleanup = false
	}
	if err := syncDirectory(exporter.Root); err != nil {
		return DiagnosticReceipt{}, err
	}
	return receipt, nil
}

// CleanupDiagnostic explicitly removes one published material directory after
// validating its receipt and configured-root containment. It does not alter Host state.
func (exporter LocalDiagnosticExporter) CleanupDiagnostic(receipt DiagnosticReceipt) error {
	if strings.TrimSpace(exporter.Root) == "" || !validDiagnosticReceipt(receipt) {
		return ErrDiagnosticReceiptMismatch
	}
	root, err := filepath.Abs(exporter.Root)
	if err != nil {
		return err
	}
	directory, err := filepath.Abs(receipt.Directory)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ErrDiagnosticReceiptMismatch
	}
	data, err := os.ReadFile(filepath.Join(directory, "receipt.json"))
	if err != nil {
		return err
	}
	var published DiagnosticReceipt
	if err := json.Unmarshal(data, &published); err != nil || !sameDiagnosticReceipt(published, receipt) {
		return ErrDiagnosticReceiptMismatch
	}
	return os.RemoveAll(directory)
}

type diagnosticFile struct {
	name string
	data []byte
}

func diagnosticFiles(artifact DiagnosticArtifact) ([]diagnosticFile, string, error) {
	manifest := struct {
		BattleID             game.BattleID
		Ref                  gsr.ServiceRef
		ConnectionGeneration ConnectionGeneration
		FailedAt             time.Time
		FailedCommand        gsr.CommandID
		CommandSequence      uint64
		RandomSeed           int64
		RandomSeedKnown      bool
		ClockAtFailure       time.Time
	}{artifact.BattleID, artifact.Ref, artifact.ConnectionGeneration, artifact.Evidence.FailedAt, artifact.Evidence.FailedCommand, artifact.Evidence.CommandSequence, artifact.Evidence.RandomSeed, artifact.Evidence.RandomSeedKnown, artifact.Evidence.ClockAtFailure}
	encode := func(value any) ([]byte, error) {
		data, err := json.MarshalIndent(value, "", "  ")
		return append(data, '\n'), err
	}
	manifestBytes, err := encode(manifest)
	if err != nil {
		return nil, "", err
	}
	snapshotBytes, err := encode(artifact.Evidence.LastStableSnapshot)
	if err != nil {
		return nil, "", err
	}
	inspectionBytes, err := encode(artifact.RuntimeInspection)
	if err != nil {
		return nil, "", err
	}
	var commands strings.Builder
	writer := bufio.NewWriter(&commands)
	for _, record := range artifact.Evidence.Commands {
		line, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			return nil, "", marshalErr
		}
		if _, writeErr := writer.Write(append(line, '\n')); writeErr != nil {
			return nil, "", writeErr
		}
	}
	if err := writer.Flush(); err != nil {
		return nil, "", err
	}
	files := []diagnosticFile{
		{name: "commands.jsonl", data: []byte(commands.String())},
		{name: "manifest.json", data: manifestBytes},
		{name: "panic.txt", data: []byte(artifact.Evidence.Stack)},
		{name: "runtime-inspection.json", data: inspectionBytes},
		{name: "snapshot.json", data: snapshotBytes},
	}
	sort.Slice(files, func(left, right int) bool { return files[left].name < files[right].name })
	hash := sha256.New()
	for _, file := range files {
		_, _ = io.WriteString(hash, file.name)
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(file.data)
		_, _ = hash.Write([]byte{0})
	}
	return files, hex.EncodeToString(hash.Sum(nil)), nil
}

func validDiagnosticArtifact(artifact DiagnosticArtifact) bool {
	return artifact.BattleID != 0 && artifact.Ref.Node != "" && artifact.Ref.ID != 0 && artifact.Host.Node != "" && artifact.Host.ID != 0 && artifact.Evidence.FailedCommand != 0 && artifact.Evidence.CommandSequence != 0 && !artifact.Evidence.FailedAt.IsZero() && artifact.Evidence.LastStableSnapshot.BattleID == artifact.BattleID && len(artifact.Evidence.Commands) > 0 && artifact.Evidence.Stack != ""
}

func validDiagnosticReceipt(receipt DiagnosticReceipt) bool {
	return receipt.ReceiptID != "" && receipt.BattleID != 0 && receipt.Ref.Node != "" && receipt.Ref.ID != 0 && len(receipt.Digest) == sha256.Size*2 && receipt.Directory != "" && !receipt.CreatedAt.IsZero()
}

func writeAndSync(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	return errors.Join(syncErr, directory.Close())
}

var _ gsr.Service = (*battleQuarantineBoundary)(nil)
