package gsr

import (
	"context"
	"sync"
	"sync/atomic"
)

type pendingCall struct {
	result chan any
}

type pendingCalls struct {
	next  atomic.Uint64
	mu    sync.Mutex
	calls map[SessionID]*pendingCall
}

func newPendingCalls() *pendingCalls {
	return &pendingCalls{calls: make(map[SessionID]*pendingCall)}
}

func (p *pendingCalls) create() (SessionID, *pendingCall) {
	session := SessionID(p.next.Add(1))
	call := &pendingCall{result: make(chan any, 1)}
	p.mu.Lock()
	p.calls[session] = call
	p.mu.Unlock()
	return session, call
}

func (p *pendingCalls) remove(session SessionID) {
	p.mu.Lock()
	delete(p.calls, session)
	p.mu.Unlock()
}

func (p *pendingCalls) complete(session SessionID, result any) bool {
	p.mu.Lock()
	call := p.calls[session]
	if call != nil {
		delete(p.calls, session)
	}
	p.mu.Unlock()
	if call == nil {
		return false
	}
	call.result <- result
	return true
}

func (p *pendingCall) wait(ctx context.Context) (any, error) {
	select {
	case result := <-p.result:
		return result, nil
	case <-ctx.Done():
		return nil, ErrTimeout
	}
}

// Call synchronously delivers a Command and waits for its Reply.
func (r *Runtime) Call(ctx context.Context, target ServiceRef, id CommandID, payload any) (any, error) {
	session, pending := r.pending.create()
	if err := r.route(Envelope{Target: target, Session: session, Command: id, Payload: payload}); err != nil {
		r.pending.remove(session)
		return nil, err
	}
	result, err := pending.wait(ctx)
	if err != nil {
		r.pending.remove(session)
	}
	return result, err
}
