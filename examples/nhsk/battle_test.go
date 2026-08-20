package nhsk

import (
	"context"
	"errors"
	"log/slog"
	mathrand "math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestBattleLifecycleAndClusterCallUseOneMailbox(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk-battle-test", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	battle, err := NewBattleService(NHSKBattleConfig{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Name: "battle/7", Service: battle})
	if err != nil {
		t.Fatal(err)
	}
	identity := BattleIdentity{BattleID: 7, ProductID: NHSKDescriptor.GameID, MatchID: 99, RoundID: 1}
	if result, err := runtime.Call(context.Background(), ref, InitializeBattleCommand, InitializeBattleRequest{Identity: identity}); err != nil || !result.(CommandResult).Accepted {
		t.Fatalf("initialize = %#v, %v", result, err)
	}
	players := make([]BattlePlayer, 4)
	for seat := range players {
		players[seat] = BattlePlayer{Player: game.PlayerID("100" + string(rune('0'+seat+1))), UserID: uint32(seat + 1), SeatID: uint8(seat)}
	}
	if _, err := runtime.Call(context.Background(), ref, UpdatePlayersCommand, UpdatePlayersRequest{Players: players}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Call(context.Background(), ref, PrepareSubgameCommand, PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}); err != nil {
		t.Fatal(err)
	}
	if value, err := runtime.Call(context.Background(), ref, ProvideCustomDeckCommand, ProvideCustomDeckRequest{
		BattleID: 7, GameNum: 1, SubgameNum: 1,
		Catalog: CustomDeckCatalog{Decks: []CustomDeck{{Cards: sequentialCustomDeckBytes(), BankerSeat: 0}}},
	}); err != nil || !value.(CommandResult).Accepted {
		t.Fatalf("provide custom deck = %#v, %v", value, err)
	}
	if _, err := runtime.Call(context.Background(), ref, StartSubgameCommand, struct{}{}); err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Call(context.Background(), ref, GetNHSKBattleSnapshotCommand, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := value.(NHSKBattleSnapshot)
	if snapshot.Phase != "playing" || snapshot.ActivePlayer == "" || snapshot.VerifyCode == 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if value, err := runtime.Call(context.Background(), ref, PlayCardsCommand, PlayCardsRequest{Player: snapshot.ActivePlayer, Cards: []byte{snapshot.Hands[snapshot.ActivePlayer][0]}, VerifyCode: snapshot.VerifyCode}); err != nil || !value.(ActionResult).Accepted {
		t.Fatalf("play = %#v, %v", value, err)
	}
}

func TestBattleUsesInjectedRandomForRotatedShuffledDeal(t *testing.T) {
	rules := DefaultNHSKConfig()
	rules.SingleCountToSwap = 0
	first, _ := newBattleForTestWithRules(t, 25, mathrand.New(mathrand.NewSource(42)), &nhskTestClock{now: time.Unix(1, 0)}, &rules)
	second, _ := newBattleForTestWithRules(t, 26, mathrand.New(mathrand.NewSource(42)), &nhskTestClock{now: time.Unix(1, 0)}, &rules)

	wantBanker := mathrand.New(mathrand.NewSource(42)).Intn(4)
	if first.activeSeat != wantBanker {
		t.Fatalf("banker seat = %d, want deterministic seat %d", first.activeSeat, wantBanker)
	}
	if first.activeSeat != second.activeSeat {
		t.Fatalf("same seed banker seats = %d and %d", first.activeSeat, second.activeSeat)
	}
	expectedDeck := make([]byte, 0, 104)
	for copyIndex := 0; copyIndex < 2; copyIndex++ {
		for suit := 0; suit < 4; suit++ {
			for value := 1; value <= 13; value++ {
				expectedDeck = append(expectedDeck, byte(suit<<4|value))
			}
		}
	}
	expectedRandom := mathrand.New(mathrand.NewSource(42))
	expectedRandom.Intn(4)
	expectedRandom.Shuffle(len(expectedDeck), func(i, j int) { expectedDeck[i], expectedDeck[j] = expectedDeck[j], expectedDeck[i] })
	for seat, player := range first.bySeat {
		if got := len(first.hands[player]); got != 26 {
			t.Fatalf("seat %d hand size = %d, want 26", seat, got)
		}
		if !reflect.DeepEqual(first.hands[player], second.hands[second.bySeat[seat]]) {
			t.Fatalf("same seed hand differs at seat %d: %x != %x", seat, first.hands[player], second.hands[second.bySeat[seat]])
		}
		offset := (seat - first.activeSeat + len(first.bySeat)) % len(first.bySeat)
		want := expectedDeck[offset*26 : offset*26+26]
		if !reflect.DeepEqual(first.hands[player], want) {
			t.Fatalf("seat %d hand does not follow banker rotation: got %x want %x", seat, first.hands[player], want)
		}
	}

	counts := make(map[byte]int, 52)
	for _, hand := range first.hands {
		for _, card := range hand {
			counts[card]++
		}
	}
	if len(counts) != 52 {
		t.Fatalf("deal distinct card count = %d, want 52", len(counts))
	}
	for card, count := range counts {
		if count != 2 {
			t.Fatalf("card %x count = %d, want 2", card, count)
		}
	}
}

func TestNewBattleServiceFailsWhenRandomSeedUnavailable(t *testing.T) {
	previous := readRandomSeed
	readRandomSeed = func([]byte) (int, error) { return 0, errors.New("seed unavailable") }
	t.Cleanup(func() { readRandomSeed = previous })
	if _, err := NewBattleService(NHSKBattleConfig{ID: 27}); !errors.Is(err, errBattleRandomFailure) {
		t.Fatalf("NewBattleService error = %v, want random failure", err)
	}
}

func TestBattleUsesInjectedClockForDeadlineAndSceneRemainingTime(t *testing.T) {
	clock := &nhskTestClock{now: time.Unix(100, 500*int64(time.Millisecond))}
	service, _ := newBattleForTest(t, 28, mathrand.New(mathrand.NewSource(3)), clock)
	wantDeadline := clock.now.Add(DefaultNHSKConfig().MsFirstOutCard)
	if !service.deadlineAt.Equal(wantDeadline) {
		t.Fatalf("deadline = %v, want %v", service.deadlineAt, wantDeadline)
	}
	if got := service.remainingActionMilliseconds(); got != 10_000 {
		t.Fatalf("remaining at start = %dms, want 10000ms", got)
	}
	clock.now = wantDeadline.Add(-500 * time.Millisecond)
	if got := service.remainingActionMilliseconds(); got != 500 {
		t.Fatalf("remaining near deadline = %dms, want 500ms", got)
	}
	clock.now = wantDeadline
	if got := service.remainingActionMilliseconds(); got != 0 {
		t.Fatalf("remaining at deadline = %dms, want 0", got)
	}
}

func TestBattleStartEmitsReferenceOpeningSequenceAndStartsActionAtAsk(t *testing.T) {
	clock := &steppingNHSKClock{now: time.Unix(300, 0), step: 10 * time.Millisecond}
	rules := DefaultNHSKConfig()
	rules.MsFirstOutCard = 2 * time.Second
	rules.MsOutCard = 3 * time.Second
	service, err := NewBattleService(NHSKBattleConfig{
		ID: 37, OutputRef: gsr.ServiceRef{Node: "test", ID: 2}, MatchID: 1, ProductID: NHSKDescriptor.GameID,
		ConnectionGeneration: 1, Random: mathrand.New(mathrand.NewSource(3)), Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	output := &recordingBattleTestServiceContext{}
	if err := service.Init(output); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	commands := []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: 37, ProductID: NHSKDescriptor.GameID, MatchID: 1}, Rules: &rules}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
		{ID: StartSubgameCommand, Payload: struct{}{}},
	}
	for _, command := range commands {
		if err := service.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}

	if len(output.sends) != 8 {
		t.Fatalf("opening output count = %d, want 8: %#v", len(output.sends), output.sends)
	}
	wantKinds := []any{OutputGameStart, GameStartedOutput{}, OutputGameInfo, OutputDeal, OutputDeal, OutputDeal, OutputDeal, OutputAskOutCard}
	for index, sent := range output.sends {
		batch := sent.(GameOutputBatch)
		if len(batch.Outputs) != 1 {
			t.Fatalf("opening batch %d outputs = %#v", index, batch.Outputs)
		}
		switch want := wantKinds[index].(type) {
		case OutputKind:
			client, ok := batch.Outputs[0].(ClientGameOutput)
			if !ok || client.Kind != want {
				t.Fatalf("opening output %d = %#v, want %s", index, batch.Outputs[0], want)
			}
		case GameStartedOutput:
			if _, ok := batch.Outputs[0].(GameStartedOutput); !ok {
				t.Fatalf("opening output %d = %#v, want GameStartedOutput", index, batch.Outputs[0])
			}
		}
	}
	for seat := 0; seat < 4; seat++ {
		deal := output.sends[3+seat].(GameOutputBatch).Outputs[0].(ClientGameOutput)
		payload := deal.Payload.(DealPayload)
		player := service.bySeat[seat]
		if !reflect.DeepEqual(deal.Targets, []game.PlayerID{player}) || payload.Players != service.bySeat || payload.SeatID != uint8(seat) || !reflect.DeepEqual(payload.Cards[:], service.hands[player]) {
			t.Fatalf("deal seat %d = targets=%v payload=%#v hand=%x", seat, deal.Targets, payload, service.hands[player])
		}
	}
	ask := output.sends[7].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	askPayload := ask.Payload.(AskOutCardPayload)
	if !reflect.DeepEqual(ask.Targets, []game.PlayerID{"1", "2", "3", "4"}) || askPayload.ActivePlayer != service.bySeat[service.activeSeat] || askPayload.VerifyCode != 3 || askPayload.ActionMilliseconds != 3000 {
		t.Fatalf("opening ask = targets=%v payload=%#v", ask.Targets, askPayload)
	}
	if got, want := service.replayDocument.StartSnapshot().StartedAt, time.Unix(300, 0); !got.Equal(want) {
		t.Fatalf("subgame start = %v, want %v", got, want)
	}
	if got, want := service.deadlineAt, time.Unix(300, 0).Add(2010*time.Millisecond); !got.Equal(want) {
		t.Fatalf("first deadline = %v, want %v", got, want)
	}
	player := service.bySeat[service.activeSeat]
	if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: player, Cards: []byte{service.hands[player][0]}, VerifyCode: service.verifyCode}}); err != nil {
		t.Fatal(err)
	}
	outCards := replayOutCardMoves(service.replayDocument.Moves())
	if len(outCards) != 1 || outCards[0].MoveMilliseconds != 10 {
		t.Fatalf("first action replay milliseconds = %#v, want 10", outCards)
	}
}

