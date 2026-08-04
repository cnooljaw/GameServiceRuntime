package legacywire

import (
	"encoding/binary"
	"testing"
)

func TestEncodeNewGameAck(t *testing.T) {
	frame := EncodeNewGameAck(12345, true)
	if len(frame) != 29 || binary.LittleEndian.Uint32(frame[12:16]) != 0x800086c0 || binary.LittleEndian.Uint32(frame[20:24]) != 29 || binary.LittleEndian.Uint32(frame[24:28]) != 12345 || frame[28] != 1 {
		t.Fatalf("ack = %x", frame)
	}
	frame = EncodeNewGameAck(12345, false)
	if frame[28] != 0 {
		t.Fatalf("rejected ack = %x", frame)
	}
}
