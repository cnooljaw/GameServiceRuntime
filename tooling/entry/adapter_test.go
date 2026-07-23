package entry

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const routedCommandID gsr.CommandID = 0x7f000001

func TestTCPAdaptersAuthenticateAndRouteBusinessCommand(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	runtime := gsr.NewRuntime(gsr.Config{Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	received := make(chan SessionIdentity, 1)
	target, err := runtime.CreateService(gsr.ServiceSpec{Service: &entryCaptureService{received: received}})
	if err != nil {
		t.Fatalf("CreateService(target) error = %v", err)
	}
	registry, err := NewInMemorySessionRegistry(RegistryConfig{Capacity: 4, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewInMemorySessionRegistry() error = %v", err)
	}
	service, err := NewLoginService(LoginServiceConfig{Registry: registry})
	if err != nil {
		t.Fatalf("NewLoginService() error = %v", err)
	}
	serviceRef, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatalf("CreateService(login) error = %v", err)
	}
	issuer, err := NewLoginClient(runtime, serviceRef)
	if err != nil {
		t.Fatalf("NewLoginClient() error = %v", err)
	}

	gatewayListener := newTCPListener(t)
	gateway, err := NewGatewayAdapter(GatewayAdapterConfig{Listener: gatewayListener, Registry: registry, Mapper: testMapper{target: target}, Dispatcher: runtime})
	if err != nil {
		t.Fatalf("NewGatewayAdapter() error = %v", err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatalf("Gateway.Start() error = %v", err)
	}
	t.Cleanup(func() { _ = gateway.Close(context.Background()) })

	loginListener := newTCPListener(t)
	login, err := NewLoginAdapter(LoginAdapterConfig{
		Listener:         loginListener,
		Handshake:        staticHandshake{login: VerifiedLogin{Identity: AuthIdentity{AccountID: "account-1", PlayerID: "player-1", Server: "asia"}, Secret: []byte("01234567890123456789012345678901"), ExpiresAt: now.Add(time.Minute)}},
		Registry:         registry,
		Issuer:           issuer,
		ConnectionCloser: gateway,
	})
	if err != nil {
		t.Fatalf("NewLoginAdapter() error = %v", err)
	}
	if err := login.Start(); err != nil {
		t.Fatalf("Login.Start() error = %v", err)
	}
	t.Cleanup(func() { _ = login.Close(context.Background()) })

	ticket := loginForTicket(t, loginListener.Addr().String())
	connection, err := net.Dial("tcp", gatewayListener.Addr().String())
	if err != nil {
		t.Fatalf("Dial(gateway) error = %v", err)
	}
	defer connection.Close()
	proof := SignGatewayProof([]byte("01234567890123456789012345678901"), GatewayProof{UID: ticket.UID, SubID: ticket.SubID, Server: ticket.Server, Generation: ticket.Generation, Sequence: 1})
	authLine, err := FormatAuthLine(proof)
	if err != nil {
		t.Fatalf("FormatAuthLine() error = %v", err)
	}
	if _, err := connection.Write([]byte(authLine)); err != nil {
		t.Fatalf("Write(AUTH) error = %v", err)
	}
	if line := readLine(t, connection); line != "OK\n" {
		t.Fatalf("AUTH response = %q, want OK", line)
	}
	if _, err := connection.Write([]byte("PING\n")); err != nil {
		t.Fatalf("Write(PING) error = %v", err)
	}
	if line := readLine(t, connection); line != "OK\n" {
		t.Fatalf("PING response = %q, want OK", line)
	}
	select {
	case identity := <-received:
		if identity.PlayerID != "player-1" || identity.Generation != ticket.Generation {
			t.Fatalf("routed identity = %#v", identity)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for routed Command")
	}
	_ = loginForTicket(t, loginListener.Addr().String())
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	if _, err := bufio.NewReader(connection).ReadString('\n'); err == nil {
		t.Fatal("replaced Gateway connection remained readable")
	}
}

func TestGatewayRejectsInvalidProofWithoutMappingPacket(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	registry, err := NewInMemorySessionRegistry(RegistryConfig{Capacity: 2, Now: func() time.Time { return now }})
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
	listener := newTCPListener(t)
	mapper := &countingMapper{}
	gateway, err := NewGatewayAdapter(GatewayAdapterConfig{Listener: listener, Registry: registry, Mapper: mapper, Dispatcher: rejectingDispatcher{}})
	if err != nil {
		t.Fatalf("NewGatewayAdapter() error = %v", err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatalf("Gateway.Start() error = %v", err)
	}
	t.Cleanup(func() { _ = gateway.Close(context.Background()) })
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer connection.Close()
	proof := SignGatewayProof([]byte("01234567890123456789012345678901"), GatewayProof{UID: ticket.UID, SubID: ticket.SubID, Server: ticket.Server, Generation: ticket.Generation, Sequence: 1})
	proof.Server = "other"
	line, err := FormatAuthLine(proof)
	if err != nil {
		t.Fatalf("FormatAuthLine() error = %v", err)
	}
	if _, err := connection.Write([]byte(line)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if line := readLine(t, connection); line != "ERR invalid_proof\n" {
		t.Fatalf("response = %q, want invalid proof", line)
	}
	if mapper.calls.Load() != 0 {
		t.Fatalf("mapper calls = %d, want 0", mapper.calls.Load())
	}
}

func TestGatewayCallWritesMapperResponse(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	runtime := gsr.NewRuntime(gsr.Config{Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	target, err := runtime.CreateService(gsr.ServiceSpec{Service: replyService{}})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	registry, err := NewInMemorySessionRegistry(RegistryConfig{Capacity: 2, Now: func() time.Time { return now }})
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
	listener := newTCPListener(t)
	gateway, err := NewGatewayAdapter(GatewayAdapterConfig{Listener: listener, Registry: registry, Mapper: callMapper{target: target}, Dispatcher: runtime})
	if err != nil {
		t.Fatalf("NewGatewayAdapter() error = %v", err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = gateway.Close(context.Background()) })
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer connection.Close()
	proof := SignGatewayProof(secret, GatewayProof{UID: ticket.UID, SubID: ticket.SubID, Server: ticket.Server, Generation: ticket.Generation, Sequence: 1})
	line, err := FormatAuthLine(proof)
	if err != nil {
		t.Fatalf("FormatAuthLine() error = %v", err)
	}
	if _, err := connection.Write([]byte(line)); err != nil {
		t.Fatalf("Write(AUTH) error = %v", err)
	}
	if line := readLine(t, connection); line != "OK\n" {
		t.Fatalf("AUTH response = %q, want OK", line)
	}
	if _, err := connection.Write([]byte("CALL\n")); err != nil {
		t.Fatalf("Write(CALL) error = %v", err)
	}
	if line := readLine(t, connection); line != "RESULT pong\n" {
		t.Fatalf("CALL response = %q, want RESULT pong", line)
	}
}

func TestGatewayTimesOutUnauthenticatedConnection(t *testing.T) {
	listener := newTCPListener(t)
	registry, err := NewInMemorySessionRegistry(RegistryConfig{})
	if err != nil {
		t.Fatalf("NewInMemorySessionRegistry() error = %v", err)
	}
	gateway, err := NewGatewayAdapter(GatewayAdapterConfig{
		Listener:       listener,
		Registry:       registry,
		Mapper:         &countingMapper{},
		Dispatcher:     rejectingDispatcher{},
		IdleTimeout:    20 * time.Millisecond,
		MaxConnections: 1,
	})
	if err != nil {
		t.Fatalf("NewGatewayAdapter() error = %v", err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = gateway.Close(context.Background()) })
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer connection.Close()
	if line := readLine(t, connection); line != "ERR timeout\n" {
		t.Fatalf("idle response = %q, want timeout", line)
	}
}

func TestGatewayRejectsConnectionBeyondCapacity(t *testing.T) {
	listener := newTCPListener(t)
	registry, err := NewInMemorySessionRegistry(RegistryConfig{})
	if err != nil {
		t.Fatalf("NewInMemorySessionRegistry() error = %v", err)
	}
	gateway, err := NewGatewayAdapter(GatewayAdapterConfig{
		Listener:       listener,
		Registry:       registry,
		Mapper:         &countingMapper{},
		Dispatcher:     rejectingDispatcher{},
		IdleTimeout:    time.Second,
		MaxConnections: 1,
	})
	if err != nil {
		t.Fatalf("NewGatewayAdapter() error = %v", err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = gateway.Close(context.Background()) })
	first, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial(first) error = %v", err)
	}
	defer first.Close()
	second, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial(second) error = %v", err)
	}
	defer second.Close()
	if line := readLine(t, second); line != "ERR busy\n" {
		t.Fatalf("capacity response = %q, want busy", line)
	}
}

func TestGatewayRateLimitsAuthenticatedPackets(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	runtime := gsr.NewRuntime(gsr.Config{Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	received := make(chan SessionIdentity, 2)
	target, err := runtime.CreateService(gsr.ServiceSpec{Service: &entryCaptureService{received: received}})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	registry, err := NewInMemorySessionRegistry(RegistryConfig{Capacity: 2, Now: func() time.Time { return now }})
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
	listener := newTCPListener(t)
	gateway, err := NewGatewayAdapter(GatewayAdapterConfig{
		Listener:            listener,
		Registry:            registry,
		Mapper:              testMapper{target: target},
		Dispatcher:          runtime,
		MaxPacketsPerSecond: 1,
	})
	if err != nil {
		t.Fatalf("NewGatewayAdapter() error = %v", err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = gateway.Close(context.Background()) })
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer connection.Close()
	proof := SignGatewayProof(secret, GatewayProof{UID: ticket.UID, SubID: ticket.SubID, Server: ticket.Server, Generation: ticket.Generation, Sequence: 1})
	line, err := FormatAuthLine(proof)
	if err != nil {
		t.Fatalf("FormatAuthLine() error = %v", err)
	}
	if _, err := connection.Write([]byte(line)); err != nil {
		t.Fatalf("Write(AUTH) error = %v", err)
	}
	if line := readLine(t, connection); line != "OK\n" {
		t.Fatalf("AUTH response = %q, want OK", line)
	}
	if _, err := connection.Write([]byte("PING\n")); err != nil {
		t.Fatalf("Write(PING) error = %v", err)
	}
	if line := readLine(t, connection); line != "OK\n" {
		t.Fatalf("first PING response = %q, want OK", line)
	}
	if _, err := connection.Write([]byte("PING\n")); err != nil {
		t.Fatalf("Write(second PING) error = %v", err)
	}
	if line := readLine(t, connection); line != "ERR rate_limited\n" {
		t.Fatalf("second PING response = %q, want rate_limited", line)
	}
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("first packet was not routed")
	}
	select {
	case <-received:
		t.Fatal("rate limited packet was routed")
	default:
	}
}

func TestGatewayRejectsPacketAfterSessionIsRevoked(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	registry, err := NewInMemorySessionRegistry(RegistryConfig{Capacity: 2, Now: func() time.Time { return now }})
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
	listener := newTCPListener(t)
	mapper := &countingMapper{}
	gateway, err := NewGatewayAdapter(GatewayAdapterConfig{Listener: listener, Registry: registry, Mapper: mapper, Dispatcher: rejectingDispatcher{}})
	if err != nil {
		t.Fatalf("NewGatewayAdapter() error = %v", err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = gateway.Close(context.Background()) })
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer connection.Close()
	proof := SignGatewayProof(secret, GatewayProof{UID: ticket.UID, SubID: ticket.SubID, Server: ticket.Server, Generation: ticket.Generation, Sequence: 1})
	line, err := FormatAuthLine(proof)
	if err != nil {
		t.Fatalf("FormatAuthLine() error = %v", err)
	}
	if _, err := connection.Write([]byte(line)); err != nil {
		t.Fatalf("Write(AUTH) error = %v", err)
	}
	if line := readLine(t, connection); line != "OK\n" {
		t.Fatalf("AUTH response = %q, want OK", line)
	}
	if revoked := registry.Revoke(ticket.UID, ticket.Server, ticket.Generation); revoked == "" {
		t.Fatal("Revoke() did not remove the authenticated session")
	}
	if _, err := connection.Write([]byte("PING\n")); err != nil {
		t.Fatalf("Write(PING) error = %v", err)
	}
	if line := readLine(t, connection); line != "ERR session_revoked\n" {
		t.Fatalf("revoked response = %q, want session_revoked", line)
	}
	if mapper.calls.Load() != 0 {
		t.Fatalf("mapper calls = %d, want 0", mapper.calls.Load())
	}
}

func TestLoginAdapterTimesOutHandshake(t *testing.T) {
	listener := newTCPListener(t)
	handshake := blockingHandshake{started: make(chan struct{}), returned: make(chan struct{})}
	registry, err := NewInMemorySessionRegistry(RegistryConfig{})
	if err != nil {
		t.Fatalf("NewInMemorySessionRegistry() error = %v", err)
	}
	adapter, err := NewLoginAdapter(LoginAdapterConfig{Listener: listener, Handshake: handshake, Registry: registry, Issuer: rejectingIssuer{}, ConnectionCloser: noopConnectionCloser{}, HandshakeTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewLoginAdapter() error = %v", err)
	}
	if err := adapter.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close(context.Background()) })
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer connection.Close()
	if line := readLine(t, connection); line != "ERR timeout\n" {
		t.Fatalf("timeout response = %q, want timeout", line)
	}
	select {
	case <-handshake.returned:
	case <-time.After(time.Second):
		t.Fatal("handshake did not return after timeout")
	}
}

func TestLoginAdapterRejectsConnectionBeyondCapacity(t *testing.T) {
	listener := newTCPListener(t)
	handshake := blockingHandshake{started: make(chan struct{}), returned: make(chan struct{})}
	registry, err := NewInMemorySessionRegistry(RegistryConfig{})
	if err != nil {
		t.Fatalf("NewInMemorySessionRegistry() error = %v", err)
	}
	adapter, err := NewLoginAdapter(LoginAdapterConfig{Listener: listener, Handshake: handshake, Registry: registry, Issuer: rejectingIssuer{}, ConnectionCloser: noopConnectionCloser{}, MaxConnections: 1, HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewLoginAdapter() error = %v", err)
	}
	if err := adapter.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close(context.Background()) })
	first, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial(first) error = %v", err)
	}
	defer first.Close()
	select {
	case <-handshake.started:
	case <-time.After(time.Second):
		t.Fatal("first handshake did not start")
	}
	second, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial(second) error = %v", err)
	}
	defer second.Close()
	if line := readLine(t, second); line != "ERR busy\n" {
		t.Fatalf("capacity response = %q, want busy", line)
	}
}

func TestLoginAdapterCloseWaitsForHandshakeReturn(t *testing.T) {
	listener := newTCPListener(t)
	handshake := blockingHandshake{started: make(chan struct{}), returned: make(chan struct{})}
	registry, err := NewInMemorySessionRegistry(RegistryConfig{})
	if err != nil {
		t.Fatalf("NewInMemorySessionRegistry() error = %v", err)
	}
	adapter, err := NewLoginAdapter(LoginAdapterConfig{Listener: listener, Handshake: handshake, Registry: registry, Issuer: rejectingIssuer{}, ConnectionCloser: noopConnectionCloser{}})
	if err != nil {
		t.Fatalf("NewLoginAdapter() error = %v", err)
	}
	if err := adapter.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer connection.Close()
	select {
	case <-handshake.started:
	case <-time.After(time.Second):
		t.Fatal("handshake did not start")
	}
	closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := adapter.Close(closeContext); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-handshake.returned:
	default:
		t.Fatal("Close returned before Handshake.Accept returned")
	}
}

func newTCPListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	return listener
}

func loginForTicket(t *testing.T, address string) LoginTicket {
	t.Helper()
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("Dial(login) error = %v", err)
	}
	defer connection.Close()
	line := readLine(t, connection)
	fields := strings.Split(strings.TrimSuffix(line, "\n"), " ")
	if len(fields) != 6 || fields[0] != "TICKET" {
		t.Fatalf("login response = %q, want TICKET line", line)
	}
	decode := func(value string) string {
		decoded, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil {
			t.Fatalf("DecodeString(%q) error = %v", value, err)
		}
		return string(decoded)
	}
	generation, ok := canonicalUint(fields[4])
	if !ok {
		t.Fatalf("invalid ticket generation %q", fields[4])
	}
	return LoginTicket{UID: decode(fields[1]), SubID: decode(fields[2]), Server: decode(fields[3]), Generation: generation}
}

