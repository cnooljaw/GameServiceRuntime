// Package drain provides Visitor lease facts for later Service drain orchestration.
package drain

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// DefaultVisitorRegistryName is the stable local name normally assigned to VisitorRegistryService.
const DefaultVisitorRegistryName gsr.ServiceName = ".visitor-registry"

// VisitorLease identifies one expiring visitor relationship owned by Visitor.
type VisitorLease struct {
	Target         gsr.ServiceRef
	Visitor        gsr.ServiceRef
	AuthorityEpoch uint64
	Generation     uint64
	Weak           bool
	ExpiresAt      time.Time
}

// VisitorRef is one read-only visitor relationship returned by List.
type VisitorRef struct {
	Visitor    gsr.ServiceRef
	Generation uint64
	Weak       bool
	ExpiresAt  time.Time
}

// VisitorRegistryConfig configures Visitor lease expiry and cleanup.
type VisitorRegistryConfig struct {
	LeaseTTL      time.Duration
	SweepInterval time.Duration
}

// GuardConfig binds a wrapped Service to its trusted Drain controller and external work Commands.
type GuardConfig struct {
	Controller       gsr.ServiceRef
	ExternalCommands []gsr.CommandID
}

// DrainStatus is an independent snapshot of one Drain Guard state.
type DrainStatus struct {
	Draining  bool
	StartedAt time.Time
}

// CommandCaller is the narrow Runtime capability required by Client.
type CommandCaller interface {
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

// Client provides typed access to one VisitorRegistryService.
type Client struct {
	caller CommandCaller
	target gsr.ServiceRef
}

// GuardClient provides typed access to one decorated Drain Guard Service.
type GuardClient struct {
	caller CommandCaller
	target gsr.ServiceRef
}
