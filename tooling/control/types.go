// Package control provides the read-only Cluster Control Plane services.
package control

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/monitor"
)

// DefaultNodeAgentName is the stable local name normally assigned to NodeAgentService.
const DefaultNodeAgentName gsr.ServiceName = ".node-agent"

// DefaultObserverName is the stable local name normally assigned to ClusterObserverService.
const DefaultObserverName gsr.ServiceName = ".cluster-observer"

// Reporter captures one independent local Monitor report.
type Reporter interface {
	Capture() monitor.Report
}

// NodeAgentConfig configures the local NodeAgentService read boundary.
type NodeAgentConfig struct {
	Reporter     Reporter
	ObserverNode gsr.NodeID
}

// NodeConfig is static deployment configuration for one cluster node.
// It is an observation target, not a reconcilable Desired State.
type NodeConfig struct {
	ID      gsr.NodeID `json:"id"`
	Address string     `json:"address"`
	Role    string     `json:"role"`
	Enabled bool       `json:"enabled"`
}

// NodeTarget joins node configuration to the current NodeAgent ServiceRef used by ClusterObserverService.
type NodeTarget struct {
	Config NodeConfig
	Agent  gsr.ServiceRef
}

// NodeStatus describes the latest Control Plane observation of one desired node.
type NodeStatus string

const (
	// NodeUnknown means an enabled node has not yet been refreshed.
	NodeUnknown NodeStatus = "unknown"
	// NodeHealthy means the latest refresh returned a valid NodeAgent report.
	NodeHealthy NodeStatus = "healthy"
	// NodeUnavailable means the latest refresh could not obtain a valid NodeAgent report.
	NodeUnavailable NodeStatus = "unavailable"
	// NodeDisabled means deployment configuration disables the node.
	NodeDisabled NodeStatus = "disabled"
)

// NodeObservedState is the cached result of the most recent refresh for one node.
type NodeObservedState struct {
	ID         gsr.NodeID    `json:"id"`
	Status     NodeStatus    `json:"status"`
	CapturedAt time.Time     `json:"captured_at"`
	Latency    time.Duration `json:"latency"`
	LastError  string        `json:"last_error"`
}

// NodeDetail combines deployment node configuration with the latest independent observation.
type NodeDetail struct {
	Config    NodeConfig        `json:"config"`
	Observed  NodeObservedState `json:"observed"`
	Report    monitor.Report    `json:"report"`
	HasReport bool              `json:"has_report"`
}

// ObserverConfig configures static node observation targets and per-node refresh calls.
type ObserverConfig struct {
	Nodes       []NodeTarget
	CallTimeout time.Duration
	Now         func() time.Time
}

// CommandCaller is the narrow typed Call capability required by Client.
type CommandCaller interface {
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

// Client calls a ClusterControlService through Runtime Call.
type Client struct {
	caller CommandCaller
	target gsr.ServiceRef
}
