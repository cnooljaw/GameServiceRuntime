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
	total := instance.policy.StopTimeout + instance.policy.CloseTimeout + 50*time.Millisecond
	timer := time.NewTimer(total)
	defer timer.Stop()
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		r.finalize(instance, ServiceFailed, ctx.Err())
		return ctx.Err()
	case <-timer.C:
		r.metrics.Inc("stop_timeouts_total")
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
	done := make(chan error, 1)
	go func() { done <- invokeService(func() error { return instance.service.Stop(ctx) }) }()
	select {
	case err := <-done:
		if errors.Is(err, ErrServiceFailed) {
			r.metrics.Inc("service_panics_total")
			r.logger.Error("service stop panic", "service", instance.ref, "error", err)
		}
		return err
	case <-ctx.Done():
		if err := parent.Err(); err != nil {
			return err
		}
		r.metrics.Inc("stop_timeouts_total")
		return ErrStopTimeout
	}
}

func (r *Runtime) runClose(instance *serviceInstance) error {
	instance.closing.Store(true)
	done := make(chan error, 1)
	go func() { done <- invokeService(instance.service.Close) }()
	timer := time.NewTimer(instance.policy.CloseTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if errors.Is(err, ErrServiceFailed) {
			r.metrics.Inc("service_panics_total")
			r.logger.Error("service close panic", "service", instance.ref, "error", err)
		}
		return err
	case <-timer.C:
		r.metrics.Inc("close_timeouts_total")
		return ErrCloseTimeout
	}
}

func (r *Runtime) finalize(instance *serviceInstance, status ServiceStatus, cause error) {
	if !instance.finalized.CompareAndSwap(false, true) {
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
	closeCtx, cancel := context.WithTimeout(ctx, r.shutdownTimeout)
	defer cancel()
	r.pending.failAll(ErrRuntimeClosed)
	created := make(chan struct{})
	go func() { r.creating.Wait(); close(created) }()
	var result error
	select {
	case <-created:
	case <-closeCtx.Done():
		result = ErrCloseTimeout
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
				result = errors.Join(result, err)
			}
		}
	case <-closeCtx.Done():
		result = errors.Join(result, ErrCloseTimeout)
	}
	for _, instance := range instances {
		r.finalize(instance, ServiceClosed, ErrRuntimeClosed)
	}
	r.timers.cancelAll()
	r.pending.failAll(ErrRuntimeClosed)
	if err := r.scheduler.close(closeCtx); err != nil {
		result = errors.Join(result, err)
	}
	r.registry.clear()
	r.state.Store(runtimeClosed)
	return result
}
