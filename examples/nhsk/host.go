package nhsk

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	applyBattleCreatedCommand       gsr.CommandID = 0x0410f001
	applyBattleStoppedCommand       gsr.CommandID = 0x0410f002
	createBattleInternalCommand     gsr.CommandID = 0x0410f010
	stopBattleInternalCommand       gsr.CommandID = 0x0410f011
	bindOutputInternalCommand       gsr.CommandID = 0x0410f012
	unbindOutputInternalCommand     gsr.CommandID = 0x0410f013
	stopGenerationCommand           gsr.CommandID = 0x0410f014
	applyFactoryStopResultCommand   gsr.CommandID = 0x0410f019
	applyFactoryCreateResultCommand gsr.CommandID = 0x0410f01b
)

var (
	errInvalidHostConfig          = errors.New("nhsk: invalid Host config")
	errBattleIDInUse              = errors.New("nhsk: battle id in use")
	errHostCapacity               = errors.New("nhsk: Host capacity exhausted")
	errInvalidBattleID            = errors.New("nhsk: invalid BattleID")
	errBattleCreateFailed         = errors.New("nhsk: Battle creation returned an empty ServiceRef")
	errConnectionGenerationClosed = errors.New("nhsk: connection generation closed")
	errConnectionGenerationSource = errors.New("nhsk: connection generation is reserved for the local Legacy adapter")
)

// NHSKHostConfig configures the stable `.nhsk-game-host` Service.
type NHSKHostConfig struct {
	MaxActiveBattles uint32
	FactoryRef       gsr.ServiceRef
	Clock            NHSKClock
	Diagnostic       DiagnosticSubmitter
}

// NHSKHostService owns BattleID-to-ServiceRef bindings and delegates lifecycle work to a factory Service.
type NHSKHostService struct {
	max              uint32
	factory          gsr.ServiceRef
	service          gsr.ServiceContext
	nextOp           HostOperationID
	active           map[game.BattleID]gsr.ServiceRef
	states           map[game.BattleID]HostOperationPhase
	operations       map[HostOperationID]CreateBattleOperation
	createRequests   map[game.BattleID]CreateBattleRequest
	battleOperations map[game.BattleID]HostOperationID
	replacements     map[game.BattleID]HostOperationID
	clock            NHSKClock
	diagnostic       DiagnosticSubmitter
	quarantine       map[game.BattleID]*quarantinedBattle
}

type quarantinedBattle struct {
	snapshot       QuarantinedBattleSnapshot
	evidence       BattleDiagnosticEvidence
	releasePending bool
}

// NewNHSKHostService creates an empty Host index.
func NewNHSKHostService(config NHSKHostConfig) (*NHSKHostService, error) {
	if config.MaxActiveBattles == 0 || config.FactoryRef.Node == "" || config.FactoryRef.ID == 0 {
		return nil, errInvalidHostConfig
	}
	clock := config.Clock
	if clock == nil {
		clock = systemNHSKClock{}
	}
	return &NHSKHostService{max: config.MaxActiveBattles, factory: config.FactoryRef, active: make(map[game.BattleID]gsr.ServiceRef), states: make(map[game.BattleID]HostOperationPhase), operations: make(map[HostOperationID]CreateBattleOperation), createRequests: make(map[game.BattleID]CreateBattleRequest), battleOperations: make(map[game.BattleID]HostOperationID), replacements: make(map[game.BattleID]HostOperationID), clock: clock, diagnostic: config.Diagnostic, quarantine: make(map[game.BattleID]*quarantinedBattle)}, nil
}

// Init captures the Host Service capability.
func (host *NHSKHostService) Init(service gsr.ServiceContext) error {
	if service == nil {
		return errInvalidHostConfig
	}
	host.service = service
	return nil
}

// Handle applies one Host lifecycle or lookup Command.
func (host *NHSKHostService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case BeginCreateBattleCommand:
		return host.beginCreate(ctx, command.Payload)
	case GetCreateBattleOperationCommand:
		return host.getOperation(ctx, command.Payload)
	case ResolveBattleCommand:
		return host.resolve(ctx, command.Payload)
	case RequestDeleteBattleCommand:
		return host.delete(ctx, command.Payload)
	case GetHostSnapshotCommand:
		return host.reply(ctx, host.snapshot())
	case applyBattleCreatedCommand:
		return host.applyCreated(ctx, command.Payload)
	case applyBattleStoppedCommand:
		return host.applyStopped(ctx, command.Payload)
	case reportBattleQuarantinedCommand:
		return host.applyQuarantined(ctx, command.Payload)
	case applyDiagnosticExportResultCommand:
		return host.applyDiagnosticResult(ctx, command.Payload)
	case listQuarantinedAdminCommand:
		return host.listQuarantined(ctx)
	case retryDiagnosticAdminCommand:
		return host.retryDiagnostic(ctx, command.Payload)
	case releaseQuarantinedAdminCommand:
		return host.releaseQuarantined(ctx, command.Payload)
	default:
		return gsr.ErrUnknownCommand
	}
}

// Stop stops accepting lifecycle changes; Runtime owns actual Service cleanup.
func (*NHSKHostService) Stop(context.Context) error { return nil }

