// Package discovery provides node leases and long-lived ServiceName discovery.
package discovery

import (
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// DefaultServiceName is the local name normally assigned to DiscoveryService.
const DefaultServiceName gsr.ServiceName = ".discovery"

// Config configures DiscoveryService lease expiration and cleanup cadence.
type Config struct {
	// LeaseTTL is the duration granted by registration and heartbeat. Zero defaults to 30 seconds.
	LeaseTTL time.Duration
	// SweepInterval controls background cleanup. Zero defaults to 5 seconds.
	SweepInterval time.Duration
}

// NodeLease identifies one registration generation from one Discovery authority.
// Node, AuthorityEpoch, and Generation form the identity; ExpiresAt is informational.
type NodeLease struct {
	Node           gsr.NodeID `json:"node"`
	AuthorityEpoch uint64     `json:"authority_epoch"`
	Generation     uint64     `json:"generation"`
	ExpiresAt      time.Time  `json:"expires_at"`
}

// NodeRecord describes one node whose lease has not expired.
type NodeRecord struct {
	ID             gsr.NodeID `json:"id"`
	Address        string     `json:"address"`
	AuthorityEpoch uint64     `json:"authority_epoch"`
	Generation     uint64     `json:"generation"`
	LastSeen       time.Time  `json:"last_seen"`
	ExpiresAt      time.Time  `json:"expires_at"`
}
