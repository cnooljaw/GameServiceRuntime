// Package drain tests VisitorRegistryService lease ownership and lifecycle.
package drain

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

const commandVisitorAction gsr.CommandID = 0x7f270001

func TestVisitorRegistryAcquireListsIndependentStrongAndWeakLeases(t *testing.T) {
	fixture := newVisitorFixture(t, VisitorRegistryConfig{LeaseTTL: time.Minute, SweepInterval: time.Hour})
	firstVisitor := fixture.newVisitor(t)
	secondVisitor := fixture.newVisitor(t)

	first, err := fixture.acquire(t, firstVisitor, false)
	if err != nil {
		t.Fatalf("Acquire(strong) error = %v", err)
	}
	second, err := fixture.acquire(t, secondVisitor, true)
	if err != nil {
		t.Fatalf("Acquire(weak) error = %v", err)
	}
	if first.AuthorityEpoch == 0 || first.Generation == 0 || first.ExpiresAt.IsZero() {
		t.Fatalf("strong lease = %#v, want complete identity", first)
	}
	if second.AuthorityEpoch != first.AuthorityEpoch {
		t.Fatalf("AuthorityEpoch = %d, want %d", second.AuthorityEpoch, first.AuthorityEpoch)
	}

	visitors, err := fixture.client.List(context.Background(), fixture.target)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(visitors) != 2 {
		t.Fatalf("List() length = %d, want 2", len(visitors))
	}
	if visitors[0].Visitor != firstVisitor || visitors[0].Weak {
		t.Fatalf("List()[0] = %#v, want strong visitor %#v", visitors[0], firstVisitor)
	}
	if visitors[1].Visitor != secondVisitor || !visitors[1].Weak {
		t.Fatalf("List()[1] = %#v, want weak visitor %#v", visitors[1], secondVisitor)
	}
	visitors[0].Weak = true

	again, err := fixture.client.List(context.Background(), fixture.target)
	if err != nil {
		t.Fatalf("List() after mutation error = %v", err)
	}
	if again[0].Weak {
		t.Fatalf("List() returned caller-mutated state: %#v", again)
	}
}

func TestVisitorRegistryReacquireAndRenewFenceLateMutations(t *testing.T) {
	fixture := newVisitorFixture(t, VisitorRegistryConfig{LeaseTTL: time.Minute, SweepInterval: time.Hour})
	visitor := fixture.newVisitor(t)

	first, err := fixture.acquire(t, visitor, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.acquire(t, visitor, true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation <= first.Generation || !second.Weak {
		t.Fatalf("reacquired lease = %#v, want newer weak lease than %#v", second, first)
	}
	if err := fixture.release(t, visitor, first); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("Release(old) error = %v, want ErrLeaseExpired", err)
	}

	fixture.clock.Advance(time.Second)
	renewed, err := fixture.renew(t, visitor, second)
	if err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
	if renewed.Generation != second.Generation || renewed.Weak != second.Weak || !renewed.ExpiresAt.After(second.ExpiresAt) {
		t.Fatalf("Renew() = %#v, want same identity with later expiry than %#v", renewed, second)
	}
	if err := fixture.release(t, visitor, second); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("Release(pre-renew lease) error = %v, want ErrLeaseExpired", err)
	}

	visitors, err := fixture.client.List(context.Background(), fixture.target)
	if err != nil || len(visitors) != 1 || visitors[0].Generation != renewed.Generation {
		t.Fatalf("List() after stale release = %#v, %v", visitors, err)
	}
	if err := fixture.release(t, visitor, renewed); err != nil {
		t.Fatalf("Release(renewed) error = %v", err)
	}
	visitors, err = fixture.client.List(context.Background(), fixture.target)
	if err != nil || len(visitors) != 0 {
		t.Fatalf("List() after release = %#v, %v, want empty", visitors, err)
	}
}