// Close releases the Host Service capability.
func (host *NHSKHostService) Close() error { host.service = nil; return nil }

type createBattleInternal struct {
	OperationID HostOperationID
	Request     CreateBattleRequest
	Host        gsr.ServiceRef
}
type applyBattleCreated struct {
	OperationID          HostOperationID
	BattleID             game.BattleID
	Ref                  gsr.ServiceRef
	ConnectionGeneration ConnectionGeneration
	Err                  string
}
type stopBattleInternal struct {
	OperationID          HostOperationID
	BattleID             game.BattleID
	Ref, Host            gsr.ServiceRef
	ReleaseQuarantined   bool
	Orphan               bool
	ConnectionGeneration ConnectionGeneration
}

type bindOutputInternal struct {
	Generation ConnectionGeneration
	Ref        gsr.ServiceRef
	Reporter   ConnectionFailureReporter
}

type unbindOutputInternal struct{ Generation ConnectionGeneration }

type stopGenerationInternal struct{ Generation ConnectionGeneration }
type applyBattleStopped struct {
	OperationID HostOperationID
	BattleID    game.BattleID
	Ref         gsr.ServiceRef
	Err         string
}

type applyFactoryStopResult struct {
	Request  stopBattleInternal
	Err      string
	Defect   bool
	Evidence BattleDiagnosticEvidence
}

func (host *NHSKHostService) beginCreate(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(CreateBattleRequest)
	if !ok || request.BattleID == 0 {
		return host.reply(ctx, CreateBattleOperation{Phase: HostOperationFailed, Rejection: errInvalidBattleID.Error()})
	}
	if request.ConnectionGeneration != 0 && !host.isLocalAdmin(ctx) {
		return host.reply(ctx, CreateBattleOperation{BattleID: request.BattleID, Phase: HostOperationFailed, Rejection: errConnectionGenerationSource.Error()})
	}
	if phase, exists := host.states[request.BattleID]; exists && phase != HostOperationFailed {
		if operationID := host.battleOperations[request.BattleID]; operationID != 0 && host.createRequests[request.BattleID] == request && (phase == HostOperationCreating || host.replacements[request.BattleID] == operationID) {
			return host.reply(ctx, host.operations[operationID])
		}
		if phase == HostOperationCompleted && request.ConnectionGeneration != 0 {
			return host.beginLegacyReplacement(ctx, request)
		}
		return host.reply(ctx, CreateBattleOperation{BattleID: request.BattleID, Phase: phase, Rejection: errBattleIDInUse.Error()})
	}
	if uint32(len(host.active))+uint32(host.creatingCount()) >= host.max {
		return host.reply(ctx, CreateBattleOperation{BattleID: request.BattleID, Phase: HostOperationFailed, Rejection: errHostCapacity.Error()})
	}
	host.nextOp++
	operation := CreateBattleOperation{OperationID: host.nextOp, BattleID: request.BattleID, Phase: HostOperationCreating}
	host.operations[operation.OperationID] = operation
	host.states[request.BattleID] = HostOperationCreating
	host.createRequests[request.BattleID] = request
	host.battleOperations[request.BattleID] = operation.OperationID
	if err := host.submitCreate(ctx.Self(), operation.OperationID, request); err != nil {
		operation.Phase, operation.Rejection = HostOperationFailed, err.Error()
		host.operations[operation.OperationID] = operation
		delete(host.states, request.BattleID)
		delete(host.createRequests, request.BattleID)
		delete(host.battleOperations, request.BattleID)
	}
	return host.reply(ctx, operation)
}

func (host *NHSKHostService) beginLegacyReplacement(ctx gsr.CommandContext, request CreateBattleRequest) error {
	ref := host.active[request.BattleID]
	host.nextOp++
	operation := CreateBattleOperation{OperationID: host.nextOp, BattleID: request.BattleID, Phase: HostOperationStopping}
	host.operations[operation.OperationID] = operation
	host.createRequests[request.BattleID] = request
	host.battleOperations[request.BattleID] = operation.OperationID
	host.replacements[request.BattleID] = operation.OperationID
	host.states[request.BattleID] = HostOperationStopping
	stop := stopBattleInternal{OperationID: operation.OperationID, BattleID: request.BattleID, Ref: ref, Host: ctx.Self()}
	if err := host.service.Send(host.factory, stopBattleInternalCommand, stop); err != nil {
		operation.Phase, operation.Rejection = HostOperationFailed, err.Error()
		host.operations[operation.OperationID] = operation
		host.states[request.BattleID] = HostOperationCompleted
		delete(host.replacements, request.BattleID)
		delete(host.createRequests, request.BattleID)
		delete(host.battleOperations, request.BattleID)
	}
	return host.reply(ctx, operation)
}

func (host *NHSKHostService) submitCreate(self gsr.ServiceRef, operationID HostOperationID, request CreateBattleRequest) error {
	return host.service.Send(host.factory, createBattleInternalCommand, createBattleInternal{OperationID: operationID, Request: request, Host: self})
}

