package control

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
	"github.com/lijiawang/GameServiceRuntime/tooling/drain"
	"github.com/lijiawang/GameServiceRuntime/tooling/monitor"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

func TestRemoteNodeStopRunsOnlyThroughGatewayCoordinatorAndLocalRunner(t *testing.T) {
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	codecB := NewCodec(drain.NewCodec(servicegroup.NewCodec(discovery.NewCodec(nil))))
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 4}, transportB, codecB)
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

	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	codecA := NewCodec(drain.NewCodec(servicegroup.NewCodec(nil)))
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 4}, transportA, codecA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeA.Close(context.Background()) })
	directoryService, err := servicegroup.NewDirectoryService(servicegroup.DirectoryConfig{PublisherNode: "node-a", WatchTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	directoryRef, err := nodeA.CreateService(gsr.ServiceSpec{Service: directoryService})
	if err != nil {
		t.Fatal(err)
	}
	registryService, err := drain.NewVisitorRegistryService(drain.VisitorRegistryConfig{LeaseTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	registryRef, err := nodeA.CreateService(gsr.ServiceSpec{Service: registryService})
	if err != nil {
		t.Fatal(err)
	}
	gateway := &drainGatewayService{}
	gatewayRef, err := nodeA.CreateService(gsr.ServiceSpec{Name: testDrainGatewayName, Service: gateway})
	if err != nil {
		t.Fatal(err)
	}
	coordinatorService, err := NewDrainCoordinatorService(DrainCoordinatorConfig{
		Gateway:           gatewayRef,
		AllowedPrincipals: []Principal{"ops"},
		Directory:         directoryRef,
		VisitorRegistry:   registryRef,
		CallTimeout:       time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinatorRef, err := nodeA.CreateService(gsr.ServiceSpec{Name: DefaultDrainCoordinatorName, Service: coordinatorService})
	if err != nil {
		t.Fatal(err)
	}
	gateway.target = coordinatorRef

	oldService, err := drain.Decorate(drainWorkService{}, drain.GuardConfig{Controller: coordinatorRef, ExternalCommands: []gsr.CommandID{commandDrainWork}})
	if err != nil {
		t.Fatal(err)
	}
	oldRef, err := nodeB.CreateService(gsr.ServiceSpec{Service: oldService})
	if err != nil {
		t.Fatal(err)
	}
	nextRef, err := nodeB.CreateService(gsr.ServiceSpec{Service: drainWorkService{}})
	if err != nil {
		t.Fatal(err)
	}
	directory, err := servicegroup.NewClient(nodeA, directoryRef)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := directory.Publish(context.Background(), "match", servicegroup.ServiceSetVersion{}, []gsr.ServiceRef{oldRef}, map[string]string{"mode": "remote-stop"})
	if err != nil {
		t.Fatal(err)
	}

	runner, err := NewNodeStopRunner(nodeB, NodeStopRunnerConfig{Directory: directoryRef, Workers: 1, QueueSize: 1, CallTimeout: time.Second, StopTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })
	recoveryRegistry, err := NewMapBlueprintRegistry(map[BlueprintID]BlueprintFactory{
		"match-v2": func() (gsr.ServiceSpec, error) { return gsr.ServiceSpec{Service: drainWorkService{}}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	recoveryRunner, err := NewRecoveryRunner(nodeB, RecoveryRunnerConfig{Registry: recoveryRegistry, Workers: 1, QueueSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recoveryRunner.Close(context.Background()) })
	reporter, err := monitor.New(nodeB)
	if err != nil {
		t.Fatal(err)
	}
	agentService, err := NewNodeAgentService(NodeAgentConfig{
		Reporter:            reporter,
		ObserverNode:        "node-a",
		Discovery:           discoveryRef,
		Address:             transportB.Address(),
		HeartbeatInterval:   time.Hour,
		CallTimeout:         time.Second,
		StopCoordinator:     coordinatorRef,
		StopExecutor:        runner,
		RecoveryCoordinator: coordinatorRef,
		RecoveryExecutor:    recoveryRunner,
	})
	if err != nil {
		t.Fatal(err)
	}
	agentRef, err := nodeB.CreateService(gsr.ServiceSpec{Name: DefaultNodeAgentName, Service: agentService})
	if err != nil {
		t.Fatal(err)
	}
	remoteAgent, err := nodeA.ResolveRemote(context.Background(), "node-b", DefaultNodeAgentName)
	if err != nil {
		t.Fatal(err)
	}
	if remoteAgent != agentRef {
		t.Fatalf("ResolveRemote(agent) = %#v, want %#v", remoteAgent, agentRef)
	}

	client, err := NewDrainClient(nodeA, gatewayRef)
	if err != nil {
		t.Fatal(err)
	}
	drainOperation, err := client.Start(context.Background(), StartDrainRequest{
		RequestID: "remote-stop",
		Principal: "ops",
		Group:     initial.Name,
		Expected:  initial.Version,
		NextRefs:  []gsr.ServiceRef{nextRef},
		NextTags:  map[string]string{"mode": "remote-stop"},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if drainOperation.Phase != DrainReadyToStop {
		t.Fatalf("Drain operation = %#v, want ReadyToStop", drainOperation)
	}
	request := BeginStopRequest{RequestID: "remote-stop", Principal: "ops", Targets: []StopTargetRequest{{Target: oldRef, Agent: remoteAgent}}}
	operation, err := client.BeginStop(context.Background(), request)
	if err != nil {
		t.Fatalf("BeginStop() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for operation.Phase != StopCompleted && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		operation, err = client.ResolveStop(context.Background(), request.RequestID, request.Principal)
		if err != nil {
			t.Fatalf("ResolveStop() error = %v", err)
		}
	}
	if operation.Phase != StopCompleted || len(operation.Targets) != 1 || operation.Targets[0].State != StopTargetStopped {
		t.Fatalf("final Stop operation = %#v", operation)
	}
	if _, err := nodeB.Call(context.Background(), oldRef, commandDrainWork, struct{}{}); !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("old Service after remote Stop error = %v, want ErrServiceClosed", err)
	}

	expected, err := directory.Get(context.Background(), initial.Name)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := client.BeginRecovery(context.Background(), BeginRecoveryRequest{RequestID: request.RequestID, Principal: request.Principal, Group: expected.Name, Expected: expected, Targets: []RecoveryTargetRequest{{Removed: oldRef, Agent: remoteAgent, Blueprint: "match-v2"}}})
	if err != nil {
		t.Fatalf("BeginRecovery() error = %v", err)
	}
	for recovery.Phase == RecoveryCreating && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		recovery, err = client.ResolveRecovery(context.Background(), request.RequestID, request.Principal)
		if err != nil {
			t.Fatalf("ResolveRecovery() error = %v", err)
		}
	}
	if recovery.Phase != RecoveryAwaitingConfirmation || recovery.Targets[0].Created == (gsr.ServiceRef{}) {
		t.Fatalf("resolved recovery = %#v", recovery)
	}
	recovery, err = client.ConfirmRecovery(context.Background(), request.RequestID, request.Principal)
	if err != nil {
		t.Fatalf("ConfirmRecovery() error = %v", err)
	}
	if recovery.Phase != RecoveryCompleted || recovery.Targets[0].State != RecoveryTargetPublished {
		t.Fatalf("confirmed recovery = %#v", recovery)
	}
	published, err := directory.Get(context.Background(), initial.Name)
	if err != nil {
		t.Fatal(err)
	}
	if containsRecoveryRef(published.Refs, oldRef) || !containsRecoveryRef(published.Refs, recovery.Targets[0].Created) {
		t.Fatalf("published recovery set = %#v", published)
	}

	direct, err := NewDrainClient(nodeA, coordinatorRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := direct.GetStop(context.Background(), request.RequestID, request.Principal); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("direct GetStop() error = %v, want ErrUnauthorized", err)
	}
	if _, err := direct.GetRecovery(context.Background(), request.RequestID, request.Principal); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("direct GetRecovery() error = %v, want ErrUnauthorized", err)
	}
}
