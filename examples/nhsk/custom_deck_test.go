package nhsk

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestParseCustomDeckKeepsLegacyGrammarAndBanker(t *testing.T) {
	text := "{\n@2\n" + sequentialCustomDeckLine() + "\n}\n"
	catalog, err := ParseCustomDeck(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Decks) != 1 || len(catalog.Decks[0].Cards) != 104 {
		t.Fatalf("catalog = %#v", catalog)
	}
	if catalog.Decks[0].BankerSeat != 2 || catalog.Decks[0].Cards[0] != 0x01 || catalog.Decks[0].Cards[103] != 0x68 {
		t.Fatalf("deck = %#v", catalog.Decks[0])
	}
}

func TestParseCustomDeckIgnoresShortBlocksAndTruncatesLongBlocks(t *testing.T) {
	short := "{\n0x01,0x02\n}\n"
	long := "{\n" + customDeckValues(106) + "\n}\n"
	catalog, err := ParseCustomDeck(short + long)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Decks) != 1 || len(catalog.Decks[0].Cards) != 104 {
		t.Fatalf("catalog = %#v", catalog)
	}
	if got := catalog.Decks[0].Cards[103]; got != 0x68 {
		t.Fatalf("last card = %#x, want 0x68", got)
	}
}

func TestParseCustomDeckTreatsOutOfRangeBankerAsUnspecified(t *testing.T) {
	catalog, err := ParseCustomDeck("{\n@9\n" + sequentialCustomDeckLine() + "\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Decks) != 1 || catalog.Decks[0].BankerSeat != -1 {
		t.Fatalf("deck = %#v", catalog.Decks)
	}
}

func TestParseCustomDeckRejectsMalformedTokenAsWholeLoad(t *testing.T) {
	_, err := ParseCustomDeck("{\n" + sequentialCustomDeckLine() + ",not-a-card\n}\n")
	if err == nil {
		t.Fatal("malformed token should fail the whole load")
	}
}

