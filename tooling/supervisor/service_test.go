package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const emitFailureCommand gsr.CommandID = 0x7f000001

func TestSupervisorRegistersAndQueriesService(t *testing.T) {
	fixture := newSupervisorFixture(t, RestartNever)
	record, err := fixture.client.Get(context.Background(), fixture.registration.Key)
	if err != nil {
		t.Fatal(err)
	}
	if record.Registration != fixture.registration || record.Status != ServiceRunning {
		t.Fatalf("record = %#v, want running %#v", record, fixture.registration)
	}
	if err := fixture.client.Register(context.Background(), fixture.registration); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("duplicate Register error = %v, want ErrAlreadyRegistered", err)
	}
}

func TestSupervisorRejectsInvalidRegistrationAndSelfReference(t *testing.T) {
	executor := newRecordingRecoveryExecutor(1)
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service, err := NewService(executor)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(runtime, ref)
	if err != nil {
		t.Fatal(err)
	}
	registration := testRegistration(RestartNever)
	registration.Ref = ref
	if err := client.Register(context.Background(), registration); !errors.Is(err, ErrInvalidRegistration) {
		t.Fatalf("self Register error = %v, want ErrInvalidRegistration", err)
	}
	registration.Ref = gsr.ServiceRef{Node: "node-b", ID: 2}
	if err := client.Register(context.Background(), registration); !errors.Is(err, ErrInvalidRegistration) {
		t.Fatalf("remote Register error = %v, want ErrInvalidRegistration", err)
	}
}

func TestFailureNoticeRequiresExactSourceRefAndGeneration(t *testing.T) {
	fixture := newSupervisorFixture(t, RestartOnFailure)
	wrong := fixture.notice()
	wrong.Generation++
	if err := fixture.emit(wrong); err != nil {
		t.Fatal(err)
	}
	waitForCounter(t, fixture.runtime, metricFailureNoticesStale, 1)
	if got := fixture.executor.count(); got != 0 {
		t.Fatalf("scheduled tasks = %d, want 0", got)
	}

	if err := fixture.emit(fixture.notice()); err != nil {
		t.Fatal(err)
	}
	task := fixture.executor.next(t)
	if task.FailedRef != fixture.registration.Ref || task.Generation != 2 || task.Attempt != 1 {
		t.Fatalf("task = %#v", task)
	}
	waitForCounter(t, fixture.runtime, metricFailureNotices, 1)
	waitForCounter(t, fixture.runtime, metricRestartsScheduled, 1)
}

func TestDuplicateAndStaleNoticeDoNotScheduleRecovery(t *testing.T) {
	fixture := newSupervisorFixture(t, RestartOnFailure)
	if err := fixture.emit(fixture.notice()); err != nil {
		t.Fatal(err)
	}
	_ = fixture.executor.next(t)
	if err := fixture.emit(fixture.notice()); err != nil {
		t.Fatal(err)
	}
	waitForCounter(t, fixture.runtime, metricFailureNoticesDuplicate, 1)
	if got := fixture.executor.count(); got != 1 {
		t.Fatalf("scheduled tasks = %d, want 1", got)
	}
}

func TestRestartNeverAndDestroyHaveDistinctTerminalStates(t *testing.T) {
	for _, test := range []struct {
		strategy RestartStrategy
		want     ServiceStatus
	}{
		{strategy: RestartNever, want: ServiceRestartStopped},
		{strategy: DestroyOnFailure, want: ServiceDestroyed},
	} {
		t.Run(fmt.Sprint(test.strategy), func(t *testing.T) {
			fixture := newSupervisorFixture(t, test.strategy)
			if err := fixture.emit(fixture.notice()); err != nil {
				t.Fatal(err)
			}
			record := fixture.waitStatus(t, test.want)
			if record.LastFailure != RecoveryFailureNone || fixture.executor.count() != 0 {
				t.Fatalf("record/tasks = %#v/%d", record, fixture.executor.count())
			}
		})
	}
}

