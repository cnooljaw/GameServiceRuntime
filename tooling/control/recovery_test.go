package control

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
)

func TestDrainCoordinatorCreatesRecoveryThenRequiresConfirmToPublish(t *testing.T) {
	fixture := newDrainFixture(t, []Principal{"ops"})
	discoveryService, err := discovery.NewService(discovery.Config{LeaseTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	discoveryRef, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: discoveryService})
	if err != nil {
		t.Fatal(err)
	}
	stopRunner, err := NewNodeStopRunner(fixture.runtime, NodeStopRunnerConfig{Directory: fixture.directory, Workers: 1, QueueSize: 1, CallTimeout: time.Second, StopTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stopRunner.Close(context.Background()) })
	registry, err := NewMapBlueprintRegistry(map[BlueprintID]BlueprintFactory{
		"match-v2": func() (gsr.ServiceSpec, error) { return gsr.ServiceSpec{Service: drainWorkService{}}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	recoveryRunner, err := NewRecoveryRunner(fixture.runtime, RecoveryRunnerConfig{Registry: registry, Workers: 1, QueueSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recoveryRunner.Close(context.Background()) })
	config := testNodeAgentConfig(&countingReporter{report: testReport("node-a")})
	config.ObserverNode = "node-a"
	config.Discovery = discoveryRef
	config.StopCoordinator, config.StopExecutor = fixture.coordinator, stopRunner
	config.RecoveryCoordinator, config.RecoveryExecutor = fixture.coordinator, recoveryRunner
	agentService, err := NewNodeAgentService(config)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: agentService})
	if err != nil {
		t.Fatal(err)
	}

	const requestID RequestID = "recover-flow"
	if operation, err := fixture.client.Start(context.Background(), fixture.request(requestID, "ops")); err != nil || operation.Phase != DrainReadyToStop {
		t.Fatalf("Start() = %#v, %v", operation, err)
	}
	stop, err := fixture.client.BeginStop(context.Background(), BeginStopRequest{RequestID: requestID, Principal: "ops", Targets: []StopTargetRequest{{Target: fixture.old, Agent: agent}}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for stop.Phase != StopCompleted && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		stop, err = fixture.client.ResolveStop(context.Background(), requestID, "ops")
		if err != nil {
			t.Fatal(err)
		}
	}
	if stop.Phase != StopCompleted {
		t.Fatalf("Stop operation = %#v", stop)
	}

	expected := fixture.getDirectory(t)
	request := BeginRecoveryRequest{RequestID: requestID, Principal: "ops", Group: expected.Name, Expected: expected, Targets: []RecoveryTargetRequest{{Removed: fixture.old, Agent: agent, Blueprint: "match-v2"}}}
	operation, err := fixture.client.BeginRecovery(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Phase != RecoveryCreating {
		t.Fatalf("BeginRecovery() = %#v, want creating", operation)
	}
	if _, err := fixture.client.ConfirmRecovery(context.Background(), requestID, "ops"); !errors.Is(err, ErrRecoveryNotReady) {
		t.Fatalf("Confirm before receipt error = %v, want ErrRecoveryNotReady", err)
	}
	for operation.Phase == RecoveryCreating && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		operation, err = fixture.client.ResolveRecovery(context.Background(), requestID, "ops")
		if err != nil {
			t.Fatal(err)
		}
	}
	if operation.Phase != RecoveryAwaitingConfirmation || len(operation.Targets) != 1 || operation.Targets[0].Created == (gsr.ServiceRef{}) {
		t.Fatalf("resolved recovery = %#v", operation)
	}
	beforeConfirm := fixture.getDirectory(t)
	if len(beforeConfirm.Refs) != len(expected.Refs) {
		t.Fatalf("created ref published before confirm: %#v", beforeConfirm)
	}
	operation, err = fixture.client.ConfirmRecovery(context.Background(), requestID, "ops")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Phase != RecoveryCompleted || operation.Targets[0].State != RecoveryTargetPublished {
		t.Fatalf("ConfirmRecovery() = %#v", operation)
	}
	published := fixture.getDirectory(t)
	if len(published.Refs) != len(expected.Refs)+1 || containsRecoveryRef(published.Refs, fixture.old) || !containsRecoveryRef(published.Refs, operation.Targets[0].Created) {
		t.Fatalf("published set = %#v", published)
	}
}
