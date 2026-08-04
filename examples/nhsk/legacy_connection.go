package nhsk

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

var (
	errLegacyConnectionClosed   = errors.New("nhsk: Legacy GM connection closed")
	errLegacyConnectionNotReady = errors.New("nhsk: Legacy GM connection not ready")
)

// LegacyGMConnectionConfig configures the single active GameLogic→GameMaster TCP owner.
type LegacyGMConnectionConfig struct {
	Address           string
	DialTimeout       time.Duration
	OriginTimeout     time.Duration
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
	Jitter            float64
	StableReset       time.Duration
	OutputQueue       int
	Dial              func(context.Context, string, string) (net.Conn, error)
	OnFrame           func(context.Context, ConnectionGeneration, legacywire.Frame) error
	OnDisconnected    func(ConnectionGeneration)
}

type legacyConnectionSession struct {
	generation ConnectionGeneration
	connection net.Conn
	outputs    chan GameOutputBatch
	done       chan struct{}
}

// LegacyGMConnection owns dial, origin handshake, frame read, output queue and reconnect lifecycle.
// It is an adapter owner, not a GSR Service, so its I/O goroutines never mutate Battle state.
type LegacyGMConnection struct {
	config     LegacyGMConnectionConfig
	runtime    game.CommandRuntime
	hostRef    gsr.ServiceRef
	ctx        context.Context
	cancel     context.CancelFunc
	started    chan struct{}
	done       chan struct{}
	startOnce  sync.Once
	closeOnce  sync.Once
	mu         sync.Mutex
	session    *legacyConnectionSession
	generation ConnectionGeneration
	closeErr   error
}

// NewLegacyGMConnection creates a stopped connection owner.
func NewLegacyGMConnection(config LegacyGMConnectionConfig) (*LegacyGMConnection, error) {
	if config.Address == "" || config.DialTimeout <= 0 || config.OriginTimeout <= 0 || config.InitialBackoff <= 0 || config.MaxBackoff < config.InitialBackoff || config.BackoffMultiplier <= 1 || config.Jitter <= 0 || config.Jitter >= 1 || config.StableReset <= 0 {
		return nil, fmt.Errorf("nhsk: invalid Legacy connection config")
	}
	if config.OutputQueue <= 0 {
		config.OutputQueue = 64
	}
	if config.Dial == nil {
		dialer := &net.Dialer{Timeout: config.DialTimeout}
		config.Dial = dialer.DialContext
	}
	return &LegacyGMConnection{config: config, started: make(chan struct{}), done: make(chan struct{})}, nil
}

// AttachRouting binds the connection's default gameplay adapter to a Runtime and Host.
// A caller may instead set OnFrame to handle the complete Legacy control plane.
func (connection *LegacyGMConnection) AttachRouting(runtime game.CommandRuntime, hostRef gsr.ServiceRef) error {
	if runtime == nil || hostRef.Node == "" || hostRef.ID == 0 {
		return errLegacyConnectionNotReady
	}
	connection.runtime, connection.hostRef = runtime, hostRef
	if connection.config.OnFrame == nil {
		connection.config.OnFrame = func(ctx context.Context, _ ConnectionGeneration, frame legacywire.Frame) error {
			if frame.Type != 0x8605 {
				return nil
			}
			return RouteLegacyGameplaySend(ctx, runtime, hostRef, frame.Bytes)
		}
	}
	return nil
}

// Start begins the reconnect loop and returns immediately.
func (connection *LegacyGMConnection) Start(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	var startErr error
	connection.startOnce.Do(func() {
		connection.ctx, connection.cancel = context.WithCancel(parent)
		close(connection.started)
		go connection.run()
	})
	select {
	case <-connection.started:
		return startErr
	default:
		return errLegacyConnectionClosed
	}
}

// Submit queues one typed output batch for the current connection generation.
func (connection *LegacyGMConnection) Submit(batch GameOutputBatch) error {
	if !validGameOutputBatch(batch) {
		return errInvalidLegacyGameOutput
	}
	connection.mu.Lock()
	session := connection.session
	connection.mu.Unlock()
	if session == nil {
		return errLegacyConnectionNotReady
	}
	select {
	case session.outputs <- cloneGameOutputBatch(batch):
		return nil
	case <-session.done:
		return errLegacyConnectionClosed
	default:
		return fmt.Errorf("%w: output queue full", errLegacyConnectionNotReady)
	}
}