func (host *NHSKHostService) getOperation(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(GetCreateBattleOperationRequest)
	if !ok || request.OperationID == 0 {
		return gsr.ErrInvalidClusterEnvelope
	}
	operation, exists := host.operations[request.OperationID]
	if !exists {
		return gsr.ErrServiceNotFound
	}
	return host.reply(ctx, operation)
}

func (host *NHSKHostService) resolve(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(ResolveBattleRequest)
	if !ok || request.BattleID == 0 {
		return errInvalidBattleID
	}
	ref, exists := host.active[request.BattleID]
	if !exists {
		return gsr.ErrServiceNotFound
	}
	if host.states[request.BattleID] == HostOperationQuarantined {
		return ErrBattleQuarantined
	}
	return host.reply(ctx, ResolveBattleResult{BattleID: request.BattleID, Ref: ref})
}

func (host *NHSKHostService) delete(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(RequestDeleteBattleRequest)
	if !ok || request.BattleID == 0 {
		return host.reply(ctx, HostCommandResult{Rejection: errInvalidBattleID.Error()})
	}
	ref, exists := host.active[request.BattleID]
	if !exists || (request.Ref.ID != 0 && request.Ref != ref) {
		return host.reply(ctx, HostCommandResult{Rejection: gsr.ErrServiceNotFound.Error()})
	}
	if retained := host.quarantine[request.BattleID]; retained != nil {
		now := host.clock.Now()
		observation := &retained.snapshot.ExternalEnd
		if observation.Count == 0 {
			observation.FirstObservedAt = now
		}
		observation.LatestObservedAt = now
		observation.Count++
		observation.LatestConnectionGeneration = request.ConnectionGeneration
		return host.reply(ctx, HostCommandResult{Accepted: true})
	}
	if host.states[request.BattleID] == HostOperationStopping {
		return host.reply(ctx, HostCommandResult{Accepted: true})
	}
	host.states[request.BattleID] = HostOperationStopping
	host.nextOp++
	if err := host.service.Send(host.factory, stopBattleInternalCommand, stopBattleInternal{OperationID: host.nextOp, BattleID: request.BattleID, Ref: ref, Host: ctx.Self()}); err != nil {
		host.states[request.BattleID] = HostOperationCompleted
		return host.reply(ctx, HostCommandResult{Rejection: err.Error()})
	}
	return host.reply(ctx, HostCommandResult{Accepted: true, OperationID: host.nextOp})
}

func (host *NHSKHostService) applyCreated(ctx gsr.CommandContext, payload any) error {
	if ctx.Source() != host.factory {
		return game.ErrUnauthorized
	}
	result, ok := payload.(applyBattleCreated)
	if !ok {
		return gsr.ErrInvalidClusterEnvelope
	}
	operation, exists := host.operations[result.OperationID]
	request, pending := host.createRequests[result.BattleID]
	if !exists || !pending || operation.BattleID != result.BattleID || host.battleOperations[result.BattleID] != result.OperationID || request.ConnectionGeneration != result.ConnectionGeneration {
		return nil
	}
	if result.Err != "" || result.Ref.ID == 0 {
		operation.Phase, operation.Rejection = HostOperationFailed, result.Err
		delete(host.states, result.BattleID)
		delete(host.createRequests, result.BattleID)
		delete(host.battleOperations, result.BattleID)
	} else {
		operation.Phase, operation.Ref = HostOperationCompleted, result.Ref
		host.active[result.BattleID] = result.Ref
		host.states[result.BattleID] = HostOperationCompleted
		delete(host.createRequests, result.BattleID)
		delete(host.battleOperations, result.BattleID)
	}
	host.operations[result.OperationID] = operation
	return nil
}

func (host *NHSKHostService) applyStopped(ctx gsr.CommandContext, payload any) error {
	if ctx.Source() != host.factory {
		return game.ErrUnauthorized
	}
	result, ok := payload.(applyBattleStopped)
	if !ok {
		return gsr.ErrInvalidClusterEnvelope
	}
	if ref, exists := host.active[result.BattleID]; !exists || ref != result.Ref {
		return nil
	}
	if operationID := host.replacements[result.BattleID]; operationID != 0 && operationID == result.OperationID {
		operation := host.operations[operationID]
		if result.Err != "" {
			operation.Phase, operation.Rejection = HostOperationFailed, result.Err
			host.operations[operationID] = operation
			host.states[result.BattleID] = HostOperationCompleted
			delete(host.replacements, result.BattleID)
			delete(host.createRequests, result.BattleID)
			delete(host.battleOperations, result.BattleID)
			return nil
		}
		delete(host.active, result.BattleID)
		operation.Phase, operation.Ref = HostOperationCreating, gsr.ServiceRef{}
		host.operations[operationID] = operation
		host.states[result.BattleID] = HostOperationCreating
		delete(host.replacements, result.BattleID)
		request := host.createRequests[result.BattleID]
		if err := host.submitCreate(ctx.Self(), operationID, request); err != nil {
			operation.Phase, operation.Rejection = HostOperationFailed, err.Error()
			host.operations[operationID] = operation
			delete(host.states, result.BattleID)
			delete(host.createRequests, result.BattleID)
			delete(host.battleOperations, result.BattleID)
		}
		return nil
	}
	if retained := host.quarantine[result.BattleID]; retained != nil {
		if !retained.releasePending {
			return nil
		}
		if result.Err != "" {
			retained.releasePending = false
			return nil
		}
		delete(host.quarantine, result.BattleID)
		delete(host.active, result.BattleID)
		delete(host.states, result.BattleID)
		host.service.Metrics().SetGauge("nhsk_quarantined_battles", int64(len(host.quarantine)))
		return nil
	}
	if result.Err != "" {
		host.states[result.BattleID] = HostOperationCompleted
		return nil
	}
	delete(host.active, result.BattleID)
	delete(host.states, result.BattleID)
	return nil
}

