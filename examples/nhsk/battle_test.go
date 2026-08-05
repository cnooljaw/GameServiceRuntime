package nhsk

import (
	"context"
	"log/slog"
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

func TestBattleRejectsOutOfTurnAndInvalidCardsWithoutMutation(t *testing.T) {
	service, err := NewBattleService(NHSKBattleConfig{ID: 8})
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
	service.hands[service.bySeat[0]] = []byte{1}
	service.hands[service.bySeat[2]] = []byte{}
	state := service.snapshot()
	if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: state.ActivePlayer, Cards: []byte{1}, VerifyCode: state.VerifyCode}}); err != nil {
		t.Fatal(err)
	}
	if service.phase != NHSKBattleAwaitingSettlement {
		t.Fatalf("phase after partner final card = %s", service.phase)
	}
	if err := service.Handle(ctx, gsr.Command{ID: CompleteSettlementCommand, Payload: CompleteSettlementRequest{Success: true, Scores: [4]int32{1, -1, 1, -1}}}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(SettlementCommandResult); !result.Accepted || service.phase != NHSKBattleFinished {
		t.Fatalf("settlement = %#v phase=%s", result, service.phase)
	}
}

func TestBattleAppliesSettlementMatrixAtomically(t *testing.T) {
	service, _ := newPlayingBattleForRestore(t, 19)
	service.phase = NHSKBattleAwaitingSettlement
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
			{PlayerID: 1, TeamID: 0},
			{PlayerID: 2, TeamID: 1},
			{PlayerID: 3, TeamID: 2},
			{PlayerID: 4, TeamID: 3},
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
	if len(output.sends) != 2 {
		t.Fatalf("pair finish outputs=%d, want out-card/show-cards", len(output.sends))
	}
	show := output.sends[1].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	if show.Kind != OutputShowCards || !reflect.DeepEqual(show.Targets, []game.PlayerID{"1", "2", "3", "4"}) {
		t.Fatalf("pair show output=%#v", show)
	}
	payload := show.Payload.(ShowCardsPayload)
	if payload.Cards[1][0] != 4 || payload.Cards[3][0] != 6 || payload.HandCounts[2] != 0 {
		t.Fatalf("pair hands=%#v counts=%v", payload.Cards, payload.HandCounts)
	}
}

func TestBattleForceFinishEmitsGameOverThenRoundOver(t *testing.T) {
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
	if len(output.sends) != 2 {
		t.Fatalf("force finish outputs = %d, want 2", len(output.sends))
	}
	gameOver, ok := output.sends[0].(GameOutputBatch).Outputs[0].(GameOverOutput)
	if !ok {
		t.Fatalf("first force finish output = %#v, want GameOverOutput", output.sends[0])
	}
	if gameOver.Reason != int32(GameOverReasonSuccess) || gameOver.IsGameOver {
		t.Fatalf("force finish GameOver = %#v, want success subgame", gameOver)
	}
	notice, ok := output.sends[1].(GameOutputBatch).Outputs[0].(NoticeRoundOverOutput)
	if !ok {
		t.Fatalf("second force finish output = %#v, want NoticeRoundOverOutput", output.sends[1])
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
	if len(output.sends) != 3 {
		t.Fatalf("reconnect output count = %d, want GameInfo/Scene/Ask", len(output.sends))
	}
	if _, ok := output.sends[0].(GameOutputBatch).Outputs[0].(ClientGameOutput); !ok {
		t.Fatalf("reconnect first output = %#v", output.sends[0])
	}
	first := output.sends[0].(GameOutputBatch).Outputs[0].(ClientGameOutput)
	if first.Kind != OutputGameInfo {
		t.Fatalf("reconnect first kind = %s, want %s", first.Kind, OutputGameInfo)
	}
	scene := output.sends[1].(GameOutputBatch).Outputs[0].(ClientGameOutput)
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
	if ask := output.sends[2].(GameOutputBatch).Outputs[0].(ClientGameOutput); ask.Kind != OutputAskOutCard {
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
	if len(output.sends) != 3 {
		t.Fatalf("scene output count = %d, want GameInfo/Scene/Ask", len(output.sends))
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

func newPlayingBattleForRestore(t *testing.T, id game.BattleID) (*NHSKBattleService, *recordingBattleTestServiceContext) {
	t.Helper()
	service, err := NewBattleService(NHSKBattleConfig{ID: id, MatchID: 1, ProductID: NHSKDescriptor.GameID, ConnectionGeneration: 1})
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

type battleTestCommandContext struct{ reply any }

func (*battleTestCommandContext) Self() gsr.ServiceRef    { return gsr.ServiceRef{Node: "test", ID: 1} }
func (*battleTestCommandContext) Source() gsr.ServiceRef  { return gsr.ServiceRef{} }
func (c *battleTestCommandContext) Reply(value any) error { c.reply = value; return nil }

type battleTestServiceContext struct{}

type recordingBattleTestServiceContext struct {
	sends []any
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
func (*recordingBattleTestServiceContext) After(time.Duration, gsr.CommandID, any) (gsr.TimerID, error) {
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
