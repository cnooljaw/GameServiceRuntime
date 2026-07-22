// Package entry provides the Runtime Tooling client login and Gateway entry boundary.
package entry

import "errors"

var (
	// ErrInvalidConfig indicates malformed entry tooling configuration.
	ErrInvalidConfig = errors.New("entry: invalid config")
	// ErrInvalidIdentity indicates malformed authenticated identity data.
	ErrInvalidIdentity = errors.New("entry: invalid identity")
	// ErrInvalidTicket indicates malformed ticket or secret-reference data.
	ErrInvalidTicket = errors.New("entry: invalid login ticket")
	// ErrUnauthorized indicates an unauthenticated or unknown entry request.
	ErrUnauthorized = errors.New("entry: unauthorized")
	// ErrTicketExpired indicates that the matching login ticket has expired.
	ErrTicketExpired = errors.New("entry: ticket expired")
	// ErrSessionRevoked indicates that a formerly valid ticket is no longer current.
	ErrSessionRevoked = errors.New("entry: session revoked")
	// ErrInvalidProof indicates an invalid Gateway AUTH line or HMAC proof.
	ErrInvalidProof = errors.New("entry: invalid proof")
	// ErrProofReplay indicates that a Gateway proof sequence was already accepted.
	ErrProofReplay = errors.New("entry: proof replay")
	// ErrSessionCapacity indicates that Registry cannot retain another active or pending session.
	ErrSessionCapacity = errors.New("entry: session capacity reached")
)
