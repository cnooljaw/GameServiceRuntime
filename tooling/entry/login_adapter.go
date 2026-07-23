package entry

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"strconv"
	"time"
)

const (
	defaultLoginMaxConnections = 1024
	defaultHandshakeTimeout    = 10 * time.Second
)

// Handshake authenticates one Login Adapter connection and returns its derived secret material.
type Handshake interface {
	Accept(context.Context, net.Conn) (VerifiedLogin, error)
}

// VerifiedLogin is authenticated identity and short-lived secret material returned by Handshake.
type VerifiedLogin struct {
	Identity  AuthIdentity
	Secret    []byte
	ExpiresAt time.Time
}

// SecretRegistry retains Adapter-owned secret material until ticket issuance completes.
type SecretRegistry interface {
	StoreSecret(AuthIdentity, []byte, time.Time) (SecretRef, error)
	DiscardSecret(SecretRef)
}

// LoginIssuer is the narrow typed ticket issuance capability required by LoginAdapter.
type LoginIssuer interface {
	IssueTicket(context.Context, IssueTicket) (TicketIssue, error)
}

// ConnectionCloser closes a superseded Gateway connection by its opaque ID.
type ConnectionCloser interface {
	CloseConnection(ConnectionID)
}

// LoginAdapterConfig configures one TCP Login Adapter.
type LoginAdapterConfig struct {
	Listener         net.Listener
	Handshake        Handshake
	Registry         SecretRegistry
	Issuer           LoginIssuer
	ConnectionCloser ConnectionCloser
	// MaxConnections limits concurrent Login Adapter connections. Zero defaults to 1024.
	MaxConnections int
	// HandshakeTimeout bounds Handshake and ticket issuance for one Login Adapter connection. Zero defaults to 10 seconds.
	HandshakeTimeout time.Duration
}

// LoginAdapter owns TCP login connections and writes short-lived ticket lines after successful issuance.
type LoginAdapter struct {
	server          *tcpServer
	config          LoginAdapterConfig
	connectionSlots chan struct{}
}

// NewLoginAdapter creates a TCP Login Adapter without starting its listener loop.
func NewLoginAdapter(config LoginAdapterConfig) (*LoginAdapter, error) {
	if nilInterface(config.Listener) || nilInterface(config.Handshake) || nilInterface(config.Registry) || nilInterface(config.Issuer) || nilInterface(config.ConnectionCloser) || config.MaxConnections < 0 || config.HandshakeTimeout < 0 {
		return nil, ErrInvalidConfig
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = defaultLoginMaxConnections
	}
	if config.HandshakeTimeout == 0 {
		config.HandshakeTimeout = defaultHandshakeTimeout
	}
	adapter := &LoginAdapter{config: config, connectionSlots: make(chan struct{}, config.MaxConnections)}
	adapter.server = newTCPServer(config.Listener, adapter.handle)
	return adapter, nil
}

// Start begins accepting login connections.
func (a *LoginAdapter) Start() error { return a.server.start() }

// Close stops accepting connections and waits for every Login Adapter task to return.
func (a *LoginAdapter) Close(ctx context.Context) error { return a.server.close(ctx) }

func (a *LoginAdapter) handle(ctx context.Context, connection net.Conn) {
	select {
	case a.connectionSlots <- struct{}{}:
		defer func() { <-a.connectionSlots }()
	default:
		writeEntryError(connection, "busy")
		return
	}
	deadline := time.Now().Add(a.config.HandshakeTimeout)
	if err := connection.SetDeadline(deadline); err != nil {
		return
	}
	handshakeContext, cancel := context.WithTimeout(ctx, a.config.HandshakeTimeout)
	defer cancel()
	verified, err := a.config.Handshake.Accept(handshakeContext, connection)
	if err != nil || !validIdentity(verified.Identity) || len(verified.Secret) < 32 || verified.ExpiresAt.IsZero() {
		_ = connection.SetDeadline(time.Time{})
		writeEntryError(connection, handshakeErrorCode(handshakeContext, err))
		return
	}
	secret := append([]byte(nil), verified.Secret...)
	defer zero(secret)
	ref, err := a.config.Registry.StoreSecret(verified.Identity, secret, verified.ExpiresAt)
	if err != nil {
		writeEntryError(connection, "unavailable")
		return
	}
	issue, err := a.config.Issuer.IssueTicket(handshakeContext, IssueTicket{Identity: verified.Identity, SecretRef: ref, ExpiresAt: verified.ExpiresAt})
	if err != nil {
		a.config.Registry.DiscardSecret(ref)
		_ = connection.SetDeadline(time.Time{})
		writeEntryError(connection, handshakeErrorCode(handshakeContext, err))
		return
	}
	if issue.ReplacedConnectionID != "" {
		a.config.ConnectionCloser.CloseConnection(issue.ReplacedConnectionID)
	}
	line, err := formatTicketLine(issue.Ticket)
	if err != nil {
		writeEntryError(connection, "unavailable")
		return
	}
	_, _ = connection.Write([]byte(line))
}

func handshakeErrorCode(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || isTimeout(err) {
		return "timeout"
	}
	return "unauthorized"
}

func formatTicketLine(ticket LoginTicket) (string, error) {
	if !validText(ticket.UID) || !validText(ticket.SubID) || !validText(ticket.Server) || ticket.Generation == 0 || ticket.ExpiresAt.IsZero() {
		return "", ErrInvalidTicket
	}
	return "TICKET " + base64.RawURLEncoding.EncodeToString([]byte(ticket.UID)) + " " + base64.RawURLEncoding.EncodeToString([]byte(ticket.SubID)) + " " + base64.RawURLEncoding.EncodeToString([]byte(ticket.Server)) + " " + strconv.FormatUint(ticket.Generation, 10) + " " + strconv.FormatInt(ticket.ExpiresAt.UnixMilli(), 10) + "\n", nil
}
