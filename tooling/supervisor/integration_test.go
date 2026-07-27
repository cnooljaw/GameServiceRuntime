package supervisor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/snapshot"
)

const (
	recoveryCounterValueCommand gsr.CommandID = 0x7f000101 + iota
	recoveryCounterPanicCommand
)

func TestSupervisorSnapshotRecoveryCreatesNewCommittedGeneration(t *testing.T) {
	fixture := newSnapshotRecoveryFixture(t, nil)
	saved, err := fixture.snapshots.Capture(context.Background(), fixture.initialRef, fixture.snapshotKey)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State.Revision != 2 {
		t.Fatalf("captured revision = %d, want 2", saved.State.Revision)
	}
	if _, err := fixture.runtime.Call(context.Background(), fixture.initialRef, recoveryCounterPanicCommand, nil); !errors.Is(err, gsr.ErrServiceFailed) {
		t.Fatalf("panic Call error = %v, want ErrServiceFailed", err)
	}

	record := fixture.waitForStatus(t, ServiceRunning, 2)
	if record.Registration.Ref == fixture.initialRef {
		t.Fatalf("recovery reused old Ref %v", fixture.initialRef)
	}
	value, err := fixture.runtime.Call(context.Background(), record.Registration.Ref, recoveryCounterValueCommand, nil)
	if err != nil {
		t.Fatal(err)
	}
	state, ok := value.(recoveryCounterValue)
	if !ok || state.Value != 2 || state.Revision != 2 {
		t.Fatalf("restored value = %#v", value)
	}
	if got := fixture.publisher.current(fixture.key); got != record.Registration.Ref {
		t.Fatalf("published Ref = %v, want %v", got, record.Registration.Ref)
	}
	if _, err := fixture.runtime.Call(context.Background(), fixture.initialRef, recoveryCounterValueCommand, nil); !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("old Ref error = %v, want ErrServiceClosed", err)
	}
}

func TestSupervisorSnapshotRecoveryStopsWhenSnapshotIsMissing(t *testing.T) {
	fixture := newSnapshotRecoveryFixture(t, nil)
	if _, err := fixture.runtime.Call(context.Background(), fixture.initialRef, recoveryCounterPanicCommand, nil); !errors.Is(err, gsr.ErrServiceFailed) {
		t.Fatalf("panic Call error = %v, want ErrServiceFailed", err)
	}
	record := fixture.waitForStatus(t, ServiceRecoveryFailed, 1)
	if record.AttemptsInFault != fixture.registration.Policy.MaxAttempts || record.LastFailure != RecoveryFailureSnapshotNotFound {
		t.Fatalf("record = %#v", record)
	}
	if got := fixture.publisher.current(fixture.key); got != fixture.initialRef {
		t.Fatalf("binding changed without Snapshot: %v", got)
	}
}

func TestSupervisorSnapshotRecoveryAbortsEveryPublishFailure(t *testing.T) {
	publishErr := errors.New("discovery unavailable")
	fixture := newSnapshotRecoveryFixture(t, publishErr)
	if _, err := fixture.snapshots.Capture(context.Background(), fixture.initialRef, fixture.snapshotKey); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.runtime.Call(context.Background(), fixture.initialRef, recoveryCounterPanicCommand, nil); !errors.Is(err, gsr.ErrServiceFailed) {
		t.Fatalf("panic Call error = %v, want ErrServiceFailed", err)
	}
	record := fixture.waitForStatus(t, ServiceRecoveryFailed, 1)
	if record.LastFailure != RecoveryFailurePublish || record.AttemptsInFault != fixture.registration.Policy.MaxAttempts {
		t.Fatalf("record = %#v", record)
	}
	published, withdrawn := fixture.publisher.attempts()
	if len(published) != fixture.registration.Policy.MaxAttempts || len(withdrawn) != len(published) {
		t.Fatalf("published/withdrawn = %v/%v", published, withdrawn)
	}
	for index := range published {
		if published[index] != withdrawn[index] {
			t.Fatalf("attempt %d published/withdrawn = %v/%v", index, published[index], withdrawn[index])
		}
		if _, err := fixture.runtime.Call(context.Background(), published[index], recoveryCounterValueCommand, nil); !errors.Is(err, gsr.ErrServiceClosed) {
			t.Fatalf("aborted Ref %v error = %v, want ErrServiceClosed", published[index], err)
		}
	}
	if got := fixture.publisher.current(fixture.key); got != (gsr.ServiceRef{}) {
		t.Fatalf("failed publish left prepared binding: %v", got)
	}
}