func TestBattleUsesRulesFrozenByInitialize(t *testing.T) {
	clock := &nhskTestClock{now: time.Unix(200, 0)}
	service, err := NewBattleService(NHSKBattleConfig{ID: 29, Random: mathrand.New(mathrand.NewSource(3)), Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	output := &recordingBattleTestServiceContext{}
	if err := service.Init(output); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	rules := DefaultNHSKConfig()
	rules.MsFirstOutCard = 2 * time.Second
	rules.MsOutCard = 3 * time.Second
	commands := []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: 29, ProductID: NHSKDescriptor.GameID, MatchID: 1}, Rules: &rules}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
		{ID: StartSubgameCommand, Payload: struct{}{}},
	}
	for _, command := range commands {
		if err := service.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	if want := clock.now.Add(2 * time.Second); !service.deadlineAt.Equal(want) {
		t.Fatalf("deadline = %v, want %v", service.deadlineAt, want)
	}
	if got := service.gameInfoPayload().OutCardSeconds; got != 3 {
		t.Fatalf("OutCardSeconds = %d, want 3", got)
	}
}

func TestBattleRejectsOutOfTurnAndInvalidCardsWithoutMutation(t *testing.T) {
	service, err := NewBattleService(NHSKBattleConfig{ID: 8, Random: mathrand.New(mathrand.NewSource(3)), Clock: &nhskTestClock{now: time.Unix(1, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	if err := service.Init(&battleTestServiceContext{}); err != nil {
		t.Fatal(err)
	}
	if err := service.Handle(ctx, gsr.Command{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: 8, ProductID: 82, MatchID: 1}}}); err != nil {
		t.Fatal(err)
	}
	players := []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}
	if err := service.Handle(ctx, gsr.Command{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: players}}); err != nil {
		t.Fatal(err)
	}
	_ = service.Handle(ctx, gsr.Command{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}})
	_ = service.Handle(ctx, gsr.Command{ID: StartSubgameCommand, Payload: struct{}{}})
	before := service.snapshot()
	if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: "2", Cards: []byte{1}, VerifyCode: before.VerifyCode}}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(ActionResult); result.Accepted || result.Rejection == "" {
		t.Fatalf("out of turn result = %#v", result)
	}
	after := service.snapshot()
	if after.Revision != before.Revision || len(after.Hands[before.ActivePlayer]) != len(before.Hands[before.ActivePlayer]) {
		t.Fatalf("invalid action mutated state: before=%#v after=%#v", before, after)
	}
}

func TestBattleAllowsPassAfterALeadWithoutReplacingTheLead(t *testing.T) {
	service, err := NewBattleService(NHSKBattleConfig{ID: 9})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	if err := service.Init(&battleTestServiceContext{}); err != nil {
		t.Fatal(err)
	}
	identity := InitializeBattleRequest{Identity: BattleIdentity{BattleID: 9, ProductID: 82, MatchID: 1}}
	_ = service.Handle(ctx, gsr.Command{ID: InitializeBattleCommand, Payload: identity})
	players := []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}
	_ = service.Handle(ctx, gsr.Command{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: players}})
	_ = service.Handle(ctx, gsr.Command{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}})
	_ = service.Handle(ctx, gsr.Command{ID: StartSubgameCommand, Payload: struct{}{}})
	first := service.snapshot()
	lead := first.Hands[first.ActivePlayer][0]
	_ = service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: first.ActivePlayer, Cards: []byte{lead}, VerifyCode: first.VerifyCode}})
	second := service.snapshot()
	_ = service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: second.ActivePlayer, VerifyCode: second.VerifyCode}})
	if result := ctx.reply.(ActionResult); !result.Accepted {
		t.Fatalf("pass result = %#v", result)
	}
	if service.lastCount != 1 || len(service.lastCards) != 1 {
		t.Fatalf("pass replaced lead: count=%d cards=%v", service.lastCount, service.lastCards)
	}
}

