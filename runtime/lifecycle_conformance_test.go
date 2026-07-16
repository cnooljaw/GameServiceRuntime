package gsr_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestStopWaitsForCurrentHandler(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 2})
	defer rt.Close(context.Background())
	svc := newSerialStopService()
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-svc.started
	stopped := make(chan error, 1)
	go func() { stopped <- rt.Stop(context.Background(), ref) }()
	select {
	case <-svc.stopCalled:
		t.Fatal("Stop ran concurrently with Handle")
	case <-time.After(20 * time.Millisecond):
	}
	close(svc.release)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if svc.concurrent.Load() {
		t.Fatal("Stop observed Handle as active")
	}
}

func TestHandlerPanicIsIsolated(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	defer rt.Close(context.Background())
	bad, _ := rt.CreateService(gsr.ServiceSpec{Service: panicService{}})
	if err := rt.Send(bad, 1, nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return rt.MetricsSnapshot().Counter("service_panics_total") == 1 })
	good := &recordingService{}
	goodRef, err := rt.CreateService(gsr.ServiceSpec{Service: good})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(goodRef, 1001, "alive"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return good.last() == "alive" })
}

func TestStopTimeoutStillRemovesService(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	svc := &releasableStopService{started: make(chan struct{}), release: make(chan struct{})}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc, Policy: gsr.ServicePolicy{StopTimeout: 20 * time.Millisecond, CloseTimeout: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = rt.Stop(context.Background(), ref)
	if !errors.Is(err, gsr.ErrStopTimeout) {
		t.Fatalf("err = %v, want ErrStopTimeout", err)
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatal("Stop remained blocked after timeout")
	}
	if err := rt.Send(ref, 1, nil); !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("send err = %v", err)
	}
	close(svc.release)
	eventually(t, func() bool {
		return rt.MetricsSnapshot().Gauge("runtime_tasks_active") == 0
	})
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTimedOutStopTaskRemainsTrackedUntilItReturns(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	svc := &releasableStopService{started: make(chan struct{}), release: make(chan struct{})}
	ref, err := rt.CreateService(gsr.ServiceSpec{
		Service: svc,
		Policy: gsr.ServicePolicy{
			StopTimeout:      20 * time.Millisecond,
			CloseTimeout:     20 * time.Millisecond,
			LifecycleTimeout: time.Second,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Stop(context.Background(), ref); !errors.Is(err, gsr.ErrStopTimeout) {
		t.Fatalf("Stop err = %v", err)
	}
	<-svc.started
	eventually(t, func() bool {
		return rt.MetricsSnapshot().Gauge("runtime_tasks_active") == 1
	})
	if got := rt.MetricsSnapshot().Counter("runtime_task_timeouts_total"); got != 1 {
		t.Fatalf("task timeouts = %d", got)
	}
	close(svc.release)
	eventually(t, func() bool {
		return rt.MetricsSnapshot().Gauge("runtime_tasks_active") == 0
	})
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleTimeoutIncludesMailboxWait(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	svc := newSerialStopService()
	ref, err := rt.CreateService(gsr.ServiceSpec{
		Service: svc,
		Policy: gsr.ServicePolicy{
			StopTimeout:      time.Second,
			CloseTimeout:     time.Second,
			LifecycleTimeout: 20 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-svc.started
	started := time.Now()
	if err := rt.Stop(context.Background(), ref); !errors.Is(err, gsr.ErrStopTimeout) {
		t.Fatalf("Stop err = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("Stop exceeded lifecycle timeout: %v", elapsed)
	}
	close(svc.release)
	eventually(t, func() bool {
		return rt.MetricsSnapshot().Gauge("runtime_tasks_active") == 0
	})
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCloseStopsServicesAndWakesPendingCall(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	svc := &pendingService{handled: make(chan struct{}), stopped: make(chan struct{}), closed: make(chan struct{})}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := rt.Call(context.Background(), ref, 1, nil); result <- err }()
	<-svc.handled
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, gsr.ErrRuntimeClosed) {
		t.Fatalf("call err = %v", err)
	}
	select {
	case <-svc.stopped:
	default:
		t.Fatal("Service.Stop was not called")
	}
	select {
	case <-svc.closed:
	default:
		t.Fatal("Service.Close was not called")
	}
	if err := rt.Send(ref, 1, nil); !errors.Is(err, gsr.ErrRuntimeClosed) {
		t.Fatalf("send err = %v", err)
	}
}

func TestRuntimeClosePreservesCallerCancellation(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", ShutdownTimeout: time.Second})
	cause := errors.New("shutdown canceled")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	err := rt.Close(ctx)
	if !errors.Is(err, cause) {
		t.Fatalf("Close err = %v, want caller cause", err)
	}
	if errors.Is(err, gsr.ErrCloseTimeout) {
		t.Fatalf("Close err = %v, must not report an internal timeout", err)
	}
}

func TestLifecycleErrorsAreObserved(t *testing.T) {
	stopErr := errors.New("stop failed")
	closeErr := errors.New("close failed")
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: lifecycleErrorService{stopErr: stopErr, closeErr: closeErr}})
	if err != nil {
		t.Fatal(err)
	}
	err = rt.Stop(context.Background(), ref)
	if !errors.Is(err, stopErr) || !errors.Is(err, closeErr) {
		t.Fatalf("Stop err = %v", err)
	}
	metrics := rt.MetricsSnapshot()
	if got := metrics.Counter("service_stop_errors_total"); got != 1 {
		t.Fatalf("stop errors = %d", got)
	}
	if got := metrics.Counter("service_close_errors_total"); got != 1 {
		t.Fatalf("close errors = %d", got)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStopCancelsTargetTimer(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	svc := &recordingService{}
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if _, err := rt.After(ref, 30*time.Millisecond, 20, "expired"); err != nil {
		t.Fatal(err)
	}
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := svc.last(); got != nil {
		t.Fatalf("timer delivered %v", got)
	}
}

func TestStopWakesPendingCall(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	svc := &pendingService{handled: make(chan struct{}), stopped: make(chan struct{}), closed: make(chan struct{})}
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
	result := make(chan error, 1)
	go func() { _, err := rt.Call(context.Background(), ref, 1, nil); result <- err }()
	<-svc.handled
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("call err = %v", err)
	}
}

func TestCloseCannotStartBusinessCall(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	defer rt.Close(context.Background())
	target, _ := rt.CreateService(gsr.ServiceSpec{Service: &fixedReplyService{reply: "pong"}})
	svc := &closeCallingService{target: target, result: make(chan error, 1)}
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := <-svc.result; !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("Close call err = %v", err)
	}
}

func TestSendAndStopHaveOneAcceptanceBoundary(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 4})
	defer rt.Close(context.Background())
	for i := 0; i < 50; i++ {
		svc := newAcceptanceService()
		ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.Send(ref, 1, nil); err != nil {
			t.Fatal(err)
		}
		<-svc.started
		gate := make(chan struct{})
		sendResult := make(chan error, 1)
		stopResult := make(chan error, 1)
		go func() { <-gate; sendResult <- rt.Send(ref, 1, nil) }()
		go func() { <-gate; stopResult <- rt.Stop(context.Background(), ref) }()
		close(gate)
		sendErr := <-sendResult
		close(svc.release)
		if err := <-stopResult; err != nil {
			t.Fatal(err)
		}
		switch {
		case sendErr == nil && svc.handled.Load() != 2:
			t.Fatalf("accepted command was not drained: handled=%d", svc.handled.Load())
		case errors.Is(sendErr, gsr.ErrServiceClosed) && svc.handled.Load() != 1:
			t.Fatalf("rejected command was handled: handled=%d", svc.handled.Load())
		case sendErr != nil && !errors.Is(sendErr, gsr.ErrServiceClosed):
			t.Fatalf("send err = %v", sendErr)
		}
	}
}

func TestLifecyclePanicsAreIsolated(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	if _, err := rt.CreateService(gsr.ServiceSpec{Service: lifecyclePanicService{phase: "init"}}); !errors.Is(err, gsr.ErrServiceFailed) {
		t.Fatalf("Init panic err = %v", err)
	}
	for _, phase := range []string{"stop", "close"} {
		ref, err := rt.CreateService(gsr.ServiceSpec{Service: lifecyclePanicService{phase: phase}})
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.Stop(context.Background(), ref); !errors.Is(err, gsr.ErrServiceFailed) {
			t.Fatalf("%s panic err = %v", phase, err)
		}
	}
	good := &recordingService{}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: good})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1001, "alive"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return good.last() == "alive" })
}

