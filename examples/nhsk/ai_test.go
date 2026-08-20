package nhsk

import (
	"context"
	"errors"
	mathrand "math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestLocalAIProviderLeadsSmallestSingleAndPassesWhenFollowing(t *testing.T) {
	request := validLegacyAIRequest()
	request.Scene.Hand = []byte{0x12, 0x03, 0x13, 0x01}
	cards, err := (LocalAIProvider{}).Move(context.Background(), request)
	if err != nil || !reflect.DeepEqual(cards, []byte{0x03}) {
		t.Fatalf("lead = %x, %v", cards, err)
	}
	request.Scene.Leading = false
	cards, err = (LocalAIProvider{}).Move(context.Background(), request)
	if err != nil || len(cards) != 0 {
		t.Fatalf("follow = %x, %v", cards, err)
	}
}

func TestAIRunnerOwnsRequestAndReportsExactOpportunity(t *testing.T) {
	runtime := &aiTestRuntime{sent: make(chan aiResult, 1)}
	provider := &recordingAIProvider{requests: make(chan AIRequest, 1)}
	runner, err := NewAIRunner(runtime, provider)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	request := validLegacyAIRequest()
	request.Scene.OutedCards = [][]byte{{4}}
	if err := runner.SubmitAI(request); err != nil {
		t.Fatal(err)
	}
	request.Scene.Hand[0] = 99
	request.Scene.OutedCards[0][0] = 99
	select {
	case got := <-provider.requests:
		if got.Scene.Hand[0] != 3 || got.Scene.OutedCards[0][0] != 4 {
			t.Fatalf("provider received aliased request: %#v", got.Scene)
		}
	case <-time.After(time.Second):
		t.Fatal("provider did not receive request")
	}
	select {
	case got := <-runtime.sent:
		if got.BattleID != 1 || got.GameNum != 1 || got.SubgameNum != 1 || got.UserID != 1 || got.SeatID != 0 || got.TurnRevision != 1 || got.VerifyCode != 1 || !reflect.DeepEqual(got.Cards, []byte{3}) {
			t.Fatalf("result = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not receive result")
	}
}

func TestBattleRobotUsesSingleAIHardDeadlineAndAppliesValidResult(t *testing.T) {
	rules := DefaultNHSKConfig()
	ai := &recordingAISubmitter{}
	service, output := newBattleForTestWithAI(t, 51, &rules, [4]bool{true, true, true, true}, ai)
	if len(ai.requests) != 1 {
		t.Fatalf("AI requests = %d", len(ai.requests))
	}
	request := ai.requests[0]
	if len(output.timers) == 0 || output.timers[len(output.timers)-1].delay != 6*time.Second {
		t.Fatalf("timer = %#v", output.timers)
	}
	before := len(service.hands[service.bySeat[service.activeSeat]])
	result := aiResult{BattleID: request.BattleID, GameNum: request.GameNum, SubgameNum: request.SubgameNum, UserID: request.UserID, SeatID: request.SeatID, TurnRevision: request.TurnRevision, VerifyCode: request.VerifyCode, StartedAt: request.StartedAt, Cards: []byte{request.Scene.Hand[0]}}
	if err := service.Handle(&battleTestCommandContext{}, gsr.Command{ID: applyAIResultCommand, Payload: result}); err != nil {
		t.Fatal(err)
	}
	if got := len(service.hands[service.bySeat[int(request.SeatID)]]); got != before-1 {
		t.Fatalf("hand count = %d, want %d", got, before-1)
	}
	moves := replayOutCardMoves(service.replayDocument.Moves())
	if len(moves) != 1 || moves[0].Source != ReplayMoveSourceAI || service.autoCount[request.SeatID] != 0 {
		t.Fatalf("moves/auto = %#v/%v", moves, service.autoCount)
	}
}

func TestBattleOfflineAIResultReplacesHardDeadlineWithMinimumDelay(t *testing.T) {
	rules := DefaultNHSKConfig()
	rules.OfflineAutoUsesAI = true
	clock := &nhskTestClock{now: time.Unix(10, 0)}
	ai := &recordingAISubmitter{}
	service, output := newBattleForTestWithAIClock(t, 52, &rules, [4]bool{}, ai, clock)
	player := service.bySeat[service.activeSeat]
	service.offline[player], service.auto[player] = true, true
	if err := service.startAction(&battleTestCommandContext{}, rules.MsOutCard); err != nil {
		t.Fatal(err)
	}
	request := ai.requests[len(ai.requests)-1]
	clock.now = request.StartedAt.Add(200 * time.Millisecond)
	result := aiResult{BattleID: request.BattleID, GameNum: request.GameNum, SubgameNum: request.SubgameNum, UserID: request.UserID, SeatID: request.SeatID, TurnRevision: request.TurnRevision, VerifyCode: request.VerifyCode, StartedAt: request.StartedAt, Cards: []byte{request.Scene.Hand[0]}}
	if err := service.Handle(&battleTestCommandContext{}, gsr.Command{ID: applyAIResultCommand, Payload: result}); err != nil {
		t.Fatal(err)
	}
	timer := output.timers[len(output.timers)-1]
	deadline := timer.payload.(actionDeadline)
	if timer.delay != 800*time.Millisecond || deadline.Source != ReplayMoveSourceAI || !reflect.DeepEqual(deadline.Cards, result.Cards) {
		t.Fatalf("replacement timer = %#v", timer)
	}
	before := len(service.hands[player])
	if err := service.Handle(&battleTestCommandContext{}, gsr.Command{ID: nhskBattleTimerCommand, Payload: deadline}); err != nil {
		t.Fatal(err)
	}
	if len(service.hands[player]) != before-1 || service.autoCount[request.SeatID] != 1 {
		t.Fatalf("hand/auto = %d/%d", len(service.hands[player]), service.autoCount[request.SeatID])
	}
}

type recordingAISubmitter struct{ requests []AIRequest }

func (submitter *recordingAISubmitter) SubmitAI(request AIRequest) error {
	submitter.requests = append(submitter.requests, cloneAIRequest(request))
	return nil
}

type recordingAIProvider struct{ requests chan AIRequest }

func (provider *recordingAIProvider) Move(_ context.Context, request AIRequest) ([]byte, error) {
	provider.requests <- request
	return []byte{request.Scene.Hand[0]}, nil
}

type aiTestRuntime struct{ sent chan aiResult }

func (runtime *aiTestRuntime) Send(_ gsr.ServiceRef, command gsr.CommandID, payload any) error {
	if command != applyAIResultCommand {
		return errors.New("unexpected command")
	}
	runtime.sent <- payload.(aiResult)
	return nil
}

func (*aiTestRuntime) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, errors.New("unexpected call")
}

