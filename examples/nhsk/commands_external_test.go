package nhsk_test

import (
	"testing"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk"
	"github.com/lijiawang/GameServiceRuntime/game"
)

func TestGameplayCommandAPIIsImportableByClusterCallers(t *testing.T) {
	request := nhsk.PlayCardsRequest{
		Player:     game.PlayerID("1001"),
		Cards:      []byte{0x03},
		VerifyCode: 5,
	}
	if nhsk.PlayCardsCommand == 0 || request.Player != "1001" || len(request.Cards) != 1 {
		t.Fatalf("imported gameplay API = command %#x request %#v", nhsk.PlayCardsCommand, request)
	}
}
