package legacywire

import (
	"encoding/binary"
	"testing"
)

func TestDecodeClientGameplayMessageUsesCanonicalMessageIDs(t *testing.T) {
	if ClientGameplayOutCard != 0x7701 || ClientGameplayCardAction != 0x7702 || ClientGameplayUserStateChange != 0x720a {
		t.Fatalf("client gameplay MessageIDs = %#x/%#x/%#x, want 0x7701/0x7702/0x720a", ClientGameplayOutCard, ClientGameplayCardAction, ClientGameplayUserStateChange)
	}

	tests := []struct {
		name string
		id   ClientGameplayMessage
	}{
		{name: "out card", id: ClientGameplayOutCard},
		{name: "card action", id: ClientGameplayCardAction},
		{name: "user state change", id: ClientGameplayUserStateChange},
		{name: "unknown remains classifiable", id: 0x7777},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := make([]byte, headerSize)
			binary.LittleEndian.PutUint32(data[12:16], uint32(test.id))
			binary.LittleEndian.PutUint32(data[20:24], uint32(len(data)))
			got, err := DecodeClientGameplayMessage(data)
			if err != nil {
				t.Fatalf("decode client gameplay MessageID: %v", err)
			}
			if got != test.id {
				t.Fatalf("decoded MessageID = %#x, want %#x", got, test.id)
			}
		})
	}
}

func TestDecodeClientGameplayMessageRejectsInvalidFrameBoundary(t *testing.T) {
	tests := [][]byte{
		make([]byte, headerSize-1),
		make([]byte, maxFrameSize+1),
		make([]byte, headerSize),
	}
	for index, data := range tests {
		if _, err := DecodeClientGameplayMessage(data); err == nil {
			t.Fatalf("invalid frame %d accepted", index)
		}
	}
}
