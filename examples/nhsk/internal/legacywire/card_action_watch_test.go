package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestEncodeCardActionWatchMatchesReferenceGoldenAndKeepsLooseSelection(t *testing.T) {
	want, err := hex.DecodeString("000000000000000000000000117600000000000037000000e9030000030325000000000000000000000000000000000000000000000003")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	got, err := EncodeCardActionWatch(CardActionWatch{UserID: 1001, Cards: [26]byte{0x03, 0x03, 0x25}, CardCount: 3})
	if err != nil {
		t.Fatalf("encode CARD_ACTION_WATCH: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CARD_ACTION_WATCH bytes = %x, want %x", got, want)
	}
}

func TestEncodeCardActionWatchAllowsEmptySelection(t *testing.T) {
	got, err := EncodeCardActionWatch(CardActionWatch{UserID: 1001})
	if err != nil {
		t.Fatalf("encode empty CARD_ACTION_WATCH: %v", err)
	}
	if len(got) != 55 || got[54] != 0 {
		t.Fatalf("empty CARD_ACTION_WATCH length/count = %d/%d, want 55/0", len(got), got[54])
	}
}
