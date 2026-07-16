package gsr

import (
	"context"
	"sync/atomic"
	"time"
)

// Config configures a Runtime instance.
type Config struct {
	NodeID      NodeID
	Workers     int
	MailboxSize int
	MaxBatch    int
}

// Runtime owns local Service instances and their message dispatch.
type Runtime struct {
	node        NodeID
	mailboxSize int
	registry    *localRegistry
	scheduler   *scheduler
	pending     *pendingCalls
	timers      *timerManager
	nextID      atomic.Uint64
}

// NewRuntime creates and starts a local Runtime.
func NewRuntime(config Config) *Runtime {
	if config.NodeID == "" {
		config.NodeID = "local"
	}
	if config.Workers < 1 {
		config.Workers = 1
	}
	if config.MailboxSize < 1 {
		config.MailboxSize = 64
	}
	if config.MaxBatch < 1 {
		config.MaxBatch = 32
	}
	runtime := &Runtime{node: config.NodeID, mailboxSize: config.MailboxSize, registry: newLocalRegistry(), pending: newPendingCalls(), timers: newTimerManager()}
	runtime.scheduler = newScheduler(runtime, config.Workers, config.MaxBatch)
	return runtime
}

// CreateService creates, initializes, and registers a Service.
func (r *Runtime) CreateService(spec ServiceSpec) (ServiceRef, error) {
	if spec.Service == nil {
		return ServiceRef{}, ErrInvalidServiceSpec
	}
	ref := ServiceRef{Node: r.node, ID: ServiceID(r.nextID.Add(1))}
	instance := &serviceInstance{ref: ref, service: spec.Service, mailbox: newMailbox(r.mailboxSize), policy: spec.Policy}
	instance.setStatus(ServiceStarting)
	r.registry.add(instance)
	if err := spec.Service.Init(serviceContext{runtime: r, self: ref}); err != nil {
		r.registry.remove(ref)
		return ServiceRef{}, err
	}
	instance.setStatus(ServiceRunning)
	return ref, nil
}

// Send asynchronously delivers a Command to a local Service.
func (r *Runtime) Send(target ServiceRef, id CommandID, payload any) error {
	return r.sendFrom(ServiceRef{}, target, id, payload)
}

func (r *Runtime) route(envelope Envelope) error {
	return r.sendEnvelope(envelope)
}

func (r *Runtime) sendFrom(source, target ServiceRef, id CommandID, payload any) error {
	return r.sendEnvelope(Envelope{Source: source, Target: target, Command: id, Payload: payload})
}

func (r *Runtime) sendEnvelope(envelope Envelope) error {
	target := envelope.Target
	if target.Node != r.node {
		return ErrServiceNotFound
	}
	instance, err := r.registry.get(target)
	if err != nil {
		return err
	}
	if ServiceStatus(instance.status.Load()) != ServiceRunning {
		return ErrServiceClosed
	}
	if err := instance.mailbox.push(envelope); err != nil {
		return err
	}
	r.scheduler.schedule(instance)
	return nil
}

// After schedules a future Command delivery to target.
func (r *Runtime) After(target ServiceRef, delay time.Duration, id CommandID, payload any) (TimerID, error) {
	if _, err := r.registry.get(target); err != nil {
		return 0, err
	}
	return r.timers.add(target, delay, func() { _ = r.Send(target, id, payload) }), nil
}

// Cancel prevents a timer from delivering its Command. It is idempotent.
func (r *Runtime) Cancel(id TimerID) error { r.timers.cancel(id); return nil }

// Close stops Runtime workers. Service lifecycle shutdown is added separately.
func (r *Runtime) Close(context.Context) error { r.scheduler.close(); return nil }
