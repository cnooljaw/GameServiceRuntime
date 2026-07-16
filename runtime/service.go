package gsr

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

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
	Call(context.Context, ServiceRef, CommandID, any) (any, error)
	After(time.Duration, CommandID, any) (TimerID, error)
	Now() time.Time
	Logger() *slog.Logger
	Metrics() Metrics
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

// MailboxStopPolicy controls queued Commands when Stop begins.
type MailboxStopPolicy int

const (
	// DrainMailbox processes Commands queued before the stop request.
	DrainMailbox MailboxStopPolicy = iota
	// DiscardMailbox drops queued Commands before stopping.
	DiscardMailbox
)

// ServicePolicy configures Service lifecycle behavior.
type ServicePolicy struct {
	StopTimeout      time.Duration
	CloseTimeout     time.Duration
	LifecycleTimeout time.Duration
	Mailbox          MailboxStopPolicy
}

// ServiceSpec describes a Service created by Runtime.
type ServiceSpec struct {
	Name    ServiceName
	Service Service
	Policy  ServicePolicy
}

type serviceContext struct {
	runtime  *Runtime
	instance *serviceInstance
}

func (c *serviceContext) Self() ServiceRef { return c.instance.ref }
func (c *serviceContext) Send(target ServiceRef, id CommandID, payload any) error {
	return c.runtime.sendFrom(c.instance.ref, target, id, payload)
}
func (c *serviceContext) Call(ctx context.Context, target ServiceRef, id CommandID, payload any) (any, error) {
	return c.runtime.callFromService(ctx, c.instance, target, id, payload)
}
func (c *serviceContext) After(delay time.Duration, id CommandID, payload any) (TimerID, error) {
	return c.runtime.After(c.instance.ref, delay, id, payload)
}
func (c *serviceContext) Now() time.Time { return c.runtime.now() }
func (c *serviceContext) Logger() *slog.Logger {
	return c.runtime.logger.With("service", c.instance.ref)
}
func (c *serviceContext) Metrics() Metrics { return c.runtime.metrics }

type commandContext struct {
	self    ServiceRef
	source  ServiceRef
	runtime *Runtime
	session SessionID
	command CommandID
	replied atomic.Bool
}

func (c *commandContext) Self() ServiceRef { return c.self }
func (c *commandContext) Reply(value any) error {
	if c.session == 0 {
		return ErrReplyUnavailable
	}
	if !c.replied.CompareAndSwap(false, true) {
		return ErrReplyTwice
	}
	return c.runtime.reply(c.self, c.source, c.command, c.session, value, nil)
}

type serviceInstance struct {
	ref        ServiceRef
	name       ServiceName
	service    Service
	commands   *commandSet
	mailbox    *mailbox
	policy     ServicePolicy
	context    *serviceContext
	status     atomic.Int32
	ready      atomic.Bool
	permitHeld atomic.Bool
	closing    atomic.Bool
	finalized  atomic.Bool
	acceptMu   sync.Mutex
	pathMu     sync.RWMutex
	callPath   []ServiceRef
	done       chan struct{}
	resultMu   sync.RWMutex
	result     error
}

func (i *serviceInstance) setStatus(status ServiceStatus) { i.status.Store(int32(status)) }
func (i *serviceInstance) path() []ServiceRef {
	i.pathMu.RLock()
	defer i.pathMu.RUnlock()
	return append([]ServiceRef(nil), i.callPath...)
}
func (i *serviceInstance) setPath(path []ServiceRef) {
	i.pathMu.Lock()
	i.callPath = append(i.callPath[:0], path...)
	i.pathMu.Unlock()
}

func (i *serviceInstance) finish(result error) {
	i.resultMu.Lock()
	i.result = result
	i.resultMu.Unlock()
	close(i.done)
}

func (i *serviceInstance) wait(ctx context.Context) error {
	select {
	case <-i.done:
		i.resultMu.RLock()
		defer i.resultMu.RUnlock()
		return i.result
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func invokeService(fn func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: panic: %v", ErrServiceFailed, recovered)
		}
	}()
	return fn()
}
