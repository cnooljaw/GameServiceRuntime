package nhsk

import (
	"encoding/binary"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestEncodeLegacyGameOutputBatchIncludesRoundOverAfterGameOver(t *testing.T) {
	batch := GameOutputBatch{BattleID: 9, MatchID: 88, ProductID: 82, Ref: gsr.ServiceRef{Node: "nhsk", ID: 1}, ConnectionGeneration: 1, Outputs: []GameOutput{
		GameOverOutput{ReplayName: "nhsk.xml", IsGameOver: true},
		NoticeRoundOverOutput{EndReason: 4, EndPlayer: "77"},
	}}
	frames, err := encodeLegacyGameOutputBatch(batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || binary.LittleEndian.Uint32(frames[0][12:16]) != 0x8641 || binary.LittleEndian.Uint32(frames[1][12:16]) != 0x864e {
		t.Fatalf("round over frames = %x", frames)
	}
}
