package tcp

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

var (
	// ErrInvalidConfig indicates that the TCP Transport configuration is incomplete.
	ErrInvalidConfig = errors.New("gsr tcp: invalid config")
	// ErrTransportClosed indicates that the TCP Transport is closing or closed.
	ErrTransportClosed = errors.New("gsr tcp: transport closed")
	// ErrPeerUnknown indicates that no current connection or static address exists for a node.
	ErrPeerUnknown = errors.New("gsr tcp: peer unknown")
)

// Config configures a TCP ClusterTransport.
type Config struct {
	ListenAddress    string
	Peers            map[gsr.NodeID]string
	DialTimeout      time.Duration
	HandshakeTimeout time.Duration
	WriteTimeout     time.Duration
	MaxFrameSize     uint32
}

// Transport moves WireEnvelopes over persistent, full-duplex TCP connections.
type Transport struct {
	config    Config
	mu        sync.Mutex
	local     gsr.NodeID
	events    gsr.ClusterEvents
	listener  net.Listener
	conns     map[gsr.NodeID]*connection
	started   bool
	closed    bool
	dialMu    sync.Mutex
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeDone chan struct{}
}

type connection struct {
	peer     gsr.NodeID
	conn     net.Conn
	outbound bool
	writeMu  sync.Mutex
}

// New creates a TCP Transport. Start opens its listener.
func New(config Config) *Transport {
	if config.DialTimeout <= 0 {
		config.DialTimeout = 3 * time.Second
	}
	if config.HandshakeTimeout <= 0 {
		config.HandshakeTimeout = 3 * time.Second
	}
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = 3 * time.Second
	}
	if config.MaxFrameSize == 0 {
		config.MaxFrameSize = defaultMaxFrameSize
	}
	peers := make(map[gsr.NodeID]string, len(config.Peers))
	for node, address := range config.Peers {
		peers[node] = address
	}
	config.Peers = peers
	return &Transport{config: config, conns: make(map[gsr.NodeID]*connection), closeDone: make(chan struct{})}
}

// Start opens the listener and begins accepting peer connections.
func (t *Transport) Start(local gsr.NodeID, events gsr.ClusterEvents) error {
	if local == "" || t.config.ListenAddress == "" || events.Receive == nil || events.Unavailable == nil {
		return ErrInvalidConfig
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrTransportClosed
	}
	if t.started {
		return ErrInvalidConfig
	}
	listener, err := net.Listen("tcp", t.config.ListenAddress)
	if err != nil {
		return err
	}
	t.local = local
	t.events = events
	t.listener = listener
	t.started = true
	t.wg.Add(1)
	go t.acceptLoop(listener)
	return nil
}

// Address returns the bound listener address after Start succeeds.
func (t *Transport) Address() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.listener == nil {
		return ""
	}
	return t.listener.Addr().String()
}

// Send writes one WireEnvelope to target.
func (t *Transport) Send(target gsr.NodeID, envelope gsr.WireEnvelope) error {
	t.mu.Lock()
	local := t.local
	started := t.started
	closed := t.closed
	t.mu.Unlock()
	if !started || closed {
		return ErrTransportClosed
	}
	if target == "" || target == local || envelope.Source.Node != local || envelope.Target.Node != target {
		return ErrPeerIdentity
	}
	body, err := encodeWireEnvelope(envelope, t.config.MaxFrameSize)
	if err != nil {
		return err
	}
	conn := t.connection(target)
	if conn == nil {
		conn, err = t.connect(target)
		if err != nil {
			return err
		}
	}
	if err := conn.write(body, t.config.MaxFrameSize, t.config.WriteTimeout); err != nil {
		t.removeConnection(conn)
		return err
	}
	return nil
}

