package legacywire

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func TestDecodeCardActionRequestMatchesReferenceGoldenAndCopiesCards(t *testing.T) {
	data, err := hex.DecodeString("000000000000000000000000027700000000000033000000030325000000000000000000000000000000000000000000000003")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	want := CardActionRequest{Cards: [26]byte{0x03, 0x03, 0x25}, CardCount: 3}

	got, err := DecodeCardActionRequest(data)
	if err != nil {
		t.Fatalf("decode CARD_ACTION: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded CARD_ACTION = %#v, want %#v", got, want)
	}

	data[24] = 0xff
	if got.Cards[0] == 0xff {
		t.Fatal("decoded CARD_ACTION retained caller storage")
	}
}

func TestDecodeCardActionRequestAllowsEmptySelection(t *testing.T) {
	data := make([]byte, 51)
	binary.LittleEndian.PutUint32(data[12:16], messageNHSKCardAction)
	binary.LittleEndian.PutUint32(data[20:24], uint32(len(data)))

	got, err := DecodeCardActionRequest(data)
	if err != nil {
		t.Fatalf("decode empty CARD_ACTION: %v", err)
	}
	if got.CardCount != 0 || got.Cards != [26]byte{} {
		t.Fatalf("decoded empty CARD_ACTION = %#v", got)
	}
}

func TestDecodeCardActionRequestRejectsMalformedPayload(t *testing.T) {
	valid := make([]byte, 51)
	binary.LittleEndian.PutUint32(valid[12:16], messageNHSKCardAction)
	binary.LittleEndian.PutUint32(valid[20:24], uint32(len(valid)))

	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "short", mutate: func(data []byte) []byte { return data[:50] }},
		{name: "trailing byte", mutate: func(data []byte) []byte { return append(data, 0) }},
		{name: "wrong type", mutate: mutateUint32(12, messageNHSKOutCard)},
		{name: "zero length", mutate: mutateUint32(20, 0)},
		{name: "wrong length", mutate: mutateUint32(20, 50)},
		{name: "card count exceeds capacity", mutate: func(data []byte) []byte { data[50] = 27; return data }},
		{name: "nonzero card after count", mutate: func(data []byte) []byte { data[25] = 0x03; return data }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := test.mutate(append([]byte(nil), valid...))
			if _, err := DecodeCardActionRequest(data); !errors.Is(err, errInvalidCardActionRequest) {
				t.Fatalf("decode malformed CARD_ACTION error = %v, want errInvalidCardActionRequest", err)
			}
		})
	}
}
