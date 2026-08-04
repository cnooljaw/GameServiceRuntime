package legacywire

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func TestDecodeOutCardRequestMatchesReferenceGolden(t *testing.T) {
	data, err := hex.DecodeString("00000000000000000000000001770000000000003700000003130000000000000000000000000000000000000000000000000205000000")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	want := OutCardRequest{Cards: [26]byte{0x03, 0x13}, CardCount: 2, VerifyCode: 5}

	got, err := DecodeOutCardRequest(data)
	if err != nil {
		t.Fatalf("decode OUT_CARD: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded OUT_CARD = %#v, want %#v", got, want)
	}

	data[24] = 0xff
	if got.Cards[0] == 0xff {
		t.Fatal("decoded OUT_CARD retained caller storage")
	}
}

func TestDecodeOutCardRequestPreservesWireValidBusinessRejections(t *testing.T) {
	tests := []struct {
		name       string
		cardCount  uint8
		verifyCode uint32
	}{
		{name: "pass with zero verify code", cardCount: 0, verifyCode: 0},
		{name: "nine cards reach gameplay count validation", cardCount: 9, verifyCode: 7},
		{name: "wire capacity reaches gameplay count validation", cardCount: 26, verifyCode: 9},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := validOutCardRequestFrame(test.cardCount, test.verifyCode)
			got, err := DecodeOutCardRequest(data)
			if err != nil {
				t.Fatalf("decode OUT_CARD: %v", err)
			}
			if got.CardCount != test.cardCount || got.VerifyCode != test.verifyCode {
				t.Fatalf("decoded OUT_CARD count/verify = %d/%d, want %d/%d", got.CardCount, got.VerifyCode, test.cardCount, test.verifyCode)
			}
		})
	}
}

func TestDecodeOutCardRequestRejectsMalformedPayload(t *testing.T) {
	valid := validOutCardRequestFrame(1, 5)
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "short", mutate: func(data []byte) []byte { return data[:54] }},
		{name: "trailing byte", mutate: func(data []byte) []byte { return append(data, 0) }},
		{name: "wrong type", mutate: mutateUint32(12, messageNHSKCardAction)},
		{name: "zero length", mutate: mutateUint32(20, 0)},
		{name: "wrong length", mutate: mutateUint32(20, 54)},
		{name: "card count exceeds capacity", mutate: func(data []byte) []byte { data[50] = 27; return data }},
		{name: "nonzero card after count", mutate: func(data []byte) []byte { data[25] = 0x03; return data }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := test.mutate(append([]byte(nil), valid...))
			if _, err := DecodeOutCardRequest(data); !errors.Is(err, errInvalidOutCardRequest) {
				t.Fatalf("decode malformed OUT_CARD error = %v, want errInvalidOutCardRequest", err)
			}
		})
	}
}

func validOutCardRequestFrame(cardCount uint8, verifyCode uint32) []byte {
	data := make([]byte, 55)
	binary.LittleEndian.PutUint32(data[12:16], messageNHSKOutCard)
	binary.LittleEndian.PutUint32(data[20:24], uint32(len(data)))
	for index := range int(cardCount) {
		data[24+index] = byte(index + 1)
	}
	data[50] = cardCount
	binary.LittleEndian.PutUint32(data[51:55], verifyCode)
	return data
}
