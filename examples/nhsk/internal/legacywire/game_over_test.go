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

func TestEncodeGameOverIncludesFourSeatIndexedPlayerRecords(t *testing.T) {
	frame, err := EncodeGameOver(GameOver{BattleID: 9, ReplayName: "NHSK.xml", Players: []GameOverPlayer{{Score: 1, Exp: 11}, {Score: -1, Exp: 12, Automated: true}, {Score: 2, Exp: 13}, {Score: -2, Exp: 14}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != gameOverFixedSize+36 || binary.LittleEndian.Uint32(frame[119:123]) != 4 || binary.LittleEndian.Uint32(frame[123:127]) != gameOverFixedSize || binary.LittleEndian.Uint32(frame[127:131]) != 36 {
		t.Fatalf("GAME_OVER fixed/suffix = %x", frame[119:139])
	}
	second := gameOverFixedSize + 9
	if int32(binary.LittleEndian.Uint32(frame[second:second+4])) != -1 || binary.LittleEndian.Uint32(frame[second+4:second+8]) != 12 || frame[second+8] != 1 {
		t.Fatalf("GAME_OVER player1 = %x", frame[second:second+9])
	}
}
