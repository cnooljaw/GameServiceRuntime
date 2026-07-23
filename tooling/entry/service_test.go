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
	firstIssue, err := client.IssueTicket(context.Background(), IssueTicket{Identity: identity, SecretRef: firstRef, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("IssueTicket(first) error = %v", err)
	}
	first := firstIssue.Ticket
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
	secondIssue, err := client.IssueTicket(context.Background(), IssueTicket{Identity: identity, SecretRef: secondRef, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("IssueTicket(second) error = %v", err)
	}
	second := secondIssue.Ticket
	if secondIssue.ReplacedConnectionID != "connection-1" {
		t.Fatalf("replaced connection = %q, want connection-1", secondIssue.ReplacedConnectionID)
	}
	if second.Generation != 2 || second.UID == first.UID || second.SubID == first.SubID {
		t.Fatalf("second ticket = %#v, want a distinct generation-2 ticket", second)
	}
	if _, err := registry.VerifyAndBind(firstProof, "connection-2"); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("VerifyAndBind(revoked first) error = %v, want ErrSessionRevoked", err)
	}
}

func TestLoginServiceRehydratesSingleSessionAfterServiceRecreate(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	runtime := gsr.NewRuntime(gsr.Config{Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	registry, err := NewInMemorySessionRegistry(RegistryConfig{Capacity: 4, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewInMemorySessionRegistry() error = %v", err)
	}
	identity := AuthIdentity{AccountID: "account-1", PlayerID: "player-1", Server: "asia"}
	secret := []byte("01234567890123456789012345678901")
	firstService, err := NewLoginService(LoginServiceConfig{Registry: registry})
	if err != nil {
		t.Fatalf("NewLoginService(first) error = %v", err)
	}
	firstRef, err := runtime.CreateService(gsr.ServiceSpec{Service: firstService})
	if err != nil {
		t.Fatalf("CreateService(first) error = %v", err)
	}
	firstClient, err := NewLoginClient(runtime, firstRef)
	if err != nil {
		t.Fatalf("NewLoginClient(first) error = %v", err)
	}
	firstSecretRef, err := registry.StoreSecret(identity, secret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("StoreSecret(first) error = %v", err)
	}
	firstIssue, err := firstClient.IssueTicket(context.Background(), IssueTicket{Identity: identity, SecretRef: firstSecretRef, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("IssueTicket(first) error = %v", err)
	}
	firstProof := SignGatewayProof(secret, GatewayProof{UID: firstIssue.Ticket.UID, SubID: firstIssue.Ticket.SubID, Server: firstIssue.Ticket.Server, Generation: firstIssue.Ticket.Generation, Sequence: 1})
	if _, err := registry.VerifyAndBind(firstProof, "connection-1"); err != nil {
		t.Fatalf("VerifyAndBind(first) error = %v", err)
	}
	if err := runtime.Stop(context.Background(), firstRef); err != nil {
		t.Fatalf("Stop(first LoginService) error = %v", err)
	}

	secondService, err := NewLoginService(LoginServiceConfig{Registry: registry})
	if err != nil {
		t.Fatalf("NewLoginService(second) error = %v", err)
	}
	secondRef, err := runtime.CreateService(gsr.ServiceSpec{Service: secondService})
	if err != nil {
		t.Fatalf("CreateService(second) error = %v", err)
	}
	secondClient, err := NewLoginClient(runtime, secondRef)
	if err != nil {
		t.Fatalf("NewLoginClient(second) error = %v", err)
	}
	secondIdentity := AuthIdentity{AccountID: identity.AccountID, PlayerID: "player-2", Server: identity.Server}
	secondSecretRef, err := registry.StoreSecret(secondIdentity, secret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("StoreSecret(second) error = %v", err)
	}
	secondIssue, err := secondClient.IssueTicket(context.Background(), IssueTicket{Identity: secondIdentity, SecretRef: secondSecretRef, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("IssueTicket(second) error = %v", err)
	}
	if secondIssue.Ticket.Generation != 2 || secondIssue.ReplacedConnectionID != "connection-1" {
		t.Fatalf("second issue = %#v, want generation 2 and connection-1 replacement", secondIssue)
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

func (*rejectingTicketRegistry) Current(AuthIdentity) (LoginTicket, bool) {
	return LoginTicket{}, false
}

func (r *rejectingTicketRegistry) Replace(LoginTicket, AuthIdentity, *LoginTicket) (ConnectionID, error) {
	r.issued++
	return "", ErrSessionCapacity
}
