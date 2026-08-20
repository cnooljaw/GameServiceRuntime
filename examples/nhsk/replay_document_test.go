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
	moves := document.Moves()
	if len(moves) != 1 || moves[0].Kind != ReplayMoveDeal || !reflect.DeepEqual(moves[0].Hands[0], []byte{0x01, 0x12}) {
		t.Fatalf("initial deal move = %#v", moves)
	}
	moves[0].Hands[0][0] = 0xbb
	if document.Moves()[0].Hands[0][0] != 0x01 {
		t.Fatal("Moves exposed mutable document state")
	}
	clone := document.Clone()
	if len(clone.Moves()) != 1 || clone.Moves()[0].Kind != ReplayMoveDeal {
		t.Fatalf("document clone lost moves: %#v", clone.Moves())
	}
	cloneMoves := clone.Moves()
	cloneMoves[0].Hands[0][0] = 0xcc
	if document.Moves()[0].Hands[0][0] != 0x01 {
		t.Fatal("document clone shares move storage")
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
	snapshot.ReplayUID = "1"
	snapshot.RelativePath = "FuPan/19700101/08"
	if !snapshot.Valid() {
		t.Fatal("complete replay start snapshot was invalid")
	}
	snapshot.Hands[2] = make([]byte, 25)
	if snapshot.Valid() {
		t.Fatal("short hand was valid")
	}
}

func TestReplayCardTypeNamesFollowLegacyReplay(t *testing.T) {
	for _, test := range []struct {
		pattern cardPattern
		count   int
		want    string
	}{
		{pattern: cardPattern{}, count: 0, want: "不出"},
		{pattern: cardPattern{kind: cardPatternSingle}, count: 1, want: "单张"},
		{pattern: cardPattern{kind: cardPatternThreeTwo}, count: 5, want: "俘虏"},
		{pattern: cardPattern{kind: cardPatternBomb, count: 4}, count: 4, want: "炸弹4"},
		{pattern: cardPattern{kind: cardPatternBomb, count: 8}, count: 8, want: "炸弹8"},
	} {
		if got := replayCardTypeName(test.pattern, test.count); got != test.want {
			t.Fatalf("replay card type %#v/%d = %q, want %q", test.pattern, test.count, got, test.want)
		}
	}
}