func TestBattleEndsTrickAfterThreePassesAndPublishesCapturedPoints(t *testing.T) {
	service, output := newPlayingBattleForRestore(t, 16)
	service.hands[service.bySeat[0]] = []byte{0x05, 0x06}
	service.hands[service.bySeat[1]] = []byte{0x07}
	service.hands[service.bySeat[2]] = []byte{0x08}
	service.hands[service.bySeat[3]] = []byte{0x09}
	service.activeSeat = 0
	service.verifyCode = 11
	output.sends = nil

	ctx := &battleTestCommandContext{}
	play := func(player game.PlayerID, cards []byte) {
		t.Helper()
		if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: player, Cards: cards, VerifyCode: service.verifyCode}}); err != nil {
			t.Fatal(err)
		}
		if result := ctx.reply.(ActionResult); !result.Accepted {
			t.Fatalf("play %s %x = %#v", player, cards, result)
		}
	}
	play("1", []byte{0x05})
	if scene := service.scenePayload("1"); scene.TrickScoreCardCount != 1 || scene.TrickScoreCards[0] != 0x05 || scene.Players[0].LastPlayCount != 1 {
		t.Fatalf("scene during trick = %#v", scene)
	}
	play("2", nil)
	play("3", nil)
	play("4", nil)

	var turnEndAt, askAt int
	for index, value := range output.sends {
		batch, ok := value.(GameOutputBatch)
		if !ok || len(batch.Outputs) != 1 {
			continue
		}
		switch payload := batch.Outputs[0].(ClientGameOutput).Payload.(type) {
		case TurnEndPayload:
			turnEndAt = index + 1
			if payload.Winner != "1" || payload.CapturedPoints != 5 {
				t.Fatalf("turn end payload = %#v", payload)
			}
		case AskOutCardPayload:
			if payload.ActivePlayer == "1" {
				askAt = index + 1
			}
		}
	}
	if turnEndAt == 0 || askAt <= turnEndAt {
		t.Fatalf("missing TurnEnd -> AskOutCard order: turnEnd=%d ask=%d sends=%#v", turnEndAt, askAt, output.sends)
	}
	if scene := service.scenePayload("1"); scene.TrickScoreCardCount != 0 || scene.Players[0].CapturedPoints != 5 || scene.Players[0].LastPlayCount != -1 {
		t.Fatalf("scene after trick = %#v", scene)
	}
}

func TestBattleStartSubgameClearsCapturedPoints(t *testing.T) {
	service, _ := newPlayingBattleForRestore(t, 17)
	service.capturedPoints = [4]uint16{5, 10, 15, 20}
	service.phase = NHSKBattlePreparing
	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: StartSubgameCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	if service.capturedPoints != [4]uint16{} {
		t.Fatalf("captured points after new subgame = %v", service.capturedPoints)
	}
}

func TestBattleRequiresSettlementAfterASeatRunsOut(t *testing.T) {
	service, err := NewBattleService(NHSKBattleConfig{ID: 10})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	if err := service.Init(&battleTestServiceContext{}); err != nil {
		t.Fatal(err)
	}
	_ = service.Handle(ctx, gsr.Command{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: 10, ProductID: 82, MatchID: 1}}})
	players := []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}
	_ = service.Handle(ctx, gsr.Command{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: players}})
	_ = service.Handle(ctx, gsr.Command{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}})
	_ = service.Handle(ctx, gsr.Command{ID: StartSubgameCommand, Payload: struct{}{}})
	service.activeSeat = 0
	service.hands[service.bySeat[0]] = []byte{1}
	service.hands[service.bySeat[2]] = []byte{}
	state := service.snapshot()
	if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: state.ActivePlayer, Cards: []byte{1}, VerifyCode: state.VerifyCode}}); err != nil {
		t.Fatal(err)
	}
	if service.phase != NHSKBattleAwaitingSettlement {
		t.Fatalf("phase after partner final card = %s", service.phase)
	}
	if err := service.Handle(ctx, gsr.Command{ID: CompleteSettlementCommand, Payload: successfulSettlementForTest(service)}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(SettlementCommandResult); !result.Accepted || service.phase != NHSKBattleFinished {
		t.Fatalf("settlement = %#v phase=%s", result, service.phase)
	}
}

func TestBattleAppliesSettlementMatrixAtomically(t *testing.T) {
	service, output := newPlayingBattleForRestore(t, 19)
	service.phase = NHSKBattleAwaitingSettlement
	output.sends = nil
	ctx := &battleTestCommandContext{}
	request := CompleteSettlementRequest{
		Success:    true,
		ResultType: 7,
		TeamCount:  4,
		Gains: []SettlementGain{
			{PayTeamID: 0, GainTeamID: 1, Score: 3},
			{PayTeamID: 2, GainTeamID: 3, Score: 5},
		},
		Players: []SettlementPlayerResult{
			{PlayerID: 1, Flag: 0x300, TeamID: 0},
			{PlayerID: 2, Flag: 0x100, TeamID: 1},
			{PlayerID: 3, TeamID: 2},
			{PlayerID: 4, Flag: 0x200, TeamID: 3},
		},
	}
	if err := service.Handle(ctx, gsr.Command{ID: CompleteSettlementCommand, Payload: request}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(SettlementCommandResult); !result.Accepted || service.phase != NHSKBattleFinished {
		t.Fatalf("settlement result = %#v phase=%s", result, service.phase)
	}
	want := [4]int32{-3, 3, -5, 5}
	for seat, player := range service.bySeat {
		if got := service.players[player].Score; got != want[seat] {
			t.Fatalf("seat %d score=%d, want %d", seat, got, want[seat])
		}
	}
	if !service.players["1"].IsBreak || !service.players["1"].IsSeal || !service.players["2"].IsSeal || service.players["2"].IsBreak || !service.players["4"].IsBreak || service.players["4"].IsSeal {
		t.Fatalf("settlement flags = %#v", service.players)
	}
	if len(output.sends) != 3 {
		t.Fatalf("settlement outputs = %d, want GAME_RESULT/ROUND_STAT/GAME_OVER", len(output.sends))
	}
	gameResult := output.sends[0].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	if gameResult.Kind != OutputGameResult || gameResult.Payload.(GameResultPayload).Scores != want {
		t.Fatalf("success GAME_RESULT = %#v", gameResult)
	}
	if roundStat := output.sends[1].(GameOutputBatch).Outputs[0].(ClientGameOutput); roundStat.Kind != OutputRoundStat {
		t.Fatalf("ROUND_STAT = %#v", roundStat)
	}
	gameOver := output.sends[2].(GameOutputBatch).Outputs[0].(GameOverOutput)
	if gameOver.Reason != int32(GameOverReasonSuccess) {
		t.Fatalf("success GAME_OVER = %#v", gameOver)
	}
	if len(gameOver.Players) != 4 || gameOver.Players[0].Score != -3 || gameOver.Players[3].Score != 5 {
		t.Fatalf("success GAME_OVER players = %#v", gameOver.Players)
	}
}

func TestBattleFencesReplayCompletionBeforeRoundStatAndGameOver(t *testing.T) {
	service, output := newPlayingBattleForRestore(t, 38)
	submitter := &recordingReplaySubmitter{}
	service.replaySubmitter = submitter
	service.replayTimeout = time.Second
	service.phase = NHSKBattleAwaitingSettlement
	output.sends = nil
	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: CompleteSettlementCommand, Payload: successfulSettlementForTest(service)}); err != nil {
		t.Fatal(err)
	}
	if service.phase != NHSKBattleFinalizingReplay || len(output.sends) != 1 || len(submitter.artifacts) != 1 {
		t.Fatalf("after settlement phase=%s outputs=%d artifacts=%d", service.phase, len(output.sends), len(submitter.artifacts))
	}
	artifact := submitter.artifacts[0]
	if len(artifact.XML) == 0 || artifact.Target != ctx.Self() || artifact.BattleID != 38 {
		t.Fatalf("artifact = %#v", artifact)
	}
	wrong := replayWriteResult{BattleID: 39, GameNum: artifact.GameNum, SubgameNum: artifact.SubgameNum, ReplayName: artifact.ReplayName}
	if err := service.Handle(ctx, gsr.Command{ID: applyReplayResultCommand, Payload: wrong}); err != nil {
		t.Fatal(err)
	}
	if service.phase != NHSKBattleFinalizingReplay || len(output.sends) != 1 {
		t.Fatalf("wrong completion changed phase=%s outputs=%d", service.phase, len(output.sends))
	}
	correct := replayWriteResult{BattleID: artifact.BattleID, GameNum: artifact.GameNum, SubgameNum: artifact.SubgameNum, ReplayName: artifact.ReplayName}
	if err := service.Handle(ctx, gsr.Command{ID: applyReplayResultCommand, Payload: correct}); err != nil {
		t.Fatal(err)
	}
	if service.phase != NHSKBattleFinished || len(output.sends) != 3 {
		t.Fatalf("correct completion phase=%s outputs=%d", service.phase, len(output.sends))
	}
	if got := output.sends[1].(GameOutputBatch).Outputs[0].(ClientGameOutput).Kind; got != OutputRoundStat {
		t.Fatalf("second output = %s, want %s", got, OutputRoundStat)
	}
	if _, ok := output.sends[2].(GameOutputBatch).Outputs[0].(GameOverOutput); !ok {
		t.Fatalf("third output = %#v, want GameOverOutput", output.sends[2])
	}
	if err := service.Handle(ctx, gsr.Command{ID: applyReplayResultCommand, Payload: correct}); err != nil {
		t.Fatal(err)
	}
	if len(output.sends) != 3 {
		t.Fatalf("duplicate completion outputs=%d, want 3", len(output.sends))
	}
}

