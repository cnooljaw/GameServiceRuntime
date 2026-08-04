package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestEncodeShowCardsMatchesReferenceGoldenAndKeepsHiddenCounts(t *testing.T) {
	want, err := hex.DecodeString(
		"000000000000000000000000067600000000000094000000" +
			"e9030000ea030000eb030000ec030000" +
			"0000000000000000000000000000000000000000000000000000" +
			"0000000000000000000000000000000000000000000000000000" +
			"0515000000000000000000000000000000000000000000000000" +
			"0000000000000000000000000000000000000000000000000000" +
			"00010201",
	)
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	got, err := EncodeShowCards(ShowCards{
		UserIDs:    [4]uint32{1001, 1002, 1003, 1004},
		Cards:      [4][26]byte{2: {0x05, 0x15}},
		CardCounts: [4]uint8{0, 1, 2, 1},
	})
	if err != nil {
		t.Fatalf("encode SHOW_CARDS: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SHOW_CARDS bytes = %x, want %x", got, want)
	}
}
