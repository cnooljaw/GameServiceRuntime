package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestEncodeGameSceneMatchesReferenceDoubleSuffixGolden(t *testing.T) {
	want, err := hex.DecodeString(
		"00000000000000000000000008760000000000001a0100002c0000002a0000000400000056000000c4000000" +
			"030000000100000003000000070000000515000000000000000000000000000000000000000000000201" +
			"e903000000000313000000000000000000000000000000000000000000000000022000000000000000010000000a000000" +
			"ea030000010000000000000000000000000000000000000000000000000000000200000000000000000000000014000100" +
			"eb03000002000000000000000000000000000000000000000000000000000000020000000000000000ffffffff1e000000" +
			"ec030000030000000000000000000000000000000000000000000000000000000223330000000000000200000028000200",
	)
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	got, err := EncodeGameScene(GameScene{
		State:               3,
		ActiveSeat:          1,
		PreviousPlayerSeat:  3,
		RemainingSeconds:    7,
		TrickScoreCards:     [24]byte{0x05, 0x15},
		TrickScoreCardCount: 2,
		FinishedPlayerCount: 1,
		Players: [4]GameScenePlayer{
			{UserID: 1001, HandCards: [26]byte{0x03, 0x13}, HandCount: 2, LastPlayedCards: [8]byte{0x20}, LastPlayCount: 1, CapturedPoints: 10},
			{UserID: 1002, State: 1, HandCount: 2, LastPlayCount: 0, CapturedPoints: 20, Rank: 1},
			{UserID: 1003, State: 2, HandCount: 2, LastPlayCount: -1, CapturedPoints: 30},
			{UserID: 1004, State: 3, HandCount: 2, LastPlayedCards: [8]byte{0x23, 0x33}, LastPlayCount: 2, CapturedPoints: 40, Rank: 2},
		},
	})
	if err != nil {
		t.Fatalf("encode GAME_SCENE: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GAME_SCENE bytes = %x, want %x", got, want)
	}
}
