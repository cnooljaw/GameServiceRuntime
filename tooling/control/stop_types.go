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
	RequestID RequestID
	Target    gsr.ServiceRef
	State     StopTargetState
	Failure   StopFailure
	UpdatedAt time.Time
}

// NodeStopExecutor accepts a bounded local Node Stop task.
type NodeStopExecutor interface {
	Submit(NodeStopTask) error
}

// NodeStopTask identifies one NodeAgent-owned local Runtime.Stop request.
type NodeStopTask struct {
	Agent     gsr.ServiceRef
	RequestID RequestID
	Target    gsr.ServiceRef
	Group     servicegroup.GroupName
	Published servicegroup.ServiceSet
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
