package entry

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const defaultGatewayPacketBytes = 4096

const (
	defaultGatewayMaxConnections   = 1024
	defaultGatewayIdleTimeout      = 30 * time.Second
	defaultGatewayPacketsPerSecond = 100
)

// GatewayRegistry validates proofs and clears bindings when Gateway connections end.
type GatewayRegistry interface {
	VerifyAndBind(GatewayProof, ConnectionID) (SessionBinding, error)
	IsBound(SessionIdentity, ConnectionID) bool
	Unbind(ConnectionID, uint64)
}

// ClientPacket is one authenticated client protocol payload with its line terminator removed.
type ClientPacket struct {
	Payload []byte
}

// Route tells Gateway which Runtime Command represents one mapped client packet.
type Route struct {
	Target  gsr.ServiceRef
	Command gsr.CommandID
	Payload any
	Call    bool
}

// ProtocolMapper maps authenticated protocol payloads to Runtime routes.
type ProtocolMapper interface {
	Map(SessionIdentity, ClientPacket) (Route, error)
}

// CallResponseMapper encodes a successful Runtime Call result for the originating client packet.
type CallResponseMapper interface {
	EncodeCallResult(SessionIdentity, ClientPacket, any) (ClientPacket, error)
}

// CommandDispatcher is the narrow Runtime Send and Call capability required by GatewayAdapter.
type CommandDispatcher interface {
	Send(gsr.ServiceRef, gsr.CommandID, any) error
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

// GatewayAdapterConfig configures one TCP Gateway Adapter.
type GatewayAdapterConfig struct {
	Listener       net.Listener
	Registry       GatewayRegistry
	Mapper         ProtocolMapper
	Dispatcher     CommandDispatcher
	MaxPacketBytes int
	CallTimeout    time.Duration
	// MaxConnections limits all unauthenticated and authenticated Gateway connections. Zero defaults to 1024.
	MaxConnections int
	// IdleTimeout bounds time spent waiting for the AUTH line and each subsequent client packet. Zero defaults to 30 seconds.
	IdleTimeout time.Duration
	// MaxPacketsPerSecond bounds authenticated client packets per connection in each fixed one-second window. Zero defaults to 100.
	MaxPacketsPerSecond int
}

// GatewayAdapter owns TCP Gateway connections, proof binding, and client packet forwarding.
type GatewayAdapter struct {
	server *tcpServer
	config GatewayAdapterConfig

	connectionsMu   sync.Mutex
	connections     map[ConnectionID]net.Conn
	connectionSlots chan struct{}
}

// NewGatewayAdapter creates a TCP Gateway Adapter without starting its listener loop.
func NewGatewayAdapter(config GatewayAdapterConfig) (*GatewayAdapter, error) {
	if nilInterface(config.Listener) || nilInterface(config.Registry) || nilInterface(config.Mapper) || nilInterface(config.Dispatcher) || config.MaxPacketBytes < 0 || config.CallTimeout < 0 || config.MaxConnections < 0 || config.IdleTimeout < 0 || config.MaxPacketsPerSecond < 0 {
		return nil, ErrInvalidConfig
	}
	if config.MaxPacketBytes == 0 {
		config.MaxPacketBytes = defaultGatewayPacketBytes
	}
	if config.MaxPacketBytes > maxAuthLine {
		return nil, ErrInvalidConfig
	}
	if config.CallTimeout == 0 {
		config.CallTimeout = 5 * time.Second
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = defaultGatewayMaxConnections
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = defaultGatewayIdleTimeout
	}
	if config.MaxPacketsPerSecond == 0 {
		config.MaxPacketsPerSecond = defaultGatewayPacketsPerSecond
	}
	adapter := &GatewayAdapter{config: config, connections: make(map[ConnectionID]net.Conn), connectionSlots: make(chan struct{}, config.MaxConnections)}
	adapter.server = newTCPServer(config.Listener, adapter.handle)
	return adapter, nil
}

// Start begins accepting Gateway connections.
func (a *GatewayAdapter) Start() error { return a.server.start() }

// Close stops accepting connections and waits for every Gateway Adapter task to return.
func (a *GatewayAdapter) Close(ctx context.Context) error { return a.server.close(ctx) }

// CloseConnection closes the current Gateway connection for an opaque ConnectionID.
func (a *GatewayAdapter) CloseConnection(id ConnectionID) {
	a.connectionsMu.Lock()
	connection := a.connections[id]
	a.connectionsMu.Unlock()
	if connection != nil {
		_ = connection.Close()
	}
}

func (a *GatewayAdapter) handle(ctx context.Context, connection net.Conn) {
	select {
	case a.connectionSlots <- struct{}{}:
		defer func() { <-a.connectionSlots }()
	default:
		writeEntryError(connection, "busy")
		return
	}
	id, err := newConnectionID()
	if err != nil {
		return
	}
	a.connectionsMu.Lock()
	a.connections[id] = connection
	a.connectionsMu.Unlock()
	defer func() {
		a.connectionsMu.Lock()
		delete(a.connections, id)
		a.connectionsMu.Unlock()
	}()
	reader := bufio.NewReaderSize(connection, a.config.MaxPacketBytes)
	line, err := a.readLine(connection, reader)
	if err != nil {
		writeEntryError(connection, readErrorCode(err, "invalid_proof"))
		return
	}
	proof, err := ParseAuthLine(line)
	if err != nil {
		writeEntryError(connection, "invalid_proof")
		return
	}
	binding, err := a.config.Registry.VerifyAndBind(proof, id)
	if err != nil {
		writeEntryError(connection, "invalid_proof")
		return
	}
	defer a.config.Registry.Unbind(id, binding.Identity.Generation)
	if binding.ReplacedConnectionID != "" {
		a.CloseConnection(binding.ReplacedConnectionID)
	}
	if _, err := connection.Write([]byte("OK\n")); err != nil {
		return
	}
	limiter := packetRate{limit: a.config.MaxPacketsPerSecond}
	for {
		line, err := a.readLine(connection, reader)
		if err != nil {
			if isTimeout(err) {
				writeEntryError(connection, "timeout")
			}
			return
		}
		packet := ClientPacket{Payload: append([]byte(nil), line[:len(line)-1]...)}
		if !a.config.Registry.IsBound(binding.Identity, id) {
			writeEntryError(connection, "session_revoked")
			return
		}
		if !limiter.allow(time.Now()) {
			writeEntryError(connection, "rate_limited")
			return
		}
		route, err := a.config.Mapper.Map(binding.Identity, packet)
		if err != nil || !validRoute(route) {
			writeEntryError(connection, "protocol")
			return
		}
		if route.Call {
			callContext, cancel := context.WithTimeout(ctx, a.config.CallTimeout)
			result, callErr := a.config.Dispatcher.Call(callContext, route.Target, route.Command, route.Payload)
			cancel()
			if callErr != nil {
				writeEntryError(connection, "command")
				return
			}
			responseMapper, ok := a.config.Mapper.(CallResponseMapper)
			if !ok {
				writeEntryError(connection, "protocol")
				return
			}
			response, encodeErr := responseMapper.EncodeCallResult(binding.Identity, packet, result)
			if encodeErr != nil || !validResponsePacket(response, a.config.MaxPacketBytes) {
				writeEntryError(connection, "protocol")
				return
			}
			if _, err := connection.Write(append(response.Payload, '\n')); err != nil {
				return
			}
			continue
		} else {
			err = a.config.Dispatcher.Send(route.Target, route.Command, route.Payload)
		}
		if err != nil {
			writeEntryError(connection, "command")
			return
		}
		if _, err := connection.Write([]byte("OK\n")); err != nil {
			return
		}
	}
}

func (a *GatewayAdapter) readLine(connection net.Conn, reader *bufio.Reader) (string, error) {
	if err := connection.SetReadDeadline(time.Now().Add(a.config.IdleTimeout)); err != nil {
		return "", err
	}
	line, err := readLimitedLine(reader, a.config.MaxPacketBytes)
	_ = connection.SetReadDeadline(time.Time{})
	return line, err
}

type packetRate struct {
	limit int
	start time.Time
	used  int
}

func (r *packetRate) allow(now time.Time) bool {
	if r.start.IsZero() || !now.Before(r.start.Add(time.Second)) {
		r.start = now
		r.used = 0
	}
	r.used++
	return r.used <= r.limit
}

func newConnectionID() (ConnectionID, error) {
	value, err := newOpaqueID()
	return ConnectionID(value), err
}

func validRoute(route Route) bool {
	return route.Target.Node != "" && route.Target.ID != 0 && route.Command != 0
}

func validResponsePacket(packet ClientPacket, limit int) bool {
	return len(packet.Payload) > 0 && len(packet.Payload)+1 <= limit && !containsLineTerminator(packet.Payload)
}

func containsLineTerminator(payload []byte) bool {
	for _, value := range payload {
		if value == '\n' || value == '\r' {
			return true
		}
	}
	return false
}

func readLimitedLine(reader *bufio.Reader, limit int) (string, error) {
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return "", err
	}
	if len(line) == 0 || len(line) > limit || line[len(line)-1] != '\n' {
		return "", errors.New("entry: line too long")
	}
	return string(line), nil
}

func readErrorCode(err error, fallback string) string {
	if isTimeout(err) {
		return "timeout"
	}
	return fallback
}

func isTimeout(err error) bool {
	networkErr, ok := err.(net.Error)
	return ok && networkErr.Timeout()
}

func writeEntryError(connection net.Conn, code string) {
	_, _ = connection.Write([]byte("ERR " + code + "\n"))
}
