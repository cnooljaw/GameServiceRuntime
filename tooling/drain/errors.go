package drain

import "errors"

var (
	// ErrInvalidConfig reports invalid VisitorRegistryService or Client configuration.
	ErrInvalidConfig = errors.New("drain: invalid config")
	// ErrInvalidCaller reports a nil Runtime capability dependency.
	ErrInvalidCaller = errors.New("drain: invalid caller")
	// ErrInvalidLease reports a malformed VisitorLease.
	ErrInvalidLease = errors.New("drain: invalid visitor lease")
	// ErrInvalidTarget reports a malformed target ServiceRef.
	ErrInvalidTarget = errors.New("drain: invalid target")
	// ErrInvalidVisitor reports a malformed visitor ServiceRef.
	ErrInvalidVisitor = errors.New("drain: invalid visitor")
	// ErrLeaseExpired reports a missing, expired, stale-generation, or stale-authority VisitorLease.
	ErrLeaseExpired = errors.New("drain: visitor lease expired")
	// ErrLeaseOwnerMismatch reports a lease mutation attempted by another ServiceRef.
	ErrLeaseOwnerMismatch = errors.New("drain: visitor lease owner mismatch")
	// ErrLeaseExhausted reports a VisitorLease generation that cannot be incremented.
	ErrLeaseExhausted = errors.New("drain: visitor lease generation exhausted")
	// ErrInvalidResponse reports a malformed VisitorRegistryService response or wire payload.
	ErrInvalidResponse = errors.New("drain: invalid response")
	// ErrUnsupportedCommand reports a payload not owned by the Drain Codec.
	ErrUnsupportedCommand = errors.New("drain: unsupported command")
)
