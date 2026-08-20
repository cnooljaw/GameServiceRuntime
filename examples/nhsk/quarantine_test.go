package nhsk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestBattleBoundaryQuarantinesHandlerPanicWithLastStableEvidence(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "quarantine-test", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	reporter := &quarantineReportService{reports: make(chan battleQuarantineReport, 1)}
	reporterRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "quarantine-reporter", Service: reporter})
	if err != nil {
		t.Fatal(err)
	}
	battle, err := NewBattleService(NHSKBattleConfig{ID: 41, Random: &panicNHSKRandom{}, Clock: fixedNHSKClock{now: time.Unix(100, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := newBattleQuarantineBoundary(battle, battleQuarantineBoundaryConfig{Host: reporterRef, ConnectionGeneration: 7})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Name: "nhsk-battle/41", Service: boundary})
	if err != nil {
		t.Fatal(err)
	}
	initializeBattleForQuarantineTest(t, runtime, ref, 41)

	if _, err := runtime.Call(context.Background(), ref, StartSubgameCommand, nil); !errors.Is(err, gsr.ErrServiceFailed) {
		t.Fatalf("StartSubgame panic error = %v, want ErrServiceFailed", err)
	}
	select {
	case report := <-reporter.reports:
		if report.BattleID != 41 || report.Ref != ref || report.ConnectionGeneration != 7 {
			t.Fatalf("report identity = %#v", report)
		}
		if report.Evidence.FailedCommand != StartSubgameCommand || report.Evidence.CommandSequence != 4 {
			t.Fatalf("failed input = %#v", report.Evidence)
		}
		if report.Evidence.LastStableSnapshot.Phase != string(NHSKBattlePreparing) {
			t.Fatalf("last stable phase = %q", report.Evidence.LastStableSnapshot.Phase)
		}
		if len(report.Evidence.Commands) != 4 || !strings.Contains(report.Evidence.Stack, "panicNHSKRandom") {
			t.Fatalf("evidence commands=%d stack=%q", len(report.Evidence.Commands), report.Evidence.Stack)
		}
	case <-time.After(time.Second):
		t.Fatal("quarantine report was not delivered")
	}
}

func TestHostRetainsQuarantinedBattleAcrossDeleteDisconnectAndCapacity(t *testing.T) {
	fixture := newQuarantineHostFixture(t, 2)
	ref := fixture.create(t, 51, 9)
	fixture.quarantine(t, 51, ref, 9)

	if _, err := fixture.runtime.Call(context.Background(), fixture.host, ResolveBattleCommand, ResolveBattleRequest{BattleID: 51}); !errors.Is(err, ErrBattleQuarantined) {
		t.Fatalf("Resolve quarantined error = %v", err)
	}
	value, err := fixture.runtime.Call(context.Background(), fixture.host, RequestDeleteBattleCommand, RequestDeleteBattleRequest{BattleID: 51, Ref: ref, ConnectionGeneration: 9})
	if err != nil || !value.(HostCommandResult).Accepted {
		t.Fatalf("delete quarantine = %#v, %v", value, err)
	}
	value, err = fixture.runtime.Call(context.Background(), fixture.host, RequestDeleteBattleCommand, RequestDeleteBattleRequest{BattleID: 51, Ref: ref, ConnectionGeneration: 10})
	if err != nil || !value.(HostCommandResult).Accepted {
		t.Fatalf("repeat delete quarantine = %#v, %v", value, err)
	}
	if err := fixture.runtime.Send(fixture.factory, stopGenerationCommand, stopGenerationInternal{Generation: 9}); err != nil {
		t.Fatal(err)
	}

	fixture.create(t, 52, 10)
	value, err = fixture.runtime.Call(context.Background(), fixture.host, BeginCreateBattleCommand, CreateBattleRequest{BattleID: 53, ConnectionGeneration: 10})
	if err != nil {
		t.Fatal(err)
	}
	if operation := value.(CreateBattleOperation); operation.Rejection != errHostCapacity.Error() {
		t.Fatalf("capacity operation = %#v", operation)
	}
	value, err = fixture.runtime.Call(context.Background(), fixture.host, BeginCreateBattleCommand, CreateBattleRequest{BattleID: 51, ConnectionGeneration: 10})
	if err != nil {
		t.Fatal(err)
	}
	if operation := value.(CreateBattleOperation); operation.Rejection != errBattleIDInUse.Error() {
		t.Fatalf("same-id operation = %#v", operation)
	}

	snapshotValue, err := fixture.runtime.Call(context.Background(), fixture.host, GetHostSnapshotCommand, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotValue.(HostSnapshot)
	if len(snapshot.Quarantined) != 1 || snapshot.Quarantined[0].BattleID != 51 {
		t.Fatalf("quarantine snapshot = %#v", snapshot.Quarantined)
	}
	observation := snapshot.Quarantined[0].ExternalEnd
	if observation.Count != 2 || observation.FirstObservedAt.IsZero() || observation.LatestConnectionGeneration != 10 {
		t.Fatalf("external end observation = %#v", observation)
	}
}

func TestLocalDiagnosticExporterPublishesCompleteReceiptAtomically(t *testing.T) {
	root := t.TempDir()
	exporter := LocalDiagnosticExporter{Root: root}
	artifact := testDiagnosticArtifact()
	receipt, err := exporter.ExportDiagnostic(context.Background(), artifact)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.BattleID != artifact.BattleID || receipt.Ref != artifact.Ref || receipt.Digest == "" || receipt.Directory == "" {
		t.Fatalf("receipt = %#v", receipt)
	}
	for _, name := range []string{"manifest.json", "snapshot.json", "commands.jsonl", "panic.txt", "runtime-inspection.json", "receipt.json"} {
		info, statErr := os.Stat(filepath.Join(receipt.Directory, name))
		if statErr != nil || info.Size() == 0 {
			t.Fatalf("diagnostic file %s: info=%v err=%v", name, info, statErr)
		}
	}
	matches, err := filepath.Glob(filepath.Join(root, ".tmp-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary directories = %v, err=%v", matches, err)
	}
	if err := exporter.CleanupDiagnostic(receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(receipt.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("diagnostic directory after cleanup: %v", err)
	}
}

func TestLocalDiagnosticExporterRefusesCleanupOutsideConfiguredRoot(t *testing.T) {
	exporter := LocalDiagnosticExporter{Root: t.TempDir()}
	receipt := DiagnosticReceipt{ReceiptID: "foreign", BattleID: 1, Ref: gsr.ServiceRef{Node: "node", ID: 1}, Digest: strings.Repeat("a", 64), Directory: filepath.Join(t.TempDir(), "foreign"), CreatedAt: time.Unix(1, 0)}
	if err := exporter.CleanupDiagnostic(receipt); !errors.Is(err, ErrDiagnosticReceiptMismatch) {
		t.Fatalf("cleanup error = %v", err)
	}
}

func TestDiagnosticRunnerQueueFullRetryAndExactReceiptRelease(t *testing.T) {
	fixture := newQuarantineHostFixture(t, 1)
	ref := fixture.create(t, 71, 12)
	fixture.quarantine(t, 71, ref, 12)
	if err := fixture.runtime.Send(fixture.factory, reportBattleQuarantinedCommand, battleQuarantineReport{BattleID: 71, Ref: ref, ConnectionGeneration: 12, Evidence: testDiagnosticArtifact().Evidence}); err != nil {
		t.Fatal(err)
	}

	receipt := DiagnosticReceipt{ReceiptID: "receipt-71", BattleID: 71, Ref: ref, Digest: strings.Repeat("a", 64), Directory: t.TempDir(), CreatedAt: time.Unix(600, 0)}
	if err := fixture.runtime.Send(fixture.host, applyDiagnosticExportResultCommand, diagnosticExportResult{BattleID: 71, Ref: ref, Receipt: receipt}); err != nil {
		t.Fatal(err)
	}
	admin := NewQuarantineAdmin(fixture.runtime, fixture.host, "")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries, err := admin.ListQuarantined(context.Background())
		if err == nil && len(entries) == 1 && entries[0].ExportStatus == DiagnosticExported {
			break
		}
	}
	if err := admin.ReleaseQuarantinedBattle(context.Background(), DiagnosticReceipt{ReceiptID: receipt.ReceiptID, BattleID: 71, Ref: gsr.ServiceRef{Node: ref.Node, ID: ref.ID + 1}, Digest: receipt.Digest}); !errors.Is(err, ErrDiagnosticReceiptMismatch) {
		t.Fatalf("mismatched release error = %v", err)
	}
	if err := admin.ReleaseQuarantinedBattle(context.Background(), receipt); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries, err := admin.ListQuarantined(context.Background())
		if err == nil && len(entries) == 0 {
			return
		}
	}
	t.Fatal("receipt release did not remove retained Host entry")
}

func TestFactoryStopTimeoutQuarantinesInsteadOfReleasingBattleID(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "stop-timeout", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	stopper := &timeoutBattleStopper{Runtime: runtime}
	factoryService, err := NewBattleFactoryService(runtime, stopper)
	if err != nil {
		t.Fatal(err)
	}
	factory, err := runtime.CreateService(gsr.ServiceSpec{Name: "factory", Service: factoryService})
	if err != nil {
		t.Fatal(err)
	}
	hostService, err := NewNHSKHostService(NHSKHostConfig{MaxActiveBattles: 1, FactoryRef: factory})
	if err != nil {
		t.Fatal(err)
	}
	host, err := runtime.CreateService(gsr.ServiceSpec{Name: "host", Service: hostService})
	if err != nil {
		t.Fatal(err)
	}
	fixture := quarantineHostFixture{runtime: runtime, host: host, factory: factory}
	ref := fixture.create(t, 72, 15)
	if _, err := runtime.Call(context.Background(), host, RequestDeleteBattleCommand, RequestDeleteBattleRequest{BattleID: 72, Ref: ref, ConnectionGeneration: 15}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		value, err := runtime.Call(context.Background(), host, GetHostSnapshotCommand, nil)
		if err == nil {
			snapshot := value.(HostSnapshot)
			if len(snapshot.Quarantined) == 1 && snapshot.Quarantined[0].BattleID == 72 {
				created, err := runtime.Call(context.Background(), host, BeginCreateBattleCommand, CreateBattleRequest{BattleID: 73})
				if err != nil {
					t.Fatal(err)
				}
				if operation := created.(CreateBattleOperation); operation.Rejection != errHostCapacity.Error() {
					t.Fatalf("capacity after Stop timeout = %#v", operation)
				}
				return
			}
		}
	}
	t.Fatal("Stop timeout did not retain quarantined binding")
}

func TestDiagnosticRunnerBoundsQueueAndReportsExporterResult(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runtime := &diagnosticRunnerTestRuntime{results: make(chan diagnosticExportResult, 3)}
	exporter := diagnosticExporterFunc(func(context.Context, DiagnosticArtifact) (DiagnosticReceipt, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return DiagnosticReceipt{ReceiptID: "ok", BattleID: 61, Ref: gsr.ServiceRef{Node: "node-a", ID: 19}, Digest: strings.Repeat("b", 64)}, nil
	})
	runner, err := NewDiagnosticRunner(runtime, exporter, DiagnosticRunnerConfig{QueueSize: 1, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	artifact := testDiagnosticArtifact()
	if err := runner.SubmitDiagnostic(artifact); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("exporter did not start")
	}
	if err := runner.SubmitDiagnostic(artifact); err != nil {
		t.Fatal(err)
	}
	if err := runner.SubmitDiagnostic(artifact); !errors.Is(err, errDiagnosticQueueFull) {
		t.Fatalf("third submit error = %v", err)
	}
	close(release)
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-runtime.results:
		if result.Receipt.ReceiptID != "ok" || result.BattleID != 61 {
			t.Fatalf("result = %#v", result)
		}
	default:
		t.Fatal("runner did not report result")
	}
}

type panicNHSKRandom struct{}

func (*panicNHSKRandom) Intn(int) int                { panic("random defect") }
func (*panicNHSKRandom) Shuffle(int, func(int, int)) {}

type fixedNHSKClock struct{ now time.Time }

func (clock fixedNHSKClock) Now() time.Time { return clock.now }

func initializeBattleForQuarantineTest(t *testing.T, runtime *gsr.Runtime, ref gsr.ServiceRef, battleID game.BattleID) {
	t.Helper()
	requests := []struct {
		command gsr.CommandID
		payload any
	}{
		{InitializeBattleCommand, InitializeBattleRequest{Identity: BattleIdentity{BattleID: battleID, ProductID: 82, MatchID: 1}}},
		{UpdatePlayersCommand, UpdatePlayersRequest{Players: []BattlePlayer{
			{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1},
			{Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3},
		}}},
		{PrepareSubgameCommand, PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
	}
	for _, request := range requests {
		if _, err := runtime.Call(context.Background(), ref, request.command, request.payload); err != nil {
			t.Fatalf("command %x: %v", request.command, err)
		}
	}
}

type quarantineReportService struct {
	service gsr.ServiceContext
	reports chan battleQuarantineReport
}

func (service *quarantineReportService) Init(ctx gsr.ServiceContext) error {
	service.service = ctx
	return nil
}
func (service *quarantineReportService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	if command.ID != reportBattleQuarantinedCommand {
		return gsr.ErrUnknownCommand
	}
	service.reports <- command.Payload.(battleQuarantineReport)
	return nil
}
func (*quarantineReportService) Stop(context.Context) error { return nil }
func (*quarantineReportService) Close() error               { return nil }

type quarantineHostFixture struct {
	runtime *gsr.Runtime
	host    gsr.ServiceRef
	factory gsr.ServiceRef
}

type diagnosticRunnerTestRuntime struct {
	results chan diagnosticExportResult
}

type timeoutBattleStopper struct{ *gsr.Runtime }

func (*timeoutBattleStopper) Stop(context.Context, gsr.ServiceRef) error { return gsr.ErrStopTimeout }

func (runtime *diagnosticRunnerTestRuntime) Send(_ gsr.ServiceRef, command gsr.CommandID, payload any) error {
	if command != applyDiagnosticExportResultCommand {
		return errors.New("unexpected command")
	}
	runtime.results <- payload.(diagnosticExportResult)
	return nil
}

func (*diagnosticRunnerTestRuntime) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, errors.New("unexpected Call")
}

func (*diagnosticRunnerTestRuntime) Inspect() gsr.RuntimeInspection {
	return gsr.RuntimeInspection{CapturedAt: time.Unix(700, 0), Node: "node-a", Status: gsr.RuntimeRunning}
}

type diagnosticExporterFunc func(context.Context, DiagnosticArtifact) (DiagnosticReceipt, error)

func (export diagnosticExporterFunc) ExportDiagnostic(ctx context.Context, artifact DiagnosticArtifact) (DiagnosticReceipt, error) {
	return export(ctx, artifact)
}

func newQuarantineHostFixture(t *testing.T, capacity uint32) quarantineHostFixture {
	t.Helper()
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "quarantine-host", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	factoryService, err := NewBattleFactoryService(runtime, runtime)
	if err != nil {
		t.Fatal(err)
	}
	factory, err := runtime.CreateService(gsr.ServiceSpec{Name: "factory", Service: factoryService})
	if err != nil {
		t.Fatal(err)
	}
	hostService, err := NewNHSKHostService(NHSKHostConfig{MaxActiveBattles: capacity, FactoryRef: factory, Clock: fixedNHSKClock{now: time.Unix(500, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	host, err := runtime.CreateService(gsr.ServiceSpec{Name: "host", Service: hostService})
	if err != nil {
		t.Fatal(err)
	}
	return quarantineHostFixture{runtime: runtime, host: host, factory: factory}
}

func (fixture quarantineHostFixture) create(t *testing.T, id game.BattleID, generation ConnectionGeneration) gsr.ServiceRef {
	t.Helper()
	if _, err := fixture.runtime.Call(context.Background(), fixture.host, BeginCreateBattleCommand, CreateBattleRequest{BattleID: id, ConnectionGeneration: generation}); err != nil {
		t.Fatal(err)
	}
	if !waitForBattleRef(t, fixture.runtime, fixture.host, id) {
		t.Fatalf("Battle %d not created", id)
	}
	value, err := fixture.runtime.Call(context.Background(), fixture.host, ResolveBattleCommand, ResolveBattleRequest{BattleID: id})
	if err != nil {
		t.Fatal(err)
	}
	return value.(ResolveBattleResult).Ref
}

func (fixture quarantineHostFixture) quarantine(t *testing.T, id game.BattleID, ref gsr.ServiceRef, generation ConnectionGeneration) {
	t.Helper()
	evidence := testDiagnosticArtifact().Evidence
	evidence.LastStableSnapshot.BattleID = id
	if err := fixture.runtime.Send(fixture.host, reportBattleQuarantinedCommand, battleQuarantineReport{BattleID: id, Ref: ref, ConnectionGeneration: generation, Evidence: evidence}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		value, err := fixture.runtime.Call(context.Background(), fixture.host, GetHostSnapshotCommand, nil)
		if err == nil && len(value.(HostSnapshot).Quarantined) == 1 {
			return
		}
	}
	t.Fatal("Host did not quarantine Battle")
}

func testDiagnosticArtifact() DiagnosticArtifact {
	ref := gsr.ServiceRef{Node: "node-a", ID: 19}
	return DiagnosticArtifact{
		Host: ref, BattleID: 61, Ref: ref, ConnectionGeneration: 3,
		Evidence: BattleDiagnosticEvidence{
			FailedAt: time.Unix(100, 0), FailedCommand: StartSubgameCommand, CommandSequence: 2,
			LastStableSnapshot: NHSKBattleSnapshot{BattleID: 61, Phase: string(NHSKBattlePreparing)},
			Commands:           []BattleCommandRecord{{Sequence: 1, RecordedAt: time.Unix(99, 0), CommandID: InitializeBattleCommand, Payload: []byte(`{"Identity":{"BattleID":61}}`)}},
			RandomSeed:         11, RandomSeedKnown: true, ClockAtFailure: time.Unix(100, 0), Stack: "panic: defect\nstack",
		},
		RuntimeInspection: gsr.RuntimeInspection{CapturedAt: time.Unix(101, 0), Node: "node-a", Status: gsr.RuntimeRunning},
	}
}
