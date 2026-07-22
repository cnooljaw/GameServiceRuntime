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

func TestDecoratorReportsHandlerPanicAndRepanics(t *testing.T) {
	metrics := newRecordingMetrics()
	serviceContext := &decoratorTestServiceContext{
		self:       gsr.ServiceRef{Node: "node-a", ID: 7},
		now:        time.Unix(100, 0),
		metrics:    metrics,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		sendTarget: gsr.ServiceRef{Node: "node-a", ID: 1},
	}
	decorated, err := Decorate(panicDecoratorService{}, DecoratorConfig{
		Key:        ServiceKey{Namespace: "player", ID: "42"},
		Generation: 3,
		Supervisor: serviceContext.sendTarget,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := decorated.Init(serviceContext); err != nil {
		t.Fatal(err)
	}

	assertPanicsWith(t, "boom", func() {
		_ = decorated.Handle(nil, gsr.Command{ID: 10})
	})

	if serviceContext.sentID != failureCommand {
		t.Fatalf("command = %v, want %v", serviceContext.sentID, failureCommand)
	}
	notice, ok := serviceContext.sentPayload.(FailureNotice)
	if !ok {
		t.Fatalf("payload = %T, want FailureNotice", serviceContext.sentPayload)
	}
	want := FailureNotice{
		Key:        ServiceKey{Namespace: "player", ID: "42"},
		FailedRef:  serviceContext.self,
		Generation: 3,
		OccurredAt: serviceContext.now,
		Kind:       FailureHandlerPanic,
	}
	if notice != want {
		t.Fatalf("notice = %#v, want %#v", notice, want)
	}
	if got := metrics.value(metricFailureNoticeDeliveryErrors); got != 0 {
		t.Fatalf("delivery errors = %d, want 0", got)
	}
}

func TestDecoratorNoticeUsesFailedServiceAsRuntimeSource(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	received := make(chan receivedFailureNotice, 1)
	supervisorRef, err := runtime.CreateService(gsr.ServiceSpec{Service: &failureNoticeCaptureService{received: received}})
	if err != nil {
		t.Fatal(err)
	}
	decorated, err := Decorate(panicDecoratorService{}, DecoratorConfig{
		Key:        ServiceKey{Namespace: "player", ID: "42"},
		Generation: 1,
		Supervisor: supervisorRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	failedRef, err := runtime.CreateService(gsr.ServiceSpec{Service: decorated})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Call(context.Background(), failedRef, 10, nil); !errors.Is(err, gsr.ErrServiceFailed) {
		t.Fatalf("Call error = %v, want ErrServiceFailed", err)
	}
	select {
	case got := <-received:
		if got.source != failedRef || got.notice.FailedRef != failedRef {
			t.Fatalf("source/notice = %#v/%#v, want %v", got.source, got.notice, failedRef)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for FailureNotice")
	}
}

func TestDecoratorPreservesCommandsAndNormalLifecycle(t *testing.T) {
	inner := &recordingDecoratorService{commands: []gsr.CommandID{10, 11}}
	decorated, err := Decorate(inner, DecoratorConfig{
		Key:        ServiceKey{Namespace: "player", ID: "42"},
		Generation: 1,
		Supervisor: gsr.ServiceRef{Node: "node-a", ID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	declarer, ok := decorated.(gsr.CommandDeclarer)
	if !ok {
		t.Fatal("decorated Service does not declare Commands")
	}
	commands := declarer.Commands()
	commands[0] = 99
	if got := declarer.Commands()[0]; got != 10 {
		t.Fatalf("Commands leaked mutable slice: %v", got)
	}

	serviceContext := newDecoratorTestContext()
	if err := decorated.Init(serviceContext); err != nil {
		t.Fatal(err)
	}
	wantHandleErr := errors.New("handle failed")
	inner.handleErr = wantHandleErr
	if err := decorated.Handle(nil, gsr.Command{ID: 10}); !errors.Is(err, wantHandleErr) {
		t.Fatalf("Handle error = %v, want %v", err, wantHandleErr)
	}
	if serviceContext.sentPayload != nil {
		t.Fatalf("normal Handle error emitted notice: %#v", serviceContext.sentPayload)
	}
	if err := decorated.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := decorated.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := inner.calls, []string{"init", "handle", "stop", "close"}; !equalStrings(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestDecoratorRecordsDeliveryFailureAndRepanics(t *testing.T) {
	metrics := newRecordingMetrics()
	serviceContext := newDecoratorTestContext()
	serviceContext.metrics = metrics
	serviceContext.sendErr = gsr.ErrMailboxFull
	decorated, err := Decorate(panicDecoratorService{}, DecoratorConfig{
		Key:        ServiceKey{Namespace: "player", ID: "42"},
		Generation: 1,
		Supervisor: serviceContext.sendTarget,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := decorated.Init(serviceContext); err != nil {
		t.Fatal(err)
	}

	assertPanicsWith(t, "boom", func() {
		_ = decorated.Handle(nil, gsr.Command{ID: 10})
	})
	if got := metrics.value(metricFailureNoticeDeliveryErrors); got != 1 {
		t.Fatalf("delivery errors = %d, want 1", got)
	}
}

func TestDecoratorRejectsSupervisorSelfReferenceDuringInit(t *testing.T) {
	serviceContext := newDecoratorTestContext()
	serviceContext.self = serviceContext.sendTarget
	decorated, err := Decorate(&recordingDecoratorService{commands: []gsr.CommandID{1}}, DecoratorConfig{
		Key:        ServiceKey{Namespace: "player", ID: "42"},
		Generation: 1,
		Supervisor: serviceContext.sendTarget,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := decorated.Init(serviceContext); !errors.Is(err, ErrInvalidRegistration) {
		t.Fatalf("Init error = %v, want ErrInvalidRegistration", err)
	}
}

type panicDecoratorService struct{}

func (panicDecoratorService) Commands() []gsr.CommandID                    { return []gsr.CommandID{10} }
func (panicDecoratorService) Init(gsr.ServiceContext) error                { return nil }
func (panicDecoratorService) Handle(gsr.CommandContext, gsr.Command) error { panic("boom") }
func (panicDecoratorService) Stop(context.Context) error                   { return nil }
func (panicDecoratorService) Close() error                                 { return nil }

type recordingDecoratorService struct {
	commands  []gsr.CommandID
	handleErr error
	calls     []string
}

type receivedFailureNotice struct {
	source gsr.ServiceRef
	notice FailureNotice
}

type failureNoticeCaptureService struct {
	received chan<- receivedFailureNotice
}

func (*failureNoticeCaptureService) Commands() []gsr.CommandID {
	return []gsr.CommandID{failureCommand}
}
func (*failureNoticeCaptureService) Init(gsr.ServiceContext) error { return nil }
func (s *failureNoticeCaptureService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	notice, ok := command.Payload.(FailureNotice)
	if !ok {
		return ErrInvalidNotice
	}
	s.received <- receivedFailureNotice{source: ctx.Source(), notice: notice}
	return nil
}
func (*failureNoticeCaptureService) Stop(context.Context) error { return nil }
func (*failureNoticeCaptureService) Close() error               { return nil }

func (s *recordingDecoratorService) Commands() []gsr.CommandID {
	return append([]gsr.CommandID(nil), s.commands...)
}
func (s *recordingDecoratorService) Init(gsr.ServiceContext) error {
	s.calls = append(s.calls, "init")
	return nil
}
func (s *recordingDecoratorService) Handle(gsr.CommandContext, gsr.Command) error {
	s.calls = append(s.calls, "handle")
	return s.handleErr
}
func (s *recordingDecoratorService) Stop(context.Context) error {
	s.calls = append(s.calls, "stop")
	return nil
}
func (s *recordingDecoratorService) Close() error {
	s.calls = append(s.calls, "close")
	return nil
}

type decoratorTestServiceContext struct {
	self        gsr.ServiceRef
	now         time.Time
	metrics     gsr.Metrics
	logger      *slog.Logger
	sendTarget  gsr.ServiceRef
	sentID      gsr.CommandID
	sentPayload any
	sendErr     error
}

func newDecoratorTestContext() *decoratorTestServiceContext {
	return &decoratorTestServiceContext{
		self:       gsr.ServiceRef{Node: "node-a", ID: 7},
		now:        time.Unix(100, 0),
		metrics:    newRecordingMetrics(),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		sendTarget: gsr.ServiceRef{Node: "node-a", ID: 1},
	}
}

func (c *decoratorTestServiceContext) Self() gsr.ServiceRef { return c.self }
func (c *decoratorTestServiceContext) Send(target gsr.ServiceRef, id gsr.CommandID, payload any) error {
	c.sendTarget = target
	c.sentID = id
	c.sentPayload = payload
	return c.sendErr
}
func (*decoratorTestServiceContext) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, errors.New("unexpected Call")
}
func (*decoratorTestServiceContext) After(time.Duration, gsr.CommandID, any) (gsr.TimerID, error) {
	return 0, errors.New("unexpected After")
}
func (c *decoratorTestServiceContext) Now() time.Time       { return c.now }
func (c *decoratorTestServiceContext) Logger() *slog.Logger { return c.logger }
func (c *decoratorTestServiceContext) Metrics() gsr.Metrics { return c.metrics }

type recordingMetrics struct {
	mu       sync.Mutex
	counters map[string]uint64
}

func newRecordingMetrics() *recordingMetrics {
	return &recordingMetrics{counters: make(map[string]uint64)}
}
func (m *recordingMetrics) Inc(name string) { m.Add(name, 1) }
func (m *recordingMetrics) Add(name string, delta uint64) {
	m.mu.Lock()
	m.counters[name] += delta
	m.mu.Unlock()
}
func (*recordingMetrics) SetGauge(string, int64)        {}
func (*recordingMetrics) Observe(string, time.Duration) {}
func (m *recordingMetrics) value(name string) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[name]
}

func assertPanicsWith(t *testing.T, want any, fn func()) {
	t.Helper()
	defer func() {
		if got := recover(); got != want {
			t.Fatalf("panic = %#v, want %#v", got, want)
		}
	}()
	fn()
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
