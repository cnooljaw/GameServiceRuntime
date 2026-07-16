package gsr

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTimerDeliveryRecordsSuccess(t *testing.T) {
	rt := NewRuntime(Config{NodeID: "local"})
	defer rt.Close(context.Background())
	svc := &timerMetricService{handled: make(chan CommandID, 1)}
	ref, err := rt.CreateService(ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.After(ref, time.Millisecond, 3, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case command := <-svc.handled:
		if command != 3 {
			t.Fatalf("handled command = %d, want 3", command)
		}
	case <-time.After(time.Second):
		t.Fatal("timer command was not handled")
	}
	waitForMetric(t, rt, "timer_deliveries_total", 1)
	snapshot := rt.MetricsSnapshot()
	if got := snapshot.Counter("timers_fired_total"); got != 1 {
		t.Fatalf("timers fired = %d, want 1", got)
	}
	if got := snapshot.Counter("timer_deliveries_total"); got != 1 {
		t.Fatalf("timer deliveries = %d, want 1", got)
	}
	if got := snapshot.Counter("timer_delivery_errors_total"); got != 0 {
		t.Fatalf("timer delivery errors = %d, want 0", got)
	}
}

func TestTimerDeliveryRecordsMailboxFull(t *testing.T) {
	rt := NewRuntime(Config{NodeID: "local", Workers: 1, MailboxSize: 1})
	svc := &blockingTimerMetricService{started: make(chan struct{}), release: make(chan struct{})}
	ref, err := rt.CreateService(ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-svc.started
	released := false
	defer func() {
		if !released {
			close(svc.release)
		}
		rt.Close(context.Background())
	}()
	if _, err := rt.After(ref, 5*time.Millisecond, 3, nil); err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 2, nil); err != nil {
		t.Fatal(err)
	}
	waitForMetric(t, rt, "timer_delivery_errors_total", 1)
	snapshot := rt.MetricsSnapshot()
	if got := snapshot.Counter("timers_fired_total"); got != 1 {
		t.Fatalf("timers fired = %d, want 1", got)
	}
	if got := snapshot.Counter("timer_deliveries_total"); got != 0 {
		t.Fatalf("timer deliveries = %d, want 0", got)
	}
	if got := snapshot.Counter("timer_delivery_mailbox_full_total"); got != 1 {
		t.Fatalf("mailbox-full timer deliveries = %d, want 1", got)
	}
	close(svc.release)
	released = true
}

func TestTimerDeliveryClassifiesErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		metric string
	}{
		{name: "mailbox full", err: ErrMailboxFull, metric: "timer_delivery_mailbox_full_total"},
		{name: "service closed", err: ErrServiceClosed, metric: "timer_delivery_target_closed_total"},
		{name: "service missing", err: ErrServiceNotFound, metric: "timer_delivery_target_closed_total"},
		{name: "runtime closed", err: ErrRuntimeClosed, metric: "timer_delivery_runtime_closed_total"},
		{name: "other", err: errors.New("unexpected"), metric: "timer_delivery_other_errors_total"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := NewRuntime(Config{NodeID: "local"})
			defer rt.Close(context.Background())
			rt.observeTimerDelivery(ServiceRef{Node: "local", ID: 1}, 3, tt.err)
			snapshot := rt.MetricsSnapshot()
			if got := snapshot.Counter("timer_delivery_errors_total"); got != 1 {
				t.Fatalf("timer delivery errors = %d, want 1", got)
			}
			if got := snapshot.Counter(tt.metric); got != 1 {
				t.Fatalf("%s = %d, want 1", tt.metric, got)
			}
		})
	}
}

func waitForMetric(t *testing.T, rt *Runtime, name string, want uint64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if rt.MetricsSnapshot().Counter(name) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("metric %s did not reach %d", name, want)
}

type timerMetricService struct{ handled chan CommandID }

func (*timerMetricService) Commands() []CommandID     { return []CommandID{1, 2, 3} }
func (*timerMetricService) Init(ServiceContext) error { return nil }
func (s *timerMetricService) Handle(_ CommandContext, command Command) error {
	s.handled <- command.ID
	return nil
}
func (*timerMetricService) Stop(context.Context) error { return nil }
func (*timerMetricService) Close() error               { return nil }

type blockingTimerMetricService struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*blockingTimerMetricService) Commands() []CommandID     { return []CommandID{1, 2, 3} }
func (*blockingTimerMetricService) Init(ServiceContext) error { return nil }
func (s *blockingTimerMetricService) Handle(_ CommandContext, command Command) error {
	if command.ID == 1 {
		s.once.Do(func() { close(s.started) })
		<-s.release
	}
	return nil
}
func (*blockingTimerMetricService) Stop(context.Context) error { return nil }
func (*blockingTimerMetricService) Close() error               { return nil }
