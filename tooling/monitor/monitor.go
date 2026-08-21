package monitor

import (
	"encoding/json"
	"io"
	"reflect"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// Inspector provides independent local Runtime inspections.
type Inspector interface {
	Inspect() gsr.RuntimeInspection
}

// Monitor converts local Runtime inspections into stable reports.
type Monitor struct {
	inspector Inspector
}

// New creates a local Monitor for inspector.
func New(inspector Inspector) (*Monitor, error) {
	if isNil(inspector) {
		return nil, ErrInvalidInspector
	}
	return &Monitor{inspector: inspector}, nil
}

// Capture returns a new independent report from one Runtime inspection.
func (m *Monitor) Capture() Report {
	inspection := m.inspector.Inspect()
	services := make([]ServiceReport, len(inspection.Services))
	for index, service := range inspection.Services {
		services[index] = ServiceReport{
			Ref:          reportRef(service.Ref),
			Name:         service.Name,
			Status:       serviceStatus(service.Status),
			MailboxDepth: service.MailboxDepth,
		}
	}
	runners := make([]RunnerReport, len(inspection.Runners))
	for index, runner := range inspection.Runners {
		runners[index] = RunnerReport{
			Name: runner.Name, Status: runnerStatus(runner.Status), Workers: runner.Workers,
			QueueDepth: runner.QueueDepth, Active: runner.Active, Submitted: runner.Submitted,
			Completed: runner.Completed, Failed: runner.Failed, Rejected: runner.Rejected,
			DeliveryFailed: runner.DeliveryFailed,
		}
	}
	tasks := make([]TaskReport, len(inspection.Tasks))
	for index, task := range inspection.Tasks {
		tasks[index] = TaskReport{
			ID:        task.ID,
			Owner:     reportRef(task.Owner),
			Kind:      taskKind(task.Kind),
			StartedAt: task.StartedAt,
			TimedOut:  task.TimedOut,
		}
	}
	durations := inspection.Metrics.Durations()
	durationsNanos := make(map[string]int64, len(durations))
	for name, duration := range durations {
		durationsNanos[name] = int64(duration)
	}
	return Report{
		CapturedAt:   inspection.CapturedAt,
		Node:         inspection.Node,
		Status:       runtimeStatus(inspection.Status),
		ServiceCount: len(services),
		Services:     services,
		RunnerCount:  len(runners),
		Runners:      runners,
		TaskCount:    len(tasks),
		Tasks:        tasks,
		PendingCalls: inspection.PendingCalls,
		Timers:       inspection.Timers,
		Metrics: MetricsReport{
			Counters:       inspection.Metrics.Counters(),
			Gauges:         inspection.Metrics.Gauges(),
			DurationsNanos: durationsNanos,
		},
	}
}

func runnerStatus(status gsr.RunnerStatus) string {
	switch status {
	case gsr.RunnerRunning:
		return "running"
	case gsr.RunnerClosing:
		return "closing"
	case gsr.RunnerClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// WriteJSON writes one newly captured report and a trailing newline to writer.
// It does not close writer.
func (m *Monitor) WriteJSON(writer io.Writer) error {
	if isNil(writer) {
		return ErrInvalidWriter
	}
	return json.NewEncoder(writer).Encode(m.Capture())
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func reportRef(ref gsr.ServiceRef) Ref {
	return Ref{Node: ref.Node, ID: ref.ID}
}

func runtimeStatus(status gsr.RuntimeStatus) string {
	switch status {
	case gsr.RuntimeRunning:
		return "running"
	case gsr.RuntimeClosing:
		return "closing"
	case gsr.RuntimeClosed:
		return "closed"
	default:
		return "unknown"
	}
}

func serviceStatus(status gsr.ServiceStatus) string {
	switch status {
	case gsr.ServiceCreated:
		return "created"
	case gsr.ServiceStarting:
		return "starting"
	case gsr.ServiceRunning:
		return "running"
	case gsr.ServiceStopping:
		return "stopping"
	case gsr.ServiceClosed:
		return "closed"
	case gsr.ServiceFailed:
		return "failed"
	case gsr.ServiceRestarting:
		return "restarting"
	default:
		return "unknown"
	}
}

func taskKind(kind gsr.RuntimeTaskKind) string {
	switch kind {
	case gsr.RuntimeTaskInit:
		return "init"
	case gsr.RuntimeTaskDispatch:
		return "dispatch"
	case gsr.RuntimeTaskStop:
		return "stop"
	case gsr.RuntimeTaskClose:
		return "close"
	default:
		return "unknown"
	}
}
