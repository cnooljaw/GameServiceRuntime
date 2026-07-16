package gsr_test

import (
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestServiceRefIsComparable(t *testing.T) {
	ref := gsr.ServiceRef{Node: "local", ID: 7}
	got := map[gsr.ServiceRef]string{ref: "service"}[ref]
	if got != "service" {
		t.Fatalf("lookup = %q, want service", got)
	}
}

func TestCommandPreservesPayload(t *testing.T) {
	payload := struct{ Value int }{Value: 42}
	cmd := gsr.Command{ID: 1001, Payload: payload}
	if got := cmd.Payload.(struct{ Value int }).Value; got != 42 {
		t.Fatalf("payload = %d, want 42", got)
	}
}
