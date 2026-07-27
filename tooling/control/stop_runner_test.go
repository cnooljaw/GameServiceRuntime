package control

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

const commandStopResultSink gsr.CommandID = 0x7f250401

func TestNodeStopRunnerDoesNotStopWhenDirectoryChanged(t *testing.T) {
	fixture := newNodeStopRunnerFixture(t, NodeStopRunnerConfig{Workers: 1, QueueSize: 1, CallTimeout: time.Second, StopTimeout: time.Second})
	changed, err := fixture.directory.Publish(context.Background(), fixture.published.Name, fixture.published.Version, []gsr.ServiceRef{fixture.replacement}, map[string]string{"generation": "next"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Version.Revision != fixture.published.Version.Revision+1 {
		t.Fatalf("changed version = %#v", changed.Version)
	}
	if err := fixture.runner.Submit(fixture.task()); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	result := fixture.awaitResult(t)
	if result.State != StopTargetSuperseded || result.Failure != StopFailureNone {
		t.Fatalf("result = %#v", result)
	}
	if err := fixture.runtime.Send(fixture.target, commandDrainWork, struct{}{}); err != nil {
		t.Fatalf("Directory-changed task stopped target: %v", err)
	}
}

func TestNodeStopRunnerStopsAndCloseWaitsForStartedStop(t *testing.T) {
	fixture := newNodeStopRunnerFixture(t, NodeStopRunnerConfig{Workers: 1, QueueSize: 1, CallTimeout: time.Second, StopTimeout: time.Second})
	blocking := &blockingStopService{entered: make(chan struct{}), release: make(chan struct{})}
	target, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: blocking})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.Submit(fixture.taskFor(target)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocking.entered:
	case <-time.After(time.Second):
		t.Fatal("Runtime.Stop did not enter target Stop")
	}
	closed := make(chan error, 1)
	go func() { closed <- fixture.runner.Close(context.Background()) }()
	select {
	case err := <-closed:
		t.Fatalf("Close() returned before target Stop: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(blocking.release)
	if err := <-closed; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := fixture.runtime.Call(context.Background(), target, commandDrainWork, struct{}{}); !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("target after Stop error = %v, want ErrServiceClosed", err)
	}
}

func TestNodeStopRunnerTreatsAlreadyClosedTargetAsStopped(t *testing.T) {
	fixture := newNodeStopRunnerFixture(t, NodeStopRunnerConfig{Workers: 1, QueueSize: 1, CallTimeout: time.Second, StopTimeout: time.Second})
	if err := fixture.runtime.Stop(context.Background(), fixture.target); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.Submit(fixture.task()); err != nil {
		t.Fatal(err)
	}
	result := fixture.awaitResult(t)
	if result.State != StopTargetStopped || result.Failure != StopFailureNone {
		t.Fatalf("result = %#v", result)
	}
}

func TestNodeStopRunnerReportsRuntimeStopFailure(t *testing.T) {
	fixture := newNodeStopRunnerFixture(t, NodeStopRunnerConfig{Workers: 1, QueueSize: 1, CallTimeout: time.Second, StopTimeout: 20 * time.Millisecond})
	blocking := &blockingStopService{entered: make(chan struct{}), release: make(chan struct{})}
	target, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: blocking})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.Submit(fixture.taskFor(target)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocking.entered:
	case <-time.After(time.Second):
		t.Fatal("Runtime.Stop did not enter target Stop")
	}
	result := fixture.awaitResult(t)
	if result.State != StopTargetFailed || result.Failure != StopFailureRuntimeStop {
		t.Fatalf("result = %#v", result)
	}
	close(blocking.release)
}

