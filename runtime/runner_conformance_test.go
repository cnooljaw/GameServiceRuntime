package gsr_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const runnerResultCommand gsr.CommandID = 0x193001

func TestRunnerValidatesConfigAndKeepsNamesUnique(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	processor := func(context.Context, int) (int, error) { return 0, nil }
	for _, config := range []gsr.RunnerConfig{
		{},
		{Name: "invalid", Workers: 0, QueueSize: 1},
		{Name: "invalid", Workers: 1, QueueSize: 0},
	} {
		if _, err := gsr.NewRunner(runtime, config, processor); !errors.Is(err, gsr.ErrInvalidRunnerConfig) {
			t.Fatalf("NewRunner(%+v) error = %v, want ErrInvalidRunnerConfig", config, err)
		}
	}
	if _, err := gsr.NewRunner[int, int](nil, gsr.RunnerConfig{Name: "nil", Workers: 1, QueueSize: 1}, processor); !errors.Is(err, gsr.ErrInvalidRunnerConfig) {
		t.Fatalf("NewRunner(nil) error = %v, want ErrInvalidRunnerConfig", err)
	}
	if _, err := gsr.NewRunner[int, int](runtime, gsr.RunnerConfig{Name: "nil-processor", Workers: 1, QueueSize: 1}, nil); !errors.Is(err, gsr.ErrInvalidRunnerConfig) {
		t.Fatalf("NewRunner(nil processor) error = %v, want ErrInvalidRunnerConfig", err)
	}

	runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "unique", Workers: 1, QueueSize: 1}, processor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "unique", Workers: 1, QueueSize: 1}, processor); !errors.Is(err, gsr.ErrRunnerNameConflict) {
		t.Fatalf("duplicate name error = %v, want ErrRunnerNameConflict", err)
	}
	if err := runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "unique", Workers: 1, QueueSize: 1}, processor); !errors.Is(err, gsr.ErrRunnerNameConflict) {
		t.Fatalf("closed duplicate name error = %v, want ErrRunnerNameConflict", err)
	}
}

func TestRunnerSubmitIsBoundedAndDeliversResultCommand(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 1, MailboxSize: 8})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	started := make(chan struct{})
	release := make(chan struct{})
	runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "bounded", Workers: 1, QueueSize: 1}, func(_ context.Context, request int) (int, error) {
		if request == 1 {
			close(started)
			<-release
		}
		return request * 10, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })

	receiver := &runnerResultService{results: make(chan receivedRunnerResult, 2)}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: receiver})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(context.Background(), ref, runnerResultCommand, 1); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := runner.Submit(context.Background(), ref, runnerResultCommand, 2); err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(context.Background(), ref, runnerResultCommand, 3); !errors.Is(err, gsr.ErrRunnerQueueFull) {
		t.Fatalf("third Submit error = %v, want ErrRunnerQueueFull", err)
	}
	if err := runner.Submit(context.Background(), gsr.ServiceRef{Node: "node-b", ID: 1}, runnerResultCommand, 4); !errors.Is(err, gsr.ErrInvalidRunnerTarget) {
		t.Fatalf("remote target error = %v, want ErrInvalidRunnerTarget", err)
	}
	close(release)

	for _, want := range []int{10, 20} {
		select {
		case result := <-receiver.results:
			if result.source != (gsr.ServiceRef{Node: "node-a"}) {
				t.Fatalf("source = %#v, want Runtime root", result.source)
			}
			if result.result.Value != want || result.result.Err != nil {
				t.Fatalf("result = %+v, want Value %d", result.result, want)
			}
		case <-time.After(time.Second):
			t.Fatal("result command was not delivered")
		}
	}

	inspection := runtime.Inspect()
	if len(inspection.Runners) != 1 {
		t.Fatalf("Runners = %+v, want one", inspection.Runners)
	}
	got := inspection.Runners[0]
	if got.Name != "bounded" || got.Submitted != 2 || got.Completed != 2 || got.Rejected < 1 {
		t.Fatalf("Runner inspection = %+v", got)
	}
	if inspection.Metrics.Counter("runner_tasks_submitted_total") != 2 || inspection.Metrics.Counter("runner_tasks_rejected_total") < 1 {
		t.Fatalf("Runner metrics = %+v", inspection.Metrics.Counters())
	}
}

