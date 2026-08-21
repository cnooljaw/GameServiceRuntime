package gsr

import (
	"sort"
	"time"
)

// RuntimeStatus describes the lifecycle state visible through Runtime.Inspect.
type RuntimeStatus int

const (
	// RuntimeRunning accepts new work.
	RuntimeRunning RuntimeStatus = iota
	// RuntimeClosing is completing shutdown work.
	RuntimeClosing
	// RuntimeClosed has completed or exhausted shutdown.
	RuntimeClosed
)

// RuntimeTaskKind identifies a Runtime-owned Service execution task.
type RuntimeTaskKind string

const (
	// RuntimeTaskInit runs Service.Init.
	RuntimeTaskInit RuntimeTaskKind = "init"
	// RuntimeTaskDispatch runs one scheduled Service mailbox batch.
	RuntimeTaskDispatch RuntimeTaskKind = "dispatch"
	// RuntimeTaskStop runs Service.Stop.
	RuntimeTaskStop RuntimeTaskKind = "stop"
	// RuntimeTaskClose runs Service.Close.
	RuntimeTaskClose RuntimeTaskKind = "close"
)

// RuntimeInspection is an independent, eventually consistent view of Runtime state.
type RuntimeInspection struct {
	CapturedAt   time.Time
	Node         NodeID
	Status       RuntimeStatus
	Services     []ServiceInspection
	Runners      []RunnerInspection
	Tasks        []RuntimeTaskInspection
	PendingCalls int
	Timers       int
	Metrics      MetricsSnapshot
}

// ServiceInspection describes one local Service without exposing its instance.
type ServiceInspection struct {
	Ref          ServiceRef
	Name         ServiceName
	Status       ServiceStatus
	MailboxDepth int
}

// RunnerInspection describes one Runtime-owned Runner without exposing its workers or queue.
type RunnerInspection struct {
	Name           RunnerName
	Status         RunnerStatus
	Workers        int
	QueueDepth     int
	Active         int
	Submitted      uint64
	Completed      uint64
	Failed         uint64
	Rejected       uint64
	DeliveryFailed uint64
}

// RuntimeTaskInspection describes one active Runtime-owned execution task.
type RuntimeTaskInspection struct {
	ID        uint64
	Owner     ServiceRef
	Kind      RuntimeTaskKind
	StartedAt time.Time
	TimedOut  bool
}

// Inspect returns independent copies of observable Runtime state.
// Subsystems are copied separately, so the result is not an atomic transaction.
func (r *Runtime) Inspect() RuntimeInspection {
	instances := r.registry.snapshot()
	services := make([]ServiceInspection, 0, len(instances))
	for _, instance := range instances {
		services = append(services, ServiceInspection{
			Ref:          instance.ref,
			Name:         instance.name,
			Status:       ServiceStatus(instance.status.Load()),
			MailboxDepth: instance.mailbox.depth(),
		})
	}
	sort.Slice(services, func(left, right int) bool {
		if services[left].Ref.Node != services[right].Ref.Node {
			return services[left].Ref.Node < services[right].Ref.Node
		}
		return services[left].Ref.ID < services[right].Ref.ID
	})
	taskSnapshots := r.tasks.active()
	tasks := make([]RuntimeTaskInspection, 0, len(taskSnapshots))
	for _, task := range taskSnapshots {
		tasks = append(tasks, RuntimeTaskInspection{
			ID:        uint64(task.id),
			Owner:     task.owner,
			Kind:      publicRuntimeTaskKind(task.kind),
			StartedAt: task.started,
			TimedOut:  task.timedOut,
		})
	}
	sort.Slice(tasks, func(left, right int) bool { return tasks[left].ID < tasks[right].ID })
	return RuntimeInspection{
		CapturedAt:   r.now(),
		Node:         r.node,
		Status:       publicRuntimeStatus(r.state.Load()),
		Services:     services,
		Runners:      r.runners.inspections(),
		Tasks:        tasks,
		PendingCalls: r.pending.count(),
		Timers:       r.timers.count(),
		Metrics:      r.metrics.snapshot(),
	}
}

func publicRuntimeStatus(state int32) RuntimeStatus {
	switch state {
	case runtimeRunning:
		return RuntimeRunning
	case runtimeClosing:
		return RuntimeClosing
	default:
		return RuntimeClosed
	}
}

func publicRuntimeTaskKind(kind runtimeTaskKind) RuntimeTaskKind {
	switch kind {
	case runtimeTaskInit:
		return RuntimeTaskInit
	case runtimeTaskDispatch:
		return RuntimeTaskDispatch
	case runtimeTaskStop:
		return RuntimeTaskStop
	case runtimeTaskClose:
		return RuntimeTaskClose
	default:
		return ""
	}
}
