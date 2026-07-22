package discovery_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
)

func TestRegisterNodeReturnsLeaseAndRecord(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})

	lease, err := fixture.client.RegisterNode(context.Background(), "node-b", "127.0.0.1:9002")
	if err != nil {
		t.Fatal(err)
	}
	if lease.Node != "node-b" || lease.AuthorityEpoch == 0 || lease.Generation == 0 {
		t.Fatalf("lease = %#v", lease)
	}
	if want := fixture.clock.Now().Add(time.Minute); !lease.ExpiresAt.Equal(want) {
		t.Fatalf("lease ExpiresAt = %v, want %v", lease.ExpiresAt, want)
	}

	record, err := fixture.client.GetNode(context.Background(), "node-b")
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != lease.Node || record.Address != "127.0.0.1:9002" || record.AuthorityEpoch != lease.AuthorityEpoch || record.Generation != lease.Generation {
		t.Fatalf("record = %#v, lease = %#v", record, lease)
	}
	if !record.LastSeen.Equal(fixture.clock.Now()) || !record.ExpiresAt.Equal(lease.ExpiresAt) {
		t.Fatalf("record times = %#v", record)
	}
}

func TestDiscoveryServiceUsesDefaultLeaseTTL(t *testing.T) {
	started := time.Date(2026, 7, 21, 13, 0, 0, 0, time.UTC)
	clock := &testClock{now: started}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Now: clock.Now})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service, err := discovery.NewService(discovery.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	client, err := discovery.NewClient(runtime, ref)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := client.RegisterNode(context.Background(), "node-a", "node-a:9000")
	if err != nil {
		t.Fatal(err)
	}
	if want := started.Add(30 * time.Second); !lease.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want default TTL deadline %v", lease.ExpiresAt, want)
	}
}

