package gsr

import (
	"context"
	"sync"
	"sync/atomic"
)

type pendingCall struct {
	target ServiceRef
	result chan callResult
}

type callResult struct {
	value any
	err   error
}

type pendingCalls struct {
	next  atomic.Uint64
	mu    sync.Mutex
	calls map[SessionID]*pendingCall
}

func newPendingCalls() *pendingCalls {
	return &pendingCalls{calls: make(map[SessionID]*pendingCall)}
}

func (p *pendingCalls) create(target ServiceRef) (SessionID, *pendingCall) {
	session := SessionID(p.next.Add(1))
	call := &pendingCall{target: target, result: make(chan callResult, 1)}
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
	call.result <- callResult{value: result}
	return true
}

func (p *pendingCalls) failTarget(target ServiceRef, err error) {
	p.mu.Lock()
	failed := make([]*pendingCall, 0)
	for session, call := range p.calls {
		if call.target == target {
			delete(p.calls, session)
			failed = append(failed, call)
		}
	}
	p.mu.Unlock()
	for _, call := range failed {
		call.result <- callResult{err: err}
	}
}

func (p *pendingCall) wait(ctx context.Context) (any, error) {
	select {
	case result := <-p.result:
		return result.value, result.err
	case <-ctx.Done():
		return nil, ErrTimeout
	}
}

// Call synchronously delivers a Command and waits for its Reply.
func (r *Runtime) Call(ctx context.Context, target ServiceRef, id CommandID, payload any) (any, error) {
	session, pending := r.pending.create(target)
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