func (host *NHSKHostService) applyQuarantined(ctx gsr.CommandContext, payload any) error {
	report, ok := payload.(battleQuarantineReport)
	if !ok || report.BattleID == 0 || report.Ref.ID == 0 || report.Evidence.FailedCommand == 0 {
		return gsr.ErrInvalidClusterEnvelope
	}
	rootSource := ctx.Source().Node == ctx.Self().Node && ctx.Source().ID == 0
	if ctx.Source() != report.Ref && ctx.Source() != host.factory && !rootSource {
		return game.ErrUnauthorized
	}
	if ref, exists := host.active[report.BattleID]; !exists || ref != report.Ref {
		return nil
	}
	if existing := host.quarantine[report.BattleID]; existing != nil {
		return nil
	}
	now := report.Evidence.FailedAt
	if now.IsZero() {
		now = host.clock.Now()
	}
	retained := &quarantinedBattle{
		snapshot: QuarantinedBattleSnapshot{
			BattleID: report.BattleID, Ref: report.Ref, ConnectionGeneration: report.ConnectionGeneration,
			QuarantinedAt: now, FailedCommand: report.Evidence.FailedCommand,
			CommandSequence: report.Evidence.CommandSequence, ExportStatus: DiagnosticExportPending,
		},
		evidence: cloneQuarantineReport(report).Evidence,
	}
	host.quarantine[report.BattleID] = retained
	host.states[report.BattleID] = HostOperationQuarantined
	host.service.Metrics().Inc("nhsk_battles_quarantined_total")
	host.service.Metrics().SetGauge("nhsk_quarantined_battles", int64(len(host.quarantine)))
	host.service.Logger().Error("NHSK Battle quarantined", "battle_id", report.BattleID, "ref", report.Ref, "connection_generation", report.ConnectionGeneration, "command", report.Evidence.FailedCommand, "sequence", report.Evidence.CommandSequence)
	if host.diagnostic != nil {
		artifact := DiagnosticArtifact{Host: ctx.Self(), BattleID: report.BattleID, Ref: report.Ref, ConnectionGeneration: report.ConnectionGeneration, Evidence: cloneQuarantineReport(report).Evidence}
		if err := host.diagnostic.SubmitDiagnostic(artifact); err != nil {
			retained.snapshot.ExportError = err.Error()
			host.service.Metrics().Inc("nhsk_diagnostic_submit_errors_total")
		}
	}
	return nil
}

func (host *NHSKHostService) applyDiagnosticResult(ctx gsr.CommandContext, payload any) error {
	if ctx.Source().Node != ctx.Self().Node || ctx.Source().ID != 0 {
		return game.ErrUnauthorized
	}
	runnerResult, ok := payload.(gsr.RunnerResult[diagnosticExportResult])
	if !ok {
		return gsr.ErrInvalidClusterEnvelope
	}
	result := runnerResult.Value
	if runnerResult.Err != nil && result.Error == "" {
		result.Error = runnerResult.Err.Error()
	}
	retained := host.quarantine[result.BattleID]
	if retained == nil || retained.snapshot.Ref != result.Ref {
		return nil
	}
	if result.Error != "" {
		retained.snapshot.ExportStatus = DiagnosticExportFailed
		retained.snapshot.ExportError = result.Error
		return nil
	}
	if !validDiagnosticReceipt(result.Receipt) || result.Receipt.BattleID != result.BattleID || result.Receipt.Ref != result.Ref {
		retained.snapshot.ExportStatus = DiagnosticExportFailed
		retained.snapshot.ExportError = errInvalidDiagnosticArtifact.Error()
		return nil
	}
	receipt := result.Receipt
	retained.snapshot.ExportStatus = DiagnosticExported
	retained.snapshot.ExportError = ""
	retained.snapshot.Receipt = &receipt
	return nil
}

func (host *NHSKHostService) listQuarantined(ctx gsr.CommandContext) error {
	if !host.isLocalAdmin(ctx) {
		return game.ErrUnauthorized
	}
	return host.reply(ctx, host.snapshot().Quarantined)
}

