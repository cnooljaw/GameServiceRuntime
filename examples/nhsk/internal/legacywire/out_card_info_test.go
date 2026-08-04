package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestEncodeOutCardInfoMatchesReferenceGolden(t *testing.T) {
	want, err := hex.DecodeString("000000000000000000000000047600000000000037000000e9030000031300000000000000000000000000000000000000000000000002")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	got, err := EncodeOutCardInfo(OutCardInfo{UserID: 1001, Cards: []byte{0x03, 0x13}})
	if err != nil {
		t.Fatalf("encode OUT_CARD_INFO: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OUT_CARD_INFO bytes = %x, want %x", got, want)
	}
}

func TestEncodeOutCardInfoRejectsMoreThanWireCapacity(t *testing.T) {
	if _, err := EncodeOutCardInfo(OutCardInfo{Cards: make([]byte, 27)}); err == nil {
		t.Fatal("EncodeOutCardInfo accepted 27 cards")
	}
}