func TestNodeStopRunnerRejectsInvalidFullAndClosedSubmissions(t *testing.T) {
	fixture := newNodeStopRunnerFixture(t, NodeStopRunnerConfig{Workers: 1, QueueSize: 1, CallTimeout: time.Second, StopTimeout: time.Second})
	if err := fixture.runner.Submit(NodeStopTask{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Submit(invalid) error = %v, want ErrInvalidConfig", err)
	}
	blocking := &blockingStopService{entered: make(chan struct{}), release: make(chan struct{})}
	first, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: blocking})
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: drainWorkService{}})
	if err != nil {
		t.Fatal(err)
	}
	third, err := fixture.runtime.CreateService(gsr.ServiceSpec{Service: drainWorkService{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.Submit(fixture.taskFor(first)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocking.entered:
	case <-time.After(time.Second):
		t.Fatal("first Stop did not start")
	}
	if err := fixture.runner.Submit(fixture.taskFor(second)); err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}
	if err := fixture.runner.Submit(fixture.taskFor(third)); !errors.Is(err, ErrNodeStopQueueFull) {
		t.Fatalf("Submit(full) error = %v, want ErrNodeStopQueueFull", err)
	}
	close(blocking.release)
	fixture.awaitResult(t)
	fixture.awaitResult(t)
	if err := fixture.runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.Submit(fixture.task()); !errors.Is(err, ErrNodeStopRunnerClosed) {
		t.Fatalf("Submit(closed) error = %v, want ErrNodeStopRunnerClosed", err)
	}
}

func TestNodeStopRunnerRejectsInvalidConfigAndLeavesDirectoryUnavailablePending(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	for _, config := range []NodeStopRunnerConfig{
		{Directory: gsr.ServiceRef{Node: "node-a", ID: 1}, QueueSize: 1},
		{Directory: gsr.ServiceRef{Node: "node-a", ID: 1}, Workers: 1},
		{Directory: gsr.ServiceRef{Node: "node-a", ID: 1}, Workers: 1, QueueSize: 1, CallTimeout: -time.Nanosecond},
		{Directory: gsr.ServiceRef{Node: "node-a", ID: 1}, Workers: 1, QueueSize: 1, StopTimeout: -time.Nanosecond},
	} {
		if _, err := NewNodeStopRunner(runtime, config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewNodeStopRunner(%#v) error = %v, want ErrInvalidConfig", config, err)
		}
	}

	fixture := newNodeStopRunnerFixture(t, NodeStopRunnerConfig{Workers: 1, QueueSize: 1, CallTimeout: time.Second, StopTimeout: time.Second})
	if err := fixture.runtime.Stop(context.Background(), fixture.directoryRef); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.Submit(fixture.task()); err != nil {
		t.Fatal(err)
	}
	result := fixture.awaitResult(t)
	if result.State != StopTargetPending || result.Failure != StopFailureDirectoryUnavailable {
		t.Fatalf("result = %#v", result)
	}
	if err := fixture.runtime.Send(fixture.target, commandDrainWork, struct{}{}); err != nil {
		t.Fatalf("Directory-unavailable task stopped target: %v", err)
	}
}

type nodeStopRunnerFixture struct {
	runtime      *gsr.Runtime
	directory    *servicegroup.Client
	directoryRef gsr.ServiceRef
	runner       *NodeStopRunner
	published    servicegroup.ServiceSet
	target       gsr.ServiceRef
	replacement  gsr.ServiceRef
	sink         *stopResultSink
	sinkRef      gsr.ServiceRef
}

func newNodeStopRunnerFixture(t *testing.T, config NodeStopRunnerConfig) nodeStopRunnerFixture {
	t.Helper()
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 3})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	directoryService, err := servicegroup.NewDirectoryService(servicegroup.DirectoryConfig{PublisherNode: "node-a", WatchTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	directoryRef, err := runtime.CreateService(gsr.ServiceSpec{Service: directoryService})
	if err != nil {
		t.Fatal(err)
	}
	target, err := runtime.CreateService(gsr.ServiceSpec{Service: drainWorkService{}})
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := runtime.CreateService(gsr.ServiceSpec{Service: drainWorkService{}})
	if err != nil {
		t.Fatal(err)
	}
	directory, err := servicegroup.NewClient(runtime, directoryRef)
	if err != nil {
		t.Fatal(err)
	}
	published, err := directory.Publish(context.Background(), "match", servicegroup.ServiceSetVersion{}, []gsr.ServiceRef{replacement}, map[string]string{"generation": "current"})
	if err != nil {
		t.Fatal(err)
	}
	sink := &stopResultSink{results: make(chan nodeStopResult, 8)}
	sinkRef, err := runtime.CreateService(gsr.ServiceSpec{Service: sink})
	if err != nil {
		t.Fatal(err)
	}
	config.Directory = directoryRef
	runner, err := NewNodeStopRunner(runtime, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })
	return nodeStopRunnerFixture{runtime: runtime, directory: directory, directoryRef: directoryRef, runner: runner, published: published, target: target, replacement: replacement, sink: sink, sinkRef: sinkRef}
}

func (f nodeStopRunnerFixture) task() NodeStopTask { return f.taskFor(f.target) }

func (f nodeStopRunnerFixture) taskFor(target gsr.ServiceRef) NodeStopTask {
	return NodeStopTask{Agent: f.sinkRef, RequestID: "stop-1", Target: target, Group: f.published.Name, Published: f.published}
}

func (f nodeStopRunnerFixture) awaitResult(t *testing.T) nodeStopResult {
	t.Helper()
	select {
	case result := <-f.sink.results:
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for NodeStopRunner result")
		return nodeStopResult{}
	}
}

type stopResultSink struct{ results chan nodeStopResult }

func (*stopResultSink) Init(gsr.ServiceContext) error { return nil }
func (s *stopResultSink) Handle(_ gsr.CommandContext, command gsr.Command) error {
	result, ok := command.Payload.(nodeStopResult)
	if !ok || command.ID != commandRecordNodeStopResult {
		return gsr.ErrUnknownCommand
	}
	s.results <- result
	return nil
}
func (*stopResultSink) Stop(context.Context) error { return nil }
func (*stopResultSink) Close() error               { return nil }

type blockingStopService struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (*blockingStopService) Init(gsr.ServiceContext) error { return nil }
func (*blockingStopService) Handle(context gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandDrainWork {
		return gsr.ErrUnknownCommand
	}
	return context.Reply("worked")
}
func (s *blockingStopService) Stop(context.Context) error {
	s.calls.Add(1)
	close(s.entered)
	<-s.release
	return nil
}
func (*blockingStopService) Close() error { return nil }
