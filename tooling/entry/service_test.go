package entry

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestLoginServiceIssuesTicketAndRevokesPriorGeneration(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	runtime := gsr.NewRuntime(gsr.Config{Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	registry, err := NewInMemorySessionRegistry(RegistryConfig{Capacity: 4, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewInMemorySessionRegistry() error = %v", err)
	}
	service, err := NewLoginService(LoginServiceConfig{Registry: registry})
	if err != nil {
		t.Fatalf("NewLoginService() error = %v", err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	client, err := NewLoginClient(runtime, ref)
	if err != nil {
		t.Fatalf("NewLoginClient() error = %v", err)
	}
	identity := AuthIdentity{AccountID: "account-1", PlayerID: "player-1", Server: "asia"}
	secret := []byte("01234567890123456789012345678901")
	firstRef, err := registry.StoreSecret(identity, secret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("StoreSecret(first) error = %v", err)
	}
	first, err := client.IssueTicket(context.Background(), IssueTicket{Identity: identity, SecretRef: firstRef, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("IssueTicket(first) error = %v", err)
	}
	if first.Generation != 1 || first.SecretRef != firstRef {
		t.Fatalf("first ticket = %#v, want generation 1 with original SecretRef", first)
	}
	firstProof := SignGatewayProof(secret, GatewayProof{UID: first.UID, SubID: first.SubID, Server: first.Server, Generation: first.Generation, Sequence: 1})
	if _, err := registry.VerifyAndBind(firstProof, "connection-1"); err != nil {
		t.Fatalf("VerifyAndBind(first) error = %v", err)
	}

	secondRef, err := registry.StoreSecret(identity, secret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("StoreSecret(second) error = %v", err)
	}
	second, err := client.IssueTicket(context.Background(), IssueTicket{Identity: identity, SecretRef: secondRef, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("IssueTicket(second) error = %v", err)
	}
	if second.Generation != 2 || second.UID == first.UID || second.SubID == first.SubID {
		t.Fatalf("second ticket = %#v, want a distinct generation-2 ticket", second)
	}
	if _, err := registry.VerifyAndBind(firstProof, "connection-2"); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("VerifyAndBind(revoked first) error = %v, want ErrSessionRevoked", err)
	}
}

func TestLoginServiceDoesNotIssueTicketWhenRegistryRejectsIt(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	registry := &rejectingTicketRegistry{}
	runtime := gsr.NewRuntime(gsr.Config{Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service, err := NewLoginService(LoginServiceConfig{Registry: registry})
	if err != nil {
		t.Fatalf("NewLoginService() error = %v", err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	client, err := NewLoginClient(runtime, ref)
	if err != nil {
		t.Fatalf("NewLoginClient() error = %v", err)
	}
	_, err = client.IssueTicket(context.Background(), IssueTicket{Identity: AuthIdentity{AccountID: "account-1", PlayerID: "player-1", Server: "asia"}, SecretRef: "secret-ref", ExpiresAt: now.Add(time.Minute)})
	if !errors.Is(err, ErrSessionCapacity) {
		t.Fatalf("IssueTicket() error = %v, want ErrSessionCapacity", err)
	}
	if registry.issued != 1 {
		t.Fatalf("registry issue calls = %d, want 1", registry.issued)
	}
}

type rejectingTicketRegistry struct{ issued int }

func (r *rejectingTicketRegistry) Replace(LoginTicket, AuthIdentity, *LoginTicket) (ConnectionID, error) {
	r.issued++
	return "", ErrSessionCapacity
}
