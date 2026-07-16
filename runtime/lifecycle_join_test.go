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

func TestConcurrentRuntimeCloseJoinsSameResult(t *testing.T) {
	stopErr := errors.New("stop failed")
	svc := newJoiningLifecycleService(stopErr)
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", ShutdownTimeout: time.Second})
	if _, err := rt.CreateService(gsr.ServiceSpec{Service: svc}); err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() { first <- rt.Close(context.Background()) }()
	<-svc.stopStarted
	released := false
	defer func() {
		if !released {
			close(svc.releaseStop)
		}
	}()

	second := make(chan error, 1)
	go func() { second <- rt.Close(context.Background()) }()
	select {
	case err := <-second:
		t.Fatalf("concurrent Close returned before the active close completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(svc.releaseStop)
	released = true

	firstErr := <-first
	secondErr := <-second
	if !errors.Is(firstErr, stopErr) || !errors.Is(secondErr, stopErr) {
		t.Fatalf("Close errors = (%v, %v), want both to contain stop error", firstErr, secondErr)
	}
	if got := svc.stopCalls.Load(); got != 1 {
		t.Fatalf("Stop calls = %d, want 1", got)
	}
	if got := svc.closeCalls.Load(); got != 1 {
		t.Fatalf("Close calls = %d, want 1", got)
	}
}

func TestRepeatedRuntimeCloseReturnsSavedTimeout(t *testing.T) {
	svc := newCloseTimeoutHandlerService()
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1, ShutdownTimeout: 20 * time.Millisecond})
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-svc.started
	defer close(svc.release)

	firstErr := rt.Close(context.Background())
	if !errors.Is(firstErr, gsr.ErrCloseTimeout) {
		t.Fatalf("first Close err = %v, want ErrCloseTimeout", firstErr)
	}
	secondErr := rt.Close(context.Background())
	if !errors.Is(secondErr, gsr.ErrCloseTimeout) {
		t.Fatalf("second Close err = %v, want saved ErrCloseTimeout", secondErr)
	}
}

func TestRuntimeCloseJoinerCanCancelWait(t *testing.T) {
	svc := newJoiningLifecycleService(nil)
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", ShutdownTimeout: time.Second})
	if _, err := rt.CreateService(gsr.ServiceSpec{Service: svc}); err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() { first <- rt.Close(context.Background()) }()
	<-svc.stopStarted
	defer func() {
		select {
		case <-svc.releaseStop:
		default:
			close(svc.releaseStop)
		}
	}()

	waitCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rt.Close(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("joining Close err = %v, want context.Canceled", err)
	}
	if got := svc.stopCalls.Load(); got != 1 {
		t.Fatalf("Stop calls before release = %d, want 1", got)
	}
	close(svc.releaseStop)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("repeated Close err = %v", err)
	}
}

func TestConcurrentServiceStopJoinsExistingResult(t *testing.T) {
	stopErr := errors.New("stop failed")
	svc := newJoiningLifecycleService(stopErr)
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() { first <- rt.Stop(context.Background(), ref) }()
	<-svc.stopStarted
	released := false
	defer func() {
		if !released {
			close(svc.releaseStop)
		}
	}()

	second := make(chan error, 1)
	go func() { second <- rt.Stop(context.Background(), ref) }()
	select {
	case err := <-second:
		t.Fatalf("concurrent Stop returned before the active stop completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(svc.releaseStop)
	released = true

	firstErr := <-first
	secondErr := <-second
	if !errors.Is(firstErr, stopErr) || !errors.Is(secondErr, stopErr) {
		t.Fatalf("Stop errors = (%v, %v), want both to contain stop error", firstErr, secondErr)
	}
	if got := svc.stopCalls.Load(); got != 1 {
		t.Fatalf("Stop calls = %d, want 1", got)
	}
	if got := svc.closeCalls.Load(); got != 1 {
		t.Fatalf("Close calls = %d, want 1", got)
	}
	if err := rt.Stop(context.Background(), ref); !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("Stop after removal err = %v, want ErrServiceClosed", err)
	}
}

type joiningLifecycleService struct {
	stopErr     error
	stopStarted chan struct{}
	releaseStop chan struct{}
	startOnce   sync.Once
	stopCalls   atomic.Int32
	closeCalls  atomic.Int32
}

func newJoiningLifecycleService(stopErr error) *joiningLifecycleService {
	return &joiningLifecycleService{
		stopErr:     stopErr,
		stopStarted: make(chan struct{}),
		releaseStop: make(chan struct{}),
	}
}

func (*joiningLifecycleService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (*joiningLifecycleService) Init(gsr.ServiceContext) error { return nil }
func (*joiningLifecycleService) Handle(gsr.CommandContext, gsr.Command) error {
	return nil
}
func (s *joiningLifecycleService) Stop(context.Context) error {
	s.stopCalls.Add(1)
	s.startOnce.Do(func() { close(s.stopStarted) })
	<-s.releaseStop
	return s.stopErr
}
func (s *joiningLifecycleService) Close() error {
	s.closeCalls.Add(1)
	return nil
}

type closeTimeoutHandlerService struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newCloseTimeoutHandlerService() *closeTimeoutHandlerService {
	return &closeTimeoutHandlerService{started: make(chan struct{}), release: make(chan struct{})}
}

func (*closeTimeoutHandlerService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (*closeTimeoutHandlerService) Init(gsr.ServiceContext) error { return nil }
func (s *closeTimeoutHandlerService) Handle(gsr.CommandContext, gsr.Command) error {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return nil
}
func (*closeTimeoutHandlerService) Stop(context.Context) error { return nil }
func (*closeTimeoutHandlerService) Close() error               { return nil }
