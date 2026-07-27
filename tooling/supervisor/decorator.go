package supervisor

import (
	"context"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type serviceDecorator struct {
	inner   gsr.Service
	config  DecoratorConfig
	context gsr.ServiceContext
}

// Decorate wraps Handle panic reporting around a Service without changing normal lifecycle behavior.
func Decorate(service gsr.Service, config DecoratorConfig) (gsr.Service, error) {
	if isNil(service) || validateServiceKey(config.Key) != nil || config.Generation == 0 || validateConcreteRef(config.Supervisor) != nil {
		return nil, ErrInvalidConfig
	}
	return &serviceDecorator{
		inner: service, config: config,
	}, nil
}

func (d *serviceDecorator) Init(ctx gsr.ServiceContext) error {
	if isNil(ctx) {
		return ErrInvalidConfig
	}
	if ctx.Self() == d.config.Supervisor {
		return ErrInvalidRegistration
	}
	d.context = ctx
	return d.inner.Init(ctx)
}

func (d *serviceDecorator) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		notice := FailureNotice{
			Key:        d.config.Key,
			FailedRef:  d.context.Self(),
			Generation: d.config.Generation,
			OccurredAt: d.context.Now(),
			Kind:       FailureHandlerPanic,
		}
		if err := d.context.Send(d.config.Supervisor, failureCommand, notice); err != nil {
			d.context.Metrics().Inc(metricFailureNoticeDeliveryErrors)
			d.context.Logger().Error("supervisor failure notice delivery failed", "error", err)
		}
		panic(recovered)
	}()
	return d.inner.Handle(ctx, command)
}

func (d *serviceDecorator) Stop(ctx context.Context) error { return d.inner.Stop(ctx) }
func (d *serviceDecorator) Close() error                   { return d.inner.Close() }
