package discovery_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
)

func TestRegisterAndResolveName(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	lease := registerNode(t, fixture.client, "node-b")
	want := gsr.ServiceRef{Node: "node-b", ID: 100}

	if err := fixture.client.RegisterName(context.Background(), lease, ".config", want); err != nil {
		t.Fatal(err)
	}
	got, err := fixture.client.ResolveName(context.Background(), ".config")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ResolveName = %v, want %v", got, want)
	}
}

func TestSameLeaseCanReplaceNameRef(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	lease := registerNode(t, fixture.client, "node-b")
	first := gsr.ServiceRef{Node: "node-b", ID: 100}
	second := gsr.ServiceRef{Node: "node-b", ID: 101}
	if err := fixture.client.RegisterName(context.Background(), lease, ".config", first); err != nil {
		t.Fatal(err)
	}
	if err := fixture.client.RegisterName(context.Background(), lease, ".config", second); err != nil {
		t.Fatal(err)
	}
	got, err := fixture.client.ResolveName(context.Background(), ".config")
	if err != nil {
		t.Fatal(err)
	}
	if got != second {
		t.Fatalf("ResolveName = %v, want replacement %v", got, second)
	}
}

func TestOtherLeaseCannotReplaceName(t *testing.T) {
	fixture := newRemoteDiscoveryFixture(t)
	owner := registerNode(t, fixture.local, "node-b")
	other := registerNode(t, fixture.remote, "node-a")
	want := gsr.ServiceRef{Node: "node-b", ID: 100}
	if err := fixture.local.RegisterName(context.Background(), owner, ".config", want); err != nil {
		t.Fatal(err)
	}
	if err := fixture.remote.RegisterName(context.Background(), other, ".config", gsr.ServiceRef{Node: "node-a", ID: 200}); !errors.Is(err, discovery.ErrNameConflict) {
		t.Fatalf("RegisterName error = %v, want ErrNameConflict", err)
	}
	got, err := fixture.remote.ResolveName(context.Background(), ".config")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("conflicting registration changed binding to %v", got)
	}
}

func TestUnregisterNameRequiresExactOwnerAndRef(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	lease := registerNode(t, fixture.client, "node-b")
	oldRef := gsr.ServiceRef{Node: "node-b", ID: 100}
	currentRef := gsr.ServiceRef{Node: "node-b", ID: 101}
	if err := fixture.client.RegisterName(context.Background(), lease, ".config", oldRef); err != nil {
		t.Fatal(err)
	}
	if err := fixture.client.RegisterName(context.Background(), lease, ".config", currentRef); err != nil {
		t.Fatal(err)
	}
	if err := fixture.client.UnregisterName(context.Background(), lease, ".config", oldRef); !errors.Is(err, discovery.ErrNameNotFound) {
		t.Fatalf("stale UnregisterName error = %v, want ErrNameNotFound", err)
	}
	if got, err := fixture.client.ResolveName(context.Background(), ".config"); err != nil || got != currentRef {
		t.Fatalf("binding after stale unregister = %v, %v", got, err)
	}
	if err := fixture.client.UnregisterName(context.Background(), lease, ".config", currentRef); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.client.ResolveName(context.Background(), ".config"); !errors.Is(err, discovery.ErrNameNotFound) {
		t.Fatalf("ResolveName error = %v, want ErrNameNotFound", err)
	}
}

func TestNodeExpiryRemovesOwnedNames(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	lease := registerNode(t, fixture.client, "node-b")
	if err := fixture.client.RegisterName(context.Background(), lease, ".config", gsr.ServiceRef{Node: "node-b", ID: 100}); err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(time.Minute + time.Nanosecond)

	if _, err := fixture.client.ResolveName(context.Background(), ".config"); !errors.Is(err, discovery.ErrNameNotFound) {
		t.Fatalf("ResolveName error = %v, want ErrNameNotFound", err)
	}
}

func TestNodeReregisterRemovesPreviousGenerationNames(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	first := registerNode(t, fixture.client, "node-b")
	if err := fixture.client.RegisterName(context.Background(), first, ".config", gsr.ServiceRef{Node: "node-b", ID: 100}); err != nil {
		t.Fatal(err)
	}
	second, err := fixture.client.RegisterNode(context.Background(), "node-b", "node-b:9010")
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation == first.Generation {
		t.Fatal("node re-registration reused generation")
	}
	if _, err := fixture.client.ResolveName(context.Background(), ".config"); !errors.Is(err, discovery.ErrNameNotFound) {
		t.Fatalf("ResolveName error = %v, want ErrNameNotFound", err)
	}
}

func TestRegisterNameRequiresMatchingNode(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	lease := registerNode(t, fixture.client, "node-b")
	tests := []struct {
		label string
		name  gsr.ServiceName
		ref   gsr.ServiceRef
	}{
		{label: "empty name", name: "", ref: gsr.ServiceRef{Node: "node-b", ID: 100}},
		{label: "other node", name: ".config", ref: gsr.ServiceRef{Node: "node-a", ID: 100}},
		{label: "zero service ID", name: ".config", ref: gsr.ServiceRef{Node: "node-b"}},
	}
	for _, test := range tests {
		err := fixture.client.RegisterName(context.Background(), lease, test.name, test.ref)
		if !errors.Is(err, discovery.ErrInvalidName) {
			t.Fatalf("%s: RegisterName(%q, %v) error = %v, want ErrInvalidName", test.label, test.name, test.ref, err)
		}
	}
}

func TestResolveNameDoesNotExposeMutableState(t *testing.T) {
	fixture := newDiscoveryFixture(t, discovery.Config{})
	lease := registerNode(t, fixture.client, "node-b")
	want := gsr.ServiceRef{Node: "node-b", ID: 100}
	if err := fixture.client.RegisterName(context.Background(), lease, ".config", want); err != nil {
		t.Fatal(err)
	}
	first, err := fixture.client.ResolveName(context.Background(), ".config")
	if err != nil {
		t.Fatal(err)
	}
	first.Node = "changed"
	first.ID = 999
	second, err := fixture.client.ResolveName(context.Background(), ".config")
	if err != nil {
		t.Fatal(err)
	}
	if second != want {
		t.Fatalf("second ResolveName = %v, want %v", second, want)
	}
}

func registerNode(t *testing.T, client *discovery.Client, node gsr.NodeID) discovery.NodeLease {
	t.Helper()
	lease, err := client.RegisterNode(context.Background(), node, string(node)+":9000")
	if err != nil {
		t.Fatal(err)
	}
	return lease
}