func (host *NHSKHostService) retryDiagnostic(ctx gsr.CommandContext, payload any) error {
	if !host.isLocalAdmin(ctx) {
		return game.ErrUnauthorized
	}
	request, ok := payload.(retryDiagnosticRequest)
	retained := host.quarantine[request.BattleID]
	if !ok || retained == nil || retained.snapshot.Ref != request.Ref {
		return host.reply(ctx, HostCommandResult{Rejection: gsr.ErrServiceNotFound.Error()})
	}
	if retained.snapshot.ExportStatus == DiagnosticExported {
		return host.reply(ctx, HostCommandResult{Accepted: true})
	}
	if host.diagnostic == nil {
		return host.reply(ctx, HostCommandResult{Rejection: errInvalidDiagnosticArtifact.Error()})
	}
	artifact := DiagnosticArtifact{Host: ctx.Self(), BattleID: request.BattleID, Ref: request.Ref, ConnectionGeneration: retained.snapshot.ConnectionGeneration, Evidence: cloneDiagnosticEvidence(retained.evidence)}
	if err := host.diagnostic.SubmitDiagnostic(artifact); err != nil {
		retained.snapshot.ExportStatus = DiagnosticExportPending
		retained.snapshot.ExportError = err.Error()
		return host.reply(ctx, HostCommandResult{Rejection: err.Error()})
	}
	retained.snapshot.ExportStatus = DiagnosticExportPending
	retained.snapshot.ExportError = ""
	return host.reply(ctx, HostCommandResult{Accepted: true})
}

func (host *NHSKHostService) releaseQuarantined(ctx gsr.CommandContext, payload any) error {
	if !host.isLocalAdmin(ctx) {
		return game.ErrUnauthorized
	}
	request, ok := payload.(releaseQuarantinedRequest)
	if !ok {
		return gsr.ErrInvalidClusterEnvelope
	}
	retained := host.quarantine[request.Receipt.BattleID]
	if retained == nil || retained.snapshot.Receipt == nil || !sameDiagnosticReceipt(*retained.snapshot.Receipt, request.Receipt) {
		return host.reply(ctx, HostCommandResult{Rejection: ErrDiagnosticReceiptMismatch.Error()})
	}
	if retained.releasePending {
		return host.reply(ctx, HostCommandResult{Accepted: true})
	}
	host.nextOp++
	requestStop := stopBattleInternal{OperationID: host.nextOp, BattleID: request.Receipt.BattleID, Ref: request.Receipt.Ref, Host: ctx.Self(), ReleaseQuarantined: true}
	if err := host.service.Send(host.factory, stopBattleInternalCommand, requestStop); err != nil {
		return host.reply(ctx, HostCommandResult{Rejection: err.Error()})
	}
	retained.releasePending = true
	return host.reply(ctx, HostCommandResult{Accepted: true, OperationID: host.nextOp})
}

func (host *NHSKHostService) isLocalAdmin(ctx gsr.CommandContext) bool {
	return ctx.Source().Node == ctx.Self().Node && ctx.Source().ID == 0
}

func sameDiagnosticReceipt(left, right DiagnosticReceipt) bool {
	return left.ReceiptID == right.ReceiptID && left.BattleID == right.BattleID && left.Ref == right.Ref && left.Digest == right.Digest && left.Directory == right.Directory && left.CreatedAt.Equal(right.CreatedAt)
}

func cloneDiagnosticEvidence(evidence BattleDiagnosticEvidence) BattleDiagnosticEvidence {
	evidence.LastStableSnapshot = cloneBattleSnapshot(evidence.LastStableSnapshot)
	evidence.Commands = cloneCommandRecords(evidence.Commands)
	return evidence
}

func (host *NHSKHostService) creatingCount() int {
	count := 0
	for _, phase := range host.states {
		if phase == HostOperationCreating {
			count++
		}
	}
	return count
}
func (host *NHSKHostService) snapshot() HostSnapshot {
	active := make(map[game.BattleID]gsr.ServiceRef, len(host.active))
	for id, ref := range host.active {
		active[id] = ref
	}
	quarantined := make([]QuarantinedBattleSnapshot, 0, len(host.quarantine))
	for _, retained := range host.quarantine {
		projection := retained.snapshot
		if projection.Receipt != nil {
			receipt := *projection.Receipt
			projection.Receipt = &receipt
		}
		quarantined = append(quarantined, projection)
	}
	sort.Slice(quarantined, func(left, right int) bool { return quarantined[left].BattleID < quarantined[right].BattleID })
	return HostSnapshot{MaxActiveBattles: host.max, ActiveBattles: active, Quarantined: quarantined}
}
func (host *NHSKHostService) reply(ctx gsr.CommandContext, value any) error {
	if err := ctx.Reply(value); err != nil && !errors.Is(err, gsr.ErrReplyUnavailable) {
		return err
	}
	return nil
}

// BattleStopper reaches the exact Battle Mailbox before stopping its ServiceRef.
type BattleStopper interface {
	game.CommandRuntime
	Stop(context.Context, gsr.ServiceRef) error
}

// BattleFactoryService is a bounded lifecycle runner used by NHSKHostService.
type BattleFactoryService struct {
	creator          game.ServiceCreator
	stopper          BattleStopper
	commands         game.CommandRuntime
	service          gsr.ServiceContext
	self             gsr.ServiceRef
	createQueue      chan factoryCreateTask
	stopQueue        chan stopBattleInternal
	runnerCancel     context.CancelFunc
	runnerWG         sync.WaitGroup
	creating         map[game.BattleID]factoryCreateTask
	battles          map[game.BattleID]factoryBattle
	closedThrough    ConnectionGeneration
	outputRef        gsr.ServiceRef
	outputGeneration ConnectionGeneration
	outputReporter   ConnectionFailureReporter
	replaySubmitter  ReplaySubmitter
	aiSubmitter      AISubmitter
}