func TestRuntimeCloseWaitsForServiceInitialization(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", ShutdownTimeout: time.Second})
	svc := &blockingInitService{started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	created := make(chan error, 1)
	go func() {
		_, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
		created <- err
	}()
	<-svc.started
	eventually(t, func() bool {
		return rt.MetricsSnapshot().Gauge("runtime_tasks_active") == 1
	})
	closed := make(chan error, 1)
	go func() { closed <- rt.Close(context.Background()) }()
	select {
	case err := <-closed:
		t.Fatalf("Runtime.Close returned during Init: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(svc.release)
	if err := <-created; !errors.Is(err, gsr.ErrRuntimeClosed) {
		t.Fatalf("CreateService err = %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	select {
	case <-svc.closed:
	default:
		t.Fatal("partially initialized Service was not closed")
	}
	if got := rt.MetricsSnapshot().Gauge("runtime_tasks_active"); got != 0 {
		t.Fatalf("active runtime tasks = %d", got)
	}
}

func TestRuntimeCloseJoinsExistingServiceStop(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", ShutdownTimeout: time.Second})
	svc := &controlledStopService{stopStarted: make(chan struct{}), releaseStop: make(chan struct{}), closed: make(chan struct{})}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- rt.Stop(context.Background(), ref) }()
	<-svc.stopStarted
	runtimeClosed := make(chan error, 1)
	go func() { runtimeClosed <- rt.Close(context.Background()) }()
	select {
	case err := <-runtimeClosed:
		t.Fatalf("Runtime.Close returned during Service.Stop: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(svc.releaseStop)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if err := <-runtimeClosed; err != nil {
		t.Fatal(err)
	}
	select {
	case <-svc.closed:
	default:
		t.Fatal("Service.Close was skipped")
	}
}
