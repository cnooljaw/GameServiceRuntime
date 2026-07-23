package control

import (
	"errors"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

func TestNodeAgentStopConfigurationRequiresCoordinatorAndExecutorTogether(t *testing.T) {
	config := testNodeAgentConfig(&countingReporter{report: testReport("node-b")})
	config.StopCoordinator = gsr.ServiceRef{Node: "node-a", ID: 1}
	if _, err := NewNodeAgentService(config); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewNodeAgentService(coordinator only) error = %v, want ErrInvalidConfig", err)
	}
	config.StopCoordinator = gsr.ServiceRef{}
	config.StopExecutor = &recordingNodeStopExecutor{}
	if _, err := NewNodeAgentService(config); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewNodeAgentService(executor only) error = %v, want ErrInvalidConfig", err)
	}
}

func TestNodeAgentRejectsDisabledAndInvalidStopTasksWithoutSubmitting(t *testing.T) {
	service, err := NewNodeAgentService(testNodeAgentConfig(&countingReporter{report: testReport("node-b")}))
	if err != nil {
		t.Fatal(err)
	}
	disabled := service.(*nodeAgent)
	disabled.context = &nodeAgentServiceContext{self: gsr.ServiceRef{Node: "node-b", ID: 7}}
	disabledContext := &recordingContext{source: gsr.ServiceRef{Node: "node-a", ID: 1}}
	if err := disabled.Handle(disabledContext, gsr.Command{ID: commandBeginNodeStop, Payload: beginNodeStopRequest{}}); err != nil {
		t.Fatalf("Handle(disabled BeginNodeStop) error = %v", err)
	}
	if response := disabledContext.reply.(nodeStopReceiptResponse); response.Error != responseStopDisabled {
		t.Fatalf("disabled response = %#v", response)
	}

	executor := &recordingNodeStopExecutor{}
	agent, serviceContext, coordinator := newStopEnabledNodeAgent(t, executor)
	for _, task := range []NodeStopTask{
		func() NodeStopTask {
			task := validNodeStopTaskFor(serviceContext.self)
			task.Target.Node = "node-c"
			return task
		}(),
		func() NodeStopTask {
			task := validNodeStopTaskFor(serviceContext.self)
			task.Target = serviceContext.self
			return task
		}(),
	} {
		commandContext := &recordingContext{source: coordinator}
		if err := agent.Handle(commandContext, gsr.Command{ID: commandBeginNodeStop, Payload: beginNodeStopRequest{Task: task}}); err != nil {
			t.Fatalf("Handle(invalid BeginNodeStop) error = %v", err)
		}
		if response := commandContext.reply.(nodeStopReceiptResponse); response.Error != responseInvalidStopRequest {
			t.Fatalf("invalid response = %#v", response)
		}
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
}

func TestNodeAgentStopRequiresExactCoordinatorAndRetriesPendingReceipt(t *testing.T) {
	executor := &recordingNodeStopExecutor{results: []error{ErrNodeStopQueueFull, nil}}
	agent, serviceContext, coordinator := newStopEnabledNodeAgent(t, executor)
	task := validNodeStopTaskFor(serviceContext.self)

	unauthorized := &recordingContext{source: gsr.ServiceRef{Node: coordinator.Node, ID: coordinator.ID + 1}}
	if err := agent.Handle(unauthorized, gsr.Command{ID: commandBeginNodeStop, Payload: beginNodeStopRequest{Task: task}}); err != nil {
		t.Fatalf("Handle(unauthorized BeginNodeStop) error = %v", err)
	}
	if response := unauthorized.reply.(nodeStopReceiptResponse); response.Error != responseUnauthorized || executor.calls != 0 {
		t.Fatalf("unauthorized response = %#v, executor calls = %d", response, executor.calls)
	}

	firstContext := &recordingContext{source: coordinator}
	if err := agent.Handle(firstContext, gsr.Command{ID: commandBeginNodeStop, Payload: beginNodeStopRequest{Task: task}}); err != nil {
		t.Fatalf("Handle(first BeginNodeStop) error = %v", err)
	}
	first := nodeStopReceiptFromReply(t, firstContext.reply)
	if first.State != StopTargetPending || first.Failure != StopFailureQueueFull || executor.calls != 1 {
		t.Fatalf("first = %#v, executor calls = %d", first, executor.calls)
	}

	secondContext := &recordingContext{source: coordinator}
	if err := agent.Handle(secondContext, gsr.Command{ID: commandBeginNodeStop, Payload: beginNodeStopRequest{Task: task}}); err != nil {
		t.Fatalf("Handle(second BeginNodeStop) error = %v", err)
	}
	second := nodeStopReceiptFromReply(t, secondContext.reply)
	if second.State != StopTargetQueued || second.Failure != StopFailureNone || executor.calls != 2 {
		t.Fatalf("second = %#v, executor calls = %d", second, executor.calls)
	}

	duplicateContext := &recordingContext{source: coordinator}
	if err := agent.Handle(duplicateContext, gsr.Command{ID: commandBeginNodeStop, Payload: beginNodeStopRequest{Task: task}}); err != nil {
		t.Fatalf("Handle(duplicate BeginNodeStop) error = %v", err)
	}
	if duplicate := nodeStopReceiptFromReply(t, duplicateContext.reply); duplicate != second || executor.calls != 2 {
		t.Fatalf("duplicate = %#v, executor calls = %d", duplicate, executor.calls)
	}
}

func TestNodeAgentAppliesOnlyLocalRunnerResultAndReturnsReceipt(t *testing.T) {
	executor := &recordingNodeStopExecutor{}
	agent, serviceContext, coordinator := newStopEnabledNodeAgent(t, executor)
	task := validNodeStopTaskFor(serviceContext.self)
	begin := &recordingContext{source: coordinator}
	if err := agent.Handle(begin, gsr.Command{ID: commandBeginNodeStop, Payload: beginNodeStopRequest{Task: task}}); err != nil {
		t.Fatalf("Handle(BeginNodeStop) error = %v", err)
	}
	if receipt := nodeStopReceiptFromReply(t, begin.reply); receipt.State != StopTargetQueued {
		t.Fatalf("queued receipt = %#v", receipt)
	}

	result := nodeStopResult{RequestID: task.RequestID, Target: task.Target, State: StopTargetStopped}
	if err := agent.Handle(&recordingContext{source: coordinator}, gsr.Command{ID: commandRecordNodeStopResult, Payload: result}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Handle(non-local result) error = %v, want ErrUnauthorized", err)
	}
	getBefore := &recordingContext{source: coordinator}
	if err := agent.Handle(getBefore, gsr.Command{ID: commandGetNodeStopReceipt, Payload: getNodeStopReceiptRequest{RequestID: task.RequestID, Target: task.Target}}); err != nil {
		t.Fatalf("Handle(GetNodeStopReceipt before result) error = %v", err)
	}
	if receipt := nodeStopReceiptFromReply(t, getBefore.reply); receipt.State != StopTargetQueued {
		t.Fatalf("receipt after rejected result = %#v", receipt)
	}

	if err := agent.Handle(&recordingContext{source: gsr.ServiceRef{Node: serviceContext.self.Node}}, gsr.Command{ID: commandRecordNodeStopResult, Payload: result}); err != nil {
		t.Fatalf("Handle(local result) error = %v", err)
	}
	get := &recordingContext{source: coordinator}
	if err := agent.Handle(get, gsr.Command{ID: commandGetNodeStopReceipt, Payload: getNodeStopReceiptRequest{RequestID: task.RequestID, Target: task.Target}}); err != nil {
		t.Fatalf("Handle(GetNodeStopReceipt) error = %v", err)
	}
	if receipt := nodeStopReceiptFromReply(t, get.reply); receipt.State != StopTargetStopped || receipt.Failure != StopFailureNone || receipt.UpdatedAt.IsZero() {
		t.Fatalf("stopped receipt = %#v", receipt)
	}
}

func validNodeStopTaskFor(agent gsr.ServiceRef) NodeStopTask {
	return NodeStopTask{
		Agent:     agent,
		RequestID: "stop-1",
		Target:    gsr.ServiceRef{Node: agent.Node, ID: agent.ID + 1},
		Group:     "match",
		Published: servicegroup.ServiceSet{Name: "match", Version: servicegroup.ServiceSetVersion{AuthorityEpoch: 1, Revision: 2}, Refs: []gsr.ServiceRef{}, Tags: map[string]string{"generation": "next"}},
	}
}

func newStopEnabledNodeAgent(t *testing.T, executor NodeStopExecutor) (*nodeAgent, *nodeAgentServiceContext, gsr.ServiceRef) {
	t.Helper()
	coordinator := gsr.ServiceRef{Node: "node-a", ID: 41}
	config := testNodeAgentConfig(&countingReporter{report: testReport("node-b")})
	config.StopCoordinator = coordinator
	config.StopExecutor = executor
	service, err := NewNodeAgentService(config)
	if err != nil {
		t.Fatal(err)
	}
	agent := service.(*nodeAgent)
	serviceContext := &nodeAgentServiceContext{self: gsr.ServiceRef{Node: "node-b", ID: 7}}
	agent.context = serviceContext
	return agent, serviceContext, coordinator
}

func nodeStopReceiptFromReply(t *testing.T, value any) NodeStopReceipt {
	t.Helper()
	response, ok := value.(nodeStopReceiptResponse)
	if !ok {
		t.Fatalf("reply = %#v, want nodeStopReceiptResponse", value)
	}
	if response.Error != responseOK {
		t.Fatalf("response error = %q", response.Error)
	}
	return response.Receipt
}

type recordingNodeStopExecutor struct {
	results []error
	calls   int
	tasks   []NodeStopTask
}

func (e *recordingNodeStopExecutor) Submit(task NodeStopTask) error {
	e.calls++
	e.tasks = append(e.tasks, task)
	if len(e.results) == 0 {
		return nil
	}
	result := e.results[0]
	e.results = e.results[1:]
	return result
}
