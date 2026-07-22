package entry

import (
	"context"
	"encoding/base64"
	"net"
	"strconv"
	"time"
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
}

// LoginAdapter owns TCP login connections and writes short-lived ticket lines after successful issuance.
type LoginAdapter struct {
	server *tcpServer
	config LoginAdapterConfig
}

// NewLoginAdapter creates a TCP Login Adapter without starting its listener loop.
func NewLoginAdapter(config LoginAdapterConfig) (*LoginAdapter, error) {
	if nilInterface(config.Listener) || nilInterface(config.Handshake) || nilInterface(config.Registry) || nilInterface(config.Issuer) {
		return nil, ErrInvalidConfig
	}
	adapter := &LoginAdapter{config: config}
	adapter.server = newTCPServer(config.Listener, adapter.handle)
	return adapter, nil
}

// Start begins accepting login connections.
func (a *LoginAdapter) Start() error { return a.server.start() }

// Close stops accepting connections and waits for every Login Adapter task to return.
func (a *LoginAdapter) Close(ctx context.Context) error { return a.server.close(ctx) }

func (a *LoginAdapter) handle(ctx context.Context, connection net.Conn) {
	verified, err := a.config.Handshake.Accept(ctx, connection)
	if err != nil || !validIdentity(verified.Identity) || len(verified.Secret) < 32 || verified.ExpiresAt.IsZero() {
		writeEntryError(connection, "unauthorized")
		return
	}
	secret := append([]byte(nil), verified.Secret...)
	defer zero(secret)
	ref, err := a.config.Registry.StoreSecret(verified.Identity, secret, verified.ExpiresAt)
	if err != nil {
		writeEntryError(connection, "unavailable")
		return
	}
	issue, err := a.config.Issuer.IssueTicket(ctx, IssueTicket{Identity: verified.Identity, SecretRef: ref, ExpiresAt: verified.ExpiresAt})
	if err != nil {
		a.config.Registry.DiscardSecret(ref)
		writeEntryError(connection, "unauthorized")
		return
	}
	if issue.ReplacedConnectionID != "" && a.config.ConnectionCloser != nil {
		a.config.ConnectionCloser.CloseConnection(issue.ReplacedConnectionID)
	}
	line, err := formatTicketLine(issue.Ticket)
	if err != nil {
		writeEntryError(connection, "unavailable")
		return
	}
	_, _ = connection.Write([]byte(line))
}

func formatTicketLine(ticket LoginTicket) (string, error) {
	if !validText(ticket.UID) || !validText(ticket.SubID) || !validText(ticket.Server) || ticket.Generation == 0 || ticket.ExpiresAt.IsZero() {
		return "", ErrInvalidTicket
	}
	return "TICKET " + base64.RawURLEncoding.EncodeToString([]byte(ticket.UID)) + " " + base64.RawURLEncoding.EncodeToString([]byte(ticket.SubID)) + " " + base64.RawURLEncoding.EncodeToString([]byte(ticket.Server)) + " " + strconv.FormatUint(ticket.Generation, 10) + " " + strconv.FormatInt(ticket.ExpiresAt.UnixMilli(), 10) + "\n", nil
}
