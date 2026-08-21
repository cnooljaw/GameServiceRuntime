// Package monitor converts local Runtime inspections into stable reports.
package monitor

import (
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// Ref is the stable report form of a ServiceRef.
type Ref struct {
	Node gsr.NodeID    `json:"node"`
	ID   gsr.ServiceID `json:"id"`
}

// Report describes one local Runtime inspection.
type Report struct {
	CapturedAt   time.Time       `json:"captured_at"`
	Node         gsr.NodeID      `json:"node"`
	Status       string          `json:"status"`
	ServiceCount int             `json:"service_count"`
	Services     []ServiceReport `json:"services"`
	RunnerCount  int             `json:"runner_count"`
	Runners      []RunnerReport  `json:"runners"`
	TaskCount    int             `json:"task_count"`
	Tasks        []TaskReport    `json:"tasks"`
	PendingCalls int             `json:"pending_calls"`
	Timers       int             `json:"timers"`
	Metrics      MetricsReport   `json:"metrics"`
}

// RunnerReport describes one Runtime-owned external worker pool.
type RunnerReport struct {
	Name           gsr.RunnerName `json:"name"`
	Status         string         `json:"status"`
	Workers        int            `json:"workers"`
	QueueDepth     int            `json:"queue_depth"`
	Active         int            `json:"active"`
	Submitted      uint64         `json:"submitted"`
	Completed      uint64         `json:"completed"`
	Failed         uint64         `json:"failed"`
	Rejected       uint64         `json:"rejected"`
	DeliveryFailed uint64         `json:"delivery_failed"`
}

// ServiceReport describes one local Service without exposing its instance.
type ServiceReport struct {
	Ref          Ref             `json:"ref"`
	Name         gsr.ServiceName `json:"name"`
	Status       string          `json:"status"`
	MailboxDepth int             `json:"mailbox_depth"`
}

// TaskReport describes one active Runtime-owned execution task.
type TaskReport struct {
	ID        uint64    `json:"id"`
	Owner     Ref       `json:"owner"`
	Kind      string    `json:"kind"`
	StartedAt time.Time `json:"started_at"`
	TimedOut  bool      `json:"timed_out"`
}

// MetricsReport contains independent Runtime and business metric values.
type MetricsReport struct {
	Counters       map[string]uint64 `json:"counters"`
	Gauges         map[string]int64  `json:"gauges"`
	DurationsNanos map[string]int64  `json:"durations_ns"`
}
