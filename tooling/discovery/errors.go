package discovery

import "errors"

var (
	// ErrInvalidConfig indicates invalid Discovery Service or Client configuration.
	ErrInvalidConfig = errors.New("discovery: invalid config")
	// ErrInvalidNode indicates an empty node, address, or lease generation.
	ErrInvalidNode = errors.New("discovery: invalid node")
	// ErrNodeNotFound indicates that no active node lease exists.
	ErrNodeNotFound = errors.New("discovery: node not found")
	// ErrLeaseExpired indicates that a node lease is absent, expired, or superseded.
	ErrLeaseExpired = errors.New("discovery: lease expired")
	// ErrInvalidName indicates an invalid long-lived ServiceName binding.
	ErrInvalidName = errors.New("discovery: invalid service name")
	// ErrNameNotFound indicates that no active long-lived ServiceName binding exists.
	ErrNameNotFound = errors.New("discovery: service name not found")
	// ErrNameConflict indicates that another active lease owns the ServiceName.
	ErrNameConflict = errors.New("discovery: service name conflict")
	// ErrInvalidResponse indicates that a Discovery Reply has an unexpected shape.
	ErrInvalidResponse = errors.New("discovery: invalid response")
	// ErrUnsupportedCommand indicates that no Codec handles a CommandID.
	ErrUnsupportedCommand = errors.New("discovery: unsupported command")
)
