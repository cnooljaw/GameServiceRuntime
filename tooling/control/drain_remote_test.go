package control

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/drain"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

const testDrainGatewayName gsr.ServiceName = ".test-drain-gateway"

func TestRemoteDrainCoordinatorRequiresGatewayAndUsesComposableCodec(t *testing.T) {
	codecB := NewCodec(drain.NewCodec(servicegroup.NewCodec(nil)))
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 4}, transportB, codecB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeB.Close(context.Background()) })

	directoryService, err := servicegroup.NewDirectoryService(servicegroup.DirectoryConfig{PublisherNode: "node-b", WatchTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	directoryRef, err := nodeB.CreateService(gsr.ServiceSpec{Service: directoryService})
	if err != nil {
		t.Fatal(err)
	}
	registryService, err := drain.NewVisitorRegistryService(drain.VisitorRegistryConfig{LeaseTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	registryRef, err := nodeB.CreateService(gsr.ServiceSpec{Service: registryService})
	if err != nil {
		t.Fatal(err)
	}
	gateway := &drainGatewayService{}
	gatewayRef, err := nodeB.CreateService(gsr.ServiceSpec{Name: testDrainGatewayName, Service: gateway})
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
	coordinatorRef, err := nodeB.CreateService(gsr.ServiceSpec{Name: DefaultDrainCoordinatorName, Service: coordinatorService})
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
	directory, err := servicegroup.NewClient(nodeB, directoryRef)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := directory.Publish(context.Background(), "match", servicegroup.ServiceSetVersion{}, []gsr.ServiceRef{oldRef}, map[string]string{"mode": "remote"})
	if err != nil {
		t.Fatal(err)
	}

	codecA := NewCodec(drain.NewCodec(servicegroup.NewCodec(nil)))
	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 2}, transportA, codecA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeA.Close(context.Background()) })
	remoteGateway, err := nodeA.ResolveRemote(context.Background(), "node-b", testDrainGatewayName)
	if err != nil {
		t.Fatal(err)
	}
	if remoteGateway != gatewayRef {
		t.Fatalf("ResolveRemote(gateway) = %#v, want %#v", remoteGateway, gatewayRef)
	}
	remoteCoordinator, err := nodeA.ResolveRemote(context.Background(), "node-b", DefaultDrainCoordinatorName)
	if err != nil {
		t.Fatal(err)
	}

	gatewayClient, err := NewDrainClient(nodeA, remoteGateway)
	if err != nil {
		t.Fatal(err)
	}
	request := StartDrainRequest{
		RequestID: "remote-drain",
		Principal: "ops",
		Group:     initial.Name,
		Expected:  initial.Version,
		NextRefs:  []gsr.ServiceRef{nextRef},
		NextTags:  map[string]string{"mode": "remote"},
	}
	operation, err := gatewayClient.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Gateway Start() error = %v", err)
	}
	if operation.Phase != DrainReadyToStop || len(operation.Targets) != 1 || !operation.Targets[0].Guarded {
		t.Fatalf("Gateway Start() = %#v", operation)
	}

	direct, err := NewDrainClient(nodeA, remoteCoordinator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := direct.Get(context.Background(), request.RequestID, request.Principal); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("node-level remote Get() error = %v, want ErrUnauthorized", err)
	}
}
