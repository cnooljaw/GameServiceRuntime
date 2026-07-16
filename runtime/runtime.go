package gsr

import (
	"context"
	"sync/atomic"
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
	runtime := &Runtime{node: config.NodeID, mailboxSize: config.MailboxSize, registry: newLocalRegistry()}
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
func (r *Runtime) sendFrom(source, target ServiceRef, id CommandID, payload any) error {
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
	if err := instance.mailbox.push(Envelope{Source: source, Target: target, Command: id, Payload: payload}); err != nil {
		return err
	}
	r.scheduler.schedule(instance)
	return nil
}

// Close stops Runtime workers. Service lifecycle shutdown is added separately.
func (r *Runtime) Close(context.Context) error { r.scheduler.close(); return nil }
