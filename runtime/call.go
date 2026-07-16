package gsr

import (
	"context"
	"sync"
	"sync/atomic"
)

type pendingCall struct {
	source  ServiceRef
	target  ServiceRef
	command CommandID
	result  chan callResult
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

func newPendingCalls() *pendingCalls { return &pendingCalls{calls: make(map[SessionID]*pendingCall)} }
func (p *pendingCalls) create(source, target ServiceRef, command CommandID) (SessionID, *pendingCall) {
	call := &pendingCall{source: source, target: target, command: command, result: make(chan callResult, 1)}
	for {
		session := SessionID(p.next.Add(1))
		if session == 0 {
			continue
		}
		p.mu.Lock()
		if _, exists := p.calls[session]; !exists {
			p.calls[session] = call
			p.mu.Unlock()
			return session, call
		}
		p.mu.Unlock()
	}
}
func (p *pendingCalls) remove(session SessionID) {
	p.mu.Lock()
	delete(p.calls, session)
	p.mu.Unlock()
}
func (p *pendingCalls) take(source, target ServiceRef, command CommandID, session SessionID) *pendingCall {
	p.mu.Lock()
	call := p.calls[session]
	if call != nil && call.source == source && call.target == target && call.command == command {
		delete(p.calls, session)
	} else {
		call = nil
	}
	p.mu.Unlock()
	return call
}
func (p *pendingCalls) complete(source, target ServiceRef, command CommandID, session SessionID, result callResult) bool {
	call := p.take(source, target, command, session)
	if call == nil {
		return false
	}
	call.result <- result
	return true
}
func (p *pendingCalls) failNode(node NodeID, err error) {
	p.mu.Lock()
	failed := make([]*pendingCall, 0)
	for session, call := range p.calls {
		if call.source.Node == node || call.target.Node == node {
			delete(p.calls, session)
			failed = append(failed, call)
		}
	}
	p.mu.Unlock()
	for _, call := range failed {
		call.result <- callResult{err: err}
	}
}
func (p *pendingCalls) failService(ref ServiceRef, err error) {
	p.mu.Lock()
	failed := make([]*pendingCall, 0)
	for session, call := range p.calls {
		if call.source == ref || call.target == ref {
			delete(p.calls, session)
			failed = append(failed, call)
		}
	}
	p.mu.Unlock()
	for _, call := range failed {
		call.result <- callResult{err: err}
	}
}
func (p *pendingCalls) failAll(err error) {
	p.mu.Lock()
	failed := make([]*pendingCall, 0, len(p.calls))
	for session, call := range p.calls {
		delete(p.calls, session)
		failed = append(failed, call)
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
		if ctx.Err() == context.DeadlineExceeded {
			return nil, ErrTimeout
		}
		return nil, ctx.Err()
	}
}

// Call synchronously delivers a Command and waits for its Reply.
func (r *Runtime) Call(ctx context.Context, target ServiceRef, id CommandID, payload any) (any, error) {
	return r.call(ctx, ServiceRef{}, target, id, payload, []ServiceRef{target})
}

func (r *Runtime) callFromService(ctx context.Context, source *serviceInstance, target ServiceRef, id CommandID, payload any) (any, error) {
	if source.closing.Load() {
		return nil, ErrServiceClosed
	}
	status := ServiceStatus(source.status.Load())
	if status != ServiceRunning && status != ServiceStopping {
		return nil, ErrServiceClosed
	}
	path := source.path()
	if len(path) == 0 {
		path = []ServiceRef{source.ref}
	}
	for _, ref := range path {
		if ref == target {
			return nil, ErrCallCycle
		}
	}
	path = append(path, target)
	if !r.scheduler.yield(source) {
		return nil, ErrCallNotAllowed
	}
	result, err := r.call(ctx, source.ref, target, id, payload, path)
	if resumeErr := r.scheduler.resume(source); resumeErr != nil && err == nil {
		err = resumeErr
	}
	return result, err
}

func (r *Runtime) call(ctx context.Context, source, target ServiceRef, id CommandID, payload any, path []ServiceRef) (any, error) {
	if r.state.Load() != runtimeRunning {
		return nil, ErrRuntimeClosed
	}
	if source == (ServiceRef{}) {
		source = ServiceRef{Node: r.node}
	}
	session, pending := r.pending.create(source, target, id)
	envelope := Envelope{Source: source, Target: target, Session: session, Command: id, Payload: payload, CallPath: path}
	if err := r.sendEnvelope(envelope); err != nil {
		r.pending.remove(session)
		return nil, err
	}
	result, err := pending.wait(ctx)
	if err != nil {
		r.pending.remove(session)
	}
	return result, err
}

func (r *Runtime) reply(responder, caller ServiceRef, command CommandID, session SessionID, value any, err error) error {
	if caller.Node != r.node {
		if caller.Node == "" || r.cluster == nil {
			return ErrRemoteUnavailable
		}
		return r.cluster.reply(responder, caller, command, session, value, err)
	}
	if !r.pending.complete(caller, responder, command, session, callResult{value: value, err: err}) {
		r.metrics.Inc("late_reply_total")
		return ErrReplyExpired
	}
	return nil
}
