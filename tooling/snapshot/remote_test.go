package snapshot

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

func TestRemoteSnapshotCaptureUsesComposableCodec(t *testing.T) {
	key := Key{Namespace: "remote", ID: "service-1"}
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 2}, transportB, NewCodec(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeB.Close(context.Background()) })
	target, err := nodeB.CreateService(gsr.ServiceSpec{Service: remoteSnapshotService{key: key}})
	if err != nil {
		t.Fatal(err)
	}

	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 2}, transportA, NewCodec(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeA.Close(context.Background()) })

	store := NewMemoryStore()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	manager, err := NewManager(nodeA, store, Config{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	captured, err := manager.Capture(context.Background(), target, key)
	if err != nil {
		t.Fatal(err)
	}
	if captured.Source != target || !captured.CapturedAt.Equal(now) {
		t.Fatalf("captured = %#v", captured)
	}
	if captured.State.Schema != "remote" || captured.State.Version != 1 || captured.State.Revision != 9 || string(captured.State.Payload) != "cluster" {
		t.Fatalf("state = %#v", captured.State)
	}
	loaded, err := store.Load(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Source != target || string(loaded.State.Payload) != "cluster" {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestRemoteSnapshotCaptureRejectsMismatchedOwnerKey(t *testing.T) {
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b-mismatch", Workers: 2}, transportB, NewCodec(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeB.Close(context.Background()) })
	target, err := nodeB.CreateService(gsr.ServiceSpec{Service: remoteSnapshotService{
		key: Key{Namespace: "remote", ID: "owner"}, allowMismatchedRequest: true,
	}})
	if err != nil {
		t.Fatal(err)
	}

	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b-mismatch": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a-mismatch", Workers: 2}, transportA, NewCodec(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeA.Close(context.Background()) })

	store := NewMemoryStore()
	manager, err := NewManager(nodeA, store, Config{})
	if err != nil {
		t.Fatal(err)
	}
	requested := Key{Namespace: "remote", ID: "requested"}
	if _, err := manager.Capture(context.Background(), target, requested); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Capture error = %v, want ErrInvalidResponse", err)
	}
	if _, err := store.Load(context.Background(), requested); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("Load error = %v, want ErrSnapshotNotFound", err)
	}
}

type remoteSnapshotService struct {
	key                    Key
	allowMismatchedRequest bool
}

func (remoteSnapshotService) Commands() []gsr.CommandID { return []gsr.CommandID{CaptureCommand} }
func (remoteSnapshotService) Init(gsr.ServiceContext) error {
	return nil
}
func (s remoteSnapshotService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	if command.ID != CaptureCommand {
		return gsr.ErrCommandNotRegistered
	}
	request, ok := command.Payload.(CaptureRequest)
	if !ok {
		return ErrInvalidResponse
	}
	if !s.allowMismatchedRequest && request.Key != s.key {
		return ErrInvalidKey
	}
	return ctx.Reply(CaptureResponse{Key: s.key, State: State{
		Schema: "remote", Version: 1, Revision: 9, Payload: []byte("cluster"),
	}})
}
func (remoteSnapshotService) Stop(context.Context) error { return nil }
func (remoteSnapshotService) Close() error               { return nil }
