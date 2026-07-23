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
	snapshot := rt.Inspect().Metrics
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
		return rt.Inspect().Metrics.Gauge("runtime_tasks_active") == 0
	})
	if got := svc.closeCalls.Load(); got != 1 {
		t.Fatalf("Service.Close calls = %d, want 1", got)
	}
}

func TestStartupCommandRunsThroughMailboxAfterServiceStarts(t *testing.T) {
	service := &startupCommandService{handled: make(chan startupCommandObservation, 1)}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	select {
	case observation := <-service.handled:
		if observation.self != ref {
			t.Fatalf("Handle self = %#v, want %#v", observation.self, ref)
		}
		if observation.source != ref {
			t.Fatalf("Handle source = %#v, want self %#v", observation.source, ref)
		}
		if observation.afterErr != nil {
			t.Fatalf("ServiceContext.After() error = %v", observation.afterErr)
		}
	case <-time.After(time.Second):
		t.Fatal("startup command was not handled")
	}
}

func TestCreateServiceRejectsUndeclaredStartupCommand(t *testing.T) {
	service := &invalidStartupCommandService{}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	if _, err := runtime.CreateService(gsr.ServiceSpec{Service: service}); !errors.Is(err, gsr.ErrInvalidServiceSpec) {
		t.Fatalf("CreateService() error = %v, want ErrInvalidServiceSpec", err)
	}
	if service.initCalls.Load() != 0 {
		t.Fatalf("Init calls = %d, want 0", service.initCalls.Load())
	}
}

type trackedBlockingInitService struct {
	started    chan struct{}
	release    chan struct{}
	closeCalls atomic.Int32
}

const startupCommand gsr.CommandID = 701

type startupCommandObservation struct {
	self     gsr.ServiceRef
	source   gsr.ServiceRef
	afterErr error
}

type startupCommandService struct {
	context gsr.ServiceContext
	handled chan startupCommandObservation
}

func (*startupCommandService) Commands() []gsr.CommandID { return []gsr.CommandID{startupCommand} }
func (s *startupCommandService) StartupCommand() (gsr.Command, bool) {
	return gsr.Command{ID: startupCommand}, true
}
func (s *startupCommandService) Init(context gsr.ServiceContext) error {
	s.context = context
	return nil
}
func (s *startupCommandService) Handle(context gsr.CommandContext, command gsr.Command) error {
	if command.ID != startupCommand {
		return nil
	}
	_, err := s.context.After(time.Hour, startupCommand, nil)
	s.handled <- startupCommandObservation{self: context.Self(), source: context.Source(), afterErr: err}
	return nil
}
func (*startupCommandService) Stop(context.Context) error { return nil }
func (*startupCommandService) Close() error               { return nil }

type invalidStartupCommandService struct {
	initCalls atomic.Int32
}

func (*invalidStartupCommandService) Commands() []gsr.CommandID {
	return []gsr.CommandID{startupCommand}
}
func (*invalidStartupCommandService) StartupCommand() (gsr.Command, bool) {
	return gsr.Command{ID: startupCommand + 1}, true
}
func (s *invalidStartupCommandService) Init(gsr.ServiceContext) error {
	s.initCalls.Add(1)
	return nil
}
func (*invalidStartupCommandService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (*invalidStartupCommandService) Stop(context.Context) error                   { return nil }
func (*invalidStartupCommandService) Close() error                                 { return nil }

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