func readLine(t *testing.T, connection net.Conn) string {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	line, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}
	return line
}

type staticHandshake struct{ login VerifiedLogin }

func (h staticHandshake) Accept(context.Context, net.Conn) (VerifiedLogin, error) {
	return h.login, nil
}

type blockingHandshake struct {
	started  chan struct{}
	returned chan struct{}
}

func (h blockingHandshake) Accept(ctx context.Context, _ net.Conn) (VerifiedLogin, error) {
	close(h.started)
	<-ctx.Done()
	close(h.returned)
	return VerifiedLogin{}, ctx.Err()
}

type rejectingIssuer struct{}

func (rejectingIssuer) IssueTicket(context.Context, IssueTicket) (TicketIssue, error) {
	return TicketIssue{}, errors.New("must not issue")
}

type noopConnectionCloser struct{}

func (noopConnectionCloser) CloseConnection(ConnectionID) {}

type testMapper struct{ target gsr.ServiceRef }

func (m testMapper) Map(identity SessionIdentity, packet ClientPacket) (Route, error) {
	if string(packet.Payload) != "PING" {
		return Route{}, errors.New("unexpected packet")
	}
	return Route{Target: m.target, Command: routedCommandID, Payload: identity}, nil
}

type countingMapper struct{ calls atomic.Int32 }

