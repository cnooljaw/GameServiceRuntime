package supervisor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestRunnerRecordsPreparedBeforeCommit(t *testing.T) {
	events := &runnerEvents{}
	caller := &runnerCaller{events: events, committed: make(chan struct{}, 1)}
	launcher := &runnerLauncher{events: events, ref: gsr.ServiceRef{Node: "node-a", ID: 8}}
	runner := newTestRunner(t, caller, launcher, 1, 1)
	if err := runner.Submit(testRecoveryTask()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-caller.committed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for committed result")
	}
	want := []string{"started", "prepare", "prepared", "commit", "committed"}
	if got := events.snapshot(); !equalStrings(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestRunnerAbortsWhenPreparedResultIsStale(t *testing.T) {
	events := &runnerEvents{}
	caller := &runnerCaller{events: events, preparedError: responseStaleRecovery, prepared: make(chan struct{}, 1)}
	launcher := &runnerLauncher{events: events, ref: gsr.ServiceRef{Node: "node-a", ID: 8}}
	runner := newTestRunner(t, caller, launcher, 1, 1)
	if err := runner.Submit(testRecoveryTask()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-caller.prepared:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prepared result")
	}
	waitForRunnerEvent(t, events, "abort")
	want := []string{"started", "prepare", "prepared", "abort"}
	if got := events.snapshot(); !equalStrings(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestRunnerAbortsPublishFailureBeforeReportingFailure(t *testing.T) {
	events := &runnerEvents{}
	caller := &runnerCaller{events: events, failed: make(chan struct{}, 1)}
	launcher := &runnerLauncher{
		events: events, ref: gsr.ServiceRef{Node: "node-a", ID: 8}, commitErr: errors.New("publish failed"),
	}
	runner := newTestRunner(t, caller, launcher, 1, 1)
	if err := runner.Submit(testRecoveryTask()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-caller.failed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failed result")
	}
	want := []string{"started", "prepare", "prepared", "commit", "abort", "failed"}
	if got := events.snapshot(); !equalStrings(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if caller.lastFailure != RecoveryFailurePublish {
		t.Fatalf("failure = %v, want RecoveryFailurePublish", caller.lastFailure)
	}
}

func TestRunnerReportsAbortFailureAsTerminalCategory(t *testing.T) {
	events := &runnerEvents{}
	caller := &runnerCaller{events: events, failed: make(chan struct{}, 1)}
	launcher := &runnerLauncher{
		events: events, ref: gsr.ServiceRef{Node: "node-a", ID: 8},
		commitErr: errors.New("publish failed"), abortErr: errors.New("withdraw failed"),
	}
	runner := newTestRunner(t, caller, launcher, 1, 1)
	if err := runner.Submit(testRecoveryTask()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-caller.failed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failed result")
	}
	if caller.lastFailure != RecoveryFailureAbort {
		t.Fatalf("failure = %v, want RecoveryFailureAbort", caller.lastFailure)
	}
}

func TestRunnerQueueIsBoundedAndSubmitDoesNotBlock(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	launcher := &runnerLauncher{
		ref: gsr.ServiceRef{Node: "node-a", ID: 8}, prepareEntered: entered, prepareRelease: release,
	}
	runner := newTestRunner(t, &runnerCaller{}, launcher, 1, 1)
	first := testRecoveryTask()
	if err := runner.Submit(first); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not enter Prepare")
	}
	second := first
	second.Attempt = 2
	if err := runner.Submit(second); err != nil {
		t.Fatal(err)
	}
	third := first
	third.Attempt = 3
	started := time.Now()
	if err := runner.Submit(third); !errors.Is(err, ErrRecoveryQueueFull) {
		t.Fatalf("third Submit error = %v, want ErrRecoveryQueueFull", err)
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("full Submit blocked for %v", elapsed)
	}
	close(release)
}

func TestRunnerRetriesMailboxFullResultDelivery(t *testing.T) {
	events := &runnerEvents{}
	caller := &runnerCaller{events: events, startedMailboxFull: 2, committed: make(chan struct{}, 1)}
	launcher := &runnerLauncher{events: events, ref: gsr.ServiceRef{Node: "node-a", ID: 8}}
	runner := newTestRunner(t, caller, launcher, 1, 1)
	if err := runner.Submit(testRecoveryTask()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-caller.committed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for committed result")
	}
	if caller.startedCalls != 3 {
		t.Fatalf("started calls = %d, want 3", caller.startedCalls)
	}
}

func TestRunnerAttemptTimeoutReportsPrepareFailure(t *testing.T) {
	release := make(chan struct{})
	events := &runnerEvents{}
	caller := &runnerCaller{events: events, failed: make(chan struct{}, 1)}
	launcher := &runnerLauncher{
		events: events, ref: gsr.ServiceRef{Node: "node-a", ID: 8}, prepareRelease: release,
	}
	config := testRunnerConfig()
	config.AttemptTimeout = 10 * time.Millisecond
	runner, err := NewRunner(caller, launcher, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })
	if err := runner.Submit(testRecoveryTask()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-caller.failed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prepare failure")
	}
	if caller.lastFailure != RecoveryFailurePrepare {
		t.Fatalf("failure = %v, want RecoveryFailurePrepare", caller.lastFailure)
	}
}

func TestRunnerCloseCancelsBackoffWithoutLaunching(t *testing.T) {
	events := &runnerEvents{}
	runner := newTestRunner(t, &runnerCaller{events: events}, &runnerLauncher{events: events}, 1, 1)
	task := testRecoveryTask()
	task.Delay = time.Hour
	if err := runner.Submit(task); err != nil {
		t.Fatal(err)
	}
	if err := runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := events.snapshot(); len(got) != 0 {
		t.Fatalf("events after canceled backoff = %v", got)
	}
}

func TestRunnerCloseTimeoutKeepsWaitingForRealLauncherReturn(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	launcher := &runnerLauncher{
		ref: gsr.ServiceRef{Node: "node-a", ID: 8}, prepareEntered: entered, prepareRelease: release, ignoreContext: true,
	}
	runner := newTestRunner(t, &runnerCaller{}, launcher, 1, 1)
	if err := runner.Submit(testRecoveryTask()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not enter Prepare")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := runner.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close error = %v, want DeadlineExceeded", err)
	}
	select {
	case <-runner.done:
		t.Fatal("Runner forgot blocked launcher task")
	default:
	}
	close(release)
	if err := runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerRejectsInvalidConfigAndTask(t *testing.T) {
	valid := testRunnerConfig()
	var nilCaller *runnerCaller
	var nilLauncher *runnerLauncher
	if _, err := NewRunner(nilCaller, &runnerLauncher{}, valid); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil caller error = %v", err)
	}
	if _, err := NewRunner(&runnerCaller{}, nilLauncher, valid); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil launcher error = %v", err)
	}
	invalid := valid
	invalid.QueueSize = 0
	if _, err := NewRunner(&runnerCaller{}, &runnerLauncher{}, invalid); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid config error = %v", err)
	}
	runner := newTestRunner(t, &runnerCaller{}, &runnerLauncher{}, 1, 1)
	if err := runner.Submit(RecoveryTask{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid task error = %v, want ErrInvalidConfig", err)
	}
	if err := runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(testRecoveryTask()); !errors.Is(err, ErrRunnerClosed) {
		t.Fatalf("closed Submit error = %v, want ErrRunnerClosed", err)
	}
}

func newTestRunner(t *testing.T, caller CommandCaller, launcher Launcher, workers, queueSize int) *Runner {
	t.Helper()
	config := testRunnerConfig()
	config.Workers = workers
	config.QueueSize = queueSize
	runner, err := NewRunner(caller, launcher, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })
	return runner
}

