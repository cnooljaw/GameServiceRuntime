package gsr_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestRuntimeCloseTimesOutWithBlockedInitAndTracksUntilReturn(t *testing.T) {
	svc := &trackedBlockingInitService{started: make(chan struct{}), release: make(chan struct{})}
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", ShutdownTimeout: 20 * time.Millisecond})
	created := make(chan error, 1)
	go func() {
		_, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
		created <- err
	}()
	<-svc.started
	released := false
	defer func() {
		if !released {
			close(svc.release)
		}
	}()

	started := time.Now()
	closeErr := rt.Close(context.Background())
	if !errors.Is(closeErr, gsr.ErrCloseTimeout) {
		t.Fatalf("Runtime.Close err = %v, want ErrCloseTimeout", closeErr)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("Runtime.Close elapsed = %v, want bounded return", elapsed)
	}
	snapshot := rt.MetricsSnapshot()
	if got := snapshot.Gauge("runtime_tasks_active"); got != 1 {
		t.Fatalf("active runtime tasks = %d, want 1", got)
	}
	if got := snapshot.Counter("runtime_tasks_abandoned_total"); got != 1 {
		t.Fatalf("reported abandoned tasks = %d, want 1", got)
	}

	close(svc.release)
	released = true
	if err := <-created; !errors.Is(err, gsr.ErrRuntimeClosed) {
		t.Fatalf("CreateService err = %v, want ErrRuntimeClosed", err)
	}
	eventually(t, func() bool {
		return rt.MetricsSnapshot().Gauge("runtime_tasks_active") == 0
	})
	if got := svc.closeCalls.Load(); got != 1 {
		t.Fatalf("Service.Close calls = %d, want 1", got)
	}
}

type trackedBlockingInitService struct {
	started    chan struct{}
	release    chan struct{}
	closeCalls atomic.Int32
}

func (*trackedBlockingInitService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (s *trackedBlockingInitService) Init(gsr.ServiceContext) error {
	close(s.started)
	<-s.release
	return nil
}
func (*trackedBlockingInitService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (*trackedBlockingInitService) Stop(context.Context) error                   { return nil }
func (s *trackedBlockingInitService) Close() error {
	s.closeCalls.Add(1)
	return nil
}
