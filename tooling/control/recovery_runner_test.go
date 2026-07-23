package control

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestRecoveryRunnerCreatesReplacementAndReportsResult(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	sink := &recoveryResultSink{results: make(chan recoveryCreateResult, 1)}
	agent, err := runtime.CreateService(gsr.ServiceSpec{Service: sink})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewMapBlueprintRegistry(map[BlueprintID]BlueprintFactory{
		"battle-v2": func() (gsr.ServiceSpec, error) { return gsr.ServiceSpec{Service: &recoveryWorkService{}}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewRecoveryRunner(runtime, RecoveryRunnerConfig{Registry: registry, Workers: 1, QueueSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })
	task := RecoveryCreateTask{Agent: agent, RequestID: "recovery-1", Removed: gsr.ServiceRef{Node: "node-a", ID: 99}, Blueprint: "battle-v2"}
	if err := runner.Submit(task); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-sink.results:
		if result.State != RecoveryTargetCreated || result.Failure != RecoveryFailureNone || result.Created.Node != "node-a" || result.Created.ID == 0 {
			t.Fatalf("result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recovery result")
	}
}

func TestRecoveryRunnerClassifiesFailuresAndCloseWaitsForBuild(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	sink := &recoveryResultSink{results: make(chan recoveryCreateResult, 2)}
	agent, err := runtime.CreateService(gsr.ServiceSpec{Service: sink})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	registry := &blockingBlueprintRegistry{entered: entered, release: release}
	runner, err := NewRecoveryRunner(runtime, RecoveryRunnerConfig{Registry: registry, Workers: 1, QueueSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	task := RecoveryCreateTask{Agent: agent, RequestID: "recovery-1", Removed: gsr.ServiceRef{Node: "node-a", ID: agent.ID + 1}, Blueprint: "missing"}
	if err := runner.Submit(task); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("runner did not enter Build")
	}
	closed := make(chan error, 1)
	go func() { closed <- runner.Close(context.Background()) }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned before Build completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-sink.results:
		if result.State != RecoveryTargetFailed || result.Failure != RecoveryFailureBlueprintUnavailable {
			t.Fatalf("result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failure result")
	}
	if err := runner.Submit(task); !errors.Is(err, ErrRecoveryRunnerClosed) {
		t.Fatalf("Submit after Close error = %v, want ErrRecoveryRunnerClosed", err)
	}
}

func TestRecoveryRunnerRejectsInvalidConfigAndFullQueue(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	for _, config := range []RecoveryRunnerConfig{
		{Workers: 1, QueueSize: 1},
		{Registry: &blockingBlueprintRegistry{}, QueueSize: 1},
		{Registry: &blockingBlueprintRegistry{}, Workers: 1},
	} {
		if _, err := NewRecoveryRunner(runtime, config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewRecoveryRunner(%#v) error = %v", config, err)
		}
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	runner, err := NewRecoveryRunner(runtime, RecoveryRunnerConfig{Registry: &blockingBlueprintRegistry{entered: entered, release: release}, Workers: 1, QueueSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close(release); _ = runner.Close(context.Background()) })
	agent := gsr.ServiceRef{Node: "node-a", ID: 1}
	first := RecoveryCreateTask{Agent: agent, RequestID: "r-1", Removed: gsr.ServiceRef{Node: "node-a", ID: 2}, Blueprint: "b"}
	second := RecoveryCreateTask{Agent: agent, RequestID: "r-2", Removed: gsr.ServiceRef{Node: "node-a", ID: 3}, Blueprint: "b"}
	third := RecoveryCreateTask{Agent: agent, RequestID: "r-3", Removed: gsr.ServiceRef{Node: "node-a", ID: 4}, Blueprint: "b"}
	if err := runner.Submit(first); err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := runner.Submit(second); err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(third); !errors.Is(err, ErrRecoveryQueueFull) {
		t.Fatalf("Submit full error = %v, want ErrRecoveryQueueFull", err)
	}
}

type recoveryResultSink struct{ results chan recoveryCreateResult }

func (*recoveryResultSink) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandRecordRecoveryCreate}
}
func (*recoveryResultSink) Init(gsr.ServiceContext) error { return nil }
func (s *recoveryResultSink) Handle(_ gsr.CommandContext, command gsr.Command) error {
	result, ok := command.Payload.(recoveryCreateResult)
	if !ok || command.ID != commandRecordRecoveryCreate {
		return gsr.ErrCommandNotRegistered
	}
	s.results <- result
	return nil
}
func (*recoveryResultSink) Stop(context.Context) error { return nil }
func (*recoveryResultSink) Close() error               { return nil }

type recoveryWorkService struct{ starts atomic.Int32 }

const recoveryRunnerWorkCommand gsr.CommandID = 0x7f250501

func (*recoveryWorkService) Commands() []gsr.CommandID {
	return []gsr.CommandID{recoveryRunnerWorkCommand}
}
func (*recoveryWorkService) Init(gsr.ServiceContext) error { return nil }
func (*recoveryWorkService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	if command.ID != recoveryRunnerWorkCommand {
		return gsr.ErrCommandNotRegistered
	}
	return nil
}
func (*recoveryWorkService) Stop(context.Context) error { return nil }
func (*recoveryWorkService) Close() error               { return nil }

type blockingBlueprintRegistry struct {
	entered chan struct{}
	release chan struct{}
}

func (r *blockingBlueprintRegistry) Build(BlueprintID) (gsr.ServiceSpec, error) {
	if r.entered != nil {
		close(r.entered)
	}
	if r.release != nil {
		<-r.release
	}
	return gsr.ServiceSpec{}, errors.New("missing")
}