// Close stops accepting connections and waits for Transport goroutines.
func (t *Transport) Close(ctx context.Context) error {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		listener := t.listener
		connections := make([]*connection, 0, len(t.conns))
		for _, conn := range t.conns {
			connections = append(connections, conn)
		}
		t.conns = make(map[gsr.NodeID]*connection)
		t.mu.Unlock()
		if listener != nil {
			_ = listener.Close()
		}
		for _, conn := range connections {
			_ = conn.conn.Close()
		}
		go func() {
			t.wg.Wait()
			close(t.closeDone)
		}()
	})
	select {
	case <-t.closeDone:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (t *Transport) acceptLoop(listener net.Listener) {
	defer t.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		t.wg.Add(1)
		go t.handleAccepted(conn)
	}
}

func (t *Transport) handleAccepted(netConn net.Conn) {
	defer t.wg.Done()
	peer, err := acceptHandshake(netConn, t.local, protocolVersion, t.config.HandshakeTimeout)
	if err != nil {
		_ = netConn.Close()
		return
	}
	candidate := &connection{peer: peer, conn: netConn}
	chosen, accepted := t.registerConnection(candidate, false)
	if !accepted {
		_ = netConn.Close()
		return
	}
	t.readLoop(chosen)
}

func (t *Transport) connect(target gsr.NodeID) (*connection, error) {
	t.dialMu.Lock()
	defer t.dialMu.Unlock()
	if current := t.connection(target); current != nil {
		return current, nil
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, ErrTransportClosed
	}
	address := t.config.Peers[target]
	local := t.local
	t.mu.Unlock()
	if address == "" {
		return nil, ErrPeerUnknown
	}
	netConn, err := net.DialTimeout("tcp", address, t.config.DialTimeout)
	if err != nil {
		return nil, err
	}
	peer, err := initiateHandshake(netConn, local, target, protocolVersion, t.config.HandshakeTimeout)
	if err != nil {
		_ = netConn.Close()
		return nil, err
	}
	candidate := &connection{peer: peer, conn: netConn, outbound: true}
	chosen, accepted := t.registerConnection(candidate, true)
	if chosen == nil {
		_ = netConn.Close()
		return nil, ErrTransportClosed
	}
	if !accepted {
		_ = netConn.Close()
	}
	return chosen, nil
}

func (t *Transport) connection(peer gsr.NodeID) *connection {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conns[peer]
}

func (t *Transport) registerConnection(candidate *connection, asyncRead bool) (*connection, bool) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, false
	}
	existing := t.conns[candidate.peer]
	if existing != nil {
		preferOutbound := string(t.local) < string(candidate.peer)
		existingPreferred := existing.outbound == preferOutbound
		candidatePreferred := candidate.outbound == preferOutbound
		if existingPreferred || !candidatePreferred {
			t.mu.Unlock()
			return existing, false
		}
	}
	t.conns[candidate.peer] = candidate
	if asyncRead {
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			t.readLoop(candidate)
		}()
	}
	t.mu.Unlock()
	if existing != nil {
		_ = existing.conn.Close()
	}
	return candidate, true
}

func (t *Transport) readLoop(conn *connection) {
	defer t.removeConnection(conn)
	for {
		body, err := readFrame(conn.conn, t.config.MaxFrameSize)
		if err != nil {
			return
		}
		envelope, err := decodeWireEnvelope(body)
		if err != nil {
			return
		}
		t.events.Receive(conn.peer, envelope)
	}
}

func (t *Transport) removeConnection(conn *connection) {
	_ = conn.conn.Close()
	t.mu.Lock()
	current := t.conns[conn.peer] == conn
	if current {
		delete(t.conns, conn.peer)
	}
	closed := t.closed
	events := t.events
	t.mu.Unlock()
	if current && !closed {
		events.Unavailable(conn.peer)
	}
}

func (c *connection) write(body []byte, maxFrameSize uint32, timeout time.Duration) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if timeout > 0 {
		if err := c.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			return err
		}
		defer c.conn.SetWriteDeadline(time.Time{})
	}
	return writeFrame(c.conn, body, maxFrameSize)
}
