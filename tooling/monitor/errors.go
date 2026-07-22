package monitor

import "errors"

var (
	// ErrInvalidInspector indicates that Monitor has no inspection source.
	ErrInvalidInspector = errors.New("monitor: invalid inspector")
	// ErrInvalidWriter indicates that JSON output has no destination.
	ErrInvalidWriter = errors.New("monitor: invalid writer")
)
