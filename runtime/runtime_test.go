package gsr_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestSendDeliversCommandThroughRuntime(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1, MailboxSize: 8})
	defer rt.Close(context.Background())
	receiver := &recordingService{}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: receiver})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1001, "hello"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return receiver.last() == "hello" })
}

func TestSendToUnknownServiceFails(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1, MailboxSize: 8})
	defer rt.Close(context.Background())
	err := rt.Send(gsr.ServiceRef{Node: "local", ID: 99}, 1001, nil)
	if !errors.Is(err, gsr.ErrServiceNotFound) {
		t.Fatalf("err = %v, want ErrServiceNotFound", err)
	}
}

func TestServiceHandlerIsSerial(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 4, MailboxSize: 128})
	defer rt.Close(context.Background())
	svc := &serialService{}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 64; i++ {
		if err := rt.Send(ref, 1, i); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, func() bool { return svc.count() == 64 })
	if got := svc.maxConcurrent(); got != 1 {
		t.Fatalf("concurrency = %d, want 1", got)
	}
}

type recordingService struct {
	mu      sync.Mutex
	payload any
}

func (s *recordingService) Init(gsr.ServiceContext) error { return nil }
func (s *recordingService) Handle(_ gsr.CommandContext, cmd gsr.Command) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payload = cmd.Payload
	return nil
}
func (s *recordingService) Stop(context.Context) error { return nil }
func (s *recordingService) Close() error               { return nil }
func (s *recordingService) last() any                  { s.mu.Lock(); defer s.mu.Unlock(); return s.payload }

type serialService struct {
	mu                    sync.Mutex
	current, max, handled int
}

func (s *serialService) Init(gsr.ServiceContext) error { return nil }
func (s *serialService) Handle(_ gsr.CommandContext, _ gsr.Command) error {
	s.mu.Lock()
	s.current++
	if s.current > s.max {
		s.max = s.current
	}
	s.mu.Unlock()
	time.Sleep(time.Millisecond)
	s.mu.Lock()
	s.current--
	s.handled++
	s.mu.Unlock()
	return nil
}
func (s *serialService) Stop(context.Context) error { return nil }
func (s *serialService) Close() error               { return nil }
func (s *serialService) count() int                 { s.mu.Lock(); defer s.mu.Unlock(); return s.handled }
func (s *serialService) maxConcurrent() int         { s.mu.Lock(); defer s.mu.Unlock(); return s.max }

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
