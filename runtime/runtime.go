package gsr

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	runtimeRunning int32 = iota + 1
	runtimeClosing
	runtimeClosed
)

// Config configures a Runtime instance.
type Config struct {
	NodeID               NodeID
	Workers              int
	MailboxSize          int
	MaxBatch             int
	SlowCommandThreshold time.Duration
	ShutdownTimeout      time.Duration
	TombstoneTTL         time.Duration
	TombstoneLimit       int
	Logger               *slog.Logger
	Now                  func() time.Time
}

// Runtime owns local Service instances and their message dispatch.
type Runtime struct {
	node                 NodeID
	mailboxSize          int
	slowCommandThreshold time.Duration
	shutdownTimeout      time.Duration
	registry             *localRegistry
	scheduler            *scheduler
	pending              *pendingCalls
	timers               *timerManager
	tasks                *taskTracker
	metrics              *metricCollector
	logger               *slog.Logger
	now                  func() time.Time
	nextID               atomic.Uint64
	state                atomic.Int32
	createMu             sync.Mutex
	creating             sync.WaitGroup
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
	if config.SlowCommandThreshold <= 0 {
		config.SlowCommandThreshold = 100 * time.Millisecond
	}
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = 5 * time.Second
	}
	if config.TombstoneTTL <= 0 {
		config.TombstoneTTL = time.Minute
	}
	if config.TombstoneLimit <= 0 {
		config.TombstoneLimit = 4096
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	metrics := newMetricCollector()
	runtime := &Runtime{node: config.NodeID, mailboxSize: config.MailboxSize, slowCommandThreshold: config.SlowCommandThreshold, shutdownTimeout: config.ShutdownTimeout, pending: newPendingCalls(), timers: newTimerManager(), metrics: metrics, logger: config.Logger, now: config.Now}
	runtime.registry = newLocalRegistry(config.TombstoneTTL, config.TombstoneLimit, config.Now)
	runtime.tasks = newTaskTracker(metrics, config.Now)
	runtime.state.Store(runtimeRunning)
	runtime.scheduler = newScheduler(runtime, config.Workers, config.MaxBatch)
	return runtime
}

// CreateService creates, initializes, and registers a Service.
func (r *Runtime) CreateService(spec ServiceSpec) (ServiceRef, error) {
	r.createMu.Lock()
	if r.state.Load() != runtimeRunning {
		r.createMu.Unlock()
		return ServiceRef{}, ErrRuntimeClosed
	}
	r.creating.Add(1)
	r.createMu.Unlock()
	defer r.creating.Done()
	if spec.Service == nil {
		return ServiceRef{}, ErrInvalidServiceSpec
	}
	commands, err := commandSetFor(spec.Service)
	if err != nil {
		return ServiceRef{}, err
	}
	if spec.Policy.StopTimeout <= 0 {
		spec.Policy.StopTimeout = 5 * time.Second
	}
	if spec.Policy.CloseTimeout <= 0 {
		spec.Policy.CloseTimeout = 5 * time.Second
	}
	if spec.Policy.LifecycleTimeout <= 0 {
		spec.Policy.LifecycleTimeout = spec.Policy.StopTimeout + spec.Policy.CloseTimeout
	}
	ref := ServiceRef{Node: r.node, ID: ServiceID(r.nextID.Add(1))}
	instance := &serviceInstance{ref: ref, name: spec.Name, service: spec.Service, commands: commands, policy: spec.Policy, done: make(chan struct{})}
	instance.mailbox = newMailbox(r.mailboxSize, r.metrics, r.now, mailboxDepthMetric(ref))
	instance.context = &serviceContext{runtime: r, instance: instance}
	instance.setStatus(ServiceCreated)
	if err := r.registry.add(instance); err != nil {
		return ServiceRef{}, err
	}
	instance.setStatus(ServiceStarting)
	if err := r.invokeInlineTask(instance.ref, runtimeTaskInit, func() error { return spec.Service.Init(instance.context) }); err != nil {
		if errors.Is(err, ErrServiceFailed) {
			r.metrics.Inc("service_panics_total")
			r.logger.Error("service init panic", "service", instance.ref, "error", err)
		}
		closeErr := r.runClose(instance)
		result := errors.Join(err, closeErr)
		r.finalize(instance, ServiceFailed, result)
		return ServiceRef{}, result
	}
	r.createMu.Lock()
	if r.state.Load() != runtimeRunning || instance.finalized.Load() || !instance.status.CompareAndSwap(int32(ServiceStarting), int32(ServiceRunning)) {
		r.createMu.Unlock()
		closeErr := r.runClose(instance)
		result := errors.Join(ErrRuntimeClosed, closeErr)
		r.finalize(instance, ServiceClosed, result)
		return ServiceRef{}, result
	}
	r.createMu.Unlock()
	r.metrics.Inc("service_created_total")
	return ref, nil
}

