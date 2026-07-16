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

	session, created := pending.create(ServiceRef{}, ServiceRef{Node: "local", ID: 2})
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

func TestPendingCallRequiresMatchingReplySource(t *testing.T) {
	pending := newPendingCalls()
	source := ServiceRef{Node: "local", ID: 1}
	session, call := pending.create(source, ServiceRef{Node: "local", ID: 2})
	if pending.complete(ServiceRef{Node: "local", ID: 3}, session, callResult{value: "wrong"}) {
		t.Fatal("reply for a different Source completed the PendingCall")
	}
	if !pending.complete(source, session, callResult{value: "right"}) {
		t.Fatal("reply for the matching Source did not complete the PendingCall")
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
	_, outgoing := pending.create(service, ServiceRef{Node: "local", ID: 2})
	_, incoming := pending.create(ServiceRef{}, service)
	pending.failService(service, sentinel)
	for _, call := range []*pendingCall{outgoing, incoming} {
		if result := <-call.result; !errors.Is(result.err, sentinel) {
			t.Fatalf("result error = %v", result.err)
		}
	}
}
