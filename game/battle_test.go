package game

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	commandBattleTestSchedule gsr.CommandID = 0x04010001
	commandBattleTestFired    gsr.CommandID = 0x04010002
	commandBattleTestReply    gsr.CommandID = 0x04010003
	commandBattleTestCapture  gsr.CommandID = 0x04010004
	commandBattleTestPanic    gsr.CommandID = 0x04010005
)

func TestBattleContextReplyAllowsSend(t *testing.T) {
	serviceContext := &battleTestServiceContext{self: gsr.ServiceRef{Node: "battle-node", ID: 1}, now: time.Unix(100, 0)}
	logic := &contextTestLogic{}
	battle, err := NewBattleService(BattleConfig{ID: 42, Participants: []Participant{{Player: "alice"}}, Logic: logic})
	if err != nil {
		t.Fatal(err)
	}
	if err := battle.Init(serviceContext); err != nil {
		t.Fatal(err)
	}
	if err := battle.Handle(&battleTestCommandContext{replyErr: gsr.ErrReplyUnavailable}, gsr.Command{ID: StartBattleCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	if err := battle.Handle(&battleTestCommandContext{replyErr: gsr.ErrReplyUnavailable}, gsr.Command{ID: commandBattleTestReply}); err != nil {
		t.Fatalf("Reply during Send-style Command error = %v", err)
	}
}

func TestBattleContextRejectsEffectsAfterHandler(t *testing.T) {
	target := gsr.ServiceRef{Node: "player-node", ID: 1}
	serviceContext := &battleTestServiceContext{self: gsr.ServiceRef{Node: "battle-node", ID: 1}, now: time.Unix(100, 0)}
	logic := &contextTestLogic{}
	battle, err := NewBattleService(BattleConfig{ID: 42, Participants: []Participant{{Player: "alice", Ref: target}}, Logic: logic})
	if err != nil {
		t.Fatal(err)
	}
	if err := battle.Init(serviceContext); err != nil {
		t.Fatal(err)
	}
	if err := battle.Handle(&battleTestCommandContext{}, gsr.Command{ID: StartBattleCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	if err := battle.Handle(&battleTestCommandContext{}, gsr.Command{ID: commandBattleTestCapture}); err != nil {
		t.Fatal(err)
	}
	if logic.context == nil {
		t.Fatal("BattleLogic did not receive a Context")
	}
	if err := logic.context.Reply("late"); !errors.Is(err, ErrContextExpired) {
		t.Fatalf("late Reply error = %v, want ErrContextExpired", err)
	}
	if err := logic.context.Send(target, commandBattleTestSchedule, nil); !errors.Is(err, ErrContextExpired) {
		t.Fatalf("late Send error = %v, want ErrContextExpired", err)
	}
	if _, err := logic.context.Broadcast(commandBattleTestSchedule, nil); !errors.Is(err, ErrContextExpired) {
		t.Fatalf("late Broadcast error = %v, want ErrContextExpired", err)
	}
	if _, err := logic.context.Timeline().After(time.Second, commandBattleTestSchedule, nil); !errors.Is(err, ErrContextExpired) {
		t.Fatalf("late Timeline.After error = %v, want ErrContextExpired", err)
	}
	if err := logic.context.Finish(FinishBattle{RequestID: "finish-42"}); !errors.Is(err, ErrContextExpired) {
		t.Fatalf("late Finish error = %v, want ErrContextExpired", err)
	}
	if len(serviceContext.sent) != 0 || len(serviceContext.after) != 0 {
		t.Fatalf("expired Context produced effects: sends=%#v timers=%#v", serviceContext.sent, serviceContext.after)
	}
}

func TestBattleContextRejectsEffectsAfterHandlerPanic(t *testing.T) {
	target := gsr.ServiceRef{Node: "player-node", ID: 1}
	serviceContext := &battleTestServiceContext{self: gsr.ServiceRef{Node: "battle-node", ID: 1}, now: time.Unix(100, 0)}
	logic := &contextTestLogic{}
	battle, err := NewBattleService(BattleConfig{ID: 42, Participants: []Participant{{Player: "alice", Ref: target}}, Logic: logic})
	if err != nil {
		t.Fatal(err)
	}
	if err := battle.Init(serviceContext); err != nil {
		t.Fatal(err)
	}
	if err := battle.Handle(&battleTestCommandContext{}, gsr.Command{ID: StartBattleCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("BattleLogic panic was not propagated")
			}
		}()
		_ = battle.Handle(&battleTestCommandContext{}, gsr.Command{ID: commandBattleTestPanic})
	}()
	if logic.context == nil {
		t.Fatal("BattleLogic did not receive a Context")
	}
	if err := logic.context.Send(target, commandBattleTestSchedule, nil); !errors.Is(err, ErrContextExpired) {
		t.Fatalf("late Send after panic error = %v, want ErrContextExpired", err)
	}
	if len(serviceContext.sent) != 0 {
		t.Fatalf("expired Context produced sends after panic: %#v", serviceContext.sent)
	}
}

func TestBattleTimelineFencesCancelledAndReplacedTimers(t *testing.T) {
	clock := time.Unix(100, 0)
	serviceContext := &battleTestServiceContext{self: gsr.ServiceRef{Node: "battle-node", ID: 1}, now: clock}
	logic := &timelineTestLogic{}
	battle, err := NewBattleService(BattleConfig{ID: 42, Participants: []Participant{{Player: "alice"}}, Logic: logic})
	if err != nil {
		t.Fatal(err)
	}
	if err := battle.Init(serviceContext); err != nil {
		t.Fatal(err)
	}
	if err := battle.Handle(&battleTestCommandContext{}, gsr.Command{ID: StartBattleCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	if err := battle.Handle(&battleTestCommandContext{}, gsr.Command{ID: commandBattleTestSchedule, Payload: "first"}); err != nil {
		t.Fatal(err)
	}
	if len(serviceContext.after) != 1 {
		t.Fatalf("After calls = %d, want 1", len(serviceContext.after))
	}
	first := serviceContext.after[0].payload.(timelineFire)
	inactive := false
	if _, err := (timelineHandle{timeline: battle.timeline, active: &inactive}).Replace(first.ID, time.Second, commandBattleTestFired, "replacement"); err == nil {
		t.Fatal("Timeline.Replace outside active Battle Handle succeeded")
	}
	if err := battle.Handle(&battleTestCommandContext{}, gsr.Command{ID: commandBattleTestSchedule, Payload: "replace"}); err != nil {
		t.Fatal(err)
	}
	if len(serviceContext.after) != 2 {
		t.Fatalf("After calls after Replace = %d, want 2", len(serviceContext.after))
	}
	replaced := serviceContext.after[1].payload.(timelineFire)
	if err := battle.Handle(&battleTestCommandContext{}, gsr.Command{ID: TimelineFireCommand, Payload: first}); err != nil {
		t.Fatal(err)
	}
	if logic.fired != 0 {
		t.Fatalf("old timer reached Logic: %d", logic.fired)
	}
	if err := battle.Handle(&battleTestCommandContext{}, gsr.Command{ID: TimelineFireCommand, Payload: replaced}); err != nil {
		t.Fatal(err)
	}
	if logic.fired != 1 || logic.lastPayload != "replacement" {
		t.Fatalf("Logic fire = %d, payload=%#v", logic.fired, logic.lastPayload)
	}
	snapshot, err := battle.snapshot(&battleTestCommandContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Timeline.Items) != 1 || snapshot.Timeline.Items[0].State != TimelineFired || snapshot.Timeline.Items[0].Revision != 2 {
		t.Fatalf("Timeline snapshot = %#v", snapshot.Timeline)
	}
}

func TestBattleFinishUsesSelfAsWalletSourceAndWaitsForResult(t *testing.T) {
	serviceContext := &battleTestServiceContext{self: gsr.ServiceRef{Node: "battle-node", ID: 1}, now: time.Unix(100, 0)}
	wallet := gsr.ServiceRef{Node: "wallet-node", ID: 2}
	battle, err := NewBattleService(BattleConfig{ID: 42, Participants: []Participant{{Player: "alice"}}, Wallet: wallet, Logic: &timelineTestLogic{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := battle.Init(serviceContext); err != nil {
		t.Fatal(err)
	}
	if err := battle.Handle(&battleTestCommandContext{}, gsr.Command{ID: StartBattleCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	finish := FinishBattle{RequestID: "finish-42", Settlements: []SettlementIntent{{RequestID: "settle-42", Currency: "coin", Entries: []SettlementEntry{{Player: "alice", Delta: 10}}}}}
	if err := battle.Handle(&battleTestCommandContext{}, gsr.Command{ID: FinishBattleCommand, Payload: finish}); err != nil {
		t.Fatal(err)
	}
	if battle.phase != BattleSettling || len(serviceContext.sent) != 1 || serviceContext.sent[0].target != wallet || serviceContext.sent[0].command != CommitSettlementCommand {
		t.Fatalf("Finish = phase=%s sends=%#v", battle.phase, serviceContext.sent)
	}
	request := serviceContext.sent[0].payload.(SettlementRequest)
	if request.Source != serviceContext.self || request.RequestID != "settle-42" {
		t.Fatalf("Wallet request = %#v", request)
	}
	if err := battle.Handle(&battleTestCommandContext{source: wallet}, gsr.Command{ID: ApplySettlementResultCommand, Payload: SettlementResult{RequestID: "settle-42", State: SettlementCommitted, Currency: "coin", Balances: []Balance{{Player: "alice", Currency: "coin", Amount: 10}}}}); err != nil {
		t.Fatal(err)
	}
	if battle.phase != BattleFinished {
		t.Fatalf("phase after committed result = %s, want finished", battle.phase)
	}
	if err := battle.Handle(&battleTestCommandContext{source: wallet}, gsr.Command{ID: ApplySettlementResultCommand, Payload: SettlementResult{RequestID: "settle-42", State: SettlementRejected, Currency: "coin", Reason: "late"}}); err != nil {
		t.Fatal(err)
	}
	if battle.phase != BattleFinished {
		t.Fatalf("late result changed terminal phase = %s", battle.phase)
	}
}

type timelineTestLogic struct {
	fired       int
	lastPayload any
}

func (l *timelineTestLogic) HandleBattle(ctx BattleContext, command gsr.Command) error {
	switch command.ID {
	case commandBattleTestSchedule:
		if command.Payload == "replace" {
			_, err := ctx.Timeline().Replace(1, time.Second, commandBattleTestFired, "replacement")
			return err
		}
		_, err := ctx.Timeline().After(time.Second, commandBattleTestFired, command.Payload)
		return err
	case commandBattleTestFired:
		l.fired++
		l.lastPayload = command.Payload
		return nil
	default:
		return ErrInvalidCommand
	}
}
func (*timelineTestLogic) Snapshot(BattleContext) ([]byte, error) { return []byte("state"), nil }

type contextTestLogic struct{ context BattleContext }

func (l *contextTestLogic) HandleBattle(ctx BattleContext, command gsr.Command) error {
	switch command.ID {
	case commandBattleTestReply:
		return ctx.Reply("accepted")
	case commandBattleTestCapture:
		l.context = ctx
		return nil
	case commandBattleTestPanic:
		l.context = ctx
		panic("battle context test panic")
	default:
		return ErrInvalidCommand
	}
}
func (*contextTestLogic) Snapshot(BattleContext) ([]byte, error) { return nil, nil }

type battleTestServiceContext struct {
	self  gsr.ServiceRef
	now   time.Time
	after []battleTestAfter
	sent  []battleTestSend
}
type battleTestAfter struct{ payload any }
type battleTestSend struct {
	target  gsr.ServiceRef
	command gsr.CommandID
	payload any
}

func (c *battleTestServiceContext) Self() gsr.ServiceRef { return c.self }
func (c *battleTestServiceContext) Send(target gsr.ServiceRef, command gsr.CommandID, payload any) error {
	c.sent = append(c.sent, battleTestSend{target: target, command: command, payload: payload})
	return nil
}
func (*battleTestServiceContext) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}
func (c *battleTestServiceContext) After(_ time.Duration, _ gsr.CommandID, payload any) (gsr.TimerID, error) {
	c.after = append(c.after, battleTestAfter{payload: payload})
	return gsr.TimerID(len(c.after)), nil
}
func (c *battleTestServiceContext) Now() time.Time { return c.now }
func (*battleTestServiceContext) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
func (*battleTestServiceContext) Metrics() gsr.Metrics { return battleTestMetrics{} }

type battleTestMetrics struct{}

func (battleTestMetrics) Inc(string)                    {}
func (battleTestMetrics) Add(string, uint64)            {}
func (battleTestMetrics) SetGauge(string, int64)        {}
func (battleTestMetrics) Observe(string, time.Duration) {}

type battleTestCommandContext struct {
	source   gsr.ServiceRef
	reply    any
	replyErr error
}

func (*battleTestCommandContext) Self() gsr.ServiceRef {
	return gsr.ServiceRef{Node: "battle-node", ID: 1}
}
func (c *battleTestCommandContext) Source() gsr.ServiceRef { return c.source }
func (c *battleTestCommandContext) Reply(value any) error {
	c.reply = value
	return c.replyErr
}
