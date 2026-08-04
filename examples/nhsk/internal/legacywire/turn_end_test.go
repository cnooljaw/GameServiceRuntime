package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestEncodeTurnEndMatchesReferenceGolden(t *testing.T) {
	want, err := hex.DecodeString("000000000000000000000000057600000000000020000000e90300000a000000")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	got := EncodeTurnEnd(TurnEnd{WinnerUserID: 1001, CapturedPoints: 10})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TURN_END bytes = %x, want %x", got, want)
	}
}
