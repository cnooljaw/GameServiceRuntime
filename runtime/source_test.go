package gsr_test

import (
	"context"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestRuntimeCommandsExposeNodeRuntimeAsSource(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	service := &sourceObservingService{sources: make(chan gsr.ServiceRef, 2)}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}

	if err := runtime.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	want := gsr.ServiceRef{Node: "node-a"}
	if got := <-service.sources; got != want {
		t.Fatalf("Send source = %#v, want %#v", got, want)
	}

	if _, err := runtime.Call(context.Background(), ref, 2, nil); err != nil {
		t.Fatal(err)
	}
	if got := <-service.sources; got != want {
		t.Fatalf("Call source = %#v, want %#v", got, want)
	}
}

func TestServiceCallExposesCallingServiceAsSource(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	target := &sourceObservingService{sources: make(chan gsr.ServiceRef, 1)}
	targetRef, err := runtime.CreateService(gsr.ServiceSpec{Service: target})
	if err != nil {
		t.Fatal(err)
	}
	caller := &sourceCallingService{target: targetRef}
	callerRef, err := runtime.CreateService(gsr.ServiceSpec{Service: caller})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runtime.Call(context.Background(), callerRef, 1, nil); err != nil {
		t.Fatal(err)
	}
	if got := <-target.sources; got != callerRef {
		t.Fatalf("service Call source = %#v, want %#v", got, callerRef)
	}
}

func TestRemoteCommandsPreserveRuntimeAndServiceSources(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntime(t, "node-b", network)

	target := &sourceObservingService{sources: make(chan gsr.ServiceRef, 2)}
	targetRef := createClusterService(t, nodeB, target)
	if _, err := nodeA.Call(context.Background(), targetRef, 2, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := <-target.sources, (gsr.ServiceRef{Node: "node-a"}); got != want {
		t.Fatalf("remote Runtime.Call source = %#v, want %#v", got, want)
	}

	caller := &sourceCallingService{target: targetRef}
	callerRef := createClusterService(t, nodeA, caller)
	if _, err := nodeA.Call(context.Background(), callerRef, 1, nil); err != nil {
		t.Fatal(err)
	}
	if got := <-target.sources; got != callerRef {
		t.Fatalf("remote service Call source = %#v, want %#v", got, callerRef)
	}
}

type sourceObservingService struct {
	sources chan gsr.ServiceRef
}

func (*sourceObservingService) Commands() []gsr.CommandID     { return []gsr.CommandID{1, 2} }
func (*sourceObservingService) Init(gsr.ServiceContext) error { return nil }
func (s *sourceObservingService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	s.sources <- ctx.Source()
	if command.ID == 2 {
		return ctx.Reply("ok")
	}
	return nil
}
func (*sourceObservingService) Stop(context.Context) error { return nil }
func (*sourceObservingService) Close() error               { return nil }

type sourceCallingService struct {
	context gsr.ServiceContext
	target  gsr.ServiceRef
}

func (*sourceCallingService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (s *sourceCallingService) Init(context gsr.ServiceContext) error {
	s.context = context
	return nil
}
func (s *sourceCallingService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	value, err := s.context.Call(context.Background(), s.target, 2, nil)
	if err != nil {
		return err
	}
	return ctx.Reply(value)
}
func (*sourceCallingService) Stop(context.Context) error { return nil }
func (*sourceCallingService) Close() error               { return nil }
