package gsr

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRuntimeCloseMarksUnfinishedServiceFailed(t *testing.T) {
	rt := NewRuntime(Config{NodeID: "local", Workers: 1, ShutdownTimeout: 20 * time.Millisecond})
	svc := newBlockingHandlerService()
	ref, err := rt.CreateService(ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := rt.registry.get(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-svc.started
	err = rt.Close(context.Background())
	if !errors.Is(err, ErrCloseTimeout) {
		t.Fatalf("Close err = %v", err)
	}
	if got := ServiceStatus(instance.status.Load()); got != ServiceFailed {
		t.Fatalf("status = %v, want ServiceFailed", got)
	}
	if !errors.Is(instance.result, ErrCloseTimeout) {
		t.Fatalf("result = %v, want ErrCloseTimeout", instance.result)
	}
	close(svc.release)
	deadline := time.Now().Add(time.Second)
	for rt.metrics.snapshot().Gauge("runtime_tasks_active") != 0 {
		if time.Now().After(deadline) {
			t.Fatal("handler task remained tracked after returning")
		}
		time.Sleep(time.Millisecond)
	}
}

type blockingHandlerService struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingHandlerService() *blockingHandlerService {
	return &blockingHandlerService{started: make(chan struct{}), release: make(chan struct{})}
}

func (*blockingHandlerService) Commands() []CommandID     { return []CommandID{1} }
func (*blockingHandlerService) Init(ServiceContext) error { return nil }
func (s *blockingHandlerService) Handle(CommandContext, Command) error {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return nil
}
func (*blockingHandlerService) Stop(context.Context) error { return nil }
func (*blockingHandlerService) Close() error               { return nil }