func TestRunnerAwaitReleasesPermitButKeepsServiceMailboxSerial(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 1, MailboxSize: 8})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	processorStarted := make(chan struct{})
	releaseProcessor := make(chan struct{})
	runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "await", Workers: 1, QueueSize: 1}, func(_ context.Context, request string) (string, error) {
		close(processorStarted)
		<-releaseProcessor
		return request + "-done", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })

	waiter := &runnerAwaitService{
		runner:        runner,
		awaited:       make(chan string, 1),
		secondHandled: make(chan struct{}),
	}
	waiterRef, err := runtime.CreateService(gsr.ServiceSpec{Service: waiter})
	if err != nil {
		t.Fatal(err)
	}
	other := &runnerSignalService{handled: make(chan struct{}, 1)}
	otherRef, err := runtime.CreateService(gsr.ServiceSpec{Service: other})
	if err != nil {
		t.Fatal(err)
	}

	if err := runtime.Send(waiterRef, 1, "work"); err != nil {
		t.Fatal(err)
	}
	<-processorStarted
	if err := runtime.Send(waiterRef, 2, nil); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Send(otherRef, 1, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-other.handled:
	case <-time.After(time.Second):
		t.Fatal("another Service did not use the released scheduler permit")
	}
	select {
	case <-waiter.secondHandled:
		t.Fatal("same Service mailbox re-entered while Await was pending")
	default:
	}

	close(releaseProcessor)
	select {
	case got := <-waiter.awaited:
		if got != "work-done" {
			t.Fatalf("Await result = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("Await did not resume at the call site")
	}
	select {
	case <-waiter.secondHandled:
	case <-time.After(time.Second):
		t.Fatal("same Service did not continue after Await returned")
	}
}

func TestRunnerAwaitRejectsUseOutsideCurrentHandler(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "outside", Workers: 1, QueueSize: 1}, func(_ context.Context, value int) (int, error) {
		return value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })
	service := &runnerCommandContextCaptureService{handled: make(chan struct{})}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-service.handled
	if _, err := runner.Await(context.Background(), service.context, 1); !errors.Is(err, gsr.ErrRunnerAwaitNotAllowed) {
		t.Fatalf("Await outside handler error = %v, want ErrRunnerAwaitNotAllowed", err)
	}
}

func TestRunnerAwaitRejectsCommandContextFromAnotherRuntime(t *testing.T) {
	ownerRuntime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = ownerRuntime.Close(context.Background()) })
	runnerRuntime := gsr.NewRuntime(gsr.Config{NodeID: "node-b"})
	t.Cleanup(func() { _ = runnerRuntime.Close(context.Background()) })
	runner, err := gsr.NewRunner(runnerRuntime, gsr.RunnerConfig{Name: "wrong-runtime", Workers: 1, QueueSize: 1}, func(_ context.Context, value int) (int, error) {
		return value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &runnerWrongRuntimeAwaitService{runner: runner, result: make(chan error, 1)}
	ref, err := ownerRuntime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	if err := ownerRuntime.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-service.result; !errors.Is(err, gsr.ErrRunnerAwaitNotAllowed) {
		t.Fatalf("wrong Runtime Await error = %v, want ErrRunnerAwaitNotAllowed", err)
	}
}

func TestRunnerAwaitRejectsRepeatedYieldOfTheSameHandler(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	nestedResult := make(chan error, 1)
	var runner *gsr.Runner[runnerNestedAwaitRequest, int]
	runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "repeated-await", Workers: 1, QueueSize: 1}, func(ctx context.Context, request runnerNestedAwaitRequest) (int, error) {
		_, nestedErr := runner.Await(ctx, request.owner, runnerNestedAwaitRequest{})
		nestedResult <- nestedErr
		return 1, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &runnerNestedAwaitService{runner: runner, result: make(chan error, 1)}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-nestedResult; !errors.Is(err, gsr.ErrRunnerAwaitNotAllowed) {
		t.Fatalf("nested Await error = %v, want ErrRunnerAwaitNotAllowed", err)
	}
	if err := <-service.result; err != nil {
		t.Fatalf("outer Await error = %v, want nil", err)
	}
}

func TestRunnerContainsProcessorPanicAndContinues(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "panic", Workers: 1, QueueSize: 2}, func(_ context.Context, value int) (int, error) {
		if value == 1 {
			panic("broken processor")
		}
		return value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })
	receiver := &runnerResultService{results: make(chan receivedRunnerResult, 2)}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: receiver})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(context.Background(), ref, runnerResultCommand, 1); err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(context.Background(), ref, runnerResultCommand, 2); err != nil {
		t.Fatal(err)
	}
	first := <-receiver.results
	second := <-receiver.results
	if !errors.Is(first.result.Err, gsr.ErrRunnerPanic) {
		t.Fatalf("panic result error = %v, want ErrRunnerPanic", first.result.Err)
	}
	if second.result.Value != 2 || second.result.Err != nil {
		t.Fatalf("worker did not continue: %+v", second.result)
	}
}