func TestSupervisorStopsContinuousCreateFailuresAtAttemptBudget(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 2})
	control := &launcherControl{createErr: errors.New("runtime create unavailable")}
	launcher, err := NewRuntimeLauncher(control, ServiceFactoryFunc(func(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error) {
		return gsr.ServiceSpec{Service: panicDecoratorService{}}, nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(runtime, launcher, RunnerConfig{
		Workers: 1, QueueSize: 4, AttemptTimeout: time.Second,
		ResultTimeout: time.Second, ResultRetryInterval: time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = runner.Close(context.Background())
		_ = runtime.Close(context.Background())
	})
	supervisorService, err := NewService(runner)
	if err != nil {
		t.Fatal(err)
	}
	supervisorRef, err := runtime.CreateService(gsr.ServiceSpec{Service: supervisorService})
	if err != nil {
		t.Fatal(err)
	}
	key := testServiceKey()
	decorated, err := Decorate(panicDecoratorService{}, DecoratorConfig{Key: key, Generation: 1, Supervisor: supervisorRef})
	if err != nil {
		t.Fatal(err)
	}
	initialRef, err := runtime.CreateService(gsr.ServiceSpec{Service: decorated})
	if err != nil {
		t.Fatal(err)
	}
	policy := testRestartPolicy()
	policy.MaxAttempts = 2
	client, err := NewClient(runtime, supervisorRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Register(context.Background(), Registration{Key: key, Ref: initialRef, Generation: 1, Policy: policy}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Call(context.Background(), initialRef, 10, nil); !errors.Is(err, gsr.ErrServiceFailed) {
		t.Fatalf("panic Call error = %v, want ErrServiceFailed", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record, err := client.Get(context.Background(), key)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == ServiceRecoveryFailed {
			if record.LastFailure != RecoveryFailureCreate || record.AttemptsInFault != 2 {
				t.Fatalf("record = %#v", record)
			}
			if control.created() != 2 {
				t.Fatalf("CreateService calls = %d, want 2", control.created())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for continuous create failure suppression")
}

type snapshotRecoveryFixture struct {
	runtime      *gsr.Runtime
	runner       *Runner
	snapshots    *snapshot.Manager
	publisher    *recoveryBindingPublisher
	client       *Client
	key          ServiceKey
	snapshotKey  snapshot.Key
	registration Registration
	initialRef   gsr.ServiceRef
}

func newSnapshotRecoveryFixture(t *testing.T, publishErr error) *snapshotRecoveryFixture {
	t.Helper()
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 2})
	store := snapshot.NewMemoryStore()
	snapshots, err := snapshot.NewManager(runtime, store, snapshot.Config{})
	if err != nil {
		t.Fatal(err)
	}
	key := ServiceKey{Namespace: "player", ID: "42"}
	snapshotKey := snapshot.Key{Namespace: key.Namespace, ID: key.ID}
	publisher := newRecoveryBindingPublisher(publishErr)
	factory := ServiceFactoryFunc(func(ctx context.Context, requested ServiceKey, _ uint64) (gsr.ServiceSpec, error) {
		loaded, err := snapshots.Load(ctx, snapshot.Key{Namespace: requested.Namespace, ID: requested.ID})
		if err != nil {
			if errors.Is(err, snapshot.ErrSnapshotNotFound) {
				return gsr.ServiceSpec{}, errors.Join(ErrSnapshotNotFound, err)
			}
			return gsr.ServiceSpec{}, err
		}
		service, err := newRecoveryCounterService(requested, loaded.State)
		if err != nil {
			return gsr.ServiceSpec{}, err
		}
		return gsr.ServiceSpec{Service: service}, nil
	})
	launcher, err := NewRuntimeLauncher(runtime, factory, publisher)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(runtime, launcher, RunnerConfig{
		Workers: 1, QueueSize: 4, AttemptTimeout: time.Second,
		ResultTimeout: time.Second, ResultRetryInterval: time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = runner.Close(context.Background())
		_ = runtime.Close(context.Background())
	})
	supervisorService, err := NewService(runner)
	if err != nil {
		t.Fatal(err)
	}
	supervisorRef, err := runtime.CreateService(gsr.ServiceSpec{Service: supervisorService})
	if err != nil {
		t.Fatal(err)
	}
	initial := &recoveryCounterService{key: key, value: 2, revision: 2}
	decorated, err := Decorate(initial, DecoratorConfig{Key: key, Generation: 1, Supervisor: supervisorRef})
	if err != nil {
		t.Fatal(err)
	}
	initialRef, err := runtime.CreateService(gsr.ServiceSpec{Service: decorated})
	if err != nil {
		t.Fatal(err)
	}
	policy := RestartPolicy{
		Strategy: RestartOnFailure, MaxAttempts: 2, MaxRestarts: 2,
		Window: time.Minute, MinBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond,
	}
	registration := Registration{Key: key, Ref: initialRef, Generation: 1, Policy: policy}
	client, err := NewClient(runtime, supervisorRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Register(context.Background(), registration); err != nil {
		t.Fatal(err)
	}
	publisher.seed(key, initialRef)
	return &snapshotRecoveryFixture{
		runtime: runtime, runner: runner, snapshots: snapshots, publisher: publisher, client: client,
		key: key, snapshotKey: snapshotKey, registration: registration, initialRef: initialRef,
	}
}

func (f *snapshotRecoveryFixture) waitForStatus(t *testing.T, status ServiceStatus, generation uint64) Record {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record, err := f.client.Get(context.Background(), f.key)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == status && record.Registration.Generation == generation {
			return record
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for status=%v generation=%d", status, generation)
	return Record{}
}

type recoveryCounterValue struct {
	Value    int
	Revision uint64
}

type recoveryCounterService struct {
	key      ServiceKey
	value    int
	revision uint64
}

func newRecoveryCounterService(key ServiceKey, state snapshot.State) (*recoveryCounterService, error) {
	value, err := strconv.Atoi(string(state.Payload))
	if err != nil || state.Schema != "counter" || state.Version != 1 || state.Revision == 0 {
		return nil, snapshot.ErrInvalidState
	}
	return &recoveryCounterService{key: key, value: value, revision: state.Revision}, nil
}

func (*recoveryCounterService) Init(gsr.ServiceContext) error { return nil }
func (s *recoveryCounterService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case recoveryCounterValueCommand:
		return ctx.Reply(recoveryCounterValue{Value: s.value, Revision: s.revision})
	case recoveryCounterPanicCommand:
		panic("counter failed")
	case snapshot.CaptureCommand:
		request, ok := command.Payload.(snapshot.CaptureRequest)
		key := snapshot.Key{Namespace: s.key.Namespace, ID: s.key.ID}
		if !ok || request.Key != key {
			return snapshot.ErrInvalidKey
		}
		return ctx.Reply(snapshot.CaptureResponse{
			Key:   key,
			State: snapshot.State{Schema: "counter", Version: 1, Revision: s.revision, Payload: []byte(strconv.Itoa(s.value))},
		})
	default:
		return gsr.ErrUnknownCommand
	}
}
func (*recoveryCounterService) Stop(context.Context) error { return nil }
func (*recoveryCounterService) Close() error               { return nil }

type recoveryBindingPublisher struct {
	mu         sync.Mutex
	bindings   map[ServiceKey]gsr.ServiceRef
	publishErr error
	published  []gsr.ServiceRef
	withdrawn  []gsr.ServiceRef
}

func newRecoveryBindingPublisher(publishErr error) *recoveryBindingPublisher {
	return &recoveryBindingPublisher{bindings: make(map[ServiceKey]gsr.ServiceRef), publishErr: publishErr}
}

func (p *recoveryBindingPublisher) Publish(_ context.Context, key ServiceKey, ref gsr.ServiceRef) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.published = append(p.published, ref)
	if p.publishErr != nil {
		// Model an ambiguous publish: binding changed even though the caller saw an error.
		p.bindings[key] = ref
		return p.publishErr
	}
	p.bindings[key] = ref
	return nil
}

func (p *recoveryBindingPublisher) Withdraw(_ context.Context, key ServiceKey, ref gsr.ServiceRef) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.withdrawn = append(p.withdrawn, ref)
	if p.bindings[key] == ref {
		delete(p.bindings, key)
	}
	return nil
}

func (p *recoveryBindingPublisher) seed(key ServiceKey, ref gsr.ServiceRef) {
	p.mu.Lock()
	p.bindings[key] = ref
	p.mu.Unlock()
}

func (p *recoveryBindingPublisher) current(key ServiceKey) gsr.ServiceRef {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bindings[key]
}

func (p *recoveryBindingPublisher) attempts() ([]gsr.ServiceRef, []gsr.ServiceRef) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]gsr.ServiceRef(nil), p.published...), append([]gsr.ServiceRef(nil), p.withdrawn...)
}
