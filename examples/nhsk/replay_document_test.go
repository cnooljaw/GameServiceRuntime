package nhsk

import (
	mathrand "math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestReplayDocumentOwnsAnImmutableStartSnapshot(t *testing.T) {
	startedAt := time.Unix(123, 456)
	input := ReplayStartSnapshot{
		BattleID:   31,
		Identity:   BattleIdentity{BattleID: 31, ProductID: NHSKDescriptor.GameID, MatchID: 9, RoundID: 4, RoundUniCode: "round"},
		GameNum:    2,
		SubgameNum: 3,
		StartedAt:  startedAt,
		ReplayName: "nhsk-31-2-3.xml",
		RoundContext: UpdateRoundContextRequest{
			SecRoundTotal: 120,
			SecRoundUsed:  18,
			RoomInfo:      "room",
		},
		BankerSeat: 2,
	}
	input.Players[0] = ReplayPlayerSnapshot{SeatID: 0, Player: "p0", UserID: 100, Nickname: "Alice", InitScore: 12, Platform: 7, Dress: "dress", Automated: true}
	input.Hands[0] = []byte{0x01, 0x12}
	for seat := 1; seat < 4; seat++ {
		input.Players[seat] = ReplayPlayerSnapshot{SeatID: uint8(seat), Player: game.PlayerID("p" + string(rune('0'+seat))), UserID: uint32(100 + seat)}
		input.Hands[seat] = []byte{byte(seat)}
	}
	document := NewReplayDocument(input)

	input.Players[0].Nickname = "changed"
	input.Hands[0][0] = 0xff
	input.Hands[1] = append(input.Hands[1], 0xee)
	got := document.StartSnapshot()
	if got.Players[0].Nickname != "Alice" || got.Hands[0][0] != 0x01 || len(got.Hands[1]) != 1 {
		t.Fatalf("document did not own start input: %#v", got)
	}

	got.Hands[0][0] = 0xaa
	got.Players[0].Dress = "changed"
	gotAgain := document.StartSnapshot()
	if gotAgain.Hands[0][0] != 0x01 || gotAgain.Players[0].Dress != "dress" {
		t.Fatalf("StartSnapshot exposed mutable document state: %#v", gotAgain)
	}
	if !reflect.DeepEqual(gotAgain.RoundContext, input.RoundContext) {
		t.Fatalf("round context changed through clone: got=%#v want=%#v", gotAgain.RoundContext, input.RoundContext)
	}
}

func TestReplayStartSnapshotValidatesFourSeatsAndHands(t *testing.T) {
	var snapshot ReplayStartSnapshot
	if snapshot.Valid() {
		t.Fatal("zero replay start snapshot was valid")
	}
	for seat := range snapshot.Players {
		snapshot.Players[seat] = ReplayPlayerSnapshot{SeatID: uint8(seat), Player: game.PlayerID(string(rune('1' + seat))), UserID: uint32(seat + 1)}
		snapshot.Hands[seat] = make([]byte, 26)
	}
	snapshot.BattleID = 32
	snapshot.Identity = BattleIdentity{BattleID: 32, ProductID: NHSKDescriptor.GameID, MatchID: 1}
	snapshot.GameNum = 1
	snapshot.SubgameNum = 1
	snapshot.StartedAt = time.Unix(1, 0)
	snapshot.ReplayName = "replay.xml"
	if !snapshot.Valid() {
		t.Fatal("complete replay start snapshot was invalid")
	}
	snapshot.Hands[2] = make([]byte, 25)
	if snapshot.Valid() {
		t.Fatal("short hand was valid")
	}
}

func TestBattleFreezesReplayDocumentAtSubgameStart(t *testing.T) {
	clock := &nhskTestClock{now: time.Unix(200, 123)}
	service, err := NewBattleService(NHSKBattleConfig{ID: 33, Random: mathrand.New(mathrand.NewSource(3)), Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Init(&battleTestServiceContext{}); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	commands := []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: 33, ProductID: NHSKDescriptor.GameID, MatchID: 2, RoundID: 4, RoundUniCode: "round"}}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: []BattlePlayer{
			{Player: "1", UserID: 1, SeatID: 0, Score: 10, Nickname: "A", ClientID: 11},
			{Player: "2", UserID: 2, SeatID: 1, Score: 20, Nickname: "B", ClientID: 12, Automated: true},
			{Player: "3", UserID: 3, SeatID: 2, Score: 30, Nickname: "C", ClientID: 13},
			{Player: "4", UserID: 4, SeatID: 3, Score: 40, Nickname: "D", ClientID: 14},
		}}},
		{ID: UpdatePlayerDressCommand, Payload: UpdatePlayerDressRequest{Player: "1", Dress: "d1"}},
		{ID: UpdatePlayerDressCommand, Payload: UpdatePlayerDressRequest{Player: "2", Dress: "d2"}},
		{ID: UpdatePlayerDressCommand, Payload: UpdatePlayerDressRequest{Player: "3", Dress: "d3"}},
		{ID: UpdatePlayerDressCommand, Payload: UpdatePlayerDressRequest{Player: "4", Dress: "d4"}},
		{ID: UpdateRoundContextCommand, Payload: UpdateRoundContextRequest{SecRoundTotal: 300, SecRoundUsed: 20, RoomInfo: "before"}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
		{ID: StartSubgameCommand, Payload: struct{}{}},
	}
	for _, command := range commands {
		if err := service.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	start := service.replayDocument.StartSnapshot()
	if !start.Valid() || !start.StartedAt.Equal(clock.now) || int(start.BankerSeat) != service.activeSeat {
		t.Fatalf("replay start snapshot = %#v", start)
	}
	if start.RoundContext.RoomInfo != "before" || start.Players[0].Nickname != "A" || start.Players[0].InitScore != 10 || start.Players[0].Platform != 11 || start.Players[1].Automated != true || start.Players[3].Dress != "d4" {
		t.Fatalf("replay metadata = %#v", start)
	}
	if !reflect.DeepEqual(start.Hands[0], service.hands[service.bySeat[0]]) {
		t.Fatalf("replay hand differs from dealt hand")
	}

	service.players["1"] = func() BattlePlayer {
		player := service.players["1"]
		player.Nickname = "after"
		player.Dress = "after-dress"
		return player
	}()
	service.hands["1"][0] ^= 0xff
	service.currentRound.RoomInfo = "after"
	unchanged := service.replayDocument.StartSnapshot()
	if unchanged.Players[0].Nickname != "A" || unchanged.Players[0].Dress != "d1" || unchanged.RoundContext.RoomInfo != "before" || reflect.DeepEqual(unchanged.Hands[0], service.hands["1"]) {
		t.Fatalf("replay start snapshot changed after Battle mutation: %#v", unchanged)
	}
}