func TestRunnerMultipleWorkersMayCompleteOutOfOrder(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "unordered", Workers: 2, QueueSize: 2}, func(_ context.Context, value int) (int, error) {
		if value == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		return value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })
	receiver := &runnerResultService{results: make(chan receivedRunnerResult, 2)}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: receiver})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(context.Background(), ref, runnerResultCommand, 1); err != nil {
		t.Fatal(err)
	}
	<-firstStarted
	if err := runner.Submit(context.Background(), ref, runnerResultCommand, 2); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-receiver.results:
		if result.result.Value != 2 {
			t.Fatalf("first completion = %d, want 2", result.result.Value)
		}
	case <-time.After(time.Second):
		t.Fatal("second worker did not complete independently")
	}
	close(releaseFirst)
}

func TestRunnerAwaitCancellationReacquiresServicePermit(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	processorCause := make(chan error, 1)
	runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "await-cancel", Workers: 1, QueueSize: 1}, func(ctx context.Context, _ int) (int, error) {
		<-ctx.Done()
		processorCause <- context.Cause(ctx)
		return 0, context.Cause(ctx)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })
	service := &runnerCancelAwaitService{runner: runner, result: make(chan error, 1), continued: make(chan struct{})}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-service.result; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Await error = %v, want context deadline", err)
	}
	if err := <-processorCause; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("processor cause = %v, want context deadline", err)
	}
	if err := runtime.Send(ref, 2, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-service.continued:
	case <-time.After(time.Second):
		t.Fatal("Service did not continue after canceled Await")
	}
}

func TestRunnerCloseTimeoutKeepsActualWorkerObservable(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	started := make(chan struct{})
	release := make(chan struct{})
	runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "slow-close", Workers: 1, QueueSize: 1}, func(context.Context, int) (int, error) {
		close(started)
		<-release // Deliberately models an adapter that has not returned after cancellation.
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	receiver := &runnerResultService{results: make(chan receivedRunnerResult, 1)}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: receiver})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(context.Background(), ref, runnerResultCommand, 1); err != nil {
		t.Fatal(err)
	}
	<-started
	closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := runner.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close error = %v, want context deadline", err)
	}
	inspection := runtime.Inspect().Runners
	if len(inspection) != 1 || inspection[0].Status != gsr.RunnerClosing || inspection[0].Active != 1 {
		t.Fatalf("Runner inspection during timed-out close = %+v", inspection)
	}
	if err := runner.Submit(context.Background(), ref, runnerResultCommand, 2); !errors.Is(err, gsr.ErrRunnerClosed) {
		t.Fatalf("Submit after Close error = %v, want ErrRunnerClosed", err)
	}
	close(release)
	if err := runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	inspection = runtime.Inspect().Runners
	if len(inspection) != 1 || inspection[0].Status != gsr.RunnerClosed || inspection[0].Active != 0 {
		t.Fatalf("Runner inspection after close = %+v", inspection)
	}
}