func TestVisitorRegistryFencesMutationOwnerAndExpiredLease(t *testing.T) {
	fixture := newVisitorFixture(t, VisitorRegistryConfig{LeaseTTL: time.Minute, SweepInterval: time.Hour})
	owner := fixture.newVisitor(t)
	other := fixture.newVisitor(t)
	lease, err := fixture.acquire(t, owner, false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.client.Acquire(context.Background(), fixture.target, owner, false); !errors.Is(err, ErrLeaseOwnerMismatch) {
		t.Fatalf("Acquire(node source) error = %v, want ErrLeaseOwnerMismatch", err)
	}
	if err := fixture.release(t, other, lease); !errors.Is(err, ErrLeaseOwnerMismatch) {
		t.Fatalf("Release(other visitor) error = %v, want ErrLeaseOwnerMismatch", err)
	}

	fixture.clock.Advance(time.Minute + time.Nanosecond)
	visitors, err := fixture.client.List(context.Background(), fixture.target)
	if err != nil || len(visitors) != 0 {
		t.Fatalf("List(expired) = %#v, %v, want empty", visitors, err)
	}
	if _, err := fixture.renew(t, owner, lease); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("Renew(expired) error = %v, want ErrLeaseExpired", err)
	}
}

func TestVisitorRegistrySweepStopsReschedulingWhenEmpty(t *testing.T) {
	fixture := newVisitorFixture(t, VisitorRegistryConfig{LeaseTTL: time.Minute, SweepInterval: 5 * time.Millisecond})
	visitor := fixture.newVisitor(t)
	if _, err := fixture.acquire(t, visitor, false); err != nil {
		t.Fatal(err)
	}
	eventuallyDrain(t, func() bool { return fixture.runtime.Inspect().Timers == 1 })
	lease, err := fixture.acquire(t, visitor, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.release(t, visitor, lease); err != nil {
		t.Fatal(err)
	}
	eventuallyDrain(t, func() bool { return fixture.runtime.Inspect().Timers == 0 })
}

func TestVisitorRegistryDoesNotCommitWhenTimerSchedulingFails(t *testing.T) {
	created, err := NewVisitorRegistryService(VisitorRegistryConfig{LeaseTTL: time.Minute, SweepInterval: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	service := created.(*visitorRegistryService)
	if err := service.Init(failingTimerContext{afterErr: gsr.ErrRuntimeClosed}); err != nil {
		t.Fatal(err)
	}
	commandContext := &recordingVisitorCommandContext{source: gsr.ServiceRef{Node: "registry-node", ID: 2}}
	err = service.Handle(commandContext, gsr.Command{
		ID: commandAcquireVisitorLease,
		Payload: acquireVisitorLeaseRequest{
			Target:  newWireServiceRef(gsr.ServiceRef{Node: "registry-node", ID: 99}),
			Visitor: newWireServiceRef(commandContext.source),
		},
	})
	if !errors.Is(err, gsr.ErrRuntimeClosed) {
		t.Fatalf("Handle() error = %v, want ErrRuntimeClosed", err)
	}
	if commandContext.replied {
		t.Fatal("timer scheduling failure was converted into a successful Reply")
	}
	if len(service.leases) != 0 || service.nextGeneration != 0 {
		t.Fatalf("failed Acquire changed state: leases=%#v generation=%d", service.leases, service.nextGeneration)
	}
}

type visitorFixture struct {
	runtime *gsr.Runtime
	client  *Client
	ref     gsr.ServiceRef
	target  gsr.ServiceRef
	clock   *visitorTestClock
}

func newVisitorFixture(t *testing.T, config VisitorRegistryConfig) visitorFixture {
	t.Helper()
	clock := &visitorTestClock{now: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "registry-node", Workers: 2, Now: clock.Now})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service, err := NewVisitorRegistryService(config)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Name: DefaultVisitorRegistryName, Service: service})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(runtime, ref)
	if err != nil {
		t.Fatal(err)
	}
	return visitorFixture{
		runtime: runtime,
		client:  client,
		ref:     ref,
		target:  gsr.ServiceRef{Node: "registry-node", ID: 99},
		clock:   clock,
	}
}

