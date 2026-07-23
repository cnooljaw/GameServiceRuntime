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

// DirectoryConfig configures the trusted publisher and future Watch lease cleanup.
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
