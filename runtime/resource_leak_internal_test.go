package gsr

import (
	"context"
	"errors"
	gort "runtime"
	"testing"
	"time"
)

func TestRuntimeRepeatedCreateCloseReleasesOwnedResources(t *testing.T) {
	baselineGoroutines := gort.NumGoroutine()
	for i := 0; i < 20; i++ {
		rt := NewRuntime(Config{NodeID: "local", Workers: 2, ShutdownTimeout: time.Second})
		defer rt.Close(context.Background())
		ref, err := rt.CreateService(ServiceSpec{Service: resourceProbeService{}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rt.Call(context.Background(), ref, resourceCommandReply, nil); err != nil {
			t.Fatal(err)
		}

		pendingResult := make(chan error, 1)
		go func() {
			_, err := rt.Call(context.Background(), ref, resourceCommandNoReply, nil)
			pendingResult <- err
		}()
		waitForInternalCount(t, func() int { return pendingCallCount(rt.pending) }, 1)
		if _, err := rt.After(ref, time.Hour, resourceCommandReply, nil); err != nil {
			t.Fatal(err)
		}
		if got := timerCount(rt.timers); got != 1 {
			t.Fatalf("iteration %d: timers = %d, want 1", i, got)
		}
		if err := rt.Close(context.Background()); err != nil {
			t.Fatalf("iteration %d: Runtime.Close err = %v", i, err)
		}
		if err := <-pendingResult; !errors.Is(err, ErrRuntimeClosed) {
			t.Fatalf("iteration %d: pending Call err = %v, want ErrRuntimeClosed", i, err)
		}
		assertRuntimeResourcesReleased(t, i, rt)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gort.GC()
		if got := gort.NumGoroutine(); got <= baselineGoroutines+4 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines = %d, baseline = %d", gort.NumGoroutine(), baselineGoroutines)
}

const (
	resourceCommandReply CommandID = iota + 1
	resourceCommandNoReply
)

type resourceProbeService struct{}

func (resourceProbeService) Commands() []CommandID {
	return []CommandID{resourceCommandReply, resourceCommandNoReply}
}
func (resourceProbeService) Init(ServiceContext) error { return nil }
func (resourceProbeService) Handle(ctx CommandContext, command Command) error {
	if command.ID == resourceCommandReply {
		return ctx.Reply("pong")
	}
	return nil
}
func (resourceProbeService) Stop(context.Context) error { return nil }
func (resourceProbeService) Close() error               { return nil }

func pendingCallCount(pending *pendingCalls) int {
	pending.mu.Lock()
	defer pending.mu.Unlock()
	return len(pending.calls)
}

func timerCount(timers *timerManager) int {
	timers.mu.Lock()
	defer timers.mu.Unlock()
	return len(timers.timers)
}

func taskCount(tasks *taskTracker) int {
	tasks.mu.Lock()
	defer tasks.mu.Unlock()
	return len(tasks.tasks)
}

func waitForInternalCount(t *testing.T, count func() int, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if count() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("internal count = %d, want %d", count(), want)
}

func assertRuntimeResourcesReleased(t *testing.T, iteration int, rt *Runtime) {
	t.Helper()
	if got := len(rt.registry.snapshot()); got != 0 {
		t.Fatalf("iteration %d: registry entries = %d", iteration, got)
	}
	if got := pendingCallCount(rt.pending); got != 0 {
		t.Fatalf("iteration %d: pending calls = %d", iteration, got)
	}
	if got := timerCount(rt.timers); got != 0 {
		t.Fatalf("iteration %d: timers = %d", iteration, got)
	}
	if got := taskCount(rt.tasks); got != 0 {
		t.Fatalf("iteration %d: runtime tasks = %d", iteration, got)
	}
	if got := rt.MetricsSnapshot().Gauge("runtime_tasks_active"); got != 0 {
		t.Fatalf("iteration %d: active task gauge = %d", iteration, got)
	}
	select {
	case <-rt.closeDone:
	default:
		t.Fatalf("iteration %d: close result was not published", iteration)
	}
}
