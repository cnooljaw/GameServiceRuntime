package entry

import "time"

// SecretRef identifies secret material retained only inside a SessionRegistry.
type SecretRef string

// ConnectionID identifies one Gateway connection without exposing its net.Conn.
type ConnectionID string

// AuthIdentity is the authenticated identity returned by a Login Adapter handshake.
type AuthIdentity struct {
	AccountID string
	PlayerID  string
	Server    string
}

// LoginTicket is a short-lived Gateway admission ticket without plaintext secret material.
type LoginTicket struct {
	UID        string
	SubID      string
	Server     string
	SecretRef  SecretRef
	Generation uint64
	ExpiresAt  time.Time
}

// SessionIdentity is the identity available after Gateway proof verification.
type SessionIdentity struct {
	UID        string
	SubID      string
	PlayerID   string
	Server     string
	Generation uint64
}

// GatewayProof is the authenticated content of one Gateway AUTH line.
type GatewayProof struct {
	UID        string
	SubID      string
	Server     string
	Generation uint64
	Sequence   uint64
	MAC        []byte
}

// SessionBinding is the result of a successful atomic proof verification and connection bind.
type SessionBinding struct {
	Identity             SessionIdentity
	ReplacedConnectionID ConnectionID
}

// RegistryConfig configures an in-memory SessionRegistry.
type RegistryConfig struct {
	// Capacity limits the combined number of pending secrets and active tickets. Zero defaults to 1024.
	Capacity int
	// Now supplies the clock for expiry checks. Nil uses time.Now.
	Now func() time.Time
}
