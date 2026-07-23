package control

import (
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

func TestNormalizeBeginRecoveryRequestSortsAndRejectsDuplicateRemovedTargets(t *testing.T) {
	request := BeginRecoveryRequest{
		RequestID: "recover-1",
		Principal: "operator-a",
		Group:     "battle",
		Expected: servicegroup.ServiceSet{
			Name: "battle", Version: servicegroup.ServiceSetVersion{AuthorityEpoch: 1, Revision: 2},
			Refs: []gsr.ServiceRef{{Node: "node-b", ID: 7}}, Tags: map[string]string{},
		},
		Targets: []RecoveryTargetRequest{
			{Removed: gsr.ServiceRef{Node: "node-b", ID: 9}, Agent: gsr.ServiceRef{Node: "node-b", ID: 1}, Blueprint: "battle-v2"},
			{Removed: gsr.ServiceRef{Node: "node-a", ID: 8}, Agent: gsr.ServiceRef{Node: "node-a", ID: 1}, Blueprint: "battle-v2"},
		},
	}
	normalized, err := normalizeBeginRecoveryRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Targets[0].Removed.Node != "node-a" || normalized.Targets[1].Removed.Node != "node-b" {
		t.Fatalf("targets = %#v, want sorted by Removed ref", normalized.Targets)
	}
	request.Targets = append(request.Targets, request.Targets[0])
	if _, err := normalizeBeginRecoveryRequest(request); !errors.Is(err, ErrInvalidRecoveryRequest) {
		t.Fatalf("Normalize duplicate error = %v, want ErrInvalidRecoveryRequest", err)
	}
}

func TestRecoveryOperationReturnsIndependentCopiesAndValidatesStates(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	expected := servicegroup.ServiceSet{
		Name: "battle", Version: servicegroup.ServiceSetVersion{AuthorityEpoch: 1, Revision: 2},
		Refs: []gsr.ServiceRef{{Node: "node-b", ID: 7}}, Tags: map[string]string{"zone": "a"},
	}
	published := expected
	published.Version.Revision = 3
	published.Refs = []gsr.ServiceRef{{Node: "node-b", ID: 7}, {Node: "node-b", ID: 11}}
	published.Tags = map[string]string{"zone": "a"}
	operation := RecoveryOperation{
		RequestID: "recover-1", Principal: "operator-a", Group: "battle", Expected: expected, Published: published,
		Targets: []RecoveryTarget{{Removed: gsr.ServiceRef{Node: "node-b", ID: 9}, Agent: gsr.ServiceRef{Node: "node-b", ID: 1}, Blueprint: "battle-v2", Created: gsr.ServiceRef{Node: "node-b", ID: 11}, State: RecoveryTargetPublished}},
		Phase:   RecoveryCompleted, CreatedAt: now, UpdatedAt: now,
	}
	if !validRecoveryOperation(operation) {
		t.Fatalf("validRecoveryOperation(%#v) = false", operation)
	}
	cloned := cloneRecoveryOperation(operation)
	cloned.Expected.Tags["zone"] = "changed"
	cloned.Published.Refs[0] = gsr.ServiceRef{Node: "node-x", ID: 1}
	if operation.Expected.Tags["zone"] != "a" || operation.Published.Refs[0].ID != 7 {
		t.Fatalf("clone mutated source: %#v", operation)
	}
	operation.Targets[0].State = RecoveryTargetCreated
	if validRecoveryOperation(operation) {
		t.Fatal("published target in completed operation must stay published")
	}
}
