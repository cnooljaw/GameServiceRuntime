package main

import (
	"context"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestWhackMoleSendStartThenCallKick(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "whack-test", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	battle, err := game.NewBattleService(game.BattleConfig{ID: "battle-42", Participants: []game.Participant{{Player: "alice"}}, Logic: newWhackMoleLogic(7)})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: battle})
	if err != nil {
		t.Fatal(err)
	}
	if err := startWhackMole(runtime, ref); err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Call(context.Background(), ref, KickCommand, KickRequest{Player: "alice", Shrew: 1, Epoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(KickResult)
	if !ok || !result.Hit || result.Score != 1 {
		t.Fatalf("Kick result = %#v, want hit score 1", value)
	}
}

func TestWhackMoleKickHitsOnlyOnceAndFinishesThroughBattleContext(t *testing.T) {
	logic := newWhackMoleLogic(7)
	ctx := &whackTestContext{battle: "battle-42", epoch: 1}
	if err := logic.HandleBattle(ctx, gsr.Command{ID: StartCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	if len(ctx.timeline.after) != 1 || logic.shrews[1] != ShrewVisible {
		t.Fatalf("Start = %#v", logic.shrews)
	}
	kick := KickRequest{Player: "alice", Shrew: 1, Epoch: 1}
	if err := logic.HandleBattle(ctx, gsr.Command{ID: KickCommand, Payload: kick}); err != nil {
		t.Fatal(err)
	}
	if result, ok := ctx.reply.(KickResult); !ok || !result.Hit || result.Score != 1 {
		t.Fatalf("first Kick = %#v", ctx.reply)
	}
	ctx.reply = nil
	if err := logic.HandleBattle(ctx, gsr.Command{ID: KickCommand, Payload: kick}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(KickResult); result.Hit || result.Score != 1 {
		t.Fatalf("second Kick = %#v", result)
	}
	if err := logic.HandleBattle(ctx, gsr.Command{ID: FinishCommand, Payload: game.RequestID("finish-42")}); err != nil {
		t.Fatal(err)
	}
	if ctx.finish.RequestID != "finish-42" || len(ctx.finish.Settlements) != 1 || ctx.finish.Settlements[0].Entries[0].Delta != 1 {
		t.Fatalf("Finish = %#v", ctx.finish)
	}
}

type whackTestTimeline struct{ after []any }

func (t *whackTestTimeline) After(_ time.Duration, _ gsr.CommandID, payload any) (game.TimelineID, error) {
	t.after = append(t.after, payload)
	return game.TimelineID(len(t.after)), nil
}
func (*whackTestTimeline) At(time.Time, gsr.CommandID, any) (game.TimelineID, error) { return 0, nil }
func (*whackTestTimeline) Replace(game.TimelineID, time.Duration, gsr.CommandID, any) (game.TimelineRevision, error) {
	return 0, nil
}
func (*whackTestTimeline) Cancel(game.TimelineID) bool     { return false }
func (*whackTestTimeline) Snapshot() game.TimelineSnapshot { return game.TimelineSnapshot{} }

type whackTestContext struct {
	battle   game.BattleID
	epoch    game.BattleEpoch
	timeline whackTestTimeline
	reply    any
	finish   game.FinishBattle
}

func (*whackTestContext) Self() gsr.ServiceRef                    { return gsr.ServiceRef{Node: "battle", ID: 1} }
func (*whackTestContext) Source() gsr.ServiceRef                  { return gsr.ServiceRef{} }
func (c *whackTestContext) Reply(value any) error                 { c.reply = value; return nil }
func (c *whackTestContext) BattleID() game.BattleID               { return c.battle }
func (c *whackTestContext) Epoch() game.BattleEpoch               { return c.epoch }
func (*whackTestContext) Now() time.Time                          { return time.Unix(1, 0) }
func (c *whackTestContext) Timeline() game.Timeline               { return &c.timeline }
func (c *whackTestContext) Finish(finish game.FinishBattle) error { c.finish = finish; return nil }
func (*whackTestContext) Broadcast(gsr.CommandID, any) (game.BroadcastResult, error) {
	return game.BroadcastResult{}, nil
}
func (*whackTestContext) Send(gsr.ServiceRef, gsr.CommandID, any) error { return nil }
