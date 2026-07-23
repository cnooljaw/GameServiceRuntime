package servicegroup

import "errors"

var (
	// ErrInvalidConfig reports invalid Directory, Client, or Router configuration.
	ErrInvalidConfig = errors.New("servicegroup: invalid config")
	// ErrInvalidCaller reports a nil Runtime capability dependency.
	ErrInvalidCaller = errors.New("servicegroup: invalid caller")
	// ErrInvalidGroup reports a malformed GroupName.
	ErrInvalidGroup = errors.New("servicegroup: invalid group")
	// ErrInvalidServiceSet reports malformed ServiceSet content or version identity.
	ErrInvalidServiceSet = errors.New("servicegroup: invalid service set")
	// ErrGroupNotFound reports a Group that has never been published.
	ErrGroupNotFound = errors.New("servicegroup: group not found")
	// ErrVersionConflict reports a failed compare-and-set publish.
	ErrVersionConflict = errors.New("servicegroup: version conflict")
	// ErrVersionExhausted reports a ServiceSet revision that cannot be incremented.
	ErrVersionExhausted = errors.New("servicegroup: version exhausted")
	// ErrUnauthorized reports a Publish from a node other than the configured publisher.
	ErrUnauthorized = errors.New("servicegroup: unauthorized")
	// ErrInvalidWatch reports a malformed subscriber or Watch lease.
	ErrInvalidWatch = errors.New("servicegroup: invalid watch")
	// ErrWatchExpired reports a missing, expired, stale-generation, or stale-authority Watch lease.
	ErrWatchExpired = errors.New("servicegroup: watch expired")
	// ErrWatchOwnerMismatch reports a Watch operation attempted by another ServiceRef.
	ErrWatchOwnerMismatch = errors.New("servicegroup: watch owner mismatch")
	// ErrInvalidResponse reports a malformed Directory response or wire payload.
	ErrInvalidResponse = errors.New("servicegroup: invalid response")
	// ErrUnsupportedCommand reports a payload not owned by the ServiceGroup Codec.
	ErrUnsupportedCommand = errors.New("servicegroup: unsupported command")
)