func TestHeartbeatRenewsMatchingLease(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	first, err := fixture.client.RegisterNode(context.Background(), "node-b", "127.0.0.1:9002")
	if err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(20 * time.Second)

	second, err := fixture.client.Heartbeat(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if second.Node != first.Node || second.AuthorityEpoch != first.AuthorityEpoch || second.Generation != first.Generation {
		t.Fatalf("renewed lease = %#v, first = %#v", second, first)
	}
	if !second.ExpiresAt.Equal(fixture.clock.Now().Add(time.Minute)) {
		t.Fatalf("renewed ExpiresAt = %v", second.ExpiresAt)
	}
	if !first.ExpiresAt.Equal(fixture.started.Add(time.Minute)) {
		t.Fatalf("first lease changed to %v", first.ExpiresAt)
	}
}

func TestRegisterNodeInvalidatesPreviousGeneration(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	first, err := fixture.client.RegisterNode(context.Background(), "node-b", "127.0.0.1:9002")
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.client.RegisterNode(context.Background(), "node-b", "127.0.0.1:9012")
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation == first.Generation {
		t.Fatalf("second generation = %d, want different from %d", second.Generation, first.Generation)
	}
	if _, err := fixture.client.Heartbeat(context.Background(), first); !errors.Is(err, discovery.ErrLeaseExpired) {
		t.Fatalf("old Heartbeat error = %v, want ErrLeaseExpired", err)
	}
	record, err := fixture.client.GetNode(context.Background(), "node-b")
	if err != nil {
		t.Fatal(err)
	}
	if record.Generation != second.Generation || record.Address != "127.0.0.1:9012" {
		t.Fatalf("record = %#v, second = %#v", record, second)
	}
}

func TestListNodesReturnsSortedCopies(t *testing.T) {
	fixture := newRemoteDiscoveryFixture(t)
	if _, err := fixture.local.RegisterNode(context.Background(), "node-b", "node-b:9000"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.remote.RegisterNode(context.Background(), "node-a", "node-a:9000"); err != nil {
		t.Fatal(err)
	}

	first, err := fixture.remote.ListNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].ID != "node-a" || first[1].ID != "node-b" {
		t.Fatalf("nodes = %#v", first)
	}
	first[0].Address = "changed"
	first = append(first, discovery.NodeRecord{})

	second, err := fixture.local.ListNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 || second[0].Address != "node-a:9000" {
		t.Fatalf("second nodes changed through first result: %#v", second)
	}
}

func TestExpiredNodeIsNotDiscoverable(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	lease, err := fixture.client.RegisterNode(context.Background(), "node-b", "127.0.0.1:9002")
	if err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(time.Minute + time.Nanosecond)

	if _, err := fixture.client.GetNode(context.Background(), "node-b"); !errors.Is(err, discovery.ErrNodeNotFound) {
		t.Fatalf("GetNode error = %v, want ErrNodeNotFound", err)
	}
	if _, err := fixture.client.Heartbeat(context.Background(), lease); !errors.Is(err, discovery.ErrLeaseExpired) {
		t.Fatalf("Heartbeat error = %v, want ErrLeaseExpired", err)
	}
}

func TestUnregisterNodeRequiresCurrentLease(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	first, err := fixture.client.RegisterNode(context.Background(), "node-b", "127.0.0.1:9002")
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.client.RegisterNode(context.Background(), "node-b", "127.0.0.1:9012")
	if err != nil {
		t.Fatal(err)
	}

	if err := fixture.client.UnregisterNode(context.Background(), first); !errors.Is(err, discovery.ErrLeaseExpired) {
		t.Fatalf("old UnregisterNode error = %v, want ErrLeaseExpired", err)
	}
	if _, err := fixture.client.GetNode(context.Background(), "node-b"); err != nil {
		t.Fatalf("current node removed by stale unregister: %v", err)
	}
	if err := fixture.client.UnregisterNode(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.client.GetNode(context.Background(), "node-b"); !errors.Is(err, discovery.ErrNodeNotFound) {
		t.Fatalf("GetNode error = %v, want ErrNodeNotFound", err)
	}
}

func TestLeaseFromPreviousAuthorityEpochIsExpired(t *testing.T) {
	firstAuthority := newDiscoveryFixture(t, discovery.Config{})
	oldLease := registerNode(t, firstAuthority.client, "node-b")
	secondAuthority := newDiscoveryFixture(t, discovery.Config{})
	currentLease := registerNode(t, secondAuthority.client, "node-b")

	if oldLease.AuthorityEpoch == currentLease.AuthorityEpoch {
		t.Fatalf("authority epoch was reused: %d", oldLease.AuthorityEpoch)
	}
	if oldLease.Generation != currentLease.Generation {
		t.Fatalf("test requires equal generations, got %d and %d", oldLease.Generation, currentLease.Generation)
	}
	if _, err := secondAuthority.client.Heartbeat(context.Background(), oldLease); !errors.Is(err, discovery.ErrLeaseExpired) {
		t.Fatalf("old authority Heartbeat error = %v, want ErrLeaseExpired", err)
	}
}

func TestLeaseMutationRequiresExactCommandSource(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	lease := registerNode(t, fixture.client, "node-b")
	probe := &leaseOwnerProbeService{target: fixture.service}
	probeRef, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: probe})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.runtime.Call(context.Background(), probeRef, 1, lease); !errors.Is(err, discovery.ErrLeaseOwnerMismatch) {
		t.Fatalf("Heartbeat from another Service error = %v, want ErrLeaseOwnerMismatch", err)
	}
	nameRequest := leaseOwnerNameRequest{lease: lease, name: ".config", ref: gsr.ServiceRef{Node: "node-b", ID: 100}}
	if _, err := fixture.runtime.Call(context.Background(), probeRef, 2, nameRequest); !errors.Is(err, discovery.ErrLeaseOwnerMismatch) {
		t.Fatalf("RegisterName from another Service error = %v, want ErrLeaseOwnerMismatch", err)
	}
}

func TestRegisterNodeRequiresMatchingSourceNode(t *testing.T) {
	fixture := newRemoteDiscoveryFixture(t)
	_, err := fixture.remote.RegisterNode(context.Background(), "node-b", "node-b:9000")
	if !errors.Is(err, discovery.ErrLeaseOwnerMismatch) {
		t.Fatalf("RegisterNode error = %v, want ErrLeaseOwnerMismatch", err)
	}
}

func TestDiscoverySweepUsesTimerCommand(t *testing.T) {
	t.Run("empty registry stops rescheduling", func(t *testing.T) {
		fixture := newDiscoveryFixture(t, discovery.Config{LeaseTTL: time.Second, SweepInterval: 10 * time.Millisecond})
		lease, err := fixture.client.RegisterNode(context.Background(), "node-b", "127.0.0.1:9002")
		if err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool { return fixture.runtime.Inspect().Timers == 1 })
		if err := fixture.client.UnregisterNode(context.Background(), lease); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool { return fixture.runtime.Inspect().Timers == 0 })
	})

	t.Run("stopping service cancels target timer", func(t *testing.T) {
		fixture := newDiscoveryFixture(t, discovery.Config{LeaseTTL: time.Hour, SweepInterval: time.Hour})
		if _, err := fixture.client.RegisterNode(context.Background(), "node-b", "127.0.0.1:9002"); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool { return fixture.runtime.Inspect().Timers == 1 })
		if err := fixture.runtime.Stop(context.Background(), fixture.service); err != nil {
			t.Fatal(err)
		}
		if got := fixture.runtime.Inspect().Timers; got != 0 {
			t.Fatalf("Timers after Stop = %d, want 0", got)
		}
	})
}

