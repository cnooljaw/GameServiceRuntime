package control

import (
	"errors"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestNodeAgentRecoveryConfigurationRequiresCoordinatorAndExecutorTogether(t *testing.T) {
	config := testNodeAgentConfig(&countingReporter{report: testReport("node-b")})
	config.RecoveryCoordinator = gsr.ServiceRef{Node: "node-a", ID: 1}
	if _, err := NewNodeAgentService(config); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewNodeAgentService(coordinator only) error = %v", err)
	}
	config.RecoveryCoordinator = gsr.ServiceRef{}
	config.RecoveryExecutor = &recordingRecoveryExecutor{}
	if _, err := NewNodeAgentService(config); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewNodeAgentService(executor only) error = %v", err)
	}
}

func TestNodeAgentRecoveryRequiresExactCoordinatorAndFencesResults(t *testing.T) {
	executor := &recordingRecoveryExecutor{results: []error{ErrRecoveryQueueFull, nil}}
	agent, serviceContext, coordinator := newRecoveryEnabledNodeAgent(t, executor)
	task := validRecoveryTaskFor(serviceContext.self)

	unauthorized := &recordingContext{source: gsr.ServiceRef{Node: coordinator.Node, ID: coordinator.ID + 1}}
	if err := agent.Handle(unauthorized, gsr.Command{ID: commandBeginRecoveryCreate, Payload: beginRecoveryCreateRequest{Task: task}}); err != nil {
		t.Fatal(err)
	}
	if response := unauthorized.reply.(recoveryReceiptResponse); response.Error != responseUnauthorized || executor.calls != 0 {
		t.Fatalf("unauthorized response = %#v, calls = %d", response, executor.calls)
	}

	first := &recordingContext{source: coordinator}
	if err := agent.Handle(first, gsr.Command{ID: commandBeginRecoveryCreate, Payload: beginRecoveryCreateRequest{Task: task}}); err != nil {
		t.Fatal(err)
	}
	if receipt := recoveryReceiptFromReply(t, first.reply); receipt.State != RecoveryTargetFailed || receipt.Failure != RecoveryFailureQueueFull || executor.calls != 1 {
		t.Fatalf("first receipt = %#v, calls = %d", receipt, executor.calls)
	}

	second := &recordingContext{source: coordinator}
	if err := agent.Handle(second, gsr.Command{ID: commandBeginRecoveryCreate, Payload: beginRecoveryCreateRequest{Task: task}}); err != nil {
		t.Fatal(err)
	}
	if receipt := recoveryReceiptFromReply(t, second.reply); receipt.State != RecoveryTargetCreating || receipt.Failure != RecoveryFailureNone || executor.calls != 2 {
		t.Fatalf("second receipt = %#v, calls = %d", receipt, executor.calls)
	}

	result := recoveryCreateResult{RequestID: task.RequestID, Removed: task.Removed, Blueprint: task.Blueprint, Created: gsr.ServiceRef{Node: serviceContext.self.Node, ID: 99}, State: RecoveryTargetCreated}
	if err := agent.Handle(&recordingContext{source: coordinator}, gsr.Command{ID: commandRecordRecoveryCreate, Payload: result}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Handle(non-local result) error = %v, want ErrUnauthorized", err)
	}
	if err := agent.Handle(&recordingContext{source: gsr.ServiceRef{Node: serviceContext.self.Node}}, gsr.Command{ID: commandRecordRecoveryCreate, Payload: result}); err != nil {
		t.Fatal(err)
	}
	get := &recordingContext{source: coordinator}
	if err := agent.Handle(get, gsr.Command{ID: commandGetRecoveryReceipt, Payload: getRecoveryReceiptRequest{RequestID: task.RequestID, Removed: task.Removed}}); err != nil {
		t.Fatal(err)
	}
	if receipt := recoveryReceiptFromReply(t, get.reply); receipt.State != RecoveryTargetCreated || receipt.Created != result.Created {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func validRecoveryTaskFor(agent gsr.ServiceRef) RecoveryCreateTask {
	return RecoveryCreateTask{Agent: agent, RequestID: "recovery-1", Removed: gsr.ServiceRef{Node: agent.Node, ID: agent.ID + 1}, Blueprint: "battle-v2"}
}

func newRecoveryEnabledNodeAgent(t *testing.T, executor RecoveryExecutor) (*nodeAgent, *nodeAgentServiceContext, gsr.ServiceRef) {
	t.Helper()
	coordinator := gsr.ServiceRef{Node: "node-a", ID: 42}
	config := testNodeAgentConfig(&countingReporter{report: testReport("node-b")})
	config.RecoveryCoordinator = coordinator
	config.RecoveryExecutor = executor
	service, err := NewNodeAgentService(config)
	if err != nil {
		t.Fatal(err)
	}
	agent := service.(*nodeAgent)
	serviceContext := &nodeAgentServiceContext{self: gsr.ServiceRef{Node: "node-b", ID: 7}}
	agent.context = serviceContext
	return agent, serviceContext, coordinator
}

func recoveryReceiptFromReply(t *testing.T, value any) RecoveryReceipt {
	t.Helper()
	response, ok := value.(recoveryReceiptResponse)
	if !ok {
		t.Fatalf("reply = %#v, want recoveryReceiptResponse", value)
	}
	if response.Error != responseOK {
		t.Fatalf("response error = %q", response.Error)
	}
	return response.Receipt
}

type recordingRecoveryExecutor struct {
	results []error
	calls   int
	tasks   []RecoveryCreateTask
}

func (e *recordingRecoveryExecutor) Submit(task RecoveryCreateTask) error {
	e.calls++
	e.tasks = append(e.tasks, task)
	if len(e.results) == 0 {
		return nil
	}
	result := e.results[0]
	e.results = e.results[1:]
	return result
}