func TestBattleRejectsMalformedSettlementWithoutMutation(t *testing.T) {
	service, _ := newPlayingBattleForRestore(t, 20)
	service.phase = NHSKBattleAwaitingSettlement
	before := service.snapshot()
	ctx := &battleTestCommandContext{}
	request := CompleteSettlementRequest{
		Success:    true,
		ResultType: 7,
		TeamCount:  4,
		Gains: []SettlementGain{
			{PayTeamID: 0, GainTeamID: 1, Score: 3},
			{PayTeamID: 0, GainTeamID: 1, Score: 5},
		},
		Players: []SettlementPlayerResult{
			{PlayerID: 1, TeamID: 0},
			{PlayerID: 2, TeamID: 1},
			{PlayerID: 3, TeamID: 2},
		},
	}
	if err := service.Handle(ctx, gsr.Command{ID: CompleteSettlementCommand, Payload: request}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(SettlementCommandResult); result.Accepted || service.phase != NHSKBattleAwaitingSettlement {
		t.Fatalf("malformed settlement result = %#v phase=%s", result, service.phase)
	}
	after := service.snapshot()
	if !reflect.DeepEqual(before.Players, after.Players) || before.Revision != after.Revision {
		t.Fatalf("malformed settlement mutated state: before=%#v after=%#v", before, after)
	}
}

func TestBattleSettlementFailureDissolvesAndClearsFlags(t *testing.T) {
	service, output := newPlayingBattleForRestore(t, 21)
	service.phase = NHSKBattleAwaitingSettlement
	service.players["1"] = BattlePlayer{Player: "1", UserID: 1, SeatID: 0, Score: 9, IsBreak: true, IsSeal: true}
	output.sends = nil
	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: CompleteSettlementCommand, Payload: CompleteSettlementRequest{
		Success:    false,
		ResultType: 99,
		TeamCount:  99,
		Gains:      []SettlementGain{{PayTeamID: 0, GainTeamID: 1, Score: -1}},
		Players:    []SettlementPlayerResult{{PlayerID: 999, Flag: 0x300, TeamID: 99}},
	}}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(SettlementCommandResult); !result.Accepted || service.phase != NHSKBattleFinished {
		t.Fatalf("failure settlement = %#v phase=%s", result, service.phase)
	}
	for seat, player := range service.bySeat {
		value := service.players[player]
		if value.Score != 0 || value.IsBreak || value.IsSeal {
			t.Fatalf("seat %d failure state = %#v", seat, value)
		}
	}
	if len(output.sends) != 3 {
		t.Fatalf("failure outputs = %d, want GAME_RESULT/ROUND_STAT/GAME_OVER", len(output.sends))
	}
	gameResult := output.sends[0].(GameOutputBatch).Outputs[0].(ClientGameOutput).Payload.(GameResultPayload)
	if gameResult.Reason != GameOverReasonDissolve || gameResult.Result != SubgameResultPeace || gameResult.Scores != [4]int32{} {
		t.Fatalf("failure GAME_RESULT = %#v", gameResult)
	}
	gameOver := output.sends[2].(GameOutputBatch).Outputs[0].(GameOverOutput)
	if gameOver.Reason != int32(GameOverReasonDissolve) || gameOver.IsGameOver {
		t.Fatalf("failure GAME_OVER = %#v", gameOver)
	}
}

func TestBattleStartSubgameClearsPreviousSettlementFlags(t *testing.T) {
	service, _ := newPlayingBattleForRestore(t, 22)
	service.phase = NHSKBattlePreparing
	service.players["1"] = BattlePlayer{Player: "1", UserID: 1, SeatID: 0, IsBreak: true, IsSeal: true}
	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: StartSubgameCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	for seat, player := range service.bySeat {
		if service.players[player].IsBreak || service.players[player].IsSeal {
			t.Fatalf("seat %d flags not reset = %#v", seat, service.players[player])
		}
	}
}

func TestBattleUpdatePlayersDoesNotAcceptSettlementFlags(t *testing.T) {
	service, _ := newPlayingBattleForRestore(t, 23)
	service.phase = NHSKBattlePreparing
	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0, IsBreak: true, IsSeal: true}}}}); err != nil {
		t.Fatal(err)
	}
	if service.players["1"].IsBreak || service.players["1"].IsSeal {
		t.Fatalf("UPDATE_PLAYER injected settlement flags = %#v", service.players["1"])
	}
}

