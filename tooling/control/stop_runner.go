package control

import (
	"context"
	"errors"
	"sync"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

// NodeStopRunner owns the fixed-size external worker pool that performs Runtime.Stop outside Service handlers.
type NodeStopRunner struct {
	runtime   NodeStopRuntime
	directory *servicegroup.Client
	config    NodeStopRunnerConfig
	context   context.Context
	cancel    context.CancelFunc
	jobs      chan NodeStopTask
	done      chan struct{}

	mu      sync.Mutex
	closed  bool
	workers int
}

// NewNodeStopRunner creates and starts one bounded Node Stop worker pool.
func NewNodeStopRunner(runtime NodeStopRuntime, config NodeStopRunnerConfig) (*NodeStopRunner, error) {
	if isNil(runtime) || !validNodeStopRunnerConfig(config) {
		return nil, ErrInvalidConfig
	}
	if config.CallTimeout == 0 {
		config.CallTimeout = defaultCallTimeout
	}
	if config.StopTimeout == 0 {
		config.StopTimeout = defaultCallTimeout
	}
	directory, err := servicegroup.NewClient(runtime, config.Directory)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	workerContext, cancel := context.WithCancel(context.Background())
	runner := &NodeStopRunner{
		runtime: runtime, directory: directory, config: config, context: workerContext, cancel: cancel,
		jobs: make(chan NodeStopTask, config.QueueSize), done: make(chan struct{}), workers: config.Workers,
	}
	for range config.Workers {
		go runner.runWorker()
	}
	return runner, nil
}

// Submit queues one frozen Node Stop task without blocking when the queue is full.
func (r *NodeStopRunner) Submit(task NodeStopTask) error {
	if !validNodeStopTask(task) {
		return ErrInvalidConfig
	}
	task.Published = cloneDrainServiceSet(task.Published)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrNodeStopRunnerClosed
	}
	select {
	case r.jobs <- task:
		return nil
	default:
		return ErrNodeStopQueueFull
	}
}

// Close rejects new tasks, cancels unstarted tasks, and waits for started Runtime.Stop calls to return.
func (r *NodeStopRunner) Close(ctx context.Context) error {
	if isNil(ctx) {
		return ErrInvalidConfig
	}
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		r.cancel()
	}
	r.mu.Unlock()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (r *NodeStopRunner) runWorker() {
	defer r.workerDone()
	for {
		select {
		case <-r.context.Done():
			r.cancelPending()
			return
		case task := <-r.jobs:
			if r.context.Err() != nil {
				r.sendResult(task, StopTargetFailed, StopFailureRunnerClosed)
				continue
			}
			r.execute(task)
		}
	}
}

func (r *NodeStopRunner) workerDone() {
	r.mu.Lock()
	r.workers--
	last := r.workers == 0
	r.mu.Unlock()
	if last {
		close(r.done)
	}
}

func (r *NodeStopRunner) cancelPending() {
	for {
		select {
		case task := <-r.jobs:
			r.sendResult(task, StopTargetFailed, StopFailureRunnerClosed)
		default:
			return
		}
	}
}

func (r *NodeStopRunner) execute(task NodeStopTask) {
	callContext, cancel := context.WithTimeout(context.Background(), r.config.CallTimeout)
	current, err := r.directory.Get(callContext, task.Group)
	cancel()
	if err != nil {
		r.sendResult(task, StopTargetPending, StopFailureDirectoryUnavailable)
		return
	}
	if !sameDrainServiceSet(current, task.Published) {
		r.sendResult(task, StopTargetSuperseded, StopFailureNone)
		return
	}
	stopContext, cancel := context.WithTimeout(context.Background(), r.config.StopTimeout)
	err = r.runtime.Stop(stopContext, task.Target)
	cancel()
	if err == nil || errors.Is(err, gsr.ErrServiceClosed) || errors.Is(err, gsr.ErrServiceNotFound) {
		r.sendResult(task, StopTargetStopped, StopFailureNone)
		return
	}
	r.sendResult(task, StopTargetFailed, StopFailureRuntimeStop)
}

func (r *NodeStopRunner) sendResult(task NodeStopTask, state StopTargetState, failure StopFailure) {
	_ = r.runtime.Send(task.Agent, commandRecordNodeStopResult, nodeStopResult{
		RequestID: task.RequestID,
		Target:    task.Target,
		State:     state,
		Failure:   failure,
	})
}

func validNodeStopRunnerConfig(config NodeStopRunnerConfig) bool {
	return validServiceRef(config.Directory) && config.Workers > 0 && config.QueueSize > 0 && config.CallTimeout >= 0 && config.StopTimeout >= 0
}

func validNodeStopTask(task NodeStopTask) bool {
	return validServiceRef(task.Agent) && validRequestID(task.RequestID) && validServiceRef(task.Target) && task.Agent != task.Target && task.Agent.Node == task.Target.Node && validDrainGroup(task.Group) && validDrainServiceSet(task.Published) && task.Published.Name == task.Group
}
