package main

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestNodeReadinessDistinguishesLinkAndQuarantineState(t *testing.T) {
	health := &fakeNodeHealth{snapshot: nodeHealthSnapshot{GMLinkReady: true}}
	node := newTestNode(t, health, nil, nil, nil)

	if got := node.readiness(); got != nodeReady {
		t.Fatalf("readiness = %q, want ready", got)
	}
	health.set(nodeHealthSnapshot{GMLinkReady: true, QuarantinedBattles: 1})
	if got := node.readiness(); got != nodeDegraded {
		t.Fatalf("readiness = %q, want degraded", got)
	}
	health.set(nodeHealthSnapshot{GMLinkReady: false, QuarantinedBattles: 1})
	if got := node.readiness(); got != nodeNotReady {
		t.Fatalf("readiness = %q, want not_ready", got)
	}
}

func TestNodeCloseUsesExplicitReverseOrderAndIsIdempotent(t *testing.T) {
	var (
		mu    sync.Mutex
		order []string
	)
	record := func(value string) {
		mu.Lock()
		order = append(order, value)
		mu.Unlock()
	}
	health := &fakeNodeHealth{snapshot: nodeHealthSnapshot{GMLinkReady: true}}
	connection := closeOwnerFunc(func(context.Context) error {
		if got := health.node.readiness(); got != nodeNotReady {
			t.Errorf("readiness during connection close = %q, want not_ready", got)
		}
		record("connection")
		return nil
	})
	factory := closeOwnerFunc(func(context.Context) error { record("factory"); return nil })
	runtime := &fakeNodeRuntime{record: record}
	services := []gsr.ServiceRef{{Node: "nhsk", ID: 1}, {Node: "nhsk", ID: 2}}
	node := newTestNode(t, health, connection, factory, runtime, services...)
	health.node = node

	if err := node.Close(context.Background()); err != nil {
		t.Fatalf("close node: %v", err)
	}
	if err := node.Close(context.Background()); err != nil {
		t.Fatalf("close node again: %v", err)
	}
	want := []string{"connection", "factory", "service:nhsk/2", "service:nhsk/1", "runtime"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("close order = %v, want %v", order, want)
	}
	status := node.shutdownStatus()
	if !status.Closed || status.Closing || status.CurrentOwner != "" || len(status.FailedOwners) != 0 {
		t.Fatalf("shutdown status = %#v, want cleanly closed", status)
	}
}

func TestNodeCloseContinuesAfterOwnerFailures(t *testing.T) {
	failure := errors.New("connection failed")
	connection := closeOwnerFunc(func(context.Context) error { return failure })
	factory := closeOwnerFunc(func(context.Context) error { return errors.New("factory failed") })
	runtime := &fakeNodeRuntime{stopError: errors.New("service failed"), closeError: errors.New("runtime failed")}
	node := newTestNode(t, &fakeNodeHealth{}, connection, factory, runtime, gsr.ServiceRef{Node: "nhsk", ID: 1})

	err := node.Close(context.Background())
	if !errors.Is(err, failure) {
		t.Fatalf("close error = %v, want joined connection failure", err)
	}
	status := node.shutdownStatus()
	want := []string{"connection", "factory", "service:nhsk/1", "runtime"}
	if !status.Closed || !reflect.DeepEqual(status.FailedOwners, want) {
		t.Fatalf("shutdown status = %#v, want failures %v", status, want)
	}
}

func TestNodeCloseAppliesConfiguredShutdownTimeout(t *testing.T) {
	deadlineSeen := false
	connection := closeOwnerFunc(func(ctx context.Context) error {
		_, deadlineSeen = ctx.Deadline()
		return nil
	})
	node, err := newGameLogicNode(
		10*time.Second,
		&fakeNodeHealth{},
		connection,
		closeOwnerFunc(func(context.Context) error { return nil }),
		&fakeNodeRuntime{},
		nil,
	)
	if err != nil {
		t.Fatalf("new node: %v", err)
	}
	if err := node.Close(context.Background()); err != nil {
		t.Fatalf("close node: %v", err)
	}
	if !deadlineSeen {
		t.Fatal("connection close context had no configured deadline")
	}
}

func TestNodeShutdownStatusNamesTheOwnerThatHasNotReturned(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	connection := closeOwnerFunc(func(ctx context.Context) error {
		close(entered)
		<-release
		return ctx.Err()
	})
	node := newTestNode(t, &fakeNodeHealth{}, connection, closeOwnerFunc(func(context.Context) error { return nil }), &fakeNodeRuntime{})

	parent, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- node.Close(parent) }()
	<-entered

	status := node.shutdownStatus()
	if !status.Closing || status.Closed || status.CurrentOwner != "connection" {
		t.Fatalf("shutdown status while blocked = %#v", status)
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("close error = %v, want context canceled", err)
	}
	status = node.shutdownStatus()
	if !status.Closed || status.CurrentOwner != "" || !reflect.DeepEqual(status.FailedOwners, []string{"connection"}) {
		t.Fatalf("final shutdown status = %#v", status)
	}
}

func newTestNode(t *testing.T, health nodeHealthSource, connection, factory closeOwner, runtime nodeRuntime, services ...gsr.ServiceRef) *gameLogicNode {
	t.Helper()
	if connection == nil {
		connection = closeOwnerFunc(func(context.Context) error { return nil })
	}
	if factory == nil {
		factory = closeOwnerFunc(func(context.Context) error { return nil })
	}
	if runtime == nil {
		runtime = &fakeNodeRuntime{}
	}
	node, err := newGameLogicNode(10*time.Second, health, connection, factory, runtime, services)
	if err != nil {
		t.Fatalf("new node: %v", err)
	}
	return node
}

type closeOwnerFunc func(context.Context) error

func (function closeOwnerFunc) Close(ctx context.Context) error { return function(ctx) }

type fakeNodeRuntime struct {
	record     func(string)
	stopError  error
	closeError error
}

func (runtime *fakeNodeRuntime) Stop(_ context.Context, ref gsr.ServiceRef) error {
	if runtime.record != nil {
		runtime.record("service:" + string(ref.Node) + "/" + serviceIDText(ref.ID))
	}
	return runtime.stopError
}

func (runtime *fakeNodeRuntime) Close(context.Context) error {
	if runtime.record != nil {
		runtime.record("runtime")
	}
	return runtime.closeError
}

type fakeNodeHealth struct {
	mu       sync.Mutex
	snapshot nodeHealthSnapshot
	node     *gameLogicNode
}

func (health *fakeNodeHealth) NodeHealth() nodeHealthSnapshot {
	health.mu.Lock()
	defer health.mu.Unlock()
	return health.snapshot
}

func (health *fakeNodeHealth) set(snapshot nodeHealthSnapshot) {
	health.mu.Lock()
	health.snapshot = snapshot
	health.mu.Unlock()
}
