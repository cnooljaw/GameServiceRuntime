// Package game provides Mailbox-owned business Service templates.
package game

import "errors"

var (
	// ErrInvalidID indicates a stable business identifier is malformed.
	ErrInvalidID = errors.New("game: invalid id")
	// ErrInvalidRequestID indicates an idempotency key is malformed.
	ErrInvalidRequestID = errors.New("game: invalid request id")
	// ErrInvalidParticipant indicates a Battle participant is malformed or duplicated.
	ErrInvalidParticipant = errors.New("game: invalid participant")
	// ErrRequestConflict indicates one RequestID was reused with different normalized input.
	ErrRequestConflict = errors.New("game: request conflict")
	// ErrStateConflict indicates a Command is illegal in the owner's current state.
	ErrStateConflict = errors.New("game: state conflict")
	// ErrUnavailable indicates an external capability rejected immediate submission.
	ErrUnavailable = errors.New("game: unavailable")
	// ErrInvalidConfig indicates a Service dependency or configuration is invalid.
	ErrInvalidConfig = errors.New("game: invalid config")
	// ErrInvalidCommand indicates a Command or payload is invalid for the target Service.
	ErrInvalidCommand = errors.New("game: invalid command")
	// ErrInvalidSettlement indicates a settlement request or result is malformed.
	ErrInvalidSettlement = errors.New("game: invalid settlement")
	// ErrNotFound indicates the requested owner-local fact is unknown.
	ErrNotFound = errors.New("game: not found")
	// ErrUnauthorized indicates a private result did not come from its trusted source.
	ErrUnauthorized = errors.New("game: unauthorized")
	// ErrClosed indicates an owner no longer accepts mutable business input.
	ErrClosed = errors.New("game: closed")
)