// BattleFactoryConfig contains process-owned adapters injected into every
// Battle created by the factory.
type BattleFactoryConfig struct {
	ReplaySubmitter ReplaySubmitter
	AISubmitter     AISubmitter
}

type factoryBattle struct {
	ref         gsr.ServiceRef
	generation  ConnectionGeneration
	host        gsr.ServiceRef
	quarantined bool
	stopping    bool
}

type factoryCreateTask struct {
	Request createBattleInternal
	Config  NHSKBattleConfig
}

type applyFactoryCreateResult struct {
	Task factoryCreateTask
	Ref  gsr.ServiceRef
	Err  string
}

// NewBattleFactoryService creates a lifecycle runner backed by the Runtime composition root.
func NewBattleFactoryService(creator game.ServiceCreator, stopper BattleStopper, configs ...BattleFactoryConfig) (*BattleFactoryService, error) {
	if creator == nil || stopper == nil {
		return nil, errInvalidHostConfig
	}
	if len(configs) > 1 {
		return nil, errInvalidHostConfig
	}
	factory := &BattleFactoryService{creator: creator, stopper: stopper, commands: stopper}
	if len(configs) == 1 {
		factory.replaySubmitter = configs[0].ReplaySubmitter
		factory.aiSubmitter = configs[0].AISubmitter
	}
	return factory, nil
}

// Init captures the factory capability.
func (factory *BattleFactoryService) Init(service gsr.ServiceContext) error {
	if service == nil {
		return errInvalidHostConfig
	}
	factory.service = service
	factory.self = service.Self()
	ctx, cancel := context.WithCancel(context.Background())
	factory.runnerCancel = cancel
	factory.createQueue = make(chan factoryCreateTask, 10000)
	factory.stopQueue = make(chan stopBattleInternal, 10000)
	factory.creating = make(map[game.BattleID]factoryCreateTask)
	factory.battles = make(map[game.BattleID]factoryBattle)
	factory.runnerWG.Add(2)
	startFactoryLifecycleRunners(factory, ctx)
	return nil
}

