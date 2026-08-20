package legacywire

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func TestDecodeUserStateChangeMatchesReferenceGolden(t *testing.T) {
	data, err := hex.DecodeString("0000000000000000000000000a7200000000000020000000e903000001000000")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	want := UserStateChangeRequest{UserID: 1001, State: 1}

	got, err := DecodeUserStateChange(data)
	if err != nil {
		t.Fatalf("decode USER_STATE_CHANGE: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded USER_STATE_CHANGE = %#v, want %#v", got, want)
	}
}

func TestDecodeUserStateChangePreservesAllStateBits(t *testing.T) {
	data := validUserStateChangeFrame(1001, 0x80000005)
	got, err := DecodeUserStateChange(data)
	if err != nil {
		t.Fatalf("decode USER_STATE_CHANGE: %v", err)
	}
	if got.State != 0x80000005 {
		t.Fatalf("decoded state = %#x, want 0x80000005", got.State)
	}
}

func TestEncodeUserStateChangeUsesAckMessageID(t *testing.T) {
	data := EncodeUserStateChange(1001, true)
	if len(data) != userStateChangeFrameSize || binary.LittleEndian.Uint32(data[12:16]) != 0x8000720a || binary.LittleEndian.Uint32(data[20:24]) != userStateChangeFrameSize || binary.LittleEndian.Uint32(data[24:28]) != 1001 || binary.LittleEndian.Uint32(data[28:32]) != 1 {
		t.Fatalf("encoded USER_STATE_CHANGE = %x", data)
	}
}

func TestDecodeUserStateChangeRejectsMalformedPayload(t *testing.T) {
	valid := validUserStateChangeFrame(1001, 1)
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "short", mutate: func(data []byte) []byte { return data[:31] }},
		{name: "trailing byte", mutate: func(data []byte) []byte { return append(data, 0) }},
		{name: "wrong type", mutate: mutateUint32(12, messageNHSKOutCard)},
		{name: "zero length", mutate: mutateUint32(20, 0)},
		{name: "wrong length", mutate: mutateUint32(20, 31)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := test.mutate(append([]byte(nil), valid...))
			if _, err := DecodeUserStateChange(data); !errors.Is(err, errInvalidUserStateChange) {
				t.Fatalf("decode malformed USER_STATE_CHANGE error = %v, want errInvalidUserStateChange", err)
			}
		})
	}
}

func validUserStateChangeFrame(userID, state uint32) []byte {
	data := make([]byte, 32)
	binary.LittleEndian.PutUint32(data[12:16], messageGameUserStateChange)
	binary.LittleEndian.PutUint32(data[20:24], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[24:28], userID)
	binary.LittleEndian.PutUint32(data[28:32], state)
	return data
}