func TestBattleFreezesReplayDocumentAtSubgameStart(t *testing.T) {
	clock := &nhskTestClock{now: time.Date(2026, 1, 2, 16, 5, 6, 123, time.UTC)}
	service, err := NewBattleService(NHSKBattleConfig{ID: 33, Random: mathrand.New(mathrand.NewSource(3)), Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Init(&battleTestServiceContext{}); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	commands := []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{
			Identity:   BattleIdentity{BattleID: 33, ProductID: NHSKDescriptor.GameID, MatchID: 2, RoundID: 4, RoundUniCode: "round"},
			MaxGameNum: 8, MaxSubgameNum: 16, Fee: 3, ScoreBase: 10, ScoreDenominator: 100,
			ReplayMetadata: ReplayMetadata{MatchName: "比赛", GameType: 1, ScoreType: 2, ScoreMode: 3, RoomID: 4, CreatorID: 99},
			ReplayRules:    ReplayRuleSnapshot{TimeOutOver: true, VoiceMode: true, RandomSeatRoundStart: true, GameNumToRandomSeat: 5},
		}},
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
	if start.ReplayName != "NHSK_M82R4_20260103_000506_1.xml" || start.ReplayUID != "176736990699" || start.RelativePath != "FuPan/20260103/00" {
		t.Fatalf("replay identity = name %q uid %q path %q", start.ReplayName, start.ReplayUID, start.RelativePath)
	}
	if start.MaxGameNum != 8 || start.MaxSubgameNum != 16 || start.Fee != 3 || start.ScoreBase != 10 || start.ScoreDenominator != 100 || start.ReplayMetadata.CreatorID != 99 || !start.ReplayRules.TimeOutOver {
		t.Fatalf("replay init projection = %#v", start)
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

func TestBattleInitializeOnlyTreatsExactNormalizedDuplicateAsIdempotent(t *testing.T) {
	service, err := NewBattleService(NHSKBattleConfig{ID: 36, Random: mathrand.New(mathrand.NewSource(1))})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	rules := DefaultNHSKConfig()
	request := InitializeBattleRequest{
		Identity:   BattleIdentity{BattleID: 36, ProductID: NHSKDescriptor.GameID, MatchID: 2, RoundID: 4},
		MaxGameNum: 8, MaxSubgameNum: 16, Fee: 3, ScoreBase: 10, ScoreDenominator: 100,
		ReplayMetadata: ReplayMetadata{CreatorID: 99}, ReplayRules: ReplayRuleSnapshot{VoiceMode: true}, Rules: &rules,
	}
	if err := service.Handle(ctx, gsr.Command{ID: InitializeBattleCommand, Payload: request}); err != nil || !ctx.reply.(CommandResult).Accepted {
		t.Fatalf("first init = %#v, %v", ctx.reply, err)
	}
	defaultService, _ := NewBattleService(NHSKBattleConfig{ID: 37, Random: mathrand.New(mathrand.NewSource(1))})
	defaultRequest := request
	defaultRequest.Identity.BattleID = 37
	defaultRequest.Rules = nil
	if err := defaultService.Handle(ctx, gsr.Command{ID: InitializeBattleCommand, Payload: defaultRequest}); err != nil || !ctx.reply.(CommandResult).Accepted {
		t.Fatalf("default init = %#v, %v", ctx.reply, err)
	}
	defaultRequest.Rules = &rules
	if err := defaultService.Handle(ctx, gsr.Command{ID: InitializeBattleCommand, Payload: defaultRequest}); err != nil || !ctx.reply.(CommandResult).Accepted {
		t.Fatalf("normalized default duplicate = %#v, %v", ctx.reply, err)
	}
	if err := service.Handle(ctx, gsr.Command{ID: InitializeBattleCommand, Payload: request}); err != nil || !ctx.reply.(CommandResult).Accepted {
		t.Fatalf("exact duplicate = %#v, %v", ctx.reply, err)
	}
	conflict := request
	conflict.ScoreBase++
	if err := service.Handle(ctx, gsr.Command{ID: InitializeBattleCommand, Payload: conflict}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(CommandResult); result.Accepted || result.Rejection == "" {
		t.Fatalf("conflicting init = %#v", result)
	}
}

func TestBattleRecordsReplayMovesInReferenceOrder(t *testing.T) {
	service, _ := newPlayingBattleForRestore(t, 34)
	initial := service.replayDocument.StartSnapshot()
	service.hands[service.bySeat[0]] = []byte{0x05, 0x06}
	service.hands[service.bySeat[1]] = []byte{0x07}
	service.hands[service.bySeat[2]] = []byte{0x08}
	service.hands[service.bySeat[3]] = []byte{0x09}
	service.activeSeat = 0
	service.verifyCode = 11
	service.resetTrick()
	ctx := &battleTestCommandContext{}
	play := func(cards []byte) {
		t.Helper()
		player := service.bySeat[service.activeSeat]
		if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: player, Cards: cards, VerifyCode: service.verifyCode}}); err != nil {
			t.Fatal(err)
		}
		if result, ok := ctx.reply.(ActionResult); !ok || !result.Accepted {
			t.Fatalf("play %s %x = %#v", player, cards, ctx.reply)
		}
	}
	play([]byte{0x05})
	play(nil)
	play(nil)
	play(nil)

	moves := service.replayDocument.Moves()
	if len(moves) != 8 {
		t.Fatalf("replay move count = %d, want 8: %#v", len(moves), moves)
	}
	wantKinds := []ReplayMoveKind{ReplayMoveDeal, ReplayMoveCurrentPoint, ReplayMoveOutCard, ReplayMoveOutCard, ReplayMoveOutCard, ReplayMoveOutCard, ReplayMoveCatchPoint, ReplayMoveTurnEnd}
	for index, want := range wantKinds {
		if moves[index].Kind != want {
			t.Fatalf("move %d kind = %q, want %q; moves=%#v", index, moves[index].Kind, want, moves)
		}
	}
	if moves[0].SeatID != 0 || !reflect.DeepEqual(moves[0].Hands, initial.Hands) {
		t.Fatalf("deal move = %#v, want frozen start hands", moves[0])
	}
	if moves[1].Point != 5 || !reflect.DeepEqual(moves[1].Cards, []byte{0x05}) || moves[1].Source != ReplayMoveSourceSystem {
		t.Fatalf("current point move = %#v", moves[1])
	}
	if moves[2].SeatID != 0 || moves[2].UserID != 1 || moves[2].CardType != "单张" || moves[2].Source != ReplayMoveSourcePlayer {
		t.Fatalf("out card move = %#v", moves[2])
	}
	if moves[3].SeatID != 1 || len(moves[3].Cards) != 0 || moves[3].CardType != "不出" {
		t.Fatalf("pass move = %#v", moves[3])
	}
	if moves[6].SeatID != 0 || moves[6].Point != 5 || !reflect.DeepEqual(moves[6].Cards, []byte{0x05}) || moves[6].Source != ReplayMoveSourceSystem {
		t.Fatalf("catch point move = %#v", moves[6])
	}
	if moves[7].Scores[0] != 5 || moves[7].Source != ReplayMoveSourceSystem {
		t.Fatalf("turn end move = %#v", moves[7])
	}
}

func TestBattleRecordsReplayMoveMillisecondsFromEachActionStart(t *testing.T) {
	clock := &nhskTestClock{now: time.Unix(100, 0)}
	service, _ := newBattleForTest(t, 35, mathrand.New(mathrand.NewSource(3)), clock)
	ctx := &battleTestCommandContext{}

	firstPlayer := service.bySeat[service.activeSeat]
	clock.now = clock.now.Add(400 * time.Millisecond)
	if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: firstPlayer, Cards: []byte{service.hands[firstPlayer][0]}, VerifyCode: service.verifyCode + 2}}); err != nil {
		t.Fatal(err)
	}
	if result, ok := ctx.reply.(ActionResult); !ok || result.Accepted {
		t.Fatalf("stale play reply = %#v, want rejection", ctx.reply)
	}

	clock.now = clock.now.Add(834 * time.Millisecond)
	if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: firstPlayer, Cards: []byte{service.hands[firstPlayer][0]}, VerifyCode: service.verifyCode}}); err != nil {
		t.Fatal(err)
	}
	if result, ok := ctx.reply.(ActionResult); !ok || !result.Accepted {
		t.Fatalf("first play reply = %#v, want accepted", ctx.reply)
	}
	if service.verifyCode != 5 {
		t.Fatalf("verify code after first play = %d, want 5", service.verifyCode)
	}

	secondPlayer := service.bySeat[service.activeSeat]
	clock.now = clock.now.Add(750 * time.Millisecond)
	if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: secondPlayer, VerifyCode: service.verifyCode}}); err != nil {
		t.Fatal(err)
	}
	if result, ok := ctx.reply.(ActionResult); !ok || !result.Accepted {
		t.Fatalf("second play reply = %#v, want accepted", ctx.reply)
	}
	if service.verifyCode != 7 {
		t.Fatalf("verify code after second play = %d, want 7", service.verifyCode)
	}

	outCards := replayOutCardMoves(service.replayDocument.Moves())
	if len(outCards) != 2 || outCards[0].MoveMilliseconds != 1234 || outCards[1].MoveMilliseconds != 750 {
		t.Fatalf("out card milliseconds = %#v, want [1234 750]", outCards)
	}
}

