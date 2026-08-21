package gsr

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// RunnerName identifies one Runtime-owned Runner for lifecycle and inspection.
type RunnerName string

// RunnerConfig configures a fixed-size Runner worker pool and bounded queue.
type RunnerConfig struct {
	Name      RunnerName
	Workers   int
	QueueSize int
}

// RunnerProcessorFunc performs one external task outside a Service Mailbox.
type RunnerProcessorFunc[Request, Result any] func(context.Context, Request) (Result, error)

// RunnerResult is the payload delivered by Runner.Submit after processing.
type RunnerResult[Result any] struct {
	Value Result
	Err   error
}

// RunnerStatus describes the lifecycle state of a Runtime-owned Runner.
type RunnerStatus int

const (
	// RunnerRunning accepts new tasks.
	RunnerRunning RunnerStatus = iota + 1
	// RunnerClosing rejects new tasks and waits for workers to return.
	RunnerClosing
	// RunnerClosed has observed every worker's true return.
	RunnerClosed
)

type runnerTask[Request, Result any] struct {
	ctx     context.Context
	request Request
	target  ServiceRef
	command CommandID
	await   chan RunnerResult[Result]
}

// Runner executes bounded external work under Runtime lifecycle ownership.
type Runner[Request, Result any] struct {
	runtime   *Runtime
	config    RunnerConfig
	processor RunnerProcessorFunc[Request, Result]
	queue     chan runnerTask[Request, Result]
	ctx       context.Context
	cancel    context.CancelCauseFunc
	done      chan struct{}
	lifecycle sync.RWMutex
	closeOnce sync.Once
	status    atomic.Int32
	remaining atomic.Int32
	active    atomic.Int32
	submitted atomic.Uint64
	completed atomic.Uint64
	failed    atomic.Uint64
	rejected  atomic.Uint64
	delivery  atomic.Uint64
}

// NewRunner creates and registers a fixed-size Runner owned by runtime.
func NewRunner[Request, Result any](runtime *Runtime, config RunnerConfig, processor RunnerProcessorFunc[Request, Result]) (*Runner[Request, Result], error) {
	if runtime == nil || config.Name == "" || config.Workers < 1 || config.QueueSize < 1 || processor == nil {
		return nil, ErrInvalidRunnerConfig
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	runner := &Runner[Request, Result]{
		runtime:   runtime,
		config:    config,
		processor: processor,
		queue:     make(chan runnerTask[Request, Result], config.QueueSize),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	runner.status.Store(int32(RunnerRunning))
	runner.remaining.Store(int32(config.Workers))

	runtime.createMu.Lock()
	defer runtime.createMu.Unlock()
	if runtime.state.Load() != runtimeRunning {
		cancel(ErrRunnerClosed)
		return nil, ErrRuntimeClosed
	}
	if err := runtime.runners.add(config.Name, runner); err != nil {
		cancel(ErrRunnerClosed)
		return nil, err
	}
	for worker := 0; worker < config.Workers; worker++ {
		go runner.runWorker()
	}
	return runner, nil
}

// Submit non-blockingly accepts a task and later sends RunnerResult to target.
func (r *Runner[Request, Result]) Submit(ctx context.Context, target ServiceRef, command CommandID, request Request) error {
	if r == nil || r.runtime == nil {
		return ErrInvalidRunnerTarget
	}
	if target.Node != r.runtime.node || target.ID == 0 || command == 0 {
		r.observeRejected()
		return ErrInvalidRunnerTarget
	}
	task := runnerTask[Request, Result]{ctx: ctx, target: target, command: command, request: request}
	r.lifecycle.RLock()
	defer r.lifecycle.RUnlock()
	if RunnerStatus(r.status.Load()) != RunnerRunning {
		r.observeRejected()
		return ErrRunnerClosed
	}
	select {
	case r.queue <- task:
		r.submitted.Add(1)
		r.runtime.metrics.Inc("runner_tasks_submitted_total")
		return nil
	default:
		r.observeRejected()
		return ErrRunnerQueueFull
	}
}

// Await executes a task outside the Mailbox while preserving the current Service's serial ownership.
func (r *Runner[Request, Result]) Await(ctx context.Context, owner CommandContext, request Request) (Result, error) {
	var zero Result
	commandContext, ok := owner.(*commandContext)
	if r == nil || !ok || commandContext.runtime != r.runtime || commandContext.instance == nil || !commandContext.active.Load() || ServiceStatus(commandContext.instance.status.Load()) != ServiceRunning {
		return zero, ErrRunnerAwaitNotAllowed
	}
	if !commandContext.awaiting.CompareAndSwap(false, true) {
		return zero, ErrRunnerAwaitNotAllowed
	}
	defer commandContext.awaiting.Store(false)
	if !r.runtime.scheduler.yield(commandContext.instance) {
		return zero, ErrRunnerAwaitNotAllowed
	}
	resultChannel := make(chan RunnerResult[Result], 1)
	task := runnerTask[Request, Result]{ctx: ctx, request: request, await: resultChannel}
	if err := r.enqueueAwait(task); err != nil {
		if resumeErr := r.runtime.scheduler.resume(commandContext.instance); resumeErr != nil {
			return zero, resumeErr
		}
		return zero, err
	}

	var result RunnerResult[Result]
	select {
	case result = <-resultChannel:
	case <-ctx.Done():
		result.Err = context.Cause(ctx)
	case <-r.ctx.Done():
		result.Err = ErrRunnerClosed
	}
	if resumeErr := r.runtime.scheduler.resume(commandContext.instance); resumeErr != nil && result.Err == nil {
		result.Err = resumeErr
	}
	return result.Value, result.Err
}

func (r *Runner[Request, Result]) enqueueAwait(task runnerTask[Request, Result]) error {
	r.lifecycle.RLock()
	defer r.lifecycle.RUnlock()
	if RunnerStatus(r.status.Load()) != RunnerRunning {
		r.observeRejected()
		return ErrRunnerClosed
	}
	select {
	case r.queue <- task:
		r.submitted.Add(1)
		r.runtime.metrics.Inc("runner_tasks_submitted_total")
		return nil
	default:
		r.observeRejected()
		return ErrRunnerQueueFull
	}
}

// Close stops admission, cancels work, and waits for every worker's true return.
func (r *Runner[Request, Result]) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.lifecycle.Lock()
		r.status.Store(int32(RunnerClosing))
		r.cancel(ErrRunnerClosed)
		for {
			select {
			case task := <-r.queue:
				r.complete(task, RunnerResult[Result]{Err: ErrRunnerClosed})
			default:
				r.lifecycle.Unlock()
				return
			}
		}
	})
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (r *Runner[Request, Result]) runWorker() {
	defer func() {
		if r.remaining.Add(-1) == 0 {
			r.status.Store(int32(RunnerClosed))
			close(r.done)
		}
	}()
	for {
		select {
		case <-r.ctx.Done():
			return
		case task := <-r.queue:
			if cause := context.Cause(r.ctx); cause != nil {
				r.complete(task, RunnerResult[Result]{Err: cause})
				continue
			}
			r.execute(task)
		}
	}
}

