package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestEncodeGameStartMatchesReferenceGolden(t *testing.T) {
	want, err := hex.DecodeString("000000000000000000000000057200000000000018000000")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if got := EncodeGameStart(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GAME_START bytes = %x, want %x", got, want)
	}
}
