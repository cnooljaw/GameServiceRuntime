package nhsk

import (
	"encoding/binary"
	"testing"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestEncodeLegacyRoundStatOutputRelaysToEachTargetList(t *testing.T) {
	batch := GameOutputBatch{
		BattleID:             9,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 1},
		ConnectionGeneration: 1,
		Outputs: []GameOutput{ClientGameOutput{
			Targets: []game.PlayerID{"77", "88"},
			Kind:    OutputRoundStat,
			Payload: RoundStatPayload{},
		}},
	}
	frames, err := encodeLegacyGameOutputBatch(batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || binary.LittleEndian.Uint32(frames[0][12:16]) != 0x8644 || binary.LittleEndian.Uint32(frames[1][12:16]) != 0x8644 {
		t.Fatalf("ROUND_STAT relay count/types = %d/%x/%x", len(frames), frames[0], frames[1])
	}
	if binary.LittleEndian.Uint32(frames[0][102:106]) != 0x7246 || binary.LittleEndian.Uint32(frames[1][102:106]) != 0x7246 {
		t.Fatalf("ROUND_STAT payload type = %x/%x", frames[0][102:106], frames[1][102:106])
	}
}