// Handle creates or stops exact Battle ServiceRefs and reports results to Host.
func (factory *BattleFactoryService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case createBattleInternalCommand:
		request, ok := command.Payload.(createBattleInternal)
		if !ok || ctx.Source() != request.Host {
			return gsr.ErrInvalidClusterEnvelope
		}
		if factory.generationClosed(request.Request.ConnectionGeneration) {
			return factory.reportCreateResult(request, gsr.ServiceRef{}, errConnectionGenerationClosed)
		}
		if _, exists := factory.creating[request.Request.BattleID]; exists {
			return factory.reportCreateResult(request, gsr.ServiceRef{}, errBattleIDInUse)
		}
		if _, exists := factory.battles[request.Request.BattleID]; exists {
			return factory.reportCreateResult(request, gsr.ServiceRef{}, errBattleIDInUse)
		}
		outputRef, outputGeneration, outputReporter := factory.outputRef, factory.outputGeneration, factory.outputReporter
		if outputGeneration != request.Request.ConnectionGeneration {
			outputRef, outputGeneration, outputReporter = gsr.ServiceRef{}, 0, nil
		}
		task := factoryCreateTask{Request: request, Config: NHSKBattleConfig{ID: request.Request.BattleID, OutputRef: outputRef, IsNewbie: request.Request.IsNewbie, ConnectionGeneration: outputGeneration, OutputReporter: outputReporter, ReplaySubmitter: factory.replaySubmitter, AISubmitter: factory.aiSubmitter}}
		factory.creating[request.Request.BattleID] = task
		select {
		case factory.createQueue <- task:
			return nil
		default:
			delete(factory.creating, request.Request.BattleID)
			return factory.reportCreateResult(request, gsr.ServiceRef{}, gsr.ErrMailboxFull)
		}
	case applyFactoryCreateResultCommand:
		result, ok := command.Payload.(applyFactoryCreateResult)
		if !ok || !factory.isRootSource(ctx.Source()) {
			return gsr.ErrInvalidClusterEnvelope
		}
		return factory.applyCreateResult(result)
	case stopBattleInternalCommand:
		request, ok := command.Payload.(stopBattleInternal)
		if !ok {
			return gsr.ErrInvalidClusterEnvelope
		}
		battle, exists := factory.battles[request.BattleID]
		if !exists || battle.ref != request.Ref || battle.host != request.Host || ctx.Source() != request.Host || battle.stopping || (request.ReleaseQuarantined && !battle.quarantined) || (!request.ReleaseQuarantined && battle.quarantined) {
			return gsr.ErrServiceNotFound
		}
		request.ConnectionGeneration = battle.generation
		select {
		case factory.stopQueue <- request:
			battle.stopping = true
			factory.battles[request.BattleID] = battle
			return nil
		default:
			return gsr.ErrMailboxFull
		}
	case bindOutputInternalCommand:
		request, ok := command.Payload.(bindOutputInternal)
		if !ok || !factory.isRootSource(ctx.Source()) || request.Generation == 0 || request.Ref.ID == 0 || request.Reporter == nil {
			return gsr.ErrInvalidClusterEnvelope
		}
		factory.outputRef, factory.outputGeneration, factory.outputReporter = request.Ref, request.Generation, request.Reporter
		return nil
	case unbindOutputInternalCommand:
		request, ok := command.Payload.(unbindOutputInternal)
		if !ok || !factory.isRootSource(ctx.Source()) {
			return gsr.ErrInvalidClusterEnvelope
		}
		if request.Generation == factory.outputGeneration {
			factory.outputRef, factory.outputGeneration, factory.outputReporter = gsr.ServiceRef{}, 0, nil
		}
		return nil
	case stopGenerationCommand:
		request, ok := command.Payload.(stopGenerationInternal)
		if !ok || !factory.isRootSource(ctx.Source()) || request.Generation == 0 {
			return gsr.ErrInvalidClusterEnvelope
		}
		if request.Generation > factory.closedThrough {
			factory.closedThrough = request.Generation
		}
		for battleID, battle := range factory.battles {
			if battle.generation != request.Generation || battle.quarantined || battle.stopping {
				continue
			}
			select {
			case factory.stopQueue <- stopBattleInternal{OperationID: 0, BattleID: battleID, Ref: battle.ref, Host: battle.host, ConnectionGeneration: battle.generation}:
				battle.stopping = true
				factory.battles[battleID] = battle
			default:
				return gsr.ErrMailboxFull
			}
		}
		return nil
	case reportBattleQuarantinedCommand:
		report, ok := command.Payload.(battleQuarantineReport)
		rootSource := factory.isRootSource(ctx.Source())
		if !ok || (ctx.Source() != report.Ref && ctx.Source() != factory.self && !rootSource) {
			return gsr.ErrInvalidClusterEnvelope
		}
		battle, exists := factory.battles[report.BattleID]
		if exists && battle.ref == report.Ref {
			battle.quarantined = true
			factory.battles[report.BattleID] = battle
		}
		return nil
	case applyFactoryStopResultCommand:
		result, ok := command.Payload.(applyFactoryStopResult)
		if !ok || !factory.isRootSource(ctx.Source()) {
			return gsr.ErrInvalidClusterEnvelope
		}
		request := result.Request
		if request.Orphan {
			if result.Err != "" {
				factory.service.Logger().Error("failed to stop orphan NHSK Battle", "battle_id", request.BattleID, "ref", request.Ref, "error", result.Err)
			}
			return nil
		}
		battle, exists := factory.battles[request.BattleID]
		if !exists || battle.ref != request.Ref {
			return nil
		}
		if result.Err == "" {
			delete(factory.battles, request.BattleID)
		} else {
			battle.stopping = false
			factory.battles[request.BattleID] = battle
		}
		if !request.ReleaseQuarantined && result.Defect && result.Evidence.CommandSequence != 0 {
			evidence := cloneDiagnosticEvidence(result.Evidence)
			evidence.FailedCommand = deleteBattleBarrierCommand
			evidence.Stack = "Battle Stop failure: " + result.Err
			report := battleQuarantineReport{BattleID: request.BattleID, Ref: request.Ref, ConnectionGeneration: request.ConnectionGeneration, Evidence: evidence}
			_ = factory.service.Send(request.Host, reportBattleQuarantinedCommand, cloneQuarantineReport(report))
			if retained, retainedExists := factory.battles[request.BattleID]; retainedExists {
				retained.quarantined = true
				factory.battles[request.BattleID] = retained
			}
		}
		if request.Host.ID != 0 {
			return factory.service.Send(request.Host, applyBattleStoppedCommand, applyBattleStopped{OperationID: request.OperationID, BattleID: request.BattleID, Ref: request.Ref, Err: result.Err})
		}
		return nil
	default:
		return gsr.ErrUnknownCommand
	}
}

func (factory *BattleFactoryService) applyCreateResult(result applyFactoryCreateResult) error {
	request := result.Task.Request
	pending, exists := factory.creating[request.Request.BattleID]
	if !exists || pending.Request != request {
		if result.Ref.ID != 0 {
			factory.enqueueOrphanStop(request.Request.BattleID, result.Ref, request.Request.ConnectionGeneration)
		}
		return nil
	}
	delete(factory.creating, request.Request.BattleID)
	if result.Err != "" || result.Ref.ID == 0 {
		if result.Err == "" {
			result.Err = errBattleCreateFailed.Error()
		}
		return factory.reportCreateResult(request, gsr.ServiceRef{}, errors.New(result.Err))
	}
	battle := factoryBattle{ref: result.Ref, generation: request.Request.ConnectionGeneration, host: request.Host}
	if factory.generationClosed(request.Request.ConnectionGeneration) {
		factory.enqueueOrphanStop(request.Request.BattleID, result.Ref, request.Request.ConnectionGeneration)
		return factory.reportCreateResult(request, gsr.ServiceRef{}, errConnectionGenerationClosed)
	}
	if _, exists := factory.battles[request.Request.BattleID]; exists {
		factory.enqueueOrphanStop(request.Request.BattleID, result.Ref, request.Request.ConnectionGeneration)
		return factory.reportCreateResult(request, gsr.ServiceRef{}, errBattleIDInUse)
	}
	factory.battles[request.Request.BattleID] = battle
	if err := factory.reportCreateResult(request, result.Ref, nil); err != nil {
		delete(factory.battles, request.Request.BattleID)
		factory.enqueueOrphanStop(request.Request.BattleID, result.Ref, request.Request.ConnectionGeneration)
		return err
	}
	return nil
}