func TestRuntimeCloseCancelsRunnerAndWakesAwait(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 1, ShutdownTimeout: time.Second})
	processorStarted := make(chan struct{})
	processorCause := make(chan error, 1)
	runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "runtime-close", Workers: 1, QueueSize: 1}, func(ctx context.Context, _ int) (int, error) {
		close(processorStarted)
		<-ctx.Done()
		processorCause <- context.Cause(ctx)
		return 0, context.Cause(ctx)
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &runnerCloseAwaitService{runner: runner, result: make(chan error, 1)}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-processorStarted
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-processorCause; !errors.Is(err, gsr.ErrRunnerClosed) {
		t.Fatalf("processor cause = %v, want ErrRunnerClosed", err)
	}
	if err := <-service.result; !errors.Is(err, gsr.ErrRunnerClosed) {
		t.Fatalf("Await result = %v, want ErrRunnerClosed", err)
	}
	inspection := runtime.Inspect()
	if len(inspection.Runners) != 1 || inspection.Runners[0].Status != gsr.RunnerClosed {
		t.Fatalf("Runner after Runtime.Close = %+v", inspection.Runners)
	}
}

func TestRunnerRecordsSingleDeliveryFailure(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	started := make(chan struct{})
	release := make(chan struct{})
	runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "delivery", Workers: 1, QueueSize: 1}, func(context.Context, int) (int, error) {
		close(started)
		<-release
		return 1, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })
	receiver := &runnerResultService{results: make(chan receivedRunnerResult, 1)}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: receiver})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(context.Background(), ref, runnerResultCommand, 1); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := runtime.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	close(release)
	eventually(t, func() bool {
		inspection := runtime.Inspect()
		return len(inspection.Runners) == 1 && inspection.Runners[0].DeliveryFailed == 1
	})
	if got := runtime.Inspect().Metrics.Counter("runner_result_delivery_failed_total"); got != 1 {
		t.Fatalf("delivery failure metric = %d, want 1", got)
	}
}

func TestRunnerInspectionIsSortedAndIndependent(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	processor := func(context.Context, int) (int, error) { return 0, nil }
	for _, name := range []gsr.RunnerName{"zeta", "alpha"} {
		if _, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: name, Workers: 1, QueueSize: 1}, processor); err != nil {
			t.Fatal(err)
		}
	}
	first := runtime.Inspect()
	if len(first.Runners) != 2 || first.Runners[0].Name != "alpha" || first.Runners[1].Name != "zeta" {
		t.Fatalf("Runners not sorted: %+v", first.Runners)
	}
	first.Runners[0].Name = "mutated"
	second := runtime.Inspect()
	if second.Runners[0].Name != "alpha" {
		t.Fatalf("inspection mutation leaked: %+v", second.Runners)
	}
}

func TestRunnerSubmitCloseRaceHasOneAdmissionOutcome(t *testing.T) {
	for iteration := 0; iteration < 50; iteration++ {
		runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
		runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "race", Workers: 1, QueueSize: 64}, func(context.Context, int) (int, error) {
			return 0, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		receiver := &runnerResultService{results: make(chan receivedRunnerResult, 64)}
		ref, err := runtime.CreateService(gsr.ServiceSpec{Service: receiver})
		if err != nil {
			t.Fatal(err)
		}
		var wait sync.WaitGroup
		wait.Add(2)
		start := make(chan struct{})
		go func() {
			defer wait.Done()
			<-start
			for request := 0; request < 64; request++ {
				err := runner.Submit(context.Background(), ref, runnerResultCommand, request)
				if err != nil && !errors.Is(err, gsr.ErrRunnerClosed) && !errors.Is(err, gsr.ErrRunnerQueueFull) {
					t.Errorf("Submit error = %v", err)
				}
			}
		}()
		go func() {
			defer wait.Done()
			<-start
			if err := runner.Close(context.Background()); err != nil {
				t.Errorf("Close error = %v", err)
			}
		}()
		close(start)
		wait.Wait()
		if err := runtime.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		inspection := runtime.Inspect().Runners[0]
		if inspection.Completed != inspection.Submitted {
			t.Fatalf("iteration %d: submitted=%d completed=%d", iteration, inspection.Submitted, inspection.Completed)
		}
	}
}

type receivedRunnerResult struct {
	source gsr.ServiceRef
	result gsr.RunnerResult[int]
}

type runnerResultService struct {
	results chan receivedRunnerResult
}

func (*runnerResultService) Init(gsr.ServiceContext) error { return nil }
func (s *runnerResultService) Handle(context gsr.CommandContext, command gsr.Command) error {
	result, ok := command.Payload.(gsr.RunnerResult[int])
	if !ok {
		return errors.New("unexpected runner result payload")
	}
	s.results <- receivedRunnerResult{source: context.Source(), result: result}
	return nil
}
func (*runnerResultService) Stop(context.Context) error { return nil }
func (*runnerResultService) Close() error               { return nil }

