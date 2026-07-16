package gsr

import (
	"errors"
	"math"
	"testing"
)

func TestPendingCallAllocationSkipsZeroAndActiveCollision(t *testing.T) {
	pending := newPendingCalls()
	existing := &pendingCall{result: make(chan callResult, 1)}
	pending.calls[1] = existing
	pending.next.Store(math.MaxUint64)

	session, created := pending.create(ServiceRef{}, ServiceRef{Node: "local", ID: 2}, 1)
	if session == 0 {
		t.Fatal("allocated reserved SessionID 0")
	}
	if session != 2 {
		t.Fatalf("session = %d, want 2 after skipping 0 and active 1", session)
	}
	if pending.calls[1] != existing {
		t.Fatal("active PendingCall was overwritten after SessionID wrap")
	}
	if pending.calls[session] != created {
		t.Fatal("new PendingCall was not registered under its SessionID")
	}
}

func TestPendingCallRequiresMatchingCallerAndResponder(t *testing.T) {
	pending := newPendingCalls()
	source := ServiceRef{Node: "local", ID: 1}
	target := ServiceRef{Node: "remote", ID: 2}
	session, call := pending.create(source, target, 7)
	if pending.complete(ServiceRef{Node: "local", ID: 3}, target, 7, session, callResult{value: "wrong caller"}) {
		t.Fatal("reply for a different caller completed the PendingCall")
	}
	if pending.complete(source, ServiceRef{Node: "remote", ID: 3}, 7, session, callResult{value: "wrong responder"}) {
		t.Fatal("reply from a different responder completed the PendingCall")
	}
	if pending.complete(source, target, 8, session, callResult{value: "wrong command"}) {
		t.Fatal("reply for a different command completed the PendingCall")
	}
	if !pending.complete(source, target, 7, session, callResult{value: "right"}) {
		t.Fatal("reply for the matching caller and responder did not complete the PendingCall")
	}
	result := <-call.result
	if result.value != "right" {
		t.Fatalf("result = %v", result.value)
	}
}

func TestPendingCallFailsWhenSourceOrTargetStops(t *testing.T) {
	sentinel := errors.New("stopped")
	pending := newPendingCalls()
	service := ServiceRef{Node: "local", ID: 1}
	_, outgoing := pending.create(service, ServiceRef{Node: "local", ID: 2}, 1)
	_, incoming := pending.create(ServiceRef{}, service, 1)
	pending.failService(service, sentinel)
	for _, call := range []*pendingCall{outgoing, incoming} {
		if result := <-call.result; !errors.Is(result.err, sentinel) {
			t.Fatalf("result error = %v", result.err)
		}
	}
}
