package supervisor

import "errors"

var (
	// ErrInvalidConfig indicates a nil dependency or malformed option.
	ErrInvalidConfig = errors.New("supervisor: invalid config")
	// ErrInvalidContext indicates that an operation has no usable context.
	ErrInvalidContext = errors.New("supervisor: invalid context")
	// ErrInvalidKey indicates a malformed stable ServiceKey.
	ErrInvalidKey = errors.New("supervisor: invalid service key")
	// ErrInvalidPolicy indicates an unknown or inconsistent RestartPolicy.
	ErrInvalidPolicy = errors.New("supervisor: invalid restart policy")
	// ErrInvalidRegistration indicates a malformed or self-referential registration.
	ErrInvalidRegistration = errors.New("supervisor: invalid registration")
	// ErrAlreadyRegistered indicates a ServiceKey already has a registration.
	ErrAlreadyRegistered = errors.New("supervisor: service already registered")
	// ErrServiceNotRegistered indicates a ServiceKey has no registration.
	ErrServiceNotRegistered = errors.New("supervisor: service not registered")
	// ErrInvalidNotice indicates malformed or mismatched failure data.
	ErrInvalidNotice = errors.New("supervisor: invalid failure notice")
	// ErrDuplicateNotice indicates the current failed generation was already decided.
	ErrDuplicateNotice = errors.New("supervisor: duplicate failure notice")
	// ErrStaleNotice indicates a notice refers to an old ServiceRef or generation.
	ErrStaleNotice = errors.New("supervisor: stale failure notice")
	// ErrRestartSuppressed indicates policy refused another restart.
	ErrRestartSuppressed = errors.New("supervisor: restart suppressed")
	// ErrRecoveryQueueFull indicates Runner cannot accept more bounded work.
	ErrRecoveryQueueFull = errors.New("supervisor: recovery queue full")
	// ErrRunnerClosed indicates Runner no longer accepts recovery work.
	ErrRunnerClosed = errors.New("supervisor: runner closed")
	// ErrSnapshotNotFound indicates no committed recovery Snapshot exists.
	ErrSnapshotNotFound = errors.New("supervisor: snapshot not found")
	// ErrRecoveryFailed indicates a recovery operation failed.
	ErrRecoveryFailed = errors.New("supervisor: recovery failed")
	// ErrCreateFailed indicates Runtime could not create the replacement Service.
	ErrCreateFailed = errors.New("supervisor: create service failed")
	// ErrNamePublishFailed indicates a long-lived binding could not be committed.
	ErrNamePublishFailed = errors.New("supervisor: name publish failed")
	// ErrAbortFailed indicates replacement cleanup could not converge.
	ErrAbortFailed = errors.New("supervisor: abort failed")
	// ErrStaleRecovery indicates a Runner result no longer matches the active attempt.
	ErrStaleRecovery = errors.New("supervisor: stale recovery result")
	// ErrInvalidResponse indicates a typed Client or Runner received the wrong reply shape.
	ErrInvalidResponse = errors.New("supervisor: invalid response")
)
