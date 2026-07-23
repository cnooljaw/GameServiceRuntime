package drain

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	metricGuardBegun          = "drain_guard_begun_total"
	metricGuardBeginDuplicate = "drain_guard_begin_duplicate_total"
	metricGuardRejected       = "drain_guard_rejected_total"
)

type guardDecorator struct {
	inner      gsr.Service
	commands   []gsr.CommandID
	controller gsr.ServiceRef
	external   map[gsr.CommandID]struct{}
	context    gsr.ServiceContext
	draining   bool
	startedAt  time.Time
}

// Decorate wraps a Service with a Mailbox-serial Drain Guard.
func Decorate(service gsr.Service, config GuardConfig) (gsr.Service, error) {
	if isNil(service) || !validServiceRef(config.Controller) || len(config.ExternalCommands) == 0 {
		return nil, ErrInvalidGuard
	}
	declarer, ok := service.(gsr.CommandDeclarer)
	if !ok {
		return nil, ErrInvalidGuard
	}
	innerCommands := declarer.Commands()
	if len(innerCommands) == 0 {
		return nil, ErrInvalidGuard
	}
	registered := make(map[gsr.CommandID]struct{}, len(innerCommands)+2)
	commands := make([]gsr.CommandID, 0, len(innerCommands)+2)
	for _, command := range innerCommands {
		if _, exists := registered[command]; exists {
			return nil, ErrInvalidGuard
		}
		registered[command] = struct{}{}
		commands = append(commands, command)
	}
	if _, exists := registered[BeginDrainCommand]; exists {
		return nil, ErrInvalidGuard
	}
	if _, exists := registered[GetDrainStatusCommand]; exists {
		return nil, ErrInvalidGuard
	}
	external := make(map[gsr.CommandID]struct{}, len(config.ExternalCommands))
	for _, command := range config.ExternalCommands {
		if _, exists := registered[command]; !exists {
			return nil, ErrInvalidGuard
		}
		if _, exists := external[command]; exists {
			return nil, ErrInvalidGuard
		}
		external[command] = struct{}{}
	}
	commands = append(commands, BeginDrainCommand, GetDrainStatusCommand)
	return &guardDecorator{
		inner:      service,
		commands:   commands,
		controller: config.Controller,
		external:   external,
	}, nil
}

func (d *guardDecorator) Commands() []gsr.CommandID {
	return append([]gsr.CommandID(nil), d.commands...)
}

func (d *guardDecorator) StartupCommand() (gsr.Command, bool) {
	declarer, ok := d.inner.(gsr.StartupCommandDeclarer)
	if !ok {
		return gsr.Command{}, false
	}
	return declarer.StartupCommand()
}

func (d *guardDecorator) Init(serviceContext gsr.ServiceContext) error {
	if isNil(serviceContext) || serviceContext.Self() == d.controller {
		return ErrInvalidGuard
	}
	d.context = serviceContext
	return d.inner.Init(serviceContext)
}

func (d *guardDecorator) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case BeginDrainCommand:
		return d.begin(commandContext, command.Payload)
	case GetDrainStatusCommand:
		return d.status(commandContext, command.Payload)
	default:
		if d.draining {
			if _, external := d.external[command.ID]; external {
				d.context.Metrics().Inc(metricGuardRejected)
				return ErrDraining
			}
		}
		return d.inner.Handle(commandContext, command)
	}
}

func (d *guardDecorator) Stop(ctx context.Context) error { return d.inner.Stop(ctx) }

func (d *guardDecorator) Close() error {
	err := d.inner.Close()
	d.context = nil
	return err
}

func (d *guardDecorator) begin(commandContext gsr.CommandContext, payload any) error {
	if _, ok := payload.(beginDrainRequest); !ok {
		return d.replyStatus(commandContext, DrainStatus{}, ErrInvalidGuard)
	}
	if commandContext.Source() != d.controller {
		return d.replyStatus(commandContext, DrainStatus{}, ErrUnauthorized)
	}
	status := d.currentStatus()
	if d.draining {
		d.context.Metrics().Inc(metricGuardBeginDuplicate)
		if err := d.replyStatus(commandContext, status, nil); err != nil {
			return err
		}
		return nil
	}
	status = DrainStatus{Draining: true, StartedAt: d.context.Now()}
	d.draining = true
	d.startedAt = status.StartedAt
	d.context.Metrics().Inc(metricGuardBegun)
	return d.replyStatus(commandContext, status, nil)
}

func (d *guardDecorator) status(commandContext gsr.CommandContext, payload any) error {
	if _, ok := payload.(getDrainStatusRequest); !ok {
		return d.replyStatus(commandContext, DrainStatus{}, ErrInvalidGuard)
	}
	return d.replyStatus(commandContext, d.currentStatus(), nil)
}

func (d *guardDecorator) currentStatus() DrainStatus {
	return DrainStatus{Draining: d.draining, StartedAt: d.startedAt}
}

func (*guardDecorator) replyStatus(commandContext gsr.CommandContext, status DrainStatus, err error) error {
	return commandContext.Reply(drainStatusResponse{
		Status: newWireDrainStatus(status),
		Error:  guardCodeFromError(err),
	})
}
