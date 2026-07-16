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
	// RuntimeTaskDispatch runs Service.Handle.
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
	return RuntimeInspection{
		CapturedAt: r.now(),
		Node:       r.node,
		Status:     publicRuntimeStatus(r.state.Load()),
		Services:   services,
		Metrics:    r.metrics.snapshot(),
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
