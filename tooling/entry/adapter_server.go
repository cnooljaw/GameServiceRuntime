package entry

import (
	"context"
	"net"
	"sync"
)

type tcpServer struct {
	listener net.Listener
	handler  func(context.Context, net.Conn)

	mu          sync.Mutex
	started     bool
	canceled    bool
	context     context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	connections map[net.Conn]struct{}
	workers     sync.WaitGroup
}

func newTCPServer(listener net.Listener, handler func(context.Context, net.Conn)) *tcpServer {
	return &tcpServer{listener: listener, handler: handler, done: make(chan struct{}), connections: make(map[net.Conn]struct{})}
}

func (s *tcpServer) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.canceled {
		return ErrInvalidConfig
	}
	s.started = true
	s.context, s.cancel = context.WithCancel(context.Background())
	go s.acceptLoop()
	return nil
}

func (s *tcpServer) acceptLoop() {
	defer func() {
		s.workers.Wait()
		close(s.done)
	}()
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.context.Done():
				return
			default:
				return
			}
		}
		s.mu.Lock()
		if s.canceled {
			s.mu.Unlock()
			_ = connection.Close()
			return
		}
		s.connections[connection] = struct{}{}
		s.workers.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.workers.Done()
			defer s.remove(connection)
			defer connection.Close()
			s.handler(s.context, connection)
		}()
	}
}

func (s *tcpServer) close(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidConfig
	}
	s.mu.Lock()
	if !s.canceled {
		s.canceled = true
		if s.cancel != nil {
			s.cancel()
		}
		_ = s.listener.Close()
		for connection := range s.connections {
			_ = connection.Close()
		}
		if !s.started {
			close(s.done)
		}
	}
	done := s.done
	s.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (s *tcpServer) remove(connection net.Conn) {
	s.mu.Lock()
	delete(s.connections, connection)
	s.mu.Unlock()
}
