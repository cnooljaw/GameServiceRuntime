package gsr_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestResolveRemoteQueriesNamedServiceWithoutBusinessCodec(t *testing.T) {
	network := newMemoryCluster()
	codec := &rejectingClusterCodec{}
	nodeA := newTestClusterRuntimeWithCodec(t, "node-a", network, codec)
	nodeB := newTestClusterRuntimeWithCodec(t, "node-b", network, codec)

	first, err := nodeB.CreateService(gsr.ServiceSpec{Name: ".discovery", Service: clusterReplyService{}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := nodeA.ResolveRemote(context.Background(), "node-b", ".discovery")
	if err != nil {
		t.Fatal(err)
	}
	if got != first {
		t.Fatalf("ResolveRemote = %#v, want %#v", got, first)
	}
	if calls := codec.calls.Load(); calls != 0 {
		t.Fatalf("business ClusterCodec calls = %d, want 0", calls)
	}

	if err := nodeB.Stop(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second, err := nodeB.CreateService(gsr.ServiceSpec{Name: ".discovery", Service: clusterReplyService{}})
	if err != nil {
		t.Fatal(err)
	}
	got, err = nodeA.ResolveRemote(context.Background(), "node-b", ".discovery")
	if err != nil {
		t.Fatal(err)
	}
	if got != second || got == first {
		t.Fatalf("ResolveRemote after restart = %#v, want new ref %#v", got, second)
	}
}

func TestResolveRemotePreservesRegistryErrors(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	newTestClusterRuntime(t, "node-b", network)

	_, err := nodeA.ResolveRemote(context.Background(), "node-b", ".missing")
	if !errors.Is(err, gsr.ErrServiceNotFound) {
		t.Fatalf("ResolveRemote error = %v, want ErrServiceNotFound", err)
	}
}

func TestResolveRemoteUsesLocalRegistryForLocalNode(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	want, err := runtime.CreateService(gsr.ServiceSpec{Name: ".local", Service: clusterReplyService{}})
	if err != nil {
		t.Fatal(err)
	}

	got, err := runtime.ResolveRemote(context.Background(), "node-a", ".local")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ResolveRemote local = %#v, want %#v", got, want)
	}
}

func TestClusterRejectsOtherCommandsAddressedToNodeEndpoint(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	newTestClusterRuntime(t, "node-b", network)
	target := gsr.ServiceRef{Node: "node-b"}

	if err := nodeA.Send(target, 999, nil); !errors.Is(err, gsr.ErrInvalidClusterEnvelope) {
		t.Fatalf("Send to node endpoint error = %v, want ErrInvalidClusterEnvelope", err)
	}
	if _, err := nodeA.Call(context.Background(), target, 999, nil); !errors.Is(err, gsr.ErrInvalidClusterEnvelope) {
		t.Fatalf("Call to node endpoint error = %v, want ErrInvalidClusterEnvelope", err)
	}
}

type rejectingClusterCodec struct {
	calls atomic.Int64
}

func (c *rejectingClusterCodec) Encode(gsr.CommandID, bool, any) ([]byte, error) {
	c.calls.Add(1)
	return nil, errors.New("business codec must not handle Core query")
}

func (c *rejectingClusterCodec) Decode(gsr.CommandID, bool, []byte) (any, error) {
	c.calls.Add(1)
	return nil, errors.New("business codec must not handle Core query")
}
