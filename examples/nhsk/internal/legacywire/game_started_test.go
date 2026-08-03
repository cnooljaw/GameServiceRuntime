package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestEncodeGameStartedMatchesReferenceGolden(t *testing.T) {
	want, err := hex.DecodeString("0000000000000000000000005486000000000000730000002200d204000000000000014e48534b2e786d6c000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if got := EncodeGameStarted(1234, "NHSK.xml"); !reflect.DeepEqual(got, want) {
		t.Fatalf("GAME_STARTED bytes = %x, want %x", got, want)
	}
}

func TestEncodeGameStartedTruncatesReplayNameLikeReferenceCopy(t *testing.T) {
	name := "12345678901234567890123456789012345678901234567890123456789012345678901234567890extra"
	got := EncodeGameStarted(1, name)
	if len(got) != 115 {
		t.Fatalf("GAME_STARTED length = %d, want 115", len(got))
	}
	if string(got[35:115]) != name[:80] {
		t.Fatalf("replay name bytes = %q, want first 80 bytes", got[35:115])
	}
}
