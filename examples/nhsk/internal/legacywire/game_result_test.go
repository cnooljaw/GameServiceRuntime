package legacywire

import (
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestEncodeGameResultMatchesReferenceSuffixGolden(t *testing.T) {
	want, err := hex.DecodeString(
		"00000000000000000000000007760000000000009a000000200000007a000000" +
			"00000000e9030000ea030000eb030000ec030000" +
			"00010001" +
			"c800000038ffffffc800000038ffffff" +
			"00010001" +
			"7800500050007800" +
			"01030204" +
			"0100" +
			"726f756e642d667570616e2d31323300000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
	)
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	got, err := EncodeGameResult(GameResult{
		Reason:         0,
		UserIDs:        [4]uint32{1001, 1002, 1003, 1004},
		Automated:      [4]bool{false, true, false, true},
		Scores:         [4]int32{200, -200, 200, -200},
		Outcomes:       [4]uint8{0, 1, 0, 1},
		CapturedPoints: [4]uint16{120, 80, 80, 120},
		Ranks:          [4]uint8{1, 3, 2, 4},
		Result:         1,
		WinningTeam:    0,
		ReplayUID:      "round-fupan-123",
	})
	if err != nil {
		t.Fatalf("encode GAME_RESULT: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GAME_RESULT bytes = %x, want %x", got, want)
	}
}

func TestEncodeGameResultTruncatesReplayUIDLikeReferenceCopy(t *testing.T) {
	got, err := EncodeGameResult(GameResult{UserIDs: [4]uint32{1, 2, 3, 4}, Ranks: [4]uint8{1, 2, 3, 4}, Result: 2, ReplayUID: strings.Repeat("x", 65)})
	if err != nil {
		t.Fatalf("encode long ReplayUID: %v", err)
	}
	if len(got) != 154 {
		t.Fatalf("GAME_RESULT length = %d, want 154", len(got))
	}
	if string(got[len(got)-gameResultReplaySize:]) != strings.Repeat("x", gameResultReplaySize) {
		t.Fatalf("ReplayUID bytes = %q, want 64-byte truncation", got[len(got)-gameResultReplaySize:])
	}
}
