package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestEncodeDealMatchesReferenceGoldenAndHidesOtherHands(t *testing.T) {
	want, err := hex.DecodeString("000000000000000000000000027600000000000090000000e9030000ea030000eb030000ec03000000000000000000000000000000000000000000000000000000000102030405060708090a0b0c0d0e0f101112131415161718191a00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	got, err := EncodeDeal(Deal{
		UserIDs: [4]uint32{1001, 1002, 1003, 1004},
		SeatID:  1,
		Cards:   referenceDealCards(),
	})
	if err != nil {
		t.Fatalf("encode DEAL: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DEAL bytes = %x, want %x", got, want)
	}
}

func TestEncodeDealRejectsInvalidSeat(t *testing.T) {
	if _, err := EncodeDeal(Deal{SeatID: 4}); err == nil {
		t.Fatal("EncodeDeal accepted seat 4")
	}
}

func referenceDealCards() [26]byte {
	return [26]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26}
}
