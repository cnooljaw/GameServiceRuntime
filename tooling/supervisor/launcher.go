package supervisor

import (
	"context"
	"errors"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// RuntimeLauncher implements two-stage recovery with narrow Runtime and binding capabilities.
type RuntimeLauncher struct {
	runtime   RuntimeControl
	factory   ServiceFactory
	publisher BindingPublisher
}

// NewRuntimeLauncher creates a two-stage launcher. publisher may be nil for unbound Services.
func NewRuntimeLauncher(runtime RuntimeControl, factory ServiceFactory, publisher BindingPublisher) (*RuntimeLauncher, error) {
	if isNil(runtime) || isNil(factory) || (publisher != nil && isNil(publisher)) {
		return nil, ErrInvalidConfig
	}
	return &RuntimeLauncher{runtime: runtime, factory: factory, publisher: publisher}, nil
}

// Prepare builds an unnamed decorated Service and creates its new Runtime instance.
func (l *RuntimeLauncher) Prepare(ctx context.Context, request LaunchRequest) (gsr.ServiceRef, error) {
	if isNil(ctx) || validateLaunchRequest(request) != nil {
		return gsr.ServiceRef{}, ErrInvalidConfig
	}
	spec, err := l.factory.Build(ctx, request.Key, request.Generation)
	if err != nil {
		return gsr.ServiceRef{}, errors.Join(ErrRecoveryFailed, err)
	}
	if spec.Name != "" || isNil(spec.Service) {
		return gsr.ServiceRef{}, ErrInvalidConfig
	}
	decorated, err := Decorate(spec.Service, DecoratorConfig{
		Key: request.Key, Generation: request.Generation, Supervisor: request.Supervisor,
	})
	if err != nil {
		return gsr.ServiceRef{}, errors.Join(ErrRecoveryFailed, err)
	}
	spec.Service = decorated
	ref, err := l.runtime.CreateService(spec)
	if err != nil {
		return gsr.ServiceRef{}, errors.Join(ErrCreateFailed, err)
	}
	return ref, nil
}

// Commit publishes the prepared Service's optional long-lived binding.
func (l *RuntimeLauncher) Commit(ctx context.Context, request LaunchRequest, ref gsr.ServiceRef) error {
	if isNil(ctx) || validateLaunchRequest(request) != nil || validateConcreteRef(ref) != nil || ref.Node != request.Supervisor.Node {
		return ErrInvalidConfig
	}
	if l.publisher == nil {
		return nil
	}
	if err := l.publisher.Publish(ctx, request.Key, ref); err != nil {
		return errors.Join(ErrNamePublishFailed, err)
	}
	return nil
}

// Abort conditionally withdraws a binding before stopping the prepared Service.
func (l *RuntimeLauncher) Abort(ctx context.Context, request LaunchRequest, ref gsr.ServiceRef) error {
	if isNil(ctx) || validateLaunchRequest(request) != nil || validateConcreteRef(ref) != nil || ref.Node != request.Supervisor.Node {
		return ErrInvalidConfig
	}
	var result error
	if l.publisher != nil {
		if err := l.publisher.Withdraw(ctx, request.Key, ref); err != nil {
			result = errors.Join(result, err)
		}
	}
	if err := l.runtime.Stop(ctx, ref); err != nil && !errors.Is(err, gsr.ErrServiceClosed) && !errors.Is(err, gsr.ErrServiceNotFound) {
		result = errors.Join(result, err)
	}
	if result != nil {
		return errors.Join(ErrAbortFailed, result)
	}
	return nil
}