func TestLocalFileCustomDeckProviderLoadsSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-deck.txt")
	if err := os.WriteFile(path, []byte("{\n@1\n"+sequentialCustomDeckLine()+"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := LocalFileCustomDeckProvider{Path: path}
	catalog, err := provider.Load(context.Background(), CustomDeckLookup{GameID: NHSKDescriptor.GameID, ProductID: NHSKDescriptor.GameID})
	if err != nil || len(catalog.Decks) != 1 || catalog.Decks[0].BankerSeat != 1 {
		t.Fatalf("catalog = %#v, err=%v", catalog, err)
	}
}

func TestCustomDeckRunnerAppliesAuthorizationBeforeProvider(t *testing.T) {
	provider := &countingCustomDeckProvider{catalog: CustomDeckCatalog{Decks: []CustomDeck{{Cards: sequentialCustomDeckBytes(), BankerSeat: 0}}}}
	runtime := &customDeckRecordingRuntime{sent: make(chan customDeckLoadResult, 2)}
	runner, err := NewCustomDeckRunner(runtime, provider, CustomDeckRunnerConfig{Enabled: true, AllowedAccounts: []uint32{2}, QueueSize: 1, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	request := CustomDeckLoadRequest{Target: gsr.ServiceRef{Node: "test", ID: 1}, BattleID: 403, GameNum: 1, SubgameNum: 1, Lookup: CustomDeckLookup{GameID: NHSKDescriptor.GameID, ProductID: NHSKDescriptor.GameID, UserIDs: []uint32{9}}}
	if err := runner.SubmitCustomDeck(request); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-runtime.sent:
		if result.Available {
			t.Fatalf("unauthorized result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("unauthorized result not sent")
	}
	if provider.loads != 0 {
		t.Fatalf("provider loads = %d, want 0", provider.loads)
	}
	request.Lookup.UserIDs = []uint32{2}
	if err := runner.SubmitCustomDeck(request); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-runtime.sent:
		if !result.Available || len(result.Catalog.Decks) != 1 {
			t.Fatalf("authorized result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("authorized result not sent")
	}
	if provider.loads != 1 {
		t.Fatalf("provider loads = %d, want 1", provider.loads)
	}
}

func TestCustomDeckRunnerTimeoutFallsBackWithoutBlockingBattle(t *testing.T) {
	runtime := &customDeckRecordingRuntime{sent: make(chan customDeckLoadResult, 1)}
	runner, err := NewCustomDeckRunner(runtime, blockingCustomDeckProvider{}, CustomDeckRunnerConfig{Enabled: true, AllowAnyAccount: true, LoadTimeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if err := runner.SubmitCustomDeck(CustomDeckLoadRequest{Target: gsr.ServiceRef{Node: "test", ID: 1}, BattleID: 404, GameNum: 1, SubgameNum: 1, Lookup: CustomDeckLookup{UserIDs: []uint32{1}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-runtime.sent:
		if result.Available {
			t.Fatalf("timeout result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout result not sent")
	}
}

func TestBattleWaitsForMatchingCustomDeckBeforeStart(t *testing.T) {
	requester := &recordingCustomDeckRequester{}
	battle, err := NewBattleService(NHSKBattleConfig{
		ID: 401, MatchID: 1, ProductID: NHSKDescriptor.GameID,
		Random: rand.New(rand.NewSource(7)), Clock: &nhskTestClock{now: time.Unix(1, 0)},
		CustomDeckRequester: requester,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := battle.Init(&recordingBattleTestServiceContext{}); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	players := []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}
	for _, command := range []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: 401, ProductID: NHSKDescriptor.GameID, MatchID: 1}}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: players}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
	} {
		if err := battle.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	if len(requester.requests) != 1 || requester.requests[0].GameNum != 1 || requester.requests[0].SubgameNum != 1 {
		t.Fatalf("custom deck requests = %#v", requester.requests)
	}
	if err := battle.Handle(ctx, gsr.Command{ID: StartSubgameCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(CommandResult); result.Accepted || result.Rejection != errCustomDeckPending.Error() {
		t.Fatalf("pending start reply = %#v", result)
	}
	requester.requests[0].Target = ctx.Self()
	if err := battle.Handle(ctx, gsr.Command{ID: applyCustomDeckResultCommand, Payload: customDeckLoadResult{
		Target: ctx.Self(), BattleID: 401, GameNum: 1, SubgameNum: 1,
		Available: true, Catalog: CustomDeckCatalog{Decks: []CustomDeck{{Cards: sequentialCustomDeckBytes(), BankerSeat: 2}}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := battle.Handle(ctx, gsr.Command{ID: StartSubgameCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	if battle.activeSeat != 2 || battle.hands[battle.bySeat[2]][0] != 0x01 || battle.hands[battle.bySeat[3]][0] != 0x1b {
		t.Fatalf("custom deal banker=%d hands=%#v", battle.activeSeat, battle.hands)
	}
}

func TestBattleIgnoresLateCustomDeckResultAfterStart(t *testing.T) {
	requester := &recordingCustomDeckRequester{}
	battle, _ := newBattleWithCustomRequester(t, 402, requester)
	ctx := &battleTestCommandContext{}
	if err := battle.Handle(ctx, gsr.Command{ID: applyCustomDeckResultCommand, Payload: customDeckLoadResult{
		Target: ctx.Self(), BattleID: 402, GameNum: 1, SubgameNum: 1,
		Available: true, Catalog: CustomDeckCatalog{Decks: []CustomDeck{{Cards: sequentialCustomDeckBytes(), BankerSeat: 3}}},
	}}); err != nil {
		t.Fatal(err)
	}
	if battle.activeSeat == 3 && battle.hands[battle.bySeat[3]][0] == 0x01 {
		t.Fatal("late custom deck result changed an already running subgame")
	}
}

type recordingCustomDeckRequester struct {
	requests []CustomDeckLoadRequest
}

type countingCustomDeckProvider struct {
	loads   int
	catalog CustomDeckCatalog
}

type blockingCustomDeckProvider struct{}

func (blockingCustomDeckProvider) Load(ctx context.Context, _ CustomDeckLookup) (CustomDeckCatalog, error) {
	<-ctx.Done()
	return CustomDeckCatalog{}, context.Cause(ctx)
}

func (provider *countingCustomDeckProvider) Load(context.Context, CustomDeckLookup) (CustomDeckCatalog, error) {
	provider.loads++
	return provider.catalog, nil
}

type customDeckRecordingRuntime struct {
	sent chan customDeckLoadResult
}

func (runtime *customDeckRecordingRuntime) Send(_ gsr.ServiceRef, command gsr.CommandID, payload any) error {
	if command != applyCustomDeckResultCommand {
		return errInvalidCustomDeckRequest
	}
	runtime.sent <- payload.(customDeckLoadResult)
	return nil
}

func (*customDeckRecordingRuntime) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, errInvalidCustomDeckRequest
}

func (requester *recordingCustomDeckRequester) SubmitCustomDeck(request CustomDeckLoadRequest) error {
	requester.requests = append(requester.requests, request)
	return nil
}

func newBattleWithCustomRequester(t *testing.T, id game.BattleID, requester CustomDeckRequester) (*NHSKBattleService, *recordingCustomDeckRequester) {
	t.Helper()
	battle, err := NewBattleService(NHSKBattleConfig{ID: id, MatchID: 1, ProductID: NHSKDescriptor.GameID, Random: rand.New(rand.NewSource(8)), Clock: &nhskTestClock{now: time.Unix(1, 0)}, CustomDeckRequester: requester})
	if err != nil {
		t.Fatal(err)
	}
	if err := battle.Init(&recordingBattleTestServiceContext{}); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	players := []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}
	for _, command := range []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: id, ProductID: NHSKDescriptor.GameID, MatchID: 1}}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: players}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
	} {
		if err := battle.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	requesterRecord, _ := requester.(*recordingCustomDeckRequester)
	// A missing catalog is a legitimate provider result and must fall back to ordinary dealing.
	if err := battle.Handle(ctx, gsr.Command{ID: applyCustomDeckResultCommand, Payload: customDeckLoadResult{Target: ctx.Self(), BattleID: id, GameNum: 1, SubgameNum: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := battle.Handle(ctx, gsr.Command{ID: StartSubgameCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	return battle, requesterRecord
}

func sequentialCustomDeckLine() string { return customDeckValues(104) }

func customDeckValues(count int) string {
	values := make([]byte, count)
	for index := range values {
		values[index] = byte(index + 1)
	}
	parts := make([]byte, 0, count*5)
	for index, value := range values {
		if index > 0 {
			parts = append(parts, ',', ' ')
		}
		parts = append(parts, []byte("0x")...)
		parts = append(parts, "0123456789abcdef"[value>>4], "0123456789abcdef"[value&0xf])
	}
	return string(parts)
}

func sequentialCustomDeckBytes() []byte {
	values := make([]byte, 104)
	for index := range values {
		values[index] = byte(index + 1)
	}
	return values
}
