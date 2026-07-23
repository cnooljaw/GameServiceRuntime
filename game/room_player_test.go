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

func TestRoomIndexesOnlyTrustedFactoryResults(t *testing.T) {
	factoryRef := gsr.ServiceRef{Node: "factory", ID: 1}
	factory := &roomTestFactory{}
	room, err := NewRoomService(RoomConfig{ID: "room-42", Capacity: 2, Factory: factory, FactoryRef: factoryRef})
	if err != nil {
		t.Fatal(err)
	}
	context := &roomPlayerTestContext{self: gsr.ServiceRef{Node: "room", ID: 1}}
	if err := room.Init(context); err != nil {
		t.Fatal(err)
	}
	for _, player := range []PlayerID{"alice", "bob"} {
		if err := room.Handle(&roomPlayerCommandContext{}, gsr.Command{ID: JoinRoomCommand, Payload: player}); err != nil {
			t.Fatalf("Join(%s) error = %v", player, err)
		}
	}
	request := BattleCreateRequest{RequestID: "create-42", Room: "room-42", Players: []PlayerID{"alice", "bob"}}
	if err := room.Handle(&roomPlayerCommandContext{}, gsr.Command{ID: StartRoomBattleCommand, Payload: request}); err != nil {
		t.Fatal(err)
	}
	if len(factory.requests) != 1 {
		t.Fatalf("factory requests = %#v", factory.requests)
	}
	created := BattleCreatedResult{RequestID: "create-42", Battle: "battle-42", Ref: gsr.ServiceRef{Node: "battle", ID: 1}}
	if err := room.Handle(&roomPlayerCommandContext{source: gsr.ServiceRef{Node: "other", ID: 1}}, gsr.Command{ID: ApplyBattleCreatedCommand, Payload: created}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("untrusted result error = %v, want ErrUnauthorized", err)
	}
	if err := room.Handle(&roomPlayerCommandContext{source: factoryRef}, gsr.Command{ID: ApplyBattleCreatedCommand, Payload: created}); err != nil {
		t.Fatal(err)
	}
	snapshot := room.snapshot()
	if snapshot.Battles["battle-42"] != created.Ref || len(snapshot.Members) != 2 {
		t.Fatalf("Room snapshot = %#v", snapshot)
	}
	if err := room.Handle(&roomPlayerCommandContext{source: created.Ref}, gsr.Command{ID: ApplyBattleFinishedCommand, Payload: BattleFinishedNotice{Battle: "battle-42", Ref: created.Ref}}); err != nil {
		t.Fatal(err)
	}
	if len(room.snapshot().Battles) != 0 {
		t.Fatal("finished Battle was retained by Room")
	}
}

func TestPlayerFencesOfflineGenerationAndOrdersModuleEvents(t *testing.T) {
	module := &playerTestModule{}
	player, err := NewPlayerService(PlayerConfig{Identity: SessionIdentity{Player: "alice", Account: "account-1"}, Modules: []PlayerModule{module}})
	if err != nil {
		t.Fatal(err)
	}
	serviceContext := &roomPlayerTestContext{self: gsr.ServiceRef{Node: "player", ID: 1}, now: time.Unix(1, 0)}
	if err := player.Init(serviceContext); err != nil {
		t.Fatal(err)
	}
	online := PlayerPresence{Identity: SessionIdentity{Player: "alice", Account: "account-1"}, Generation: "2"}
	if err := player.Handle(&roomPlayerCommandContext{}, gsr.Command{ID: SetPlayerOnlineCommand, Payload: online}); err != nil {
		t.Fatal(err)
	}
	if err := player.Handle(&roomPlayerCommandContext{}, gsr.Command{ID: SetPlayerOfflineCommand, Payload: PlayerPresence{Identity: online.Identity, Generation: "1"}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := player.snapshot(&roomPlayerCommandContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.State.Online || snapshot.State.Player != "alice" {
		t.Fatalf("late Offline changed Player state: %#v", snapshot.State)
	}
	if got := module.events; len(got) != 2 || got[0] != PlayerActivated || got[1] != PlayerOnline {
		t.Fatalf("module events = %#v", got)
	}
	snapshot.Modules["test"][0] = '!'
	again, err := player.snapshot(&roomPlayerCommandContext{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Modules["test"][0] == '!' {
		t.Fatal("PlayerSnapshot returned shared module bytes")
	}
}

type roomTestFactory struct{ requests []BattleCreateRequest }

func (f *roomTestFactory) RequestBattle(request BattleCreateRequest) error {
	f.requests = append(f.requests, request)
	return nil
}

type playerTestModule struct{ events []PlayerEventKind }

func (*playerTestModule) Name() string                            { return "test" }
func (*playerTestModule) Commands() []gsr.CommandID               { return nil }
func (*playerTestModule) Handle(PlayerContext, gsr.Command) error { return nil }
func (m *playerTestModule) HandleEvent(_ PlayerContext, event PlayerEvent) error {
	m.events = append(m.events, event.Kind)
	return nil
}
func (*playerTestModule) Snapshot(PlayerContext) ([]byte, error) { return []byte("module"), nil }

type roomPlayerTestContext struct {
	self gsr.ServiceRef
	now  time.Time
}

func (c *roomPlayerTestContext) Self() gsr.ServiceRef                        { return c.self }
func (*roomPlayerTestContext) Send(gsr.ServiceRef, gsr.CommandID, any) error { return nil }
func (*roomPlayerTestContext) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}
func (*roomPlayerTestContext) After(time.Duration, gsr.CommandID, any) (gsr.TimerID, error) {
	return 0, nil
}
func (c *roomPlayerTestContext) Now() time.Time { return c.now }
func (*roomPlayerTestContext) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
func (*roomPlayerTestContext) Metrics() gsr.Metrics { return roomPlayerMetrics{} }

type roomPlayerMetrics struct{}

func (roomPlayerMetrics) Inc(string)                    {}
func (roomPlayerMetrics) Add(string, uint64)            {}
func (roomPlayerMetrics) SetGauge(string, int64)        {}
func (roomPlayerMetrics) Observe(string, time.Duration) {}

type roomPlayerCommandContext struct {
	source gsr.ServiceRef
	reply  any
}

func (*roomPlayerCommandContext) Self() gsr.ServiceRef     { return gsr.ServiceRef{Node: "test", ID: 1} }
func (c *roomPlayerCommandContext) Source() gsr.ServiceRef { return c.source }
func (c *roomPlayerCommandContext) Reply(value any) error  { c.reply = value; return nil }
