package control

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

// StopTargetState describes one target's local Node Stop execution state.
type StopTargetState string

const (
	// StopTargetPending means the target has not yet been accepted by a local runner.
	StopTargetPending StopTargetState = "pending"
	// StopTargetQueued means the local runner accepted the target and has not yet reported a conclusion.
	StopTargetQueued StopTargetState = "queued"
	// StopTargetStopped means Runtime.Stop completed or the target was already closed.
	StopTargetStopped StopTargetState = "stopped"
	// StopTargetSuperseded means Directory no longer matches the frozen published ServiceSet.
	StopTargetSuperseded StopTargetState = "superseded"
	// StopTargetFailed means the target reached a non-retryable local execution failure.
	StopTargetFailed StopTargetState = "failed"
)

// StopFailure classifies the reason a target did not reach the normal stopped state.
type StopFailure string

const (
	// StopFailureNone means the target has no failure classification.
	StopFailureNone StopFailure = ""
	// StopFailureQueueFull means the bounded Node Stop queue had no capacity.
	StopFailureQueueFull StopFailure = "queue_full"
	// StopFailureRunnerClosed means the Node Stop runner was already closed.
	StopFailureRunnerClosed StopFailure = "runner_closed"
	// StopFailureDirectoryUnavailable means Directory could not be read before Runtime.Stop.
	StopFailureDirectoryUnavailable StopFailure = "directory_unavailable"
	// StopFailureRuntimeStop means Runtime.Stop returned an error other than an already-closed target.
	StopFailureRuntimeStop StopFailure = "runtime_stop"
)

// NodeStopReceipt is the Mailbox-owned execution fact for one local Stop target.
type NodeStopReceipt struct {
	RequestID RequestID       `json:"request_id"`
	Target    gsr.ServiceRef  `json:"target"`
	State     StopTargetState `json:"state"`
	Failure   StopFailure     `json:"failure"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// StopTargetRequest pairs one drained Service with the NodeAgent that owns its local Stop receipt.
type StopTargetRequest struct {
	Target gsr.ServiceRef `json:"target"`
	Agent  gsr.ServiceRef `json:"agent"`
}

// StopTarget is the Coordinator-owned state for one requested Stop target.
type StopTarget struct {
	Target  gsr.ServiceRef  `json:"target"`
	Agent   gsr.ServiceRef  `json:"agent"`
	State   StopTargetState `json:"state"`
	Failure StopFailure     `json:"failure"`
}

// StopPhase describes the durable in-memory conclusion of one controlled Stop operation.
type StopPhase string

const (
	// StopDispatching means the Coordinator is submitting the frozen target set to NodeAgents.
	StopDispatching StopPhase = "dispatching"
	// StopWaiting means at least one target is pending or queued for an explicit ResolveStop.
	StopWaiting StopPhase = "waiting"
	// StopCompleted means every target has reached the stopped state.
	StopCompleted StopPhase = "completed"
	// StopFailed means every target is terminal and at least one target failed.
	StopFailed StopPhase = "failed"
	// StopSuperseded means Directory or a target receipt invalidated the frozen publish.
	StopSuperseded StopPhase = "superseded"
)

// StopOperation is an independent Coordinator-owned snapshot of one authorized controlled Stop.
type StopOperation struct {
	RequestID RequestID               `json:"request_id"`
	Principal Principal               `json:"principal"`
	Group     servicegroup.GroupName  `json:"group"`
	Published servicegroup.ServiceSet `json:"published"`
	Targets   []StopTarget            `json:"targets"`
	Phase     StopPhase               `json:"phase"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// BeginStopRequest specifies the complete target-to-NodeAgent pairing for one ReadyToStop Drain operation.
type BeginStopRequest struct {
	RequestID RequestID           `json:"request_id"`
	Principal Principal           `json:"principal"`
	Targets   []StopTargetRequest `json:"targets"`
}

// NodeStopExecutor accepts a bounded local Node Stop task.
type NodeStopExecutor interface {
	Submit(NodeStopTask) error
}

// NodeStopTask identifies one NodeAgent-owned local Runtime.Stop request.
type NodeStopTask struct {
	Agent     gsr.ServiceRef          `json:"agent"`
	RequestID RequestID               `json:"request_id"`
	Target    gsr.ServiceRef          `json:"target"`
	Group     servicegroup.GroupName  `json:"group"`
	Published servicegroup.ServiceSet `json:"published"`
}

// NodeStopRuntime is the narrow Runtime capability required by NodeStopRunner.
type NodeStopRuntime interface {
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
	Send(gsr.ServiceRef, gsr.CommandID, any) error
	Stop(context.Context, gsr.ServiceRef) error
}

// NodeStopRunnerConfig configures one composition-root-owned Node Stop worker pool.
type NodeStopRunnerConfig struct {
	Directory   gsr.ServiceRef
	Workers     int
	QueueSize   int
	CallTimeout time.Duration
	StopTimeout time.Duration
}

// nodeStopResult is the private Runner-to-NodeAgent result payload.
//
// It must only be delivered through commandRecordNodeStopResult on the local Runtime.
type nodeStopResult struct {
	RequestID RequestID
	Target    gsr.ServiceRef
	State     StopTargetState
	Failure   StopFailure
}
