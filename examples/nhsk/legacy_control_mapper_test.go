package nhsk

import (
	"encoding/binary"
	"testing"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
	"github.com/lijiawang/GameServiceRuntime/game"
)

func TestMapLegacyControlUsesHostThenBattleCommands(t *testing.T) {
	newGame, err := legacywire.DecodeControl(controlTestFrame(0x86c1, 44, func(data []byte) {
		putControl32(data, 24, 12)
		putControl32(data, 28, 7)
		putControl32(data, 32, 0)
		putControl32(data, 40, NHSKDescriptor.GameID)
	}))
	if err != nil {
		t.Fatal(err)
	}
	route, err := MapLegacyControl(newGame, 4)
	if err != nil {
		t.Fatal(err)
	}
	if route.Target != LegacyControlTargetHost || route.Command.ID != BeginCreateBattleCommand {
		t.Fatalf("new game route = %#v", route)
	}
	request, ok := route.Command.Payload.(CreateBattleRequest)
	if !ok || request.BattleID != game.BattleID(12) || request.ConnectionGeneration != 4 {
		t.Fatalf("new game payload = %#v", route.Command.Payload)
	}

	init, err := legacywire.DecodeControl(controlTestFrame(0x8600, 144, func(data []byte) {
		putControlGLHeader(data, 12, 0)
		putControl32(data, 34, 7)
		putControl32(data, 46, 88)
		putControl32(data, 56, 9)
		putControl32(data, 116, 4)
		putControl32(data, 120, 8)
		putControl32(data, 64, NHSKDescriptor.GameID)
		putControlSuffix(data, 136, 144, nil)
	}))
	if err != nil {
		t.Fatal(err)
	}
	route, err = MapLegacyControl(init, 4)
	if err != nil {
		t.Fatal(err)
	}
	if route.Target != LegacyControlTargetBattle || route.Command.ID != InitializeBattleCommand {
		t.Fatalf("init route = %#v", route)
	}

	players := legacywire.LegacyControl{Kind: legacywire.ControlUpdatePlayers, BattleID: 12, Players: []legacywire.LegacyPlayer{{UserID: 101, SeatID: 0, Nickname: "a"}}}
	route, err = MapLegacyControl(players, 4)
	if err != nil {
		t.Fatal(err)
	}
	if route.Command.ID != UpdatePlayersCommand {
		t.Fatalf("players command = %#v", route.Command)
	}
	playerRequest, ok := route.Command.Payload.(UpdatePlayersRequest)
	if !ok || len(playerRequest.Players) != 1 || playerRequest.Players[0].Player != game.PlayerID("101") {
		t.Fatalf("players payload = %#v", route.Command.Payload)
	}
}

func TestMapLegacyControlRoundCommandKeepsOnlySupportedTransitions(t *testing.T) {
	for _, test := range []struct {
		value int32
		want  bool
		id    any
	}{
		{value: 0, want: true, id: StartSubgameCommand},
		{value: 4, want: true, id: ForceFinishSubgameCommand},
		{value: 2, want: false},
	} {
		route, err := MapLegacyControl(legacywire.LegacyControl{Kind: legacywire.ControlCommand, BattleID: 1, Command: test.value}, 1)
		if test.want {
			if err != nil || route.Command.ID != test.id {
				t.Fatalf("command %d = %#v, %v", test.value, route, err)
			}
		} else if err == nil {
			t.Fatalf("command %d unexpectedly accepted", test.value)
		}
	}
}

func controlTestFrame(message uint32, length int, fill func([]byte)) []byte {
	data := make([]byte, length)
	putControl32(data, 12, message)
	putControl32(data, 20, uint32(length))
	if fill != nil {
		fill(data)
	}
	return data
}

func putControlGLHeader(data []byte, battleID, userID uint32) {
	binary.LittleEndian.PutUint16(data[24:26], 34)
	putControl32(data, 26, battleID)
	putControl32(data, 30, userID)
}

func putControlSuffix(data []byte, index, fixed int, value []byte) {
	putControl32(data, index, uint32(fixed))
	putControl32(data, index+4, uint32(len(value)))
	copy(data[fixed:], value)
}

func putControl32(data []byte, index int, value uint32) {
	binary.LittleEndian.PutUint32(data[index:index+4], value)
}
