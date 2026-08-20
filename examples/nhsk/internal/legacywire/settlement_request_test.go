package legacywire

import (
	"encoding/binary"
	"testing"
)

func TestEncodeSettlementRequestMatchesLegacyFixedAndSuffixLayout(t *testing.T) {
	frame, err := EncodeSettlementRequest(SettlementRequest{
		BattleID: 9, ResultType: 1, TeamCount: 4, LevelScoreType: 1,
		Gains: []SettlementGain{{PayTeamID: 1, GainTeamID: 0, Score: 2}},
		Players: []SettlementPlayer{
			{PlayerID: 1001, TeamID: 0, Exp: 11},
			{PlayerID: 1002, TeamID: 1, Flag: 0x11, Exp: 12},
			{PlayerID: 1003, TeamID: 2, Exp: 13},
			{PlayerID: 1004, TeamID: 3, Flag: 1, Exp: 14},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != 71+12+80 || binary.LittleEndian.Uint32(frame[12:16]) != 0x8650 || binary.LittleEndian.Uint32(frame[20:24]) != uint32(len(frame)) || binary.LittleEndian.Uint16(frame[24:26]) != 34 || binary.LittleEndian.Uint32(frame[26:30]) != 9 {
		t.Fatalf("header/fixed = %x", frame[:34])
	}
	if binary.LittleEndian.Uint32(frame[39:43]) != 1 || binary.LittleEndian.Uint32(frame[43:47]) != 4 || binary.LittleEndian.Uint32(frame[51:55]) != 71 || binary.LittleEndian.Uint32(frame[55:59]) != 12 || binary.LittleEndian.Uint32(frame[59:63]) != 83 || binary.LittleEndian.Uint32(frame[63:67]) != 80 || frame[70] != 1 {
		t.Fatalf("settlement fixed = %x", frame[34:71])
	}
	if binary.LittleEndian.Uint32(frame[71:75]) != 1 || binary.LittleEndian.Uint32(frame[75:79]) != 0 || binary.LittleEndian.Uint32(frame[79:83]) != 2 {
		t.Fatalf("gain = %x", frame[71:83])
	}
	second := 83 + 20
	if binary.LittleEndian.Uint32(frame[second:second+4]) != 1002 || binary.LittleEndian.Uint32(frame[second+4:second+8]) != 0x11 || binary.LittleEndian.Uint32(frame[second+12:second+16]) != 12 || binary.LittleEndian.Uint32(frame[second+16:second+20]) != 1 {
		t.Fatalf("player1 = %x", frame[second:second+20])
	}
}