func (factory *BattleFactoryService) reportCreateResult(request createBattleInternal, ref gsr.ServiceRef, err error) error {
	return factory.service.Send(request.Host, applyBattleCreatedCommand, applyBattleCreated{
		OperationID:          request.OperationID,
		BattleID:             request.Request.BattleID,
		Ref:                  ref,
		ConnectionGeneration: request.Request.ConnectionGeneration,
		Err:                  errorText(err),
	})
}

func (factory *BattleFactoryService) enqueueOrphanStop(battleID game.BattleID, ref gsr.ServiceRef, generation ConnectionGeneration) {
	select {
	case factory.stopQueue <- stopBattleInternal{BattleID: battleID, Ref: ref, Orphan: true, ConnectionGeneration: generation}:
	default:
		factory.service.Logger().Error("NHSK orphan stop queue is full", "battle_id", battleID, "ref", ref)
	}
}

func (factory *BattleFactoryService) generationClosed(generation ConnectionGeneration) bool {
	return generation != 0 && generation <= factory.closedThrough
}

func (factory *BattleFactoryService) isRootSource(source gsr.ServiceRef) bool {
	return source.Node == factory.self.Node && source.ID == 0
}

// Stop stops the runner's command intake.
func (*BattleFactoryService) Stop(context.Context) error { return nil }

// Close releases the runner capability.
func (factory *BattleFactoryService) Close() error {
	if factory.runnerCancel != nil {
		factory.runnerCancel()
	}
	factory.runnerWG.Wait()
	factory.service = nil
	factory.self = gsr.ServiceRef{}
	return nil
}

func (factory *BattleFactoryService) createLoop(ctx context.Context) {
	defer factory.runnerWG.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-factory.createQueue:
			battle, err := NewBattleService(task.Config)
			var service gsr.Service
			if err == nil {
				service, err = newBattleQuarantineBoundary(battle, battleQuarantineBoundaryConfig{Host: task.Request.Host, Factory: factory.self, ConnectionGeneration: task.Request.Request.ConnectionGeneration})
			}
			var ref gsr.ServiceRef
			if err == nil {
				ref, err = factory.creator.CreateService(gsr.ServiceSpec{Name: gsr.ServiceName(fmt.Sprintf("nhsk-battle/%d", task.Request.Request.BattleID)), Service: service})
			}
			result := applyFactoryCreateResult{Task: task, Ref: ref, Err: errorText(err)}
			if sendErr := factory.commands.Send(factory.self, applyFactoryCreateResultCommand, result); sendErr != nil && ref.ID != 0 {
				_ = factory.stopper.Stop(context.Background(), ref)
			}
		}
	}
}

func (factory *BattleFactoryService) stopLoop(ctx context.Context) {
	defer factory.runnerWG.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-factory.stopQueue:
			var err error
			var evidence BattleDiagnosticEvidence
			if !request.ReleaseQuarantined {
				_, err = factory.commands.Call(ctx, request.Ref, deleteBattleBarrierCommand, deleteBattleBarrier{BattleID: request.BattleID})
				if err == nil {
					value, captureErr := factory.commands.Call(ctx, request.Ref, captureBattleDiagnosticCommand, nil)
					if captureErr == nil {
						evidence, _ = value.(BattleDiagnosticEvidence)
					}
				}
			}
			if err == nil {
				err = factory.stopper.Stop(ctx, request.Ref)
			}
			if request.ReleaseQuarantined && (errors.Is(err, gsr.ErrServiceClosed) || errors.Is(err, gsr.ErrServiceNotFound)) {
				err = nil
			}
			_ = factory.commands.Send(factory.self, applyFactoryStopResultCommand, applyFactoryStopResult{Request: request, Err: errorText(err), Defect: isLifecycleDefect(err), Evidence: evidence})
		}
	}
}

func isLifecycleDefect(err error) bool {
	return errors.Is(err, gsr.ErrStopTimeout) || errors.Is(err, gsr.ErrServiceFailed) || errors.Is(err, context.DeadlineExceeded)
}

// startFactoryLifecycleRunners owns fixed create/stop workers outside Service handlers.
func startFactoryLifecycleRunners(factory *BattleFactoryService, ctx context.Context) {
	go factory.createLoop(ctx)
	go factory.stopLoop(ctx)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Compile-time checks keep lifecycle services on the Runtime Service boundary.
var _ gsr.Service = (*NHSKHostService)(nil)
var _ gsr.Service = (*BattleFactoryService)(nil)