type runnerAwaitService struct {
	runner        *gsr.Runner[string, string]
	awaited       chan string
	secondHandled chan struct{}
	secondOnce    atomic.Bool
}

func (*runnerAwaitService) Init(gsr.ServiceContext) error { return nil }
func (s *runnerAwaitService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case 1:
		value, err := s.runner.Await(context.Background(), commandContext, command.Payload.(string))
		if err != nil {
			return err
		}
		s.awaited <- value
	case 2:
		if s.secondOnce.CompareAndSwap(false, true) {
			close(s.secondHandled)
		}
	}
	return nil
}
func (*runnerAwaitService) Stop(context.Context) error { return nil }
func (*runnerAwaitService) Close() error               { return nil }

type runnerSignalService struct {
	handled chan struct{}
}

func (*runnerSignalService) Init(gsr.ServiceContext) error { return nil }
func (s *runnerSignalService) Handle(gsr.CommandContext, gsr.Command) error {
	s.handled <- struct{}{}
	return nil
}
func (*runnerSignalService) Stop(context.Context) error { return nil }
func (*runnerSignalService) Close() error               { return nil }

type runnerCommandContextCaptureService struct {
	context gsr.CommandContext
	handled chan struct{}
}

func (*runnerCommandContextCaptureService) Init(gsr.ServiceContext) error { return nil }
func (s *runnerCommandContextCaptureService) Handle(commandContext gsr.CommandContext, _ gsr.Command) error {
	s.context = commandContext
	close(s.handled)
	return nil
}
func (*runnerCommandContextCaptureService) Stop(context.Context) error { return nil }
func (*runnerCommandContextCaptureService) Close() error               { return nil }

type runnerWrongRuntimeAwaitService struct {
	runner *gsr.Runner[int, int]
	result chan error
}

func (*runnerWrongRuntimeAwaitService) Init(gsr.ServiceContext) error { return nil }
func (s *runnerWrongRuntimeAwaitService) Handle(commandContext gsr.CommandContext, _ gsr.Command) error {
	_, err := s.runner.Await(context.Background(), commandContext, 1)
	s.result <- err
	return nil
}
func (*runnerWrongRuntimeAwaitService) Stop(context.Context) error { return nil }
func (*runnerWrongRuntimeAwaitService) Close() error               { return nil }

type runnerNestedAwaitRequest struct {
	owner gsr.CommandContext
}

type runnerNestedAwaitService struct {
	runner *gsr.Runner[runnerNestedAwaitRequest, int]
	result chan error
}

func (*runnerNestedAwaitService) Init(gsr.ServiceContext) error { return nil }
func (s *runnerNestedAwaitService) Handle(commandContext gsr.CommandContext, _ gsr.Command) error {
	_, err := s.runner.Await(context.Background(), commandContext, runnerNestedAwaitRequest{owner: commandContext})
	s.result <- err
	return nil
}
func (*runnerNestedAwaitService) Stop(context.Context) error { return nil }
func (*runnerNestedAwaitService) Close() error               { return nil }

type runnerCloseAwaitService struct {
	runner *gsr.Runner[int, int]
	result chan error
}

func (*runnerCloseAwaitService) Init(gsr.ServiceContext) error { return nil }
func (s *runnerCloseAwaitService) Handle(commandContext gsr.CommandContext, _ gsr.Command) error {
	_, err := s.runner.Await(context.Background(), commandContext, 1)
	s.result <- err
	return nil
}
func (*runnerCloseAwaitService) Stop(context.Context) error { return nil }
func (*runnerCloseAwaitService) Close() error               { return nil }

type runnerCancelAwaitService struct {
	runner    *gsr.Runner[int, int]
	result    chan error
	continued chan struct{}
}

func (*runnerCancelAwaitService) Init(gsr.ServiceContext) error { return nil }
func (s *runnerCancelAwaitService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	if command.ID == 2 {
		close(s.continued)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := s.runner.Await(ctx, commandContext, 1)
	s.result <- err
	return nil
}
func (*runnerCancelAwaitService) Stop(context.Context) error { return nil }
func (*runnerCancelAwaitService) Close() error               { return nil }