func (f visitorFixture) newVisitor(t *testing.T) gsr.ServiceRef {
	t.Helper()
	ref, err := f.runtime.CreateService(gsr.ServiceSpec{Service: &visitorService{registry: f.ref}})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func (f visitorFixture) acquire(t *testing.T, visitor gsr.ServiceRef, weak bool) (VisitorLease, error) {
	t.Helper()
	value, err := f.action(visitor, visitorAction{kind: visitorActionAcquire, target: f.target, weak: weak})
	if err != nil {
		return VisitorLease{}, err
	}
	lease, ok := value.(VisitorLease)
	if !ok {
		t.Fatalf("Acquire result = %#v", value)
	}
	return lease, nil
}

func (f visitorFixture) renew(t *testing.T, visitor gsr.ServiceRef, lease VisitorLease) (VisitorLease, error) {
	t.Helper()
	value, err := f.action(visitor, visitorAction{kind: visitorActionRenew, lease: lease})
	if err != nil {
		return VisitorLease{}, err
	}
	renewed, ok := value.(VisitorLease)
	if !ok {
		t.Fatalf("Renew result = %#v", value)
	}
	return renewed, nil
}

func (f visitorFixture) release(t *testing.T, visitor gsr.ServiceRef, lease VisitorLease) error {
	t.Helper()
	_, err := f.action(visitor, visitorAction{kind: visitorActionRelease, lease: lease})
	return err
}

func (f visitorFixture) action(visitor gsr.ServiceRef, action visitorAction) (any, error) {
	return f.runtime.Call(context.Background(), visitor, commandVisitorAction, action)
}

type visitorActionKind uint8

const (
	visitorActionAcquire visitorActionKind = iota + 1
	visitorActionRenew
	visitorActionRelease
)

type visitorAction struct {
	kind   visitorActionKind
	target gsr.ServiceRef
	weak   bool
	lease  VisitorLease
}

type visitorService struct {
	context  gsr.ServiceContext
	registry gsr.ServiceRef
	client   *Client
}

func (*visitorService) Commands() []gsr.CommandID { return []gsr.CommandID{commandVisitorAction} }

func (s *visitorService) Init(serviceContext gsr.ServiceContext) error {
	client, err := NewClient(serviceContext, s.registry)
	if err != nil {
		return err
	}
	s.context = serviceContext
	s.client = client
	return nil
}

func (s *visitorService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	action, ok := command.Payload.(visitorAction)
	if !ok {
		return ErrInvalidLease
	}
	switch action.kind {
	case visitorActionAcquire:
		lease, err := s.client.Acquire(context.Background(), action.target, commandContext.Self(), action.weak)
		if err != nil {
			return err
		}
		return commandContext.Reply(lease)
	case visitorActionRenew:
		lease, err := s.client.Renew(context.Background(), action.lease)
		if err != nil {
			return err
		}
		return commandContext.Reply(lease)
	case visitorActionRelease:
		if err := s.client.Release(context.Background(), action.lease); err != nil {
			return err
		}
		return commandContext.Reply(struct{}{})
	default:
		return ErrInvalidLease
	}
}

func (*visitorService) Stop(context.Context) error { return nil }
func (*visitorService) Close() error               { return nil }

type visitorTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *visitorTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *visitorTestClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func eventuallyDrain(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before deadline")
}

type failingTimerContext struct {
	afterErr error
}

func (failingTimerContext) Self() gsr.ServiceRef                          { return gsr.ServiceRef{Node: "registry-node", ID: 1} }
func (failingTimerContext) Send(gsr.ServiceRef, gsr.CommandID, any) error { return nil }
func (failingTimerContext) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}
func (context failingTimerContext) After(time.Duration, gsr.CommandID, any) (gsr.TimerID, error) {
	return 0, context.afterErr
}
func (failingTimerContext) Now() time.Time { return time.Now() }
func (failingTimerContext) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
func (failingTimerContext) Metrics() gsr.Metrics { return noOpMetrics{} }

type recordingVisitorCommandContext struct {
	source  gsr.ServiceRef
	replied bool
}

func (*recordingVisitorCommandContext) Self() gsr.ServiceRef {
	return gsr.ServiceRef{Node: "registry-node", ID: 1}
}
func (context *recordingVisitorCommandContext) Source() gsr.ServiceRef { return context.source }
func (context *recordingVisitorCommandContext) Reply(any) error {
	context.replied = true
	return nil
}

type noOpMetrics struct{}

func (noOpMetrics) Inc(string)                    {}
func (noOpMetrics) Add(string, uint64)            {}
func (noOpMetrics) SetGauge(string, int64)        {}
func (noOpMetrics) Observe(string, time.Duration) {}