func TestBattleSingleSeatOutKeepsPlayingAndShowsPartnerCards(t *testing.T) {
	service, output := newPlayingBattleForRestore(t, 16)
	service.activeSeat = 0
	service.verifyCode = 9
	service.hands[service.bySeat[0]] = []byte{1}
	service.hands[service.bySeat[2]] = []byte{2, 3}
	service.lastCards = nil
	output.sends = nil

	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: service.bySeat[0], Cards: []byte{1}, VerifyCode: 9}}); err != nil {
		t.Fatal(err)
	}
	if service.phase != NHSKBattlePlaying || service.activeSeat != 1 {
		t.Fatalf("single seat out state phase=%s active=%d", service.phase, service.activeSeat)
	}
	if len(output.sends) != 3 {
		t.Fatalf("single seat out outputs=%d, want out-card/show-cards/ask", len(output.sends))
	}
	show := output.sends[1].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	if show.Kind != OutputShowCards || !reflect.DeepEqual(show.Targets, []game.PlayerID{"1"}) {
		t.Fatalf("show output=%#v", show)
	}
	payload := show.Payload.(ShowCardsPayload)
	if payload.HandCounts[2] != 2 || payload.Cards[2][0] != 2 || payload.Cards[2][1] != 3 {
		t.Fatalf("partner hand not shown: counts=%v cards=%x", payload.HandCounts, payload.Cards[2][:4])
	}
	for seat := range payload.Cards {
		if seat != 2 && payload.Cards[seat] != [26]byte{} {
			t.Fatalf("unexpected hand reveal at seat %d: %x", seat, payload.Cards[seat][:4])
		}
	}
}

func TestBattleSceneRevealsPartnerForFinishedRequester(t *testing.T) {
	service, output := newPlayingBattleForRestore(t, 17)
	service.hands["1"] = nil
	service.hands["3"] = []byte{7, 8}
	service.finished[0] = true
	service.ranks[0] = 1
	output.sends = nil

	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: RequestGameSceneCommand, Payload: ReconnectPlayerRequest{Player: "1"}}); err != nil {
		t.Fatal(err)
	}
	if len(output.sends) < 2 {
		t.Fatalf("scene outputs=%d", len(output.sends))
	}
	scene := output.sends[1].(GameOutputBatch).Outputs[0].(ClientGameOutput).Payload.(GameScenePayload)
	if scene.Players[2].HandCount != 2 || scene.Players[2].HandCards[0] != 7 || scene.Players[2].HandCards[1] != 8 {
		t.Fatalf("partner scene hand not shown: %#v", scene.Players[2])
	}
	if scene.FinishedPlayerCount != 1 || scene.Players[0].Rank != 1 {
		t.Fatalf("scene finish metadata = count:%d rank:%d", scene.FinishedPlayerCount, scene.Players[0].Rank)
	}
	for seat, player := range scene.Players {
		if seat != 0 && seat != 2 && player.HandCards != [26]byte{} {
			t.Fatalf("scene leaked hand at seat %d: %x", seat, player.HandCards[:4])
		}
	}
}

func TestBattlePartnerPairFinishShowsAllRemainingHands(t *testing.T) {
	service, output := newPlayingBattleForRestore(t, 18)
	service.activeSeat = 0
	service.verifyCode = 10
	service.hands[service.bySeat[0]] = []byte{1}
	service.hands[service.bySeat[2]] = nil
	service.hands[service.bySeat[1]] = []byte{4, 5}
	service.hands[service.bySeat[3]] = []byte{6}
	service.lastCards = nil
	output.sends = nil

	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: service.bySeat[0], Cards: []byte{1}, VerifyCode: 10}}); err != nil {
		t.Fatal(err)
	}
	if service.phase != NHSKBattleAwaitingSettlement {
		t.Fatalf("pair finish phase=%s, want awaiting_settlement", service.phase)
	}
	if len(output.sends) != 3 {
		t.Fatalf("pair finish outputs=%d, want out-card/show-cards/settlement", len(output.sends))
	}
	show := output.sends[1].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	if show.Kind != OutputShowCards || !reflect.DeepEqual(show.Targets, []game.PlayerID{"1", "2", "3", "4"}) {
		t.Fatalf("pair show output=%#v", show)
	}
	payload := show.Payload.(ShowCardsPayload)
	if payload.Cards[1][0] != 4 || payload.Cards[3][0] != 6 || payload.HandCounts[2] != 0 {
		t.Fatalf("pair hands=%#v counts=%v", payload.Cards, payload.HandCounts)
	}
	settlement := output.sends[2].(GameOutputBatch).Outputs[0].(SettlementRequestOutput)
	if settlement.TeamCount != 4 || len(settlement.Players) != 4 {
		t.Fatalf("settlement output=%#v", settlement)
	}
}

