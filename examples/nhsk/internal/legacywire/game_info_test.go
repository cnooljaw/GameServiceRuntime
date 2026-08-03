package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestEncodeGameInfoMatchesReferenceGolden(t *testing.T) {
	want, err := hex.DecodeString("0000000000000000000000000176000000000000320000000a00000002000000f6ffffff14000000e2ffffff280000000700")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	got := EncodeGameInfo(GameInfo{
		OutCardSeconds: 10,
		ServiceFee:     2,
		Scores:         [4]int32{-10, 20, -30, 40},
		GameNum:        7,
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GAME_INFO bytes = %x, want %x", got, want)
	}
}
