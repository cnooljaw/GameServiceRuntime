package gsr

import (
	"context"
	"errors"
	"sync"
	"time"
)

type stopRequest struct {
	ctx    context.Context
	result chan error
}

// Stop serializes Service shutdown after Commands already being handled.
func (r *Runtime) Stop(ctx context.Context, ref ServiceRef) error {
	instance, err := r.registry.get(ref)
	if err != nil {
		return err
	}
	instance.acceptMu.Lock()
	if !instance.status.CompareAndSwap(int32(ServiceRunning), int32(ServiceStopping)) {
		instance.acceptMu.Unlock()
		return ErrServiceClosed
	}
	if instance.policy.Mailbox == DiscardMailbox {
		instance.mailbox.discard()
	}
	request := &stopRequest{ctx: ctx, result: make(chan error, 1)}
	if err := instance.mailbox.pushStop(request); err != nil {
		instance.acceptMu.Unlock()
		return err
	}
	instance.acceptMu.Unlock()
	r.scheduler.schedule(instance)
	timer := time.NewTimer(instance.policy.LifecycleTimeout)
	defer timer.Stop()
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		cause := context.Cause(ctx)
		r.tasks.timeoutOwner(instance.ref)
		r.finalize(instance, ServiceFailed, cause)
		return cause
	case <-timer.C:
		r.metrics.Inc("stop_timeouts_total")
		r.tasks.timeoutOwner(instance.ref)
		r.finalize(instance, ServiceFailed, ErrStopTimeout)
		return ErrStopTimeout
	}
}

func (r *Runtime) executeStop(instance *serviceInstance, request *stopRequest) {
	if instance.finalized.Load() {
		request.result <- ErrServiceClosed
		return
	}
	stopErr := r.runStop(instance, request.ctx)
	if instance.finalized.Load() {
		request.result <- ErrServiceClosed
		return
	}
	if errors.Is(stopErr, ErrStopTimeout) || errors.Is(stopErr, context.Canceled) || errors.Is(stopErr, context.DeadlineExceeded) {
		r.finalize(instance, ServiceFailed, stopErr)
		request.result <- stopErr
		return
	}
	closeErr := r.runClose(instance)
	result := errors.Join(stopErr, closeErr)
	status := ServiceClosed
	if result != nil {
		status = ServiceFailed
	}
	r.finalize(instance, status, result)
	request.result <- result
}

func (r *Runtime) runStop(instance *serviceInstance, parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, instance.policy.StopTimeout)
	defer cancel()
	task, done := r.invokeTask(instance.ref, runtimeTaskStop, cancel, func() error {
		return instance.service.Stop(ctx)
	})
	select {
	case err := <-done:
		r.observeLifecycleError(instance, "stop", err)
		return err
	case <-ctx.Done():
		r.tasks.timeout(task)
		if err := parent.Err(); err != nil {
			return context.Cause(parent)
		}
		r.metrics.Inc("stop_timeouts_total")
		return ErrStopTimeout
	}
}

func (r *Runtime) runClose(instance *serviceInstance) error {
	instance.closing.Store(true)
	task, done := r.invokeTask(instance.ref, runtimeTaskClose, nil, instance.service.Close)
	timer := time.NewTimer(instance.policy.CloseTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		r.observeLifecycleError(instance, "close", err)
		return err
	case <-timer.C:
		r.tasks.timeout(task)
		r.metrics.Inc("close_timeouts_total")
		return ErrCloseTimeout
	}
}

func (r *Runtime) observeLifecycleError(instance *serviceInstance, phase string, err error) {
	if err == nil {
		return
	}
	r.metrics.Inc("service_" + phase + "_errors_total")
	if errors.Is(err, ErrServiceFailed) {
		r.metrics.Inc("service_panics_total")
		r.logger.Error("service lifecycle panic", "service", instance.ref, "phase", phase, "error", err)
		return
	}
	r.logger.Error("service lifecycle error", "service", instance.ref, "phase", phase, "error", err)
}

func (r *Runtime) finalize(instance *serviceInstance, status ServiceStatus, cause error) {
	if !instance.finalized.CompareAndSwap(false, true) {
		<-instance.done
		return
	}
	instance.acceptMu.Lock()
	defer instance.acceptMu.Unlock()
	instance.mailbox.close()
	r.timers.cancelTarget(instance.ref)
	pendingCause := cause
	if pendingCause == nil {
		pendingCause = ErrServiceClosed
	}
	r.pending.failService(instance.ref, pendingCause)
	r.registry.remove(instance.ref)
	instance.setStatus(status)
	r.metrics.Inc("service_stopped_total")
	instance.finish(cause)
}

// Close stops all Services and releases Runtime-owned resources.
func (r *Runtime) Close(ctx context.Context) error {
	r.createMu.Lock()
	if !r.state.CompareAndSwap(runtimeRunning, runtimeClosing) {
		r.createMu.Unlock()
		if r.state.Load() == runtimeClosed {
			return nil
		}
		return ErrRuntimeClosed
	}
	r.createMu.Unlock()
	closeCtx, cancel := context.WithTimeoutCause(ctx, r.shutdownTimeout, ErrCloseTimeout)
	defer cancel()
	r.pending.failAll(ErrRuntimeClosed)
	created := make(chan struct{})
	go func() { r.creating.Wait(); close(created) }()
	var result error
	select {
	case <-created:
	case <-closeCtx.Done():
		result = runtimeCloseCause(ctx)
	}
	instances := r.registry.snapshot()
	stopResults := make(chan error, len(instances))
	var wg sync.WaitGroup
	for _, instance := range instances {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.Stop(closeCtx, instance.ref)
			if errors.Is(err, ErrServiceClosed) {
				err = instance.wait(closeCtx)
			}
			stopResults <- err
		}()
	}
	stopped := make(chan struct{})
	go func() { wg.Wait(); close(stopped) }()
	select {
	case <-stopped:
		for range instances {
			if err := <-stopResults; err != nil && !errors.Is(err, ErrServiceClosed) {
				if closeCtx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
					err = runtimeCloseCause(ctx)
				}
				result = errors.Join(result, err)
			}
		}
	case <-closeCtx.Done():
		result = errors.Join(result, runtimeCloseCause(ctx))
	}
	forcedCause := error(ErrRuntimeClosed)
	forcedStatus := ServiceClosed
	if closeCtx.Err() != nil {
		forcedCause = runtimeCloseCause(ctx)
		forcedStatus = ServiceFailed
	}
	for _, instance := range instances {
		if forcedStatus == ServiceFailed {
			r.tasks.timeoutOwner(instance.ref)
		}
		r.finalize(instance, forcedStatus, forcedCause)
	}
	r.timers.cancelAll()
	r.pending.failAll(ErrRuntimeClosed)
	if err := r.scheduler.close(closeCtx); err != nil {
		if closeCtx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			err = runtimeCloseCause(ctx)
		}
		result = errors.Join(result, err)
	}
	r.reportActiveTasks()
	r.registry.clear()
	r.state.Store(runtimeClosed)
	return result
}

func runtimeCloseCause(parent context.Context) error {
	if parent.Err() != nil {
		return context.Cause(parent)
	}
	return ErrCloseTimeout
}
