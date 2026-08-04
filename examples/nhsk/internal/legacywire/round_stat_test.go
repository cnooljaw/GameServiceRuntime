package legacywire

import (
	"encoding/binary"
	"testing"
)

func TestEncodeRoundStatUsesEmptyReferenceProjection(t *testing.T) {
	frame := EncodeRoundStat()
	if len(frame) != 36 || binary.LittleEndian.Uint32(frame[12:16]) != 0x7246 || binary.LittleEndian.Uint32(frame[20:24]) != 36 {
		t.Fatalf("ROUND_STAT header = %x", frame)
	}
	if binary.LittleEndian.Uint32(frame[24:28]) != 0 || binary.LittleEndian.Uint32(frame[28:32]) != 36 || binary.LittleEndian.Uint32(frame[32:36]) != 0 {
		t.Fatalf("ROUND_STAT empty projection = %x", frame)
	}
}