func TestRestartOnFailureSchedulesExponentialBackoff(t *testing.T) {
	fixture := newSupervisorFixture(t, RestartOnFailure)
	if err := fixture.emit(fixture.notice()); err != nil {
		t.Fatal(err)
	}
	first := fixture.executor.next(t)
	if first.Delay != 10*time.Millisecond {
		t.Fatalf("first delay = %v", first.Delay)
	}
	fixture.reportFailed(t, first, RecoveryFailurePrepare)
	second := fixture.executor.next(t)
	if second.Delay != 20*time.Millisecond || second.Attempt != 2 || second.Generation != 2 {
		t.Fatalf("second task = %#v", second)
	}
	fixture.reportFailed(t, second, RecoveryFailurePrepare)
	third := fixture.executor.next(t)
	if third.Delay != 40*time.Millisecond || third.Attempt != 3 {
		t.Fatalf("third task = %#v", third)
	}
	fixture.reportFailed(t, third, RecoveryFailurePrepare)
	record := fixture.waitStatus(t, ServiceRecoveryFailed)
	if record.AttemptsInFault != 3 || record.LastFailure != RecoveryFailurePrepare {
		t.Fatalf("record = %#v", record)
	}
	if got := fixture.executor.count(); got != 3 {
		t.Fatalf("scheduled tasks = %d, want 3", got)
	}
	waitForCounter(t, fixture.runtime, metricRestartsFailed, 3)
	waitForCounter(t, fixture.runtime, metricRestartsSuppressed, 1)
}

func TestRestartWindowSuppressesRapidRepeatedFailures(t *testing.T) {
	fixture := newSupervisorFixture(t, RestartOnFailure)
	fixture.registration.Policy.MaxRestarts = 1
	// Replace the fixture registration before the first notice.
	fixture = newSupervisorFixtureWithRegistration(t, fixture.registration)
	if err := fixture.emit(fixture.notice()); err != nil {
		t.Fatal(err)
	}
	task := fixture.executor.next(t)
	replacement := fixture.createEmitter(t)
	fixture.reportStarted(t, task)
	fixture.reportPrepared(t, task, replacement)
	fixture.reportCommitted(t, task, replacement)
	waitForCounter(t, fixture.runtime, metricRestartsSucceeded, 1)
	fixture.registration.Ref = replacement
	fixture.registration.Generation = 2
	if err := fixture.emitFrom(replacement, fixture.notice()); err != nil {
		t.Fatal(err)
	}
	record := fixture.waitStatus(t, ServiceRestartSuppressed)
	if record.RestartsInWindow != 1 || record.LastFailure != RecoveryFailureSuppressed {
		t.Fatalf("record = %#v", record)
	}
	if got := fixture.executor.count(); got != 1 {
		t.Fatalf("scheduled tasks = %d, want 1", got)
	}
	waitForCounter(t, fixture.runtime, metricRestartsSuppressed, 1)
}

func TestRecoveryQueueFailureIsTerminalAndObservable(t *testing.T) {
	executor := &recordingRecoveryExecutor{submitErr: ErrRecoveryQueueFull}
	fixture := newSupervisorFixtureWithExecutor(t, testRegistration(RestartOnFailure), executor)
	if err := fixture.emit(fixture.notice()); err != nil {
		t.Fatal(err)
	}
	record := fixture.waitStatus(t, ServiceRecoveryFailed)
	if record.LastFailure != RecoveryFailureQueueFull {
		t.Fatalf("record = %#v", record)
	}
	waitForCounter(t, fixture.runtime, metricRestartsFailed, 1)
}

type supervisorFixture struct {
	runtime      *gsr.Runtime
	supervisor   gsr.ServiceRef
	client       *Client
	executor     *recordingRecoveryExecutor
	registration Registration
}

func newSupervisorFixture(t *testing.T, strategy RestartStrategy) *supervisorFixture {
	return newSupervisorFixtureWithRegistration(t, testRegistration(strategy))
}

func newSupervisorFixtureWithRegistration(t *testing.T, registration Registration) *supervisorFixture {
	return newSupervisorFixtureWithExecutor(t, registration, newRecordingRecoveryExecutor(16))
}

