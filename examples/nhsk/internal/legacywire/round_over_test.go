package legacywire

import (
	"encoding/binary"
	"testing"
)

func TestEncodeNoticeRoundOver(t *testing.T) {
	frame, err := EncodeNoticeRoundOver(NoticeRoundOver{BattleID: 123, YueJuEndReason: 4, YueJuEndPlayer: 77})
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != 42 || binary.LittleEndian.Uint32(frame[12:16]) != 0x864e || binary.LittleEndian.Uint32(frame[20:24]) != 42 || binary.LittleEndian.Uint16(frame[24:26]) != 34 || binary.LittleEndian.Uint32(frame[26:30]) != 123 || binary.LittleEndian.Uint32(frame[34:38]) != 4 || binary.LittleEndian.Uint32(frame[38:42]) != 77 {
		t.Fatalf("NOTICE_ROUND_OVER = %x", frame)
	}
}