func newBattleForTestWithAI(t *testing.T, id game.BattleID, rules *NHSKConfig, automated [4]bool, ai AISubmitter) (*NHSKBattleService, *recordingBattleTestServiceContext) {
	return newBattleForTestWithAIClock(t, id, rules, automated, ai, &nhskTestClock{now: time.Unix(1, 0)})
}

func newBattleForTestWithAIClock(t *testing.T, id game.BattleID, rules *NHSKConfig, automated [4]bool, ai AISubmitter, clock NHSKClock) (*NHSKBattleService, *recordingBattleTestServiceContext) {
	t.Helper()
	service, err := NewBattleService(NHSKBattleConfig{ID: id, MatchID: 1, ProductID: NHSKDescriptor.GameID, ConnectionGeneration: 1, Random: mathrand.New(mathrand.NewSource(3)), Clock: clock, AISubmitter: ai})
	if err != nil {
		t.Fatal(err)
	}
	output := &recordingBattleTestServiceContext{}
	if err := service.Init(output); err != nil {
		t.Fatal(err)
	}
	players := make([]BattlePlayer, 4)
	for seat := range players {
		players[seat] = BattlePlayer{Player: game.PlayerID(string(rune('1' + seat))), UserID: uint32(seat + 1), SeatID: uint8(seat), Automated: automated[seat]}
	}
	ctx := &battleTestCommandContext{}
	for _, command := range []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: id, ProductID: NHSKDescriptor.GameID, MatchID: 1}, Rules: rules}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: players}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
		{ID: StartSubgameCommand, Payload: struct{}{}},
	} {
		if err := service.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	return service, output
}
