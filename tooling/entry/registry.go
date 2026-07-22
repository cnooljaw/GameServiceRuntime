package entry

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

const defaultRegistryCapacity = 1024

// InMemorySessionRegistry retains short-lived secret material and Gateway bindings in process memory.
type InMemorySessionRegistry struct {
	mu       sync.Mutex
	capacity int
	now      func() time.Time
	pending  map[SecretRef]secretMaterial
	sessions map[sessionKey]sessionRecord
}

type sessionKey struct {
	uid    string
	server string
}

type secretMaterial struct {
	identity AuthIdentity
	secret   []byte
	expires  time.Time
}

type sessionRecord struct {
	ticket     LoginTicket
	identity   AuthIdentity
	secret     []byte
	lastSeq    uint64
	connection ConnectionID
}

// NewInMemorySessionRegistry creates a bounded in-memory SessionRegistry.
func NewInMemorySessionRegistry(config RegistryConfig) (*InMemorySessionRegistry, error) {
	if config.Capacity < 0 {
		return nil, ErrInvalidConfig
	}
	if config.Capacity == 0 {
		config.Capacity = defaultRegistryCapacity
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &InMemorySessionRegistry{capacity: config.Capacity, now: config.Now, pending: make(map[SecretRef]secretMaterial), sessions: make(map[sessionKey]sessionRecord)}, nil
}

// StoreSecret retains authenticated secret material until LoginService issues or rejects its ticket.
func (r *InMemorySessionRegistry) StoreSecret(identity AuthIdentity, secret []byte, expiresAt time.Time) (SecretRef, error) {
	if !validIdentity(identity) || len(secret) < 32 || expiresAt.IsZero() || !expiresAt.After(r.now()) {
		return "", ErrInvalidIdentity
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneExpiredLocked(r.now())
	if len(r.pending)+len(r.sessions) >= r.capacity {
		return "", ErrSessionCapacity
	}
	ref, err := newSecretRef()
	if err != nil {
		return "", err
	}
	r.pending[ref] = secretMaterial{identity: identity, secret: append([]byte(nil), secret...), expires: expiresAt}
	return ref, nil
}

// DiscardSecret removes unissued secret material after LoginService rejects a ticket request.
func (r *InMemorySessionRegistry) DiscardSecret(ref SecretRef) {
	r.mu.Lock()
	material, exists := r.pending[ref]
	delete(r.pending, ref)
	r.mu.Unlock()
	if exists {
		zero(material.secret)
	}
}

// Issue atomically turns stored secret material into the ticket that Gateway will verify.
func (r *InMemorySessionRegistry) Issue(ticket LoginTicket, identity AuthIdentity) error {
	_, err := r.Replace(ticket, identity, nil)
	return err
}

// Replace atomically issues ticket and revokes previous when it is still the current ticket.
func (r *InMemorySessionRegistry) Replace(ticket LoginTicket, identity AuthIdentity, previous *LoginTicket) (ConnectionID, error) {
	if !validTicket(ticket) || !validIdentity(identity) || ticket.Server != identity.Server {
		return "", ErrInvalidTicket
	}
	if previous != nil && !validTicket(*previous) {
		return "", ErrInvalidTicket
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	material, exists := r.pending[ticket.SecretRef]
	if !exists || material.identity != identity || !material.expires.Equal(ticket.ExpiresAt) {
		return "", ErrUnauthorized
	}
	if !ticket.ExpiresAt.After(r.now()) {
		return "", ErrTicketExpired
	}
	var replaced ConnectionID
	if previous != nil {
		key := sessionKey{uid: previous.UID, server: previous.Server}
		if old, current := r.sessions[key]; current && old.ticket.Generation == previous.Generation && old.ticket.SubID == previous.SubID {
			delete(r.sessions, key)
			replaced = old.connection
			zero(old.secret)
		}
	}
	delete(r.pending, ticket.SecretRef)
	key := sessionKey{uid: ticket.UID, server: ticket.Server}
	if old, exists := r.sessions[key]; exists {
		zero(old.secret)
	}
	r.sessions[key] = sessionRecord{ticket: ticket, identity: identity, secret: material.secret}
	return replaced, nil
}

// VerifyAndBind validates one proof and records its strictly increasing sequence with a connection binding.
func (r *InMemorySessionRegistry) VerifyAndBind(proof GatewayProof, connectionID ConnectionID) (SessionBinding, error) {
	if !validProofFields(proof) || !validText(string(connectionID)) {
		return SessionBinding{}, ErrInvalidProof
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := sessionKey{uid: proof.UID, server: proof.Server}
	record, exists := r.sessions[key]
	if !exists {
		return SessionBinding{}, ErrSessionRevoked
	}
	if !record.ticket.ExpiresAt.After(r.now()) {
		delete(r.sessions, key)
		zero(record.secret)
		return SessionBinding{}, ErrTicketExpired
	}
	if record.ticket.SubID != proof.SubID || record.ticket.Generation != proof.Generation || !VerifyGatewayProof(record.secret, proof) {
		return SessionBinding{}, ErrInvalidProof
	}
	if proof.Sequence <= record.lastSeq {
		return SessionBinding{}, ErrProofReplay
	}
	replaced := record.connection
	record.lastSeq = proof.Sequence
	record.connection = connectionID
	r.sessions[key] = record
	return SessionBinding{Identity: SessionIdentity{UID: record.ticket.UID, SubID: record.ticket.SubID, PlayerID: record.identity.PlayerID, Server: record.ticket.Server, Generation: record.ticket.Generation}, ReplacedConnectionID: replaced}, nil
}

// Unbind clears a connection only when it still belongs to the supplied generation and connection ID.
func (r *InMemorySessionRegistry) Unbind(connectionID ConnectionID, generation uint64) {
	if !validText(string(connectionID)) || generation == 0 {
		return
	}
	r.mu.Lock()
	for key, record := range r.sessions {
		if record.ticket.Generation == generation && record.connection == connectionID {
			record.connection = ""
			r.sessions[key] = record
		}
	}
	r.mu.Unlock()
}

// Revoke removes the matching current ticket and returns its bound connection, if any.
func (r *InMemorySessionRegistry) Revoke(uid, server string, generation uint64) ConnectionID {
	if !validText(uid) || !validText(server) || generation == 0 {
		return ""
	}
	r.mu.Lock()
	key := sessionKey{uid: uid, server: server}
	record, exists := r.sessions[key]
	if exists && record.ticket.Generation == generation {
		delete(r.sessions, key)
	}
	r.mu.Unlock()
	if !exists || record.ticket.Generation != generation {
		return ""
	}
	zero(record.secret)
	return record.connection
}

// Connection returns the currently bound Gateway connection for uid and server.
func (r *InMemorySessionRegistry) Connection(uid, server string) ConnectionID {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.sessions[sessionKey{uid: uid, server: server}]
	return record.connection
}

func (r *InMemorySessionRegistry) pruneExpiredLocked(now time.Time) {
	for ref, material := range r.pending {
		if !material.expires.After(now) {
			delete(r.pending, ref)
			zero(material.secret)
		}
	}
	for key, record := range r.sessions {
		if !record.ticket.ExpiresAt.After(now) {
			delete(r.sessions, key)
			zero(record.secret)
		}
	}
}

func newSecretRef() (SecretRef, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return SecretRef(base64.RawURLEncoding.EncodeToString(bytes)), nil
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