// Resolve resolves a long-lived ServiceName to its current ServiceRef.
func (r *Runtime) Resolve(name ServiceName) (ServiceRef, error) { return r.registry.resolve(name) }

// MetricsSnapshot returns an immutable metrics snapshot.
func (r *Runtime) MetricsSnapshot() MetricsSnapshot { return r.metrics.snapshot() }

// Send asynchronously delivers a Command to a local Service.
func (r *Runtime) Send(target ServiceRef, id CommandID, payload any) error {
	return r.sendFrom(ServiceRef{}, target, id, payload)
}
func (r *Runtime) sendFrom(source, target ServiceRef, id CommandID, payload any) error {
	if source != (ServiceRef{}) {
		instance, err := r.registry.get(source)
		if err != nil {
			return err
		}
		status := ServiceStatus(instance.status.Load())
		if status != ServiceRunning && status != ServiceStopping {
			return ErrServiceClosed
		}
	}
	return r.sendEnvelope(Envelope{Source: source, Target: target, Command: id, Payload: payload})
}
func (r *Runtime) sendEnvelope(envelope Envelope) error {
	if r.state.Load() != runtimeRunning {
		return ErrRuntimeClosed
	}
	if envelope.Target.Node != r.node {
		return ErrServiceNotFound
	}
	instance, err := r.registry.get(envelope.Target)
	if err != nil {
		return err
	}
	instance.acceptMu.Lock()
	defer instance.acceptMu.Unlock()
	if ServiceStatus(instance.status.Load()) != ServiceRunning || instance.finalized.Load() {
		return ErrServiceClosed
	}
	if !instance.commands.supports(envelope.Command) {
		return ErrCommandNotRegistered
	}
	if err := instance.mailbox.pushEnvelope(envelope); err != nil {
		return err
	}
	r.scheduler.schedule(instance)
	return nil
}

// After schedules a future Command delivery to target.
func (r *Runtime) After(target ServiceRef, delay time.Duration, id CommandID, payload any) (TimerID, error) {
	if r.state.Load() != runtimeRunning {
		return 0, ErrRuntimeClosed
	}
	instance, err := r.registry.get(target)
	if err != nil {
		return 0, err
	}
	instance.acceptMu.Lock()
	defer instance.acceptMu.Unlock()
	if ServiceStatus(instance.status.Load()) != ServiceRunning || instance.finalized.Load() {
		return 0, ErrServiceClosed
	}
	if !instance.commands.supports(id) {
		return 0, ErrCommandNotRegistered
	}
	return r.timers.add(target, delay, func() { _ = r.Send(target, id, payload) }), nil
}

// Cancel prevents a timer from delivering its Command. It is idempotent.
func (r *Runtime) Cancel(id TimerID) error { r.timers.cancel(id); return nil }

func (r *Runtime) executeEnvelope(instance *serviceInstance, envelope Envelope) {
	started := r.now()
	instance.setPath(envelope.CallPath)
	defer instance.setPath(nil)
	ctx := &commandContext{self: instance.ref, source: envelope.Source, runtime: r, session: envelope.Session}
	defer func() {
		elapsed := r.now().Sub(started)
		r.metrics.Inc("commands_handled_total")
		r.metrics.Observe("command_duration", elapsed)
		if elapsed >= r.slowCommandThreshold {
			r.metrics.Inc("slow_commands_total")
			r.logger.Warn("slow command", "service", instance.ref, "command", envelope.Command, "duration", elapsed)
		}
		if recovered := recover(); recovered != nil {
			r.metrics.Inc("service_panics_total")
			r.logger.Error("service handler panic", "service", instance.ref, "command", envelope.Command, "panic", recovered)
			if envelope.Session != 0 && !ctx.replied.Load() {
				_ = r.reply(envelope.Source, envelope.Session, nil, ErrServiceFailed)
			}
			r.closeAfterFailure(instance)
		}
	}()
	if err := instance.service.Handle(ctx, Command{ID: envelope.Command, Payload: envelope.Payload}); err != nil {
		r.metrics.Inc("handler_errors_total")
		r.logger.Error("service handler error", "service", instance.ref, "command", envelope.Command, "error", err)
		if envelope.Session != 0 && !ctx.replied.Load() {
			_ = r.reply(envelope.Source, envelope.Session, nil, err)
		}
	}
}

func (r *Runtime) closeAfterFailure(instance *serviceInstance) {
	instance.setStatus(ServiceFailed)
	closeErr := r.runClose(instance)
	r.finalize(instance, ServiceFailed, errors.Join(ErrServiceFailed, closeErr))
}
