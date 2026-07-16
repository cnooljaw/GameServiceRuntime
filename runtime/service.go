package gsr

import "context"
import "time"

// Service handles Commands addressed to one Runtime ServiceRef.
type Service interface {
	Init(ServiceContext) error
	Handle(CommandContext, Command) error
	Stop(context.Context) error
	Close() error
}

// ServiceContext exposes Runtime capabilities to a Service.
type ServiceContext interface {
	Self() ServiceRef
	Send(ServiceRef, CommandID, any) error
	After(time.Duration, CommandID, any) (TimerID, error)
}

// CommandContext describes the Service currently handling a Command.
type CommandContext interface {
	Self() ServiceRef
	Reply(any) error
}

// ServiceStatus describes a Service lifecycle state.
type ServiceStatus int

const (
	ServiceCreated ServiceStatus = iota
	ServiceStarting
	ServiceRunning
	ServiceStopping
	ServiceClosed
	ServiceFailed
	ServiceRestarting
)

// ServicePolicy configures Service lifecycle behavior.
type ServicePolicy struct{ StopTimeout time.Duration }

// ServiceSpec describes a Service created by Runtime.
type ServiceSpec struct {
	Service Service
	Policy  ServicePolicy
}

type serviceContext struct {
	runtime *Runtime
	self    ServiceRef
}

func (c serviceContext) Self() ServiceRef { return c.self }
func (c serviceContext) Send(target ServiceRef, id CommandID, payload any) error {
	return c.runtime.sendFrom(c.self, target, id, payload)
}
func (c serviceContext) After(delay time.Duration, id CommandID, payload any) (TimerID, error) {
	return c.runtime.After(c.self, delay, id, payload)
}

type commandContext struct {
	self    ServiceRef
	runtime *Runtime
	session SessionID
}

func (c commandContext) Self() ServiceRef { return c.self }
func (c commandContext) Reply(value any) error {
	if c.session == 0 || !c.runtime.pending.complete(c.session, value) {
		return ErrReplyTwice
	}
	return nil
}
