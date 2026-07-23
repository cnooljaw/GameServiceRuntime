package control

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

func TestDrainCoordinatorBeginsAndResolvesNodeStop(t *testing.T) {
	fixture := newDrainFixture(t, []Principal{"ops"})
	discoveryService, err := discovery.NewService(discovery.Config{LeaseTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	discoveryRef, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: discoveryService})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewNodeStopRunner(fixture.runtime, NodeStopRunnerConfig{Directory: fixture.directory, Workers: 1, QueueSize: 1, CallTimeout: time.Second, StopTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })
	config := testNodeAgentConfig(&countingReporter{report: testReport("node-a")})
	config.ObserverNode = "node-a"
	config.Discovery = discoveryRef
	config.StopCoordinator = fixture.coordinator
	config.StopExecutor = runner
	agentService, err := NewNodeAgentService(config)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: agentService})
	if err != nil {
		t.Fatal(err)
	}

	drainOperation, err := fixture.client.Start(context.Background(), fixture.request("drain-stop", "ops"))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if drainOperation.Phase != DrainReadyToStop {
		t.Fatalf("Drain operation = %#v, want ReadyToStop", drainOperation)
	}
	request := BeginStopRequest{RequestID: "drain-stop", Principal: "ops", Targets: []StopTargetRequest{{Target: fixture.old, Agent: agent}}}
	operation, err := fixture.client.BeginStop(context.Background(), request)
	if err != nil {
		t.Fatalf("BeginStop() error = %v", err)
	}
	if (operation.Phase != StopWaiting && operation.Phase != StopCompleted) || len(operation.Targets) != 1 || (operation.Targets[0].State != StopTargetQueued && operation.Targets[0].State != StopTargetStopped) {
		t.Fatalf("BeginStop() = %#v", operation)
	}

	deadline := time.Now().Add(time.Second)
	for operation.Phase != StopCompleted && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		operation, err = fixture.client.ResolveStop(context.Background(), request.RequestID, request.Principal)
		if err != nil {
			t.Fatalf("ResolveStop() error = %v", err)
		}
	}
	if operation.Phase != StopCompleted || operation.Targets[0].State != StopTargetStopped {
		t.Fatalf("final Stop operation = %#v", operation)
	}
	if _, err := fixture.runtime.Call(context.Background(), fixture.old, commandDrainWork, struct{}{}); !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("old service after completed Stop error = %v, want ErrServiceClosed", err)
	}
}

func TestDrainCoordinatorSupersedesStopBeforeRetryWhenDirectoryChanges(t *testing.T) {
	fixture := newDrainFixture(t, []Principal{"ops"})
	discoveryService, err := discovery.NewService(discovery.Config{LeaseTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	discoveryRef, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: discoveryService})
	if err != nil {
		t.Fatal(err)
	}
	executor := &recordingNodeStopExecutor{results: []error{ErrNodeStopQueueFull}}
	config := testNodeAgentConfig(&countingReporter{report: testReport("node-a")})
	config.ObserverNode = "node-a"
	config.Discovery = discoveryRef
	config.StopCoordinator = fixture.coordinator
	config.StopExecutor = executor
	agentService, err := NewNodeAgentService(config)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: agentService})
	if err != nil {
		t.Fatal(err)
	}

	if operation, err := fixture.client.Start(context.Background(), fixture.request("stop-superseded", "ops")); err != nil || operation.Phase != DrainReadyToStop {
		t.Fatalf("Start() = %#v, %v; want ReadyToStop", operation, err)
	}
	request := BeginStopRequest{RequestID: "stop-superseded", Principal: "ops", Targets: []StopTargetRequest{{Target: fixture.old, Agent: agent}}}
	mismatched := request
	mismatched.Targets = []StopTargetRequest{{Target: fixture.next, Agent: agent}}
	if _, err := fixture.client.BeginStop(context.Background(), mismatched); !errors.Is(err, ErrStopTargetMismatch) {
		t.Fatalf("BeginStop(mismatched targets) error = %v, want ErrStopTargetMismatch", err)
	}
	if executor.calls != 0 {
		t.Fatalf("mismatched BeginStop submitted executor %d times", executor.calls)
	}
	operation, err := fixture.client.BeginStop(context.Background(), request)
	if err != nil {
		t.Fatalf("BeginStop() error = %v", err)
	}
	if operation.Phase != StopWaiting || operation.Targets[0].State != StopTargetPending || operation.Targets[0].Failure != StopFailureQueueFull || executor.calls != 1 {
		t.Fatalf("BeginStop() = %#v, executor calls = %d", operation, executor.calls)
	}
	duplicate, err := fixture.client.BeginStop(context.Background(), request)
	if err != nil || duplicate.Phase != operation.Phase || len(duplicate.Targets) != 1 || duplicate.Targets[0] != operation.Targets[0] || executor.calls != 1 {
		t.Fatalf("duplicate BeginStop() = %#v, %v, executor calls = %d", duplicate, err, executor.calls)
	}
	conflicting := request
	conflicting.Targets = []StopTargetRequest{{Target: fixture.old, Agent: gsr.ServiceRef{Node: agent.Node, ID: agent.ID + 1}}}
	if _, err := fixture.client.BeginStop(context.Background(), conflicting); !errors.Is(err, ErrStopRequestConflict) {
		t.Fatalf("BeginStop(conflicting request) error = %v, want ErrStopRequestConflict", err)
	}

	directory, err := servicegroup.NewClient(fixture.runtime, fixture.directory)
	if err != nil {
		t.Fatal(err)
	}
	current := fixture.getDirectory(t)
	if _, err := directory.Publish(context.Background(), current.Name, current.Version, current.Refs, map[string]string{"generation": "manual"}); err != nil {
		t.Fatalf("manual Publish() error = %v", err)
	}
	operation, err = fixture.client.ResolveStop(context.Background(), request.RequestID, request.Principal)
	if err != nil {
		t.Fatalf("ResolveStop() error = %v", err)
	}
	if operation.Phase != StopSuperseded || executor.calls != 1 {
		t.Fatalf("ResolveStop() = %#v, executor calls = %d", operation, executor.calls)
	}
}