func newSupervisorFixtureWithExecutor(t *testing.T, registration Registration, executor *recordingRecoveryExecutor) *supervisorFixture {
	t.Helper()
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service, err := NewService(executor)
	if err != nil {
		t.Fatal(err)
	}
	supervisorRef, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	emitterRef, err := runtime.CreateService(gsr.ServiceSpec{Service: &failureEmitterService{target: supervisorRef}})
	if err != nil {
		t.Fatal(err)
	}
	registration.Ref = emitterRef
	client, err := NewClient(runtime, supervisorRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Register(context.Background(), registration); err != nil {
		t.Fatal(err)
	}
	return &supervisorFixture{runtime: runtime, supervisor: supervisorRef, client: client, executor: executor, registration: registration}
}

func (f *supervisorFixture) notice() FailureNotice {
	return FailureNotice{
		Key: f.registration.Key, FailedRef: f.registration.Ref, Generation: f.registration.Generation,
		OccurredAt: time.Now(), Kind: FailureHandlerPanic,
	}
}

func (f *supervisorFixture) emit(notice FailureNotice) error {
	return f.emitFrom(f.registration.Ref, notice)
}

func (f *supervisorFixture) emitFrom(source gsr.ServiceRef, notice FailureNotice) error {
	return f.runtime.Send(source, emitFailureCommand, notice)
}

func (f *supervisorFixture) createEmitter(t *testing.T) gsr.ServiceRef {
	t.Helper()
	ref, err := f.runtime.CreateService(gsr.ServiceSpec{Service: &failureEmitterService{target: f.supervisor}})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func (f *supervisorFixture) reportStarted(t *testing.T, task RecoveryTask) {
	t.Helper()
	f.callOperation(t, recoveryStartedCommand, recoveryStartedRequest{Task: task})
}

func (f *supervisorFixture) reportPrepared(t *testing.T, task RecoveryTask, ref gsr.ServiceRef) {
	t.Helper()
	f.callOperation(t, recoveryPreparedCommand, recoveryPreparedRequest{Task: task, Ref: ref})
}

func (f *supervisorFixture) reportCommitted(t *testing.T, task RecoveryTask, ref gsr.ServiceRef) {
	t.Helper()
	f.callOperation(t, recoveryCommittedCommand, recoveryCommittedRequest{Task: task, Ref: ref})
}

func (f *supervisorFixture) reportFailed(t *testing.T, task RecoveryTask, failure RecoveryFailure) {
	t.Helper()
	f.callOperation(t, recoveryFailedCommand, recoveryFailedRequest{Task: task, Failure: failure})
}

func (f *supervisorFixture) callOperation(t *testing.T, command gsr.CommandID, payload any) {
	t.Helper()
	value, err := f.runtime.Call(context.Background(), f.supervisor, command, payload)
	if err != nil {
		t.Fatal(err)
	}
	response, ok := value.(operationResponse)
	if !ok {
		t.Fatalf("response = %T", value)
	}
	if err := errorFromResponse(response.Error); err != nil {
		t.Fatalf("operation error = %v", err)
	}
}

func (f *supervisorFixture) waitStatus(t *testing.T, status ServiceStatus) Record {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		record, err := f.client.Get(context.Background(), f.registration.Key)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == status {
			return record
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for status %v", status)
	return Record{}
}

type failureEmitterService struct {
	context gsr.ServiceContext
	target  gsr.ServiceRef
}

func (*failureEmitterService) Commands() []gsr.CommandID { return []gsr.CommandID{emitFailureCommand} }
func (s *failureEmitterService) Init(ctx gsr.ServiceContext) error {
	s.context = ctx
	return nil
}
func (s *failureEmitterService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	return s.context.Send(s.target, failureCommand, command.Payload)
}
func (*failureEmitterService) Stop(context.Context) error { return nil }
func (*failureEmitterService) Close() error               { return nil }

type recordingRecoveryExecutor struct {
	mu        sync.Mutex
	tasks     []RecoveryTask
	queued    chan RecoveryTask
	submitErr error
}

func newRecordingRecoveryExecutor(capacity int) *recordingRecoveryExecutor {
	return &recordingRecoveryExecutor{queued: make(chan RecoveryTask, capacity)}
}

func (e *recordingRecoveryExecutor) Submit(task RecoveryTask) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.submitErr != nil {
		return e.submitErr
	}
	e.tasks = append(e.tasks, task)
	e.queued <- task
	return nil
}

func (e *recordingRecoveryExecutor) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.tasks)
}

func (e *recordingRecoveryExecutor) next(t *testing.T) RecoveryTask {
	t.Helper()
	select {
	case task := <-e.queued:
		return task
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recovery task")
		return RecoveryTask{}
	}
}

func waitForCounter(t *testing.T, runtime *gsr.Runtime, name string, want uint64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := runtime.Inspect().Metrics.Counter(name); got >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("metric %s = %d, want at least %d", name, runtime.Inspect().Metrics.Counter(name), want)
}
