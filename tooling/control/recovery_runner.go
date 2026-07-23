package control

import (
	"context"
	"sync"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// BlueprintFactory builds one fresh ServiceSpec for a registered BlueprintID.
type BlueprintFactory func() (gsr.ServiceSpec, error)

// MapBlueprintRegistry is a composition-root-owned immutable BlueprintRegistry.
type MapBlueprintRegistry struct {
	factories map[BlueprintID]BlueprintFactory
}

// NewMapBlueprintRegistry copies and validates one set of composition-root factories.
func NewMapBlueprintRegistry(factories map[BlueprintID]BlueprintFactory) (*MapBlueprintRegistry, error) {
	if factories == nil {
		return nil, ErrInvalidConfig
	}
	cloned := make(map[BlueprintID]BlueprintFactory, len(factories))
	for id, factory := range factories {
		if !validBlueprintID(id) || factory == nil {
			return nil, ErrInvalidConfig
		}
		cloned[id] = factory
	}
	return &MapBlueprintRegistry{factories: cloned}, nil
}

// Build creates a fresh unnamed ServiceSpec for one registered BlueprintID.
func (r *MapBlueprintRegistry) Build(id BlueprintID) (gsr.ServiceSpec, error) {
	if r == nil || !validBlueprintID(id) {
		return gsr.ServiceSpec{}, ErrInvalidConfig
	}
	factory := r.factories[id]
	if factory == nil {
		return gsr.ServiceSpec{}, ErrInvalidConfig
	}
	spec, err := factory()
	if err != nil || spec.Name != "" || isNil(spec.Service) {
		return gsr.ServiceSpec{}, ErrInvalidConfig
	}
	return spec, nil
}

// RecoveryRunner owns the fixed-size external worker pool that creates replacement Services.
type RecoveryRunner struct {
	runtime RecoveryRuntime
	config  RecoveryRunnerConfig
	context context.Context
	cancel  context.CancelFunc
	jobs    chan RecoveryCreateTask
	done    chan struct{}

	mu      sync.Mutex
	closed  bool
	workers int
}

// NewRecoveryRunner creates and starts one bounded replacement creation worker pool.
func NewRecoveryRunner(runtime RecoveryRuntime, config RecoveryRunnerConfig) (*RecoveryRunner, error) {
	if isNil(runtime) || !validRecoveryRunnerConfig(config) {
		return nil, ErrInvalidConfig
	}
	workerContext, cancel := context.WithCancel(context.Background())
	runner := &RecoveryRunner{
		runtime: runtime, config: config, context: workerContext, cancel: cancel,
		jobs: make(chan RecoveryCreateTask, config.QueueSize), done: make(chan struct{}), workers: config.Workers,
	}
	for range config.Workers {
		go runner.runWorker()
	}
	return runner, nil
}

// Submit queues one frozen replacement task without blocking when the queue is full.
func (r *RecoveryRunner) Submit(task RecoveryCreateTask) error {
	if !validRecoveryCreateTask(task) {
		return ErrInvalidConfig
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrRecoveryRunnerClosed
	}
	select {
	case r.jobs <- task:
		return nil
	default:
		return ErrRecoveryQueueFull
	}
}

// Close rejects new tasks, cancels unstarted tasks, and waits for started creations to return.
func (r *RecoveryRunner) Close(ctx context.Context) error {
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

func (r *RecoveryRunner) runWorker() {
	defer r.workerDone()
	for {
		select {
		case <-r.context.Done():
			r.cancelPending()
			return
		case task := <-r.jobs:
			if r.context.Err() != nil {
				r.sendResult(task, RecoveryTargetFailed, RecoveryFailureRunnerClosed, gsr.ServiceRef{})
				continue
			}
			r.execute(task)
		}
	}
}

func (r *RecoveryRunner) workerDone() {
	r.mu.Lock()
	r.workers--
	last := r.workers == 0
	r.mu.Unlock()
	if last {
		close(r.done)
	}
}

func (r *RecoveryRunner) cancelPending() {
	for {
		select {
		case task := <-r.jobs:
			r.sendResult(task, RecoveryTargetFailed, RecoveryFailureRunnerClosed, gsr.ServiceRef{})
		default:
			return
		}
	}
}

func (r *RecoveryRunner) execute(task RecoveryCreateTask) {
	spec, err := r.config.Registry.Build(task.Blueprint)
	if err != nil {
		r.sendResult(task, RecoveryTargetFailed, RecoveryFailureBlueprintUnavailable, gsr.ServiceRef{})
		return
	}
	created, err := r.runtime.CreateService(spec)
	if err != nil || !validServiceRef(created) || created.Node != task.Agent.Node || created == task.Removed || created == task.Agent {
		r.sendResult(task, RecoveryTargetFailed, RecoveryFailureCreate, gsr.ServiceRef{})
		return
	}
	r.sendResult(task, RecoveryTargetCreated, RecoveryFailureNone, created)
}

func (r *RecoveryRunner) sendResult(task RecoveryCreateTask, state RecoveryTargetState, failure RecoveryFailure, created gsr.ServiceRef) {
	_ = r.runtime.Send(task.Agent, commandRecordRecoveryCreate, recoveryCreateResult{
		RequestID: task.RequestID,
		Removed:   task.Removed,
		Blueprint: task.Blueprint,
		Created:   created,
		State:     state,
		Failure:   failure,
	})
}

func validRecoveryRunnerConfig(config RecoveryRunnerConfig) bool {
	return !isNil(config.Registry) && config.Workers > 0 && config.QueueSize > 0
}

var _ RecoveryExecutor = (*RecoveryRunner)(nil)
var _ BlueprintRegistry = (*MapBlueprintRegistry)(nil)
