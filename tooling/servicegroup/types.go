// Package servicegroup provides versioned ServiceGroup facts and routing policies.
package servicegroup

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// DefaultDirectoryName is the stable local name normally assigned to DirectoryService.
const DefaultDirectoryName gsr.ServiceName = ".service-directory"

// GroupName identifies one group of Services that share a responsibility.
type GroupName string

// ServiceSetVersion identifies one ServiceSet revision from one Directory authority.
type ServiceSetVersion struct {
	AuthorityEpoch uint64 `json:"authority_epoch"`
	Revision       uint64 `json:"revision"`
}

// ServiceSet is one complete immutable-by-contract snapshot of a ServiceGroup.
type ServiceSet struct {
	Name    GroupName         `json:"name"`
	Version ServiceSetVersion `json:"version"`
	Refs    []gsr.ServiceRef  `json:"refs"`
	Tags    map[string]string `json:"tags"`
}

// WatchLease identifies one subscriber generation from one Directory authority.
type WatchLease struct {
	Group          GroupName      `json:"group"`
	Subscriber     gsr.ServiceRef `json:"subscriber"`
	AuthorityEpoch uint64         `json:"authority_epoch"`
	Generation     uint64         `json:"generation"`
	ExpiresAt      time.Time      `json:"expires_at"`
}

// WatchResult combines a new Watch lease with the current same-mailbox snapshot.
type WatchResult struct {
	Lease   WatchLease
	Current ServiceSet
	Found   bool
}

// ServiceSetChanged is the complete snapshot delivered to a Watch subscriber.
type ServiceSetChanged struct {
	Set ServiceSet
}

// RoutingKey is the caller-provided key consumed by a RoutingPolicy.
type RoutingKey string

// RoutingPolicy maps one complete ServiceSet to one or more member ServiceRefs.
type RoutingPolicy interface {
	Pick(ServiceSet, RoutingKey) ([]gsr.ServiceRef, error)
}

// CommandDispatcher is the narrow Runtime capability required by Router.
type CommandDispatcher interface {
	Send(gsr.ServiceRef, gsr.CommandID, any) error
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

// Router dispatches Commands to targets selected from an explicit ServiceSet.
type Router struct {
	dispatcher CommandDispatcher
}

// DirectoryConfig configures the trusted publisher and Watch lease cleanup.
type DirectoryConfig struct {
	// PublisherNode is the trusted cluster node allowed to publish ServiceSets.
	PublisherNode gsr.NodeID
	// WatchTTL is the duration granted to a Watch lease. Zero defaults to 30 seconds.
	WatchTTL time.Duration
	// SweepInterval controls Watch lease cleanup. Zero defaults to 5 seconds.
	SweepInterval time.Duration
}

// CommandCaller is the narrow Runtime capability required by Client.
type CommandCaller interface {
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

// Client provides typed access to one DirectoryService.
type Client struct {
	caller CommandCaller
	target gsr.ServiceRef
}