func testRunnerConfig() RunnerConfig {
	return RunnerConfig{
		Workers: 1, QueueSize: 8, AttemptTimeout: time.Second,
		ResultTimeout: time.Second, ResultRetryInterval: time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func testRecoveryTask() RecoveryTask {
	return RecoveryTask{
		Supervisor: gsr.ServiceRef{Node: "node-a", ID: 1},
		Key:        testServiceKey(), FailedRef: gsr.ServiceRef{Node: "node-a", ID: 7},
		Generation: 2, Attempt: 1,
	}
}

type runnerEvents struct {
	mu     sync.Mutex
	events []string
}

func (e *runnerEvents) add(event string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.events = append(e.events, event)
	e.mu.Unlock()
}

func (e *runnerEvents) snapshot() []string {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.events...)
}

type runnerCaller struct {
	mu                 sync.Mutex
	events             *runnerEvents
	preparedError      responseError
	startedMailboxFull int
	startedCalls       int
	lastFailure        RecoveryFailure
	prepared           chan struct{}
	committed          chan struct{}
	failed             chan struct{}
}

func (c *runnerCaller) Call(_ context.Context, _ gsr.ServiceRef, command gsr.CommandID, payload any) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch command {
	case recoveryStartedCommand:
		c.startedCalls++
		if c.startedCalls <= c.startedMailboxFull {
			return nil, gsr.ErrMailboxFull
		}
		c.events.add("started")
	case recoveryPreparedCommand:
		c.events.add("prepared")
		if c.prepared != nil {
			select {
			case c.prepared <- struct{}{}:
			default:
			}
		}
		return operationResponse{Error: c.preparedError}, nil
	case recoveryCommittedCommand:
		c.events.add("committed")
		if c.committed != nil {
			select {
			case c.committed <- struct{}{}:
			default:
			}
		}
	case recoveryFailedCommand:
		c.events.add("failed")
		c.lastFailure = payload.(recoveryFailedRequest).Failure
		if c.failed != nil {
			select {
			case c.failed <- struct{}{}:
			default:
			}
		}
	}
	return operationResponse{}, nil
}

type runnerLauncher struct {
	events         *runnerEvents
	ref            gsr.ServiceRef
	prepareErr     error
	commitErr      error
	abortErr       error
	prepareEntered chan<- struct{}
	prepareRelease <-chan struct{}
	ignoreContext  bool
}

func (l *runnerLauncher) Prepare(ctx context.Context, _ LaunchRequest) (gsr.ServiceRef, error) {
	l.events.add("prepare")
	if l.prepareEntered != nil {
		select {
		case l.prepareEntered <- struct{}{}:
		default:
		}
	}
	if l.prepareRelease != nil {
		if l.ignoreContext {
			<-l.prepareRelease
		} else {
			select {
			case <-l.prepareRelease:
			case <-ctx.Done():
				return gsr.ServiceRef{}, context.Cause(ctx)
			}
		}
	}
	return l.ref, l.prepareErr
}

func (l *runnerLauncher) Commit(context.Context, LaunchRequest, gsr.ServiceRef) error {
	l.events.add("commit")
	return l.commitErr
}

func (l *runnerLauncher) Abort(context.Context, LaunchRequest, gsr.ServiceRef) error {
	l.events.add("abort")
	return l.abortErr
}

func waitForRunnerEvent(t *testing.T, events *runnerEvents, event string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, got := range events.snapshot() {
			if got == event {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for event %q; got %v", event, events.snapshot())
}
