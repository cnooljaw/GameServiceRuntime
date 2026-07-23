package control

import (
	"context"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
	"github.com/lijiawang/GameServiceRuntime/tooling/monitor"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

func TestRemoteClusterObserverRefreshesNodeThroughComposableCodec(t *testing.T) {
	codecB := NewCodec(discovery.NewCodec(nil))
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 2}, transportB, codecB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeB.Close(context.Background()) })
	discoveryService, err := discovery.NewService(discovery.Config{LeaseTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	discoveryRef, err := nodeB.CreateService(gsr.ServiceSpec{Name: discovery.DefaultServiceName, Service: discoveryService})
	if err != nil {
		t.Fatal(err)
	}
	reporter, err := monitor.New(nodeB)
	if err != nil {
		t.Fatal(err)
	}
	agentService, err := NewNodeAgentService(NodeAgentConfig{
		Reporter:          reporter,
		ObserverNode:      "node-a",
		Discovery:         discoveryRef,
		Address:           transportB.Address(),
		HeartbeatInterval: time.Hour,
		CallTimeout:       time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	agentRef, err := nodeB.CreateService(gsr.ServiceSpec{Name: DefaultNodeAgentName, Service: agentService})
	if err != nil {
		t.Fatal(err)
	}

	codecA := NewCodec(discovery.NewCodec(nil))
	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 2}, transportA, codecA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeA.Close(context.Background()) })
	remoteAgent, err := nodeA.ResolveRemote(context.Background(), "node-b", DefaultNodeAgentName)
	if err != nil {
		t.Fatal(err)
	}
	if remoteAgent != agentRef {
		t.Fatalf("ResolveRemote() = %#v, want %#v", remoteAgent, agentRef)
	}
	observerService, err := NewClusterObserverService(ObserverConfig{Nodes: []NodeTarget{{
		Config: NodeConfig{ID: "node-b", Address: transportB.Address(), Enabled: true},
		Agent:  remoteAgent,
	}}, CallTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	observerRef, err := nodeA.CreateService(gsr.ServiceSpec{Name: DefaultObserverName, Service: observerService})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(nodeA, observerRef)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := client.RefreshNode(context.Background(), "node-b")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Observed.Status != NodeHealthy || !detail.HasReport || detail.Report.Node != "node-b" {
		t.Fatalf("RefreshNode() = %#v", detail)
	}
}