// FailConnection closes only the matching current ConnectionGeneration.
func (connection *LegacyGMConnection) FailConnection(generation ConnectionGeneration, _ ConnectionFailureKind) {
	connection.mu.Lock()
	session := connection.session
	connection.mu.Unlock()
	if session != nil && session.generation == generation {
		_ = session.connection.Close()
	}
}

// Close stops reconnecting, closes the active socket and waits for I/O owners.
func (connection *LegacyGMConnection) Close(parent context.Context) error {
	connection.closeOnce.Do(func() {
		if parent == nil {
			parent = context.Background()
		}
		if connection.cancel != nil {
			connection.cancel()
		}
		connection.mu.Lock()
		session := connection.session
		connection.mu.Unlock()
		if session != nil {
			_ = session.connection.Close()
		}
		if connection.cancel == nil {
			connection.closeErr = nil
			return
		}
		select {
		case <-connection.done:
		case <-parent.Done():
			connection.closeErr = context.Cause(parent)
		}
	})
	return connection.closeErr
}

func (connection *LegacyGMConnection) run() {
	defer close(connection.done)
	config := legacywire.ConnectionConfig{DialTimeout: connection.config.DialTimeout, OriginTimeout: connection.config.OriginTimeout, InitialBackoff: connection.config.InitialBackoff, MaxBackoff: connection.config.MaxBackoff, BackoffMultiplier: connection.config.BackoffMultiplier, JitterRatio: connection.config.Jitter, StableResetAfter: connection.config.StableReset}
	policy, err := legacywire.NewBackoffPolicy(config, rand.Float64)
	if err != nil {
		connection.closeErr = err
		return
	}
	for {
		if connection.ctx.Err() != nil {
			return
		}
		connection.mu.Lock()
		connection.generation++
		generation := connection.generation
		connection.mu.Unlock()
		conn, dialErr := connection.config.Dial(connection.ctx, "tcp", connection.config.Address)
		if dialErr == nil {
			dialErr = legacywire.PerformOriginHandshake(conn, connection.config.OriginTimeout)
		}
		if dialErr == nil {
			session := &legacyConnectionSession{generation: generation, connection: conn, outputs: make(chan GameOutputBatch, connection.config.OutputQueue), done: make(chan struct{})}
			connection.activate(session)
			startedAt := time.Now()
			dialErr = connection.runSession(session)
			connection.deactivate(session)
			if connection.config.OnDisconnected != nil {
				connection.config.OnDisconnected(generation)
			}
			policy.ResetIfStable(time.Since(startedAt))
		} else if conn != nil {
			_ = conn.Close()
		}
		if connection.ctx.Err() != nil {
			return
		}
		_ = dialErr
		if !waitContext(connection.ctx, policy.Next()) {
			return
		}
	}
}

func (connection *LegacyGMConnection) runSession(session *legacyConnectionSession) error {
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-connection.ctx.Done():
				return
			case <-session.done:
				return
			case batch := <-session.outputs:
				frames, err := encodeLegacyGameOutputBatch(batch)
				if err != nil {
					_ = session.connection.Close()
					return
				}
				for _, frame := range frames {
					if err := legacywire.WriteFrame(session.connection, frame); err != nil {
						_ = session.connection.Close()
						return
					}
				}
			}
		}
	}()
	defer func() { close(session.done); <-writerDone }()
	for {
		frame, err := legacywire.ReadFrame(session.connection)
		if err != nil {
			return err
		}
		if connection.config.OnFrame != nil {
			if err := connection.config.OnFrame(connection.ctx, session.generation, frame); err != nil {
				return err
			}
		}
	}
}

func (connection *LegacyGMConnection) activate(session *legacyConnectionSession) {
	connection.mu.Lock()
	connection.session = session
	connection.mu.Unlock()
}

func (connection *LegacyGMConnection) deactivate(session *legacyConnectionSession) {
	connection.mu.Lock()
	if connection.session == session {
		connection.session = nil
	}
	connection.mu.Unlock()
	_ = session.connection.Close()
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func cloneGameOutputBatch(batch GameOutputBatch) GameOutputBatch {
	batch.Outputs = append([]GameOutput(nil), batch.Outputs...)
	return batch
}
