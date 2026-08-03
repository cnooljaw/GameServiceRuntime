package game

import (
	"errors"
	"reflect"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestBattleIDIsUint32AndZeroIsInvalid(t *testing.T) {
	if kind := reflect.TypeOf(BattleID(0)).Kind(); kind != reflect.Uint32 {
		t.Fatalf("BattleID kind = %v, want uint32", kind)
	}

	_, err := NewBattleService(BattleConfig{
		ID:           0,
		Participants: []Participant{{Player: "alice"}},
		Logic:        &testBattleLogic{},
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewBattleService(zero ID) error = %v, want ErrInvalidConfig", err)
	}
}

func TestBattleIDIndexesSortsAndSnapshotsNumerically(t *testing.T) {
	refs := map[BattleID]gsr.ServiceRef{
		10: {Node: "node", ID: 10},
		2:  {Node: "node", ID: 2},
	}
	if got := sortBattleIDs(refs); !reflect.DeepEqual(got, []BattleID{2, 10}) {
		t.Fatalf("sortBattleIDs = %v, want [2 10]", got)
	}

	battle, err := NewBattleService(BattleConfig{
		ID:           10000,
		Participants: []Participant{{Player: "alice"}},
		Logic:        &testBattleLogic{},
	})
	if err != nil {
		t.Fatalf("NewBattleService: %v", err)
	}
	if battle.id != 10000 {
		t.Fatalf("battle ID = %d, want 10000", battle.id)
	}
}