func TestEnablingAutoDoesNotResetReplayActionStart(t *testing.T) {
	clock := &nhskTestClock{now: time.Unix(200, 0)}
	service, _ := newBattleForTest(t, 36, mathrand.New(mathrand.NewSource(3)), clock)
	ctx := &battleTestCommandContext{}
	player := service.bySeat[service.activeSeat]

	clock.now = clock.now.Add(200 * time.Millisecond)
	if err := service.Handle(ctx, gsr.Command{ID: SetPlayerAutoStateCommand, Payload: SetPlayerAutoStateRequest{Player: player, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Second)
	if err := service.Handle(ctx, gsr.Command{ID: nhskBattleTimerCommand, Payload: service.turnRevision}); err != nil {
		t.Fatal(err)
	}

	outCards := replayOutCardMoves(service.replayDocument.Moves())
	if len(outCards) != 1 || outCards[0].Source != ReplayMoveSourceAuto || outCards[0].MoveMilliseconds != 1200 {
		t.Fatalf("auto out card = %#v, want auto source at 1200ms", outCards)
	}
}

func replayOutCardMoves(moves []ReplayMove) []ReplayMove {
	outCards := make([]ReplayMove, 0, len(moves))
	for _, move := range moves {
		if move.Kind == ReplayMoveOutCard {
			outCards = append(outCards, move)
		}
	}
	return outCards
}
