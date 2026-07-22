package entry

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryVerifyAndBindRejectsReplayAndStaleUnbind(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	registry, err := NewInMemorySessionRegistry(RegistryConfig{Capacity: 4, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewInMemorySessionRegistry() error = %v", err)
	}
	identity := AuthIdentity{AccountID: "account-1", PlayerID: "player-1", Server: "asia"}
	secretRef, err := registry.StoreSecret(identity, []byte("01234567890123456789012345678901"), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("StoreSecret() error = %v", err)
	}
	ticket := LoginTicket{UID: "uid-1", SubID: "sub-1", Server: "asia", SecretRef: secretRef, Generation: 1, ExpiresAt: now.Add(time.Minute)}
	if err := registry.Issue(ticket, identity); err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	first := SignGatewayProof([]byte("01234567890123456789012345678901"), GatewayProof{UID: ticket.UID, SubID: ticket.SubID, Server: ticket.Server, Generation: ticket.Generation, Sequence: 1})
	firstBinding, err := registry.VerifyAndBind(first, "connection-1")
	if err != nil {
		t.Fatalf("VerifyAndBind(first) error = %v", err)
	}
	if firstBinding.Identity.Generation != 1 || firstBinding.ReplacedConnectionID != "" {
		t.Fatalf("first binding = %#v, want generation 1 without replacement", firstBinding)
	}
	if _, err := registry.VerifyAndBind(first, "connection-2"); !errors.Is(err, ErrProofReplay) {
		t.Fatalf("VerifyAndBind(replay) error = %v, want ErrProofReplay", err)
	}

	second := SignGatewayProof([]byte("01234567890123456789012345678901"), GatewayProof{UID: ticket.UID, SubID: ticket.SubID, Server: ticket.Server, Generation: ticket.Generation, Sequence: 2})
	secondBinding, err := registry.VerifyAndBind(second, "connection-2")
	if err != nil {
		t.Fatalf("VerifyAndBind(second) error = %v", err)
	}
	if secondBinding.ReplacedConnectionID != "connection-1" {
		t.Fatalf("replacement = %q, want connection-1", secondBinding.ReplacedConnectionID)
	}

	registry.Unbind("connection-1", 1)
	if current := registry.Connection(ticket.UID, ticket.Server); current != "connection-2" {
		t.Fatalf("Connection() after stale unbind = %q, want connection-2", current)
	}
}

func TestRegistryVerifyAndBindIsAtomicAcrossConcurrentProofs(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	registry, err := NewInMemorySessionRegistry(RegistryConfig{Capacity: 4, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewInMemorySessionRegistry() error = %v", err)
	}
	identity := AuthIdentity{AccountID: "account-1", PlayerID: "player-1", Server: "asia"}
	secret := []byte("01234567890123456789012345678901")
	secretRef, err := registry.StoreSecret(identity, secret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("StoreSecret() error = %v", err)
	}
	ticket := LoginTicket{UID: "uid-1", SubID: "sub-1", Server: "asia", SecretRef: secretRef, Generation: 1, ExpiresAt: now.Add(time.Minute)}
	if err := registry.Issue(ticket, identity); err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	proof := SignGatewayProof(secret, GatewayProof{UID: ticket.UID, SubID: ticket.SubID, Server: ticket.Server, Generation: ticket.Generation, Sequence: 1})

	var successes atomic.Int32
	var group sync.WaitGroup
	for index := 0; index < 32; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if _, err := registry.VerifyAndBind(proof, ConnectionID("connection-"+string(rune('a'+index)))); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrProofReplay) {
				t.Errorf("VerifyAndBind() error = %v, want ErrProofReplay", err)
			}
		}(index)
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful bindings = %d, want 1", successes.Load())
	}
}

func TestRegistryRejectsExpiredTicketAndCapacityOverflow(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	registry, err := NewInMemorySessionRegistry(RegistryConfig{Capacity: 1, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewInMemorySessionRegistry() error = %v", err)
	}
	identity := AuthIdentity{AccountID: "account-1", PlayerID: "player-1", Server: "asia"}
	secret := []byte("01234567890123456789012345678901")
	secretRef, err := registry.StoreSecret(identity, secret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("StoreSecret() error = %v", err)
	}
	ticket := LoginTicket{UID: "uid-1", SubID: "sub-1", Server: "asia", SecretRef: secretRef, Generation: 1, ExpiresAt: now.Add(time.Minute)}
	if err := registry.Issue(ticket, identity); err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if _, err := registry.StoreSecret(AuthIdentity{AccountID: "account-2", PlayerID: "player-2", Server: "asia"}, secret, now.Add(time.Minute)); !errors.Is(err, ErrSessionCapacity) {
		t.Fatalf("StoreSecret(capacity) error = %v, want ErrSessionCapacity", err)
	}
	now = now.Add(2 * time.Minute)
	proof := SignGatewayProof(secret, GatewayProof{UID: ticket.UID, SubID: ticket.SubID, Server: ticket.Server, Generation: ticket.Generation, Sequence: 1})
	if _, err := registry.VerifyAndBind(proof, "connection-1"); !errors.Is(err, ErrTicketExpired) {
		t.Fatalf("VerifyAndBind(expired) error = %v, want ErrTicketExpired", err)
	}
}
