package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestEncodeOutCardResultMatchesReferenceGolden(t *testing.T) {
	want, err := hex.DecodeString("00000000000000000000000009760000000000001c00000003000000")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	got, err := EncodeOutCardResult(3)
	if err != nil {
		t.Fatalf("encode OUT_CARD_RESULT: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OUT_CARD_RESULT bytes = %x, want %x", got, want)
	}
}