func TestBattleForceFinishUsesLocalResultAndEmitsNoticeAfterGameOver(t *testing.T) {
	service, err := NewBattleService(NHSKBattleConfig{
		ID:                   11,
		MatchID:              1,
		ProductID:            NHSKDescriptor.GameID,
		ConnectionGeneration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	output := &recordingBattleTestServiceContext{}
	if err := service.Init(output); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: 11, ProductID: NHSKDescriptor.GameID, MatchID: 1}}}); err != nil {
		t.Fatal(err)
	}
	players := []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}
	if err := service.Handle(ctx, gsr.Command{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: players}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Handle(ctx, gsr.Command{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Handle(ctx, gsr.Command{ID: StartSubgameCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	service.outputRef = gsr.ServiceRef{Node: "test", ID: 2}
	if err := service.Handle(ctx, gsr.Command{ID: ForceFinishSubgameCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	if len(output.sends) != 5 {
		t.Fatalf("force finish outputs = %d, want Show/GameResult/RoundStat/GameOver/Notice", len(output.sends))
	}
	show := output.sends[0].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	result := output.sends[1].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	if show.Kind != OutputShowCards || result.Kind != OutputGameResult {
		t.Fatalf("force client outputs = %#v / %#v", show, result)
	}
	roundStat := output.sends[2].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	if roundStat.Kind != OutputRoundStat {
		t.Fatalf("third force finish output = %#v, want RoundStat", roundStat)
	}
	gameOver, ok := output.sends[3].(GameOutputBatch).Outputs[0].(GameOverOutput)
	if !ok {
		t.Fatalf("fourth force finish output = %#v, want GameOverOutput", output.sends[3])
	}
	if gameOver.Reason != int32(GameOverReasonSuccess) || gameOver.IsGameOver {
		t.Fatalf("force finish GameOver = %#v, want success subgame", gameOver)
	}
	notice, ok := output.sends[4].(GameOutputBatch).Outputs[0].(NoticeRoundOverOutput)
	if !ok {
		t.Fatalf("fifth force finish output = %#v, want NoticeRoundOverOutput", output.sends[4])
	}
	if notice.EndReason != int32(GameOverReasonSuccess) {
		t.Fatalf("force finish Notice = %#v, want success reason", notice)
	}
}

func TestBattleForceFinishBeforePlayingIsNoop(t *testing.T) {
	service, err := NewBattleService(NHSKBattleConfig{ID: 12})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	if err := service.Init(&battleTestServiceContext{}); err != nil {
		t.Fatal(err)
	}
	if err := service.Handle(ctx, gsr.Command{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: 12, ProductID: NHSKDescriptor.GameID, MatchID: 1}}}); err != nil {
		t.Fatal(err)
	}
	players := []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}
	if err := service.Handle(ctx, gsr.Command{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: players}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Handle(ctx, gsr.Command{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Handle(ctx, gsr.Command{ID: ForceFinishSubgameCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	if service.phase != NHSKBattlePreparing {
		t.Fatalf("pre-playing force finish phase = %s, want preparing", service.phase)
	}
	if result, ok := ctx.reply.(CommandResult); !ok || !result.Accepted {
		t.Fatalf("pre-playing force finish reply = %#v, want accepted no-op", ctx.reply)
	}
}

func TestBattleForceFinishFencesSettlementAndDefersNoticeUntilReplay(t *testing.T) {
	replay := &recordingReplaySubmitter{}
	service, err := NewBattleService(NHSKBattleConfig{ID: 55, MatchID: 1, ProductID: NHSKDescriptor.GameID, OutputRef: gsr.ServiceRef{Node: "test", ID: 2}, ConnectionGeneration: 1, Random: mathrand.New(mathrand.NewSource(3)), Clock: &nhskTestClock{now: time.Unix(1, 0)}, ReplaySubmitter: replay})
	if err != nil {
		t.Fatal(err)
	}
	output := &recordingBattleTestServiceContext{}
	if err := service.Init(output); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	for _, command := range []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: 55, ProductID: NHSKDescriptor.GameID, MatchID: 1}}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
		{ID: StartSubgameCommand, Payload: struct{}{}},
	} {
		if err := service.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	service.phase = NHSKBattleAwaitingSettlement
	output.sends = nil
	if err := service.Handle(ctx, gsr.Command{ID: ForceFinishSubgameCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	if service.phase != NHSKBattleFinalizingReplay || len(replay.artifacts) != 1 || len(output.sends) != 2 {
		t.Fatalf("force pending state phase=%s artifacts=%d outputs=%d", service.phase, len(replay.artifacts), len(output.sends))
	}
	if err := service.Handle(&battleTestCommandContext{}, gsr.Command{ID: CompleteSettlementCommand, Payload: CompleteSettlementRequest{Success: false}}); err != nil {
		t.Fatal(err)
	}
	if len(output.sends) != 2 {
		t.Fatal("late settlement produced output")
	}
	artifact := replay.artifacts[0]
	if err := service.Handle(ctx, gsr.Command{ID: applyReplayResultCommand, Payload: replayWriteResult{BattleID: artifact.BattleID, GameNum: artifact.GameNum, SubgameNum: artifact.SubgameNum, ReplayName: artifact.ReplayName}}); err != nil {
		t.Fatal(err)
	}
	if len(output.sends) != 5 {
		t.Fatalf("final force outputs = %d, want Show/Result/RoundStat/GameOver/Notice", len(output.sends))
	}
	if _, ok := output.sends[4].(GameOutputBatch).Outputs[0].(NoticeRoundOverOutput); !ok {
		t.Fatalf("last output = %#v", output.sends[4])
	}
}

func TestBattleRecordsPropFactOnlyDuringWritableSubgame(t *testing.T) {
	service := newPlayingBattleWithSeed(t, 53, 3)
	request := RecordPropUseRequest{SenderID: 1, PropID: "flower", SendCount: 2, TargetIDs: []uint32{2, 2, 1}}
	if err := service.Handle(&battleTestCommandContext{}, gsr.Command{ID: RecordPropUseCommand, Payload: request}); err != nil {
		t.Fatal(err)
	}
	request.TargetIDs[0] = 99
	moves := service.replayDocument.Moves()
	move := moves[len(moves)-1]
	if move.Kind != ReplayMoveProp || move.UserID != 1 || move.PropID != "flower" || move.PropCount != 2 || !reflect.DeepEqual(move.TargetIDs, []uint32{2, 2, 1}) {
		t.Fatalf("prop move = %#v", move)
	}
	before := len(moves)
	if err := service.Handle(&battleTestCommandContext{}, gsr.Command{ID: RecordPropUseCommand, Payload: RecordPropUseRequest{SenderID: 999}}); err != nil {
		t.Fatal(err)
	}
	if len(service.replayDocument.Moves()) != before {
		t.Fatal("unknown sender changed replay")
	}
}

func TestBattleDeleteBarrierFencesOutputsAndLateResults(t *testing.T) {
	service, output := newBattleForTest(t, 54, mathrand.New(mathrand.NewSource(3)), &nhskTestClock{now: time.Unix(1, 0)})
	before := len(output.sends)
	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: deleteBattleBarrierCommand, Payload: deleteBattleBarrier{BattleID: 54}}); err != nil {
		t.Fatal(err)
	}
	if !service.deleting || service.phase != NHSKBattleStopping || !ctx.reply.(CommandResult).Accepted {
		t.Fatalf("barrier state = deleting:%t phase:%s reply:%#v", service.deleting, service.phase, ctx.reply)
	}
	player := service.bySeat[service.activeSeat]
	_ = service.Handle(&battleTestCommandContext{}, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: player, Cards: []byte{service.hands[player][0]}, VerifyCode: service.verifyCode}})
	_ = service.Handle(&battleTestCommandContext{}, gsr.Command{ID: ForceFinishSubgameCommand, Payload: struct{}{}})
	_ = service.Handle(&battleTestCommandContext{}, gsr.Command{ID: applyAIResultCommand, Payload: aiResult{BattleID: 54}})
	_ = service.Handle(&battleTestCommandContext{}, gsr.Command{ID: applyReplayResultCommand, Payload: replayWriteResult{BattleID: 54}})
	if len(output.sends) != before {
		t.Fatalf("post-barrier outputs = %d, want %d", len(output.sends), before)
	}
}

func TestBattleReconnectRestoresSceneAndDoesNotLeakHands(t *testing.T) {
	service, output := newPlayingBattleForRestore(t, 13)
	service.offline["1"] = true
	service.auto["1"] = true
	output.sends = nil

	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: ReconnectPlayerCommand, Payload: ReconnectPlayerRequest{Player: "1"}}); err != nil {
		t.Fatal(err)
	}
	if result, ok := ctx.reply.(CommandResult); !ok || !result.Accepted {
		t.Fatalf("reconnect reply = %#v", ctx.reply)
	}
	if service.offline["1"] || service.auto["1"] || !service.clientReady["1"] {
		t.Fatalf("reconnect state offline=%t auto=%t ready=%t", service.offline["1"], service.auto["1"], service.clientReady["1"])
	}
	if len(output.sends) != 4 {
		t.Fatalf("reconnect output count = %d, want Auto/GameInfo/Scene/Ask", len(output.sends))
	}
	if auto := output.sends[0].(GameOutputBatch).Outputs[0].(ClientGameOutput); auto.Kind != OutputPlayerAutoState {
		t.Fatalf("reconnect first output = %#v", auto)
	}
	if _, ok := output.sends[1].(GameOutputBatch).Outputs[0].(ClientGameOutput); !ok {
		t.Fatalf("reconnect first output = %#v", output.sends[0])
	}
	first := output.sends[1].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	if first.Kind != OutputGameInfo {
		t.Fatalf("reconnect first kind = %s, want %s", first.Kind, OutputGameInfo)
	}
	scene := output.sends[2].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	if scene.Kind != OutputGameScene {
		t.Fatalf("reconnect second kind = %s, want %s", scene.Kind, OutputGameScene)
	}
	payload := scene.Payload.(GameScenePayload)
	for seat, player := range payload.Players {
		if player.Player == "1" && player.HandCards == [26]byte{} {
			t.Fatalf("reconnect own hand is hidden at seat %d", seat)
		}
		if player.Player != "1" && player.HandCards != [26]byte{} {
			t.Fatalf("reconnect leaked hand at seat %d: %x", seat, player.HandCards)
		}
	}
	if ask := output.sends[3].(GameOutputBatch).Outputs[0].(ClientGameOutput); ask.Kind != OutputAskOutCard {
		t.Fatalf("reconnect third kind = %s, want %s", ask.Kind, OutputAskOutCard)
	}
}

func TestBattleSceneRequestDoesNotClearOffline(t *testing.T) {
	service, output := newPlayingBattleForRestore(t, 14)
	service.offline["1"] = true
	service.auto["1"] = true
	output.sends = nil

	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: RequestGameSceneCommand, Payload: ReconnectPlayerRequest{Player: "1"}}); err != nil {
		t.Fatal(err)
	}
	if !service.offline["1"] || service.auto["1"] || !service.clientReady["1"] {
		t.Fatalf("scene state offline=%t auto=%t ready=%t", service.offline["1"], service.auto["1"], service.clientReady["1"])
	}
	if len(output.sends) != 4 {
		t.Fatalf("scene output count = %d, want Auto/GameInfo/Scene/Ask", len(output.sends))
	}
}

