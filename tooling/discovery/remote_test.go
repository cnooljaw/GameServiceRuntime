package discovery_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

func TestRemoteDiscoveryRegisterHeartbeatAndResolveName(t *testing.T) {
	fixture := newRemoteDiscoveryFixture(t)
	leaseB, err := fixture.local.RegisterNode(context.Background(), "node-b", fixture.transportB.Address())
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.local.RegisterName(context.Background(), leaseB, ".config", fixture.configRef); err != nil {
		t.Fatal(err)
	}
	leaseA, err := fixture.remote.RegisterNode(context.Background(), "node-a", fixture.transportA.Address())
	if err != nil {
		t.Fatal(err)
	}

	renewed, err := fixture.remote.Heartbeat(context.Background(), leaseA)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Node != leaseA.Node || renewed.AuthorityEpoch != leaseA.AuthorityEpoch || renewed.Generation != leaseA.Generation {
		t.Fatalf("renewed lease = %#v, want identity %#v", renewed, leaseA)
	}
	resolved, err := fixture.remote.ResolveName(context.Background(), ".config")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != fixture.configRef {
		t.Fatalf("ResolveName = %v, want %v", resolved, fixture.configRef)
	}
}

func TestRemoteDiscoveryPreservesDomainErrors(t *testing.T) {
	fixture := newRemoteDiscoveryFixture(t)
	owner := registerNode(t, fixture.local, "node-b")
	if err := fixture.local.RegisterName(context.Background(), owner, ".config", fixture.configRef); err != nil {
		t.Fatal(err)
	}
	other, err := fixture.remote.RegisterNode(context.Background(), "node-a", fixture.transportA.Address())
	if err != nil {
		t.Fatal(err)
	}

	err = fixture.remote.RegisterName(context.Background(), other, ".config", gsr.ServiceRef{Node: "node-a", ID: 100})
	if !errors.Is(err, discovery.ErrNameConflict) {
		t.Fatalf("RegisterName error = %T %v, want ErrNameConflict", err, err)
	}
	var remoteError *gsr.RemoteError
	if errors.As(err, &remoteError) {
		t.Fatalf("domain error degraded to RemoteError: %v", remoteError)
	}
}

type remoteDiscoveryFixture struct {
	local      *discovery.Client
	remote     *discovery.Client
	configRef  gsr.ServiceRef
	transportA *clustertcp.Transport
	transportB *clustertcp.Transport
}

func newRemoteDiscoveryFixture(t *testing.T) remoteDiscoveryFixture {
	t.Helper()
	codecB := discovery.NewCodec(nil)
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 2}, transportB, codecB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeB.Close(context.Background()) })
	service, err := discovery.NewService(discovery.Config{LeaseTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	discoveryRef, err := nodeB.CreateService(gsr.ServiceSpec{Name: discovery.DefaultServiceName, Service: service})
	if err != nil {
		t.Fatal(err)
	}
	configRef, err := nodeB.CreateService(gsr.ServiceSpec{Service: remoteFixtureService{}})
	if err != nil {
		t.Fatal(err)
	}
	local, err := discovery.NewClient(nodeB, discoveryRef)
	if err != nil {
		t.Fatal(err)
	}

	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 2}, transportA, discovery.NewCodec(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeA.Close(context.Background()) })
	remote, err := discovery.NewClient(nodeA, discoveryRef)
	if err != nil {
		t.Fatal(err)
	}
	return remoteDiscoveryFixture{local: local, remote: remote, configRef: configRef, transportA: transportA, transportB: transportB}
}

type remoteFixtureService struct{}

func (remoteFixtureService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (remoteFixtureService) Init(gsr.ServiceContext) error { return nil }
func (remoteFixtureService) Handle(gsr.CommandContext, gsr.Command) error {
	return nil
}
func (remoteFixtureService) Stop(context.Context) error { return nil }
func (remoteFixtureService) Close() error               { return nil }