func (m *countingMapper) Map(SessionIdentity, ClientPacket) (Route, error) {
	m.calls.Add(1)
	return Route{}, errors.New("must not map")
}

type callMapper struct{ target gsr.ServiceRef }

func (m callMapper) Map(_ SessionIdentity, packet ClientPacket) (Route, error) {
	if string(packet.Payload) != "CALL" {
		return Route{}, errors.New("unexpected packet")
	}
	return Route{Target: m.target, Command: routedCommandID, Call: true}, nil
}

func (callMapper) EncodeCallResult(_ SessionIdentity, _ ClientPacket, result any) (ClientPacket, error) {
	value, ok := result.(string)
	if !ok {
		return ClientPacket{}, errors.New("unexpected result")
	}
	return ClientPacket{Payload: []byte("RESULT " + value)}, nil
}

type rejectingDispatcher struct{}

func (rejectingDispatcher) Send(gsr.ServiceRef, gsr.CommandID, any) error {
	return errors.New("must not send")
}
func (rejectingDispatcher) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, errors.New("must not call")
}

type entryCaptureService struct{ received chan<- SessionIdentity }

func (*entryCaptureService) Commands() []gsr.CommandID     { return []gsr.CommandID{routedCommandID} }
func (*entryCaptureService) Init(gsr.ServiceContext) error { return nil }
func (s *entryCaptureService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	identity, ok := command.Payload.(SessionIdentity)
	if !ok {
		return errors.New("missing session identity")
	}
	s.received <- identity
	return nil
}
func (*entryCaptureService) Stop(context.Context) error { return nil }
func (*entryCaptureService) Close() error               { return nil }

type replyService struct{}

func (replyService) Commands() []gsr.CommandID     { return []gsr.CommandID{routedCommandID} }
func (replyService) Init(gsr.ServiceContext) error { return nil }
func (replyService) Handle(context gsr.CommandContext, command gsr.Command) error {
	if command.ID != routedCommandID {
		return gsr.ErrCommandNotRegistered
	}
	return context.Reply("pong")
}
func (replyService) Stop(context.Context) error { return nil }
func (replyService) Close() error               { return nil }
