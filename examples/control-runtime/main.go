package main

import (
	"context"
	"fmt"
	"log"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/control"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
	"github.com/lijiawang/GameServiceRuntime/tooling/monitor"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

func main() {
	codecB := control.NewCodec(discovery.NewCodec(nil))
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 2}, transportB, codecB)
	if err != nil {
		log.Fatal(err)
	}
	defer nodeB.Close(context.Background())
	discoveryService, err := discovery.NewService(discovery.Config{LeaseTTL: time.Minute})
	if err != nil {
		log.Fatal(err)
	}
	discoveryRef, err := nodeB.CreateService(gsr.ServiceSpec{Name: discovery.DefaultServiceName, Service: discoveryService})
	if err != nil {
		log.Fatal(err)
	}
	reporter, err := monitor.New(nodeB)
	if err != nil {
		log.Fatal(err)
	}
	agent, err := control.NewNodeAgentService(control.NodeAgentConfig{
		Reporter:          reporter,
		ObserverNode:      "node-a",
		Discovery:         discoveryRef,
		Address:           transportB.Address(),
		HeartbeatInterval: 10 * time.Second,
		CallTimeout:       time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := nodeB.CreateService(gsr.ServiceSpec{Name: control.DefaultNodeAgentName, Service: agent}); err != nil {
		log.Fatal(err)
	}

	codecA := control.NewCodec(discovery.NewCodec(nil))
	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 2}, transportA, codecA)
	if err != nil {
		log.Fatal(err)
	}
	defer nodeA.Close(context.Background())
	agentRef, err := nodeA.ResolveRemote(context.Background(), "node-b", control.DefaultNodeAgentName)
	if err != nil {
		log.Fatal(err)
	}
	observer, err := control.NewClusterObserverService(control.ObserverConfig{Nodes: []control.NodeTarget{{
		Config: control.NodeConfig{ID: "node-b", Address: transportB.Address(), Enabled: true},
		Agent:  agentRef,
	}}})
	if err != nil {
		log.Fatal(err)
	}
	observerRef, err := nodeA.CreateService(gsr.ServiceSpec{Name: control.DefaultObserverName, Service: observer})
	if err != nil {
		log.Fatal(err)
	}
	client, err := control.NewClient(nodeA, observerRef)
	if err != nil {
		log.Fatal(err)
	}
	detail, err := client.RefreshNode(context.Background(), "node-b")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("node=%s status=%s services=%d\n", detail.Config.ID, detail.Observed.Status, detail.Report.ServiceCount)
}
