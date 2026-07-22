package snapshot

import "errors"

var (
	// ErrInvalidConfig indicates that a Manager dependency or option is invalid.
	ErrInvalidConfig = errors.New("snapshot: invalid config")
	// ErrInvalidContext indicates that an operation has no usable context.
	ErrInvalidContext = errors.New("snapshot: invalid context")
	// ErrInvalidKey indicates that a stable business key is malformed.
	ErrInvalidKey = errors.New("snapshot: invalid key")
	// ErrInvalidTarget indicates that a Snapshot target is not a concrete ServiceRef.
	ErrInvalidTarget = errors.New("snapshot: invalid target")
	// ErrInvalidState indicates that Snapshot state metadata or payload is malformed.
	ErrInvalidState = errors.New("snapshot: invalid state")
	// ErrPayloadTooLarge indicates that a Snapshot exceeds the Manager payload limit.
	ErrPayloadTooLarge = errors.New("snapshot: payload too large")
	// ErrSnapshotNotFound indicates that no Snapshot exists for a Key.
	ErrSnapshotNotFound = errors.New("snapshot: not found")
	// ErrStaleSnapshot indicates that a newer revision is already stored.
	ErrStaleSnapshot = errors.New("snapshot: stale revision")
	// ErrSnapshotConflict indicates that one revision has different state contents.
	ErrSnapshotConflict = errors.New("snapshot: revision conflict")
	// ErrInvalidResponse indicates that a Capture reply or Codec value has the wrong shape.
	ErrInvalidResponse = errors.New("snapshot: invalid response")
	// ErrUnsupportedCommand indicates that a Codec cannot handle a Command.
	ErrUnsupportedCommand = errors.New("snapshot: unsupported command")
)
