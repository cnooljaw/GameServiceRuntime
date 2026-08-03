package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestEncodeAskOutCardMatchesReferenceGolden(t *testing.T) {
	want, err := hex.DecodeString("000000000000000000000000037600000000000024000000e90300000300000028230000")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	got := EncodeAskOutCard(AskOutCard{
		UserID:             1001,
		VerifyCode:         3,
		ActionMilliseconds: 9000,
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ASK_OUT_CARD bytes = %x, want %x", got, want)
	}
}