func TestDiscoveryRejectsInvalidNodeInput(t *testing.T) {
	if _, err := discovery.NewService(discovery.Config{LeaseTTL: -time.Second}); !errors.Is(err, discovery.ErrInvalidConfig) {
		t.Fatalf("NewService error = %v, want ErrInvalidConfig", err)
	}
	if _, err := discovery.NewClient(nil, gsr.ServiceRef{}); !errors.Is(err, discovery.ErrInvalidConfig) {
		t.Fatalf("NewClient error = %v, want ErrInvalidConfig", err)
	}

	fixture := newDiscoveryFixture(t, discovery.Config{})
	if _, err := fixture.client.RegisterNode(context.Background(), "", "127.0.0.1:9002"); !errors.Is(err, discovery.ErrInvalidNode) {
		t.Fatalf("empty NodeID error = %v, want ErrInvalidNode", err)
	}
	if _, err := fixture.client.RegisterNode(context.Background(), "node-b", ""); !errors.Is(err, discovery.ErrInvalidNode) {
		t.Fatalf("empty address error = %v, want ErrInvalidNode", err)
	}
	if _, err := fixture.client.Heartbeat(context.Background(), discovery.NodeLease{Node: "node-b"}); !errors.Is(err, discovery.ErrInvalidNode) {
		t.Fatalf("zero generation error = %v, want ErrInvalidNode", err)
	}
}

func TestDiscoveryClientRejectsInvalidResponse(t *testing.T) {
	client, err := discovery.NewClient(invalidResponseCaller{}, gsr.ServiceRef{Node: "node-a", ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetNode(context.Background(), "node-b"); !errors.Is(err, discovery.ErrInvalidResponse) {
		t.Fatalf("GetNode error = %v, want ErrInvalidResponse", err)
	}
}

type discoveryFixture struct {
	runtime *gsr.Runtime
	client  *discovery.Client
	service gsr.ServiceRef
	clock   *testClock
	started time.Time
}

func newDiscoveryFixture(t *testing.T, config discovery.Config) discoveryFixture {
	t.Helper()
	if config.LeaseTTL == 0 {
		config.LeaseTTL = time.Minute
	}
	if config.SweepInterval == 0 {
		config.SweepInterval = time.Hour
	}
	started := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: started}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-b", Now: clock.Now})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service, err := discovery.NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Name: discovery.DefaultServiceName, Service: service})
	if err != nil {
		t.Fatal(err)
	}
	client, err := discovery.NewClient(runtime, ref)
	if err != nil {
		t.Fatal(err)
	}
	return discoveryFixture{runtime: runtime, client: client, service: ref, clock: clock, started: started}
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

type invalidResponseCaller struct{}

func (invalidResponseCaller) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return "invalid", nil
}

type leaseOwnerProbeService struct {
	client *discovery.Client
	target gsr.ServiceRef
}

type leaseOwnerNameRequest struct {
	lease discovery.NodeLease
	name  gsr.ServiceName
	ref   gsr.ServiceRef
}

func (*leaseOwnerProbeService) Commands() []gsr.CommandID { return []gsr.CommandID{1, 2} }
func (s *leaseOwnerProbeService) Init(serviceContext gsr.ServiceContext) error {
	client, err := discovery.NewClient(serviceContext, s.target)
	if err != nil {
		return err
	}
	s.client = client
	return nil
}
func (s *leaseOwnerProbeService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	if command.ID == 1 {
		_, err := s.client.Heartbeat(context.Background(), command.Payload.(discovery.NodeLease))
		return err
	}
	request := command.Payload.(leaseOwnerNameRequest)
	return s.client.RegisterName(context.Background(), request.lease, request.name, request.ref)
}
func (*leaseOwnerProbeService) Stop(context.Context) error { return nil }
func (*leaseOwnerProbeService) Close() error               { return nil }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func eventually(t *testing.T, condition func() bool) {
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