func (r *Runner[Request, Result]) execute(task runnerTask[Request, Result]) {
	r.active.Add(1)
	defer r.active.Add(-1)
	workContext, cancel := context.WithCancelCause(r.ctx)
	stopTaskCancellation := context.AfterFunc(task.ctx, func() { cancel(context.Cause(task.ctx)) })
	result := invokeRunnerProcessor(workContext, r.processor, task.request)
	stopTaskCancellation()
	cancel(nil)
	r.complete(task, result)
}

func invokeRunnerProcessor[Request, Result any](ctx context.Context, processor RunnerProcessorFunc[Request, Result], request Request) (result RunnerResult[Result]) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result.Err = fmt.Errorf("%w: %v", ErrRunnerPanic, recovered)
		}
	}()
	result.Value, result.Err = processor(ctx, request)
	return result
}

func (r *Runner[Request, Result]) complete(task runnerTask[Request, Result], result RunnerResult[Result]) {
	r.completed.Add(1)
	r.runtime.metrics.Inc("runner_tasks_completed_total")
	if result.Err != nil {
		r.failed.Add(1)
		r.runtime.metrics.Inc("runner_tasks_failed_total")
		if errors.Is(result.Err, ErrRunnerPanic) {
			r.runtime.logger.Error("runner processor panic", "runner", r.config.Name, "error", result.Err)
		}
	}
	if task.await != nil {
		select {
		case task.await <- result:
		default:
		}
		return
	}
	if err := r.runtime.Send(task.target, task.command, result); err != nil {
		r.delivery.Add(1)
		r.runtime.metrics.Inc("runner_result_delivery_failed_total")
		r.runtime.logger.Error("runner result delivery failed", "runner", r.config.Name, "service", task.target, "command", task.command, "error", err)
	}
}

func (r *Runner[Request, Result]) observeRejected() {
	r.rejected.Add(1)
	r.runtime.metrics.Inc("runner_tasks_rejected_total")
}

func (r *Runner[Request, Result]) inspection() RunnerInspection {
	return RunnerInspection{
		Name:           r.config.Name,
		Status:         RunnerStatus(r.status.Load()),
		Workers:        r.config.Workers,
		QueueDepth:     len(r.queue),
		Active:         int(r.active.Load()),
		Submitted:      r.submitted.Load(),
		Completed:      r.completed.Load(),
		Failed:         r.failed.Load(),
		Rejected:       r.rejected.Load(),
		DeliveryFailed: r.delivery.Load(),
	}
}
