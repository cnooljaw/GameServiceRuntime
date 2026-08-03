package game

import (
	"errors"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestBusinessIdentifiersAndSettlementRequestsAreCanonical(t *testing.T) {
	for _, value := range []string{"", " player", "player ", string([]byte{0xff}), string(make([]byte, 129))} {
		if err := validateID(PlayerID(value)); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("validateID(%q) error = %v, want ErrInvalidID", value, err)
		}
	}
	if err := validateRequestID("settlement-42"); err != nil {
		t.Fatal(err)
	}
	request := SettlementRequest{
		RequestID: "settlement-42",
		Source:    gsr.ServiceRef{Node: "node-a", ID: 9},
		Currency:  "coin",
		Entries: []SettlementEntry{
			{Player: "alice", Delta: 3},
			{Player: "bob", Delta: -3},
		},
	}
	if err := validateSettlementRequest(request); err != nil {
		t.Fatal(err)
	}
	request.Entries[0].Player = "bob"
	if err := validateSettlementRequest(request); !errors.Is(err, ErrInvalidSettlement) {
		t.Fatalf("validateSettlementRequest(duplicate) error = %v, want ErrInvalidSettlement", err)
	}

	original := SettlementRequest{
		RequestID: "settlement-43", Source: gsr.ServiceRef{Node: "node-a", ID: 9}, Currency: "coin",
		Entries: []SettlementEntry{{Player: "alice", Delta: 3}, {Player: "bob", Delta: -3}},
	}
	clone := cloneSettlementRequest(original)
	clone.Entries[0].Delta = 99
	if original.Entries[0].Delta != 3 {
		t.Fatal("cloneSettlementRequest returned shared Entries storage")
	}
}

func TestBattleConfigRejectsInvalidParticipantsAndLogic(t *testing.T) {
	valid := BattleConfig{
		ID:           42,
		Participants: []Participant{{Player: "alice"}, {Player: "bob"}},
		Logic:        &testBattleLogic{},
	}
	if err := validateBattleConfig(valid); err != nil {
		t.Fatal(err)
	}
	valid.Participants[1].Player = "alice"
	if err := validateBattleConfig(valid); !errors.Is(err, ErrInvalidParticipant) {
		t.Fatalf("validateBattleConfig(duplicate) error = %v, want ErrInvalidParticipant", err)
	}
}

type testBattleLogic struct{}

func (*testBattleLogic) HandleBattle(BattleContext, gsr.Command) error { return nil }
func (*testBattleLogic) Snapshot(BattleContext) ([]byte, error)        { return []byte{}, nil }
