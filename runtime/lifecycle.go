package gsr

import (
	"context"
	"errors"
)

// Stop stops a Service and releases Runtime-owned resources for its address.
func (r *Runtime) Stop(ctx context.Context, ref ServiceRef) error {
	instance, err := r.registry.get(ref)
	if err != nil {
		return err
	}
	if !instance.status.CompareAndSwap(int32(ServiceRunning), int32(ServiceStopping)) {
		return ErrServiceClosed
	}
	stopCtx, cancel := context.WithTimeout(ctx, instance.policy.StopTimeout)
	defer cancel()
	stopErr := instance.service.Stop(stopCtx)
	closeErr := instance.service.Close()
	r.timers.cancelTarget(ref)
	r.pending.failTarget(ref, ErrServiceClosed)
	r.registry.remove(ref)
	instance.setStatus(ServiceClosed)
	return errors.Join(stopErr, closeErr)
}