func TestBattleRoundStatTargetsRequireReadyAndNonExitedPlayers(t *testing.T) {
	service, _ := newPlayingBattleForRestore(t, 15)
	service.clientReady["2"] = false
	service.players["3"] = BattlePlayer{Player: "3", UserID: 3, SeatID: 2, Exited: true}
	if got := service.roundStatPlayers(); !reflect.DeepEqual(got, []game.PlayerID{"1", "4"}) {
		t.Fatalf("round stat targets = %v, want [1 4]", got)
	}
}

func TestBattleOrdinaryTimeoutEnablesAutoAndPlaysSmallestSingle(t *testing.T) {
	clock := &nhskTestClock{now: time.Unix(500, 0)}
	rules := DefaultNHSKConfig()
	rules.MsFirstOutCard = 2 * time.Second
	service, output := newBattleForTestWithRules(t, 39, mathrand.New(mathrand.NewSource(2)), clock, &rules)
	service.outputRef = gsr.ServiceRef{Node: "test", ID: 2}
	active := service.bySeat[service.activeSeat]
	service.hands[active] = []byte{0x12, 0x04, 0x23, 0x13}
	output.sends = nil
	deadline := actionDeadline{TurnRevision: service.turnRevision, Source: ReplayMoveSourceTimeout}
	clock.now = clock.now.Add(2 * time.Second)
	if err := service.Handle(&battleTestCommandContext{}, gsr.Command{ID: nhskBattleTimerCommand, Payload: deadline}); err != nil {
		t.Fatal(err)
	}
	if !service.auto[active] || len(service.hands[active]) != 3 {
		t.Fatalf("timeout state auto=%t hand=%x", service.auto[active], service.hands[active])
	}
	moves := replayOutCardMoves(service.replayDocument.Moves())
	last := moves[len(moves)-1]
	if last.Source != ReplayMoveSourceTimeout || !reflect.DeepEqual(last.Cards, []byte{0x13}) {
		t.Fatalf("timeout move = %#v, want smallest logical 3", last)
	}
	if len(output.sends) < 2 || output.sends[0].(GameOutputBatch).Outputs[0].(ClientGameOutput).Kind != OutputPlayerAutoState {
		t.Fatalf("timeout outputs = %#v", output.sends)
	}
}

func TestBattleTimeoutAutoDisabledLeavesOpportunityWaiting(t *testing.T) {
	rules := DefaultNHSKConfig()
	rules.TimeoutAutoMove = false
	service, output := newBattleForTestWithRules(t, 40, mathrand.New(mathrand.NewSource(2)), &nhskTestClock{now: time.Unix(1, 0)}, &rules)
	service.outputRef = gsr.ServiceRef{Node: "test", ID: 2}
	active := service.bySeat[service.activeSeat]
	before := append([]byte(nil), service.hands[active]...)
	output.sends = nil
	if err := service.Handle(&battleTestCommandContext{}, gsr.Command{ID: nhskBattleTimerCommand, Payload: actionDeadline{TurnRevision: service.turnRevision, Source: ReplayMoveSourceTimeout}}); err != nil {
		t.Fatal(err)
	}
	if service.auto[active] || !reflect.DeepEqual(service.hands[active], before) || len(output.sends) != 0 || !service.deadlineAt.IsZero() {
		t.Fatalf("disabled timeout auto=%t changedHand=%t outputs=%d deadline=%v", service.auto[active], !reflect.DeepEqual(service.hands[active], before), len(output.sends), service.deadlineAt)
	}
}

