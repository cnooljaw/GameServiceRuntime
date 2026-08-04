package legacywire

import (
	"encoding/binary"
	"testing"
)

func TestEncodeGameOverUsesEmptyPlayerSuffix(t *testing.T) {
	frame, err := EncodeGameOver(GameOver{BattleID: 9, Reason: 0, ReplayName: "nhsk.xml", IsGameOver: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != 139 || binary.LittleEndian.Uint32(frame[12:16]) != 0x8641 || binary.LittleEndian.Uint32(frame[20:24]) != 139 || binary.LittleEndian.Uint32(frame[123:127]) != 139 || binary.LittleEndian.Uint32(frame[127:131]) != 0 || frame[118] != 1 {
		t.Fatalf("GAME_OVER = %x", frame)
	}
}
