package supervisor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// Runner owns the bounded worker pool that performs recovery outside Service handlers.
type Runner struct {
	caller   CommandCaller
	launcher Launcher
	config   RunnerConfig
	context  context.Context
	cancel   context.CancelFunc
	jobs     chan RecoveryTask
	done     chan struct{}
	mu       sync.Mutex
	closed   bool
	workers  atomic.Int32
}

// NewRunner starts a fixed-size recovery worker pool.
func NewRunner(caller CommandCaller, launcher Launcher, config RunnerConfig) (*Runner, error) {
	if isNil(caller) || isNil(launcher) || validateRunnerConfig(config) != nil {
		return nil, ErrInvalidConfig
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := &Runner{
		caller: caller, launcher: launcher, config: config, context: ctx, cancel: cancel,
		jobs: make(chan RecoveryTask, config.QueueSize), done: make(chan struct{}),
	}
	runner.workers.Store(int32(config.Workers))
	for range config.Workers {
		go runner.runWorker()
	}
	return runner, nil
}

// Submit enqueues one recovery task without blocking when the bounded queue is full.
func (r *Runner) Submit(task RecoveryTask) error {
	if validateRecoveryTask(task) != nil {
		return ErrInvalidConfig
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrRunnerClosed
	}
	select {
	case r.jobs <- task:
		return nil
	default:
		return ErrRecoveryQueueFull
	}
}

// Close cancels pending work and waits until all launcher calls have really returned.
func (r *Runner) Close(ctx context.Context) error {
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

func (r *Runner) runWorker() {
	defer func() {
		if r.workers.Add(-1) == 0 {
			for {
				select {
				case <-r.jobs:
					continue
				default:
					close(r.done)
					return
				}
			}
		}
	}()
	for {
		select {
		case <-r.context.Done():
			return
		case task := <-r.jobs:
			if r.context.Err() != nil {
				return
			}
			r.execute(task)
		}
	}
}

func (r *Runner) execute(task RecoveryTask) {
	if !r.wait(task.Delay) {
		return
	}
	if err := r.callOperation(task.Supervisor, recoveryStartedCommand, recoveryStartedRequest{Task: task}); err != nil {
		r.logResultFailure(task, "started", err)
		return
	}
	request := LaunchRequest{
		Supervisor: task.Supervisor,
		Key:        task.Key,
		FailedRef:  task.FailedRef,
		Generation: task.Generation,
		Attempt:    task.Attempt,
	}
	attemptContext, cancel := context.WithTimeout(r.context, r.config.AttemptTimeout)
	ref, err := r.launcher.Prepare(attemptContext, request)
	if err != nil {
		cancel()
		if r.context.Err() == nil {
			r.logLaunchFailure(task, "prepare", err)
			r.reportFailure(task, classifyPrepareFailure(err))
		}
		return
	}
	if validateConcreteRef(ref) != nil || ref.Node != task.Supervisor.Node {
		cancel()
		r.logLaunchFailure(task, "prepare", ErrCreateFailed)
		r.reportFailure(task, RecoveryFailureCreate)
		return
	}
	if err := r.callOperation(task.Supervisor, recoveryPreparedCommand, recoveryPreparedRequest{Task: task, Ref: ref}); err != nil {
		cancel()
		r.abort(task, request, ref, err, RecoveryFailurePrepare)
		return
	}
	if err := r.launcher.Commit(attemptContext, request, ref); err != nil {
		cancel()
		r.logLaunchFailure(task, "commit", err)
		if abortErr := r.abortLaunch(request, ref); abortErr != nil {
			r.logLaunchFailure(task, "abort", abortErr)
			r.reportFailure(task, RecoveryFailureAbort)
			return
		}
		r.reportFailure(task, RecoveryFailurePublish)
		return
	}
	cancel()
	if err := r.callOperation(task.Supervisor, recoveryCommittedCommand, recoveryCommittedRequest{Task: task, Ref: ref}); err != nil {
		r.abort(task, request, ref, err, RecoveryFailurePublish)
	}
}

func (r *Runner) abort(task RecoveryTask, request LaunchRequest, ref gsr.ServiceRef, cause error, failure RecoveryFailure) {
	r.logResultFailure(task, "result", cause)
	if err := r.abortLaunch(request, ref); err != nil {
		r.logLaunchFailure(task, "abort", err)
		r.reportFailure(task, RecoveryFailureAbort)
		return
	}
	if !errors.Is(cause, ErrStaleRecovery) {
		r.reportFailure(task, failure)
	}
}

func (r *Runner) abortLaunch(request LaunchRequest, ref gsr.ServiceRef) error {
	ctx, cancel := context.WithTimeout(r.context, r.config.AttemptTimeout)
	defer cancel()
	return r.launcher.Abort(ctx, request, ref)
}

func (r *Runner) reportFailure(task RecoveryTask, failure RecoveryFailure) {
	if err := r.callOperation(task.Supervisor, recoveryFailedCommand, recoveryFailedRequest{Task: task, Failure: failure}); err != nil {
		r.logResultFailure(task, "failed", err)
	}
}

func (r *Runner) callOperation(target gsr.ServiceRef, command gsr.CommandID, payload any) error {
	ctx, cancel := context.WithTimeout(r.context, r.config.ResultTimeout)
	defer cancel()
	for {
		value, err := r.caller.Call(ctx, target, command, payload)
		if err == nil {
			response, ok := value.(operationResponse)
			if !ok {
				return ErrInvalidResponse
			}
			return errorFromResponse(response.Error)
		}
		if !errors.Is(err, gsr.ErrMailboxFull) {
			return err
		}
		timer := time.NewTimer(r.config.ResultRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return context.Cause(ctx)
		case <-timer.C:
		}
	}
}

func (r *Runner) wait(delay time.Duration) bool {
	if delay == 0 {
		return r.context.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-r.context.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *Runner) logLaunchFailure(task RecoveryTask, stage string, err error) {
	r.config.Logger.Error("supervisor recovery launcher failed", "key_namespace", task.Key.Namespace, "key_id", task.Key.ID, "attempt", task.Attempt, "stage", stage, "error", err)
}

func (r *Runner) logResultFailure(task RecoveryTask, stage string, err error) {
	r.config.Logger.Error("supervisor recovery result delivery failed", "key_namespace", task.Key.Namespace, "key_id", task.Key.ID, "attempt", task.Attempt, "stage", stage, "error", err)
}

func classifyPrepareFailure(err error) RecoveryFailure {
	switch {
	case errors.Is(err, ErrSnapshotNotFound):
		return RecoveryFailureSnapshotNotFound
	case errors.Is(err, ErrCreateFailed):
		return RecoveryFailureCreate
	default:
		return RecoveryFailurePrepare
	}
}