func TestBattleActivePlayerEnablesAutoImmediatelyWhenDeadlineHasRoom(t *testing.T) {
	clock := &nhskTestClock{now: time.Unix(600, 0)}
	service, output := newBattleForTest(t, 41, mathrand.New(mathrand.NewSource(2)), clock)
	service.outputRef = gsr.ServiceRef{Node: "test", ID: 2}
	active := service.bySeat[service.activeSeat]
	service.hands[active] = []byte{0x04, 0x03}
	service.deadlineAt = clock.now.Add(time.Second)
	output.sends = nil
	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: SetPlayerAutoStateCommand, Payload: SetPlayerAutoStateRequest{Player: active, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if result, ok := ctx.reply.(CommandResult); !ok || !result.Accepted {
		t.Fatalf("reply = %#v", ctx.reply)
	}
	if len(service.hands[active]) != 1 || len(output.sends) < 3 {
		t.Fatalf("active auto hand=%x outputs=%d", service.hands[active], len(output.sends))
	}
	first := output.sends[0].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	second := output.sends[1].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	if first.Kind != OutputPlayerAutoState || len(first.Targets) != 4 || second.Kind != OutputPlayerAutoState || !reflect.DeepEqual(second.Targets, []game.PlayerID{active}) {
		t.Fatalf("auto state outputs = %#v / %#v", first, second)
	}
}

func TestBattleFreezesRoundContextAndDressAtStart(t *testing.T) {
	service, err := NewBattleService(NHSKBattleConfig{ID: 16, MatchID: 1, ProductID: NHSKDescriptor.GameID, Random: mathrand.New(mathrand.NewSource(3)), Clock: &nhskTestClock{now: time.Unix(1, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Init(&recordingBattleTestServiceContext{}); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	commands := []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: 16, ProductID: NHSKDescriptor.GameID, MatchID: 1}}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: []BattlePlayer{
			{Player: "1", UserID: 1, SeatID: 0, Dress: "initial"},
			{Player: "2", UserID: 2, SeatID: 1},
			{Player: "3", UserID: 3, SeatID: 2},
			{Player: "4", UserID: 4, SeatID: 3},
		}}},
		{ID: UpdateRoundContextCommand, Payload: UpdateRoundContextRequest{SecRoundTotal: 300, SecRoundUsed: 12, RoomInfo: "before"}},
		{ID: UpdatePlayerDressCommand, Payload: UpdatePlayerDressRequest{Player: "1", Dress: "before"}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
		{ID: StartSubgameCommand, Payload: struct{}{}},
	}
	for _, command := range commands {
		if err := service.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	if service.currentRound != (UpdateRoundContextRequest{SecRoundTotal: 300, SecRoundUsed: 12, RoomInfo: "before"}) || service.startedDresses[0] != "before" {
		t.Fatalf("start snapshots = round=%#v dresses=%q", service.currentRound, service.startedDresses)
	}
	revision := service.revision
	if err := service.Handle(ctx, gsr.Command{ID: UpdateRoundContextCommand, Payload: UpdateRoundContextRequest{SecRoundTotal: 600, SecRoundUsed: 20, RoomInfo: "after"}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Handle(ctx, gsr.Command{ID: UpdatePlayerDressCommand, Payload: UpdatePlayerDressRequest{Player: "1", Dress: "after"}}); err != nil {
		t.Fatal(err)
	}
	if err := service.Handle(ctx, gsr.Command{ID: UpdatePlayerDressCommand, Payload: UpdatePlayerDressRequest{Player: "1", Dress: ""}}); err != nil {
		t.Fatal(err)
	}
	if service.currentRound.RoomInfo != "before" || service.startedDresses[0] != "before" || service.nextRound.RoomInfo != "after" || service.players["1"].Dress != "" || service.revision != revision {
		t.Fatalf("post-start metadata = current=%#v next=%#v startedDress=%q playerDress=%q revision=%d want=%d", service.currentRound, service.nextRound, service.startedDresses[0], service.players["1"].Dress, service.revision, revision)
	}
}

func newPlayingBattleForRestore(t *testing.T, id game.BattleID) (*NHSKBattleService, *recordingBattleTestServiceContext) {
	t.Helper()
	service, err := NewBattleService(NHSKBattleConfig{ID: id, MatchID: 1, ProductID: NHSKDescriptor.GameID, ConnectionGeneration: 1, Random: mathrand.New(mathrand.NewSource(3)), Clock: &nhskTestClock{now: time.Unix(1, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	output := &recordingBattleTestServiceContext{}
	if err := service.Init(output); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	commands := []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: id, ProductID: NHSKDescriptor.GameID, MatchID: 1}}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
		{ID: StartSubgameCommand, Payload: struct{}{}},
	}
	for _, command := range commands {
		if err := service.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	service.outputRef = gsr.ServiceRef{Node: "test", ID: 2}
	return service, output
}

func newPlayingBattleWithSeed(t *testing.T, id game.BattleID, seed int64) *NHSKBattleService {
	t.Helper()
	service, _ := newBattleForTest(t, id, mathrand.New(mathrand.NewSource(seed)), &nhskTestClock{now: time.Unix(1, 0)})
	return service
}

func newBattleForTest(t *testing.T, id game.BattleID, random NHSKRandomSource, clock NHSKClock) (*NHSKBattleService, *recordingBattleTestServiceContext) {
	return newBattleForTestWithRules(t, id, random, clock, nil)
}

func newBattleForTestWithRules(t *testing.T, id game.BattleID, random NHSKRandomSource, clock NHSKClock, rules *NHSKConfig) (*NHSKBattleService, *recordingBattleTestServiceContext) {
	return newBattleForTestWithOptions(t, id, random, clock, rules, false, [4]bool{})
}

func newBattleForTestWithOptions(t *testing.T, id game.BattleID, random NHSKRandomSource, clock NHSKClock, rules *NHSKConfig, isNewbie bool, automated [4]bool) (*NHSKBattleService, *recordingBattleTestServiceContext) {
	t.Helper()
	service, err := NewBattleService(NHSKBattleConfig{ID: id, MatchID: 1, ProductID: NHSKDescriptor.GameID, IsNewbie: isNewbie, ConnectionGeneration: 1, Random: random, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	output := &recordingBattleTestServiceContext{}
	if err := service.Init(output); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	players := make([]BattlePlayer, 4)
	for seat := range players {
		players[seat] = BattlePlayer{Player: game.PlayerID(string(rune('1' + seat))), UserID: uint32(seat + 1), SeatID: uint8(seat), Automated: automated[seat]}
	}
	commands := []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: id, ProductID: NHSKDescriptor.GameID, MatchID: 1}, Rules: rules}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: players}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
		{ID: StartSubgameCommand, Payload: struct{}{}},
	}
	for _, command := range commands {
		if err := service.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	return service, output
}

type nhskTestClock struct {
	now time.Time
}

func successfulSettlementForTest(service *NHSKBattleService) CompleteSettlementRequest {
	players := make([]SettlementPlayerResult, 4)
	for seat, playerID := range service.bySeat {
		players[seat] = SettlementPlayerResult{PlayerID: service.players[playerID].UserID, TeamID: uint32(seat)}
	}
	return CompleteSettlementRequest{Success: true, ResultType: 1, TeamCount: 4, Gains: []SettlementGain{{PayTeamID: 1, GainTeamID: 0, Score: 1}, {PayTeamID: 3, GainTeamID: 2, Score: 1}}, Players: players}
}

func (clock *nhskTestClock) Now() time.Time { return clock.now }

type steppingNHSKClock struct {
	now  time.Time
	step time.Duration
}

func (clock *steppingNHSKClock) Now() time.Time {
	now := clock.now
	clock.now = clock.now.Add(clock.step)
	return now
}

type battleTestCommandContext struct{ reply any }

func (*battleTestCommandContext) Self() gsr.ServiceRef    { return gsr.ServiceRef{Node: "test", ID: 1} }
func (*battleTestCommandContext) Source() gsr.ServiceRef  { return gsr.ServiceRef{} }
func (c *battleTestCommandContext) Reply(value any) error { c.reply = value; return nil }

type battleTestServiceContext struct{}

type recordingBattleTestServiceContext struct {
	sends  []any
	timers []battleTestTimer
}

type battleTestTimer struct {
	delay   time.Duration
	command gsr.CommandID
	payload any
}

func (*recordingBattleTestServiceContext) Self() gsr.ServiceRef {
	return gsr.ServiceRef{Node: "test", ID: 1}
}
func (c *recordingBattleTestServiceContext) Send(_ gsr.ServiceRef, _ gsr.CommandID, payload any) error {
	c.sends = append(c.sends, payload)
	return nil
}
func (*recordingBattleTestServiceContext) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}

func (c *recordingBattleTestServiceContext) After(delay time.Duration, command gsr.CommandID, payload any) (gsr.TimerID, error) {
	c.timers = append(c.timers, battleTestTimer{delay: delay, command: command, payload: payload})
	return 1, nil
}
func (*recordingBattleTestServiceContext) Now() time.Time       { return time.Unix(1, 0) }
func (*recordingBattleTestServiceContext) Logger() *slog.Logger { return slog.Default() }
func (*recordingBattleTestServiceContext) Metrics() gsr.Metrics { return battleTestMetrics{} }

func (*battleTestServiceContext) Self() gsr.ServiceRef                          { return gsr.ServiceRef{Node: "test", ID: 1} }
func (*battleTestServiceContext) Send(gsr.ServiceRef, gsr.CommandID, any) error { return nil }
func (*battleTestServiceContext) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}
func (*battleTestServiceContext) After(time.Duration, gsr.CommandID, any) (gsr.TimerID, error) {
	return 1, nil
}
func (*battleTestServiceContext) Now() time.Time       { return time.Unix(1, 0) }
func (*battleTestServiceContext) Logger() *slog.Logger { return slog.Default() }
func (*battleTestServiceContext) Metrics() gsr.Metrics { return battleTestMetrics{} }

type battleTestMetrics struct{}

func (battleTestMetrics) Inc(string)                    {}
func (battleTestMetrics) Add(string, uint64)            {}
func (battleTestMetrics) SetGauge(string, int64)        {}
func (battleTestMetrics) Observe(string, time.Duration) {}

type recordingReplaySubmitter struct{ artifacts []ReplayArtifact }

func (submitter *recordingReplaySubmitter) SubmitReplay(artifact ReplayArtifact) error {
	submitter.artifacts = append(submitter.artifacts, artifact)
	return nil
}
