package nhsk

import (
	"context"
	"log/slog"
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

type battleTestCommandContext struct{ reply any }

func (*battleTestCommandContext) Self() gsr.ServiceRef    { return gsr.ServiceRef{Node: "test", ID: 1} }
func (*battleTestCommandContext) Source() gsr.ServiceRef  { return gsr.ServiceRef{} }
func (c *battleTestCommandContext) Reply(value any) error { c.reply = value; return nil }

type battleTestServiceContext struct{}

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
