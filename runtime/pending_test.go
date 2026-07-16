package gsr

import (
	"errors"
	"testing"
)

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
