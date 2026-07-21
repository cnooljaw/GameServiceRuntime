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
	LeaseTTL      time.Duration
	SweepInterval time.Duration
}

// NodeLease identifies one registration generation for a Runtime node.
// Node and Generation form the identity; ExpiresAt is informational.
type NodeLease struct {
	Node       gsr.NodeID
	Generation uint64
	ExpiresAt  time.Time
}

// NodeRecord describes one node whose lease has not expired.
type NodeRecord struct {
	ID         gsr.NodeID
	Address    string
	Generation uint64
	LastSeen   time.Time
	ExpiresAt  time.Time
}
