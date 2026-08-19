package nhsk

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestRedisCustomDeckProviderUsesProductKeyBeforeGameKey(t *testing.T) {
	getter := &recordingRedisStringGetter{values: map[string]redisStringValue{
		"game:makecard:100": {value: "{\n@2\n" + sequentialCustomDeckLine() + "\n}\n", found: true},
		"game:makecard:82":  {value: "{\n@1\n" + sequentialCustomDeckLine() + "\n}\n", found: true},
	}}
	provider := RedisCustomDeckProvider{Getter: getter}
	catalog, err := provider.Load(context.Background(), CustomDeckLookup{GameID: 82, ProductID: 100})
	if err != nil || len(catalog.Decks) != 1 || catalog.Decks[0].BankerSeat != 2 {
		t.Fatalf("catalog = %#v, err=%v", catalog, err)
	}
	if len(getter.keys) != 1 || getter.keys[0] != "game:makecard:100" {
		t.Fatalf("redis keys = %#v", getter.keys)
	}
}

func TestRedisCustomDeckProviderFallsBackToGameKeyOnEmptyProductValue(t *testing.T) {
	getter := &recordingRedisStringGetter{values: map[string]redisStringValue{
		"game:makecard:100": {value: "", found: true},
		"game:makecard:82":  {value: "{\n@1\n" + sequentialCustomDeckLine() + "\n}\n", found: true},
	}}
	provider := RedisCustomDeckProvider{Getter: getter}
	catalog, err := provider.Load(context.Background(), CustomDeckLookup{GameID: 82, ProductID: 100})
	if err != nil || len(catalog.Decks) != 1 || catalog.Decks[0].BankerSeat != 1 {
		t.Fatalf("catalog = %#v, err=%v", catalog, err)
	}
	if len(getter.keys) != 2 || getter.keys[1] != "game:makecard:82" {
		t.Fatalf("redis keys = %#v", getter.keys)
	}
}

func TestRedisCustomDeckProviderDoesNotHideProductParseError(t *testing.T) {
	getter := &recordingRedisStringGetter{values: map[string]redisStringValue{
		"game:makecard:100": {value: "{\nnot-a-card\n}\n", found: true},
		"game:makecard:82":  {value: "{\n@1\n" + sequentialCustomDeckLine() + "\n}\n", found: true},
	}}
	provider := RedisCustomDeckProvider{Getter: getter}
	if _, err := provider.Load(context.Background(), CustomDeckLookup{GameID: 82, ProductID: 100}); err == nil {
		t.Fatal("product parse error should fail the load")
	}
	if len(getter.keys) != 1 {
		t.Fatalf("redis keys after parse error = %#v", getter.keys)
	}
}

func TestTCPRedisStringGetterReadsRESPBulkValue(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	serverErr := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(server)
		command, readErr := readRESPTestCommand(reader)
		if readErr != nil {
			serverErr <- readErr
			return
		}
		if len(command) != 2 || strings.ToUpper(command[0]) != "GET" || command[1] != "game:makecard:100" {
			serverErr <- fmt.Errorf("command = %#v", command)
			return
		}
		value := sequentialCustomDeckLine()
		_, writeErr := fmt.Fprintf(server, "$%d\r\n%s\r\n", len(value), value)
		serverErr <- writeErr
	}()

	getter := TCPRedisStringGetter{Address: "redis-test", Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
	value, found, err := getter.Get(context.Background(), "game:makecard:100")
	if err != nil || !found || value != sequentialCustomDeckLine() {
		t.Fatalf("value=%q found=%v err=%v", value, found, err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func readRESPTestCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("array line = %q", line)
	}
	command := make([]string, 0, count)
	for index := 0; index < count; index++ {
		lengthLine, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		length, err := strconv.Atoi(strings.TrimSpace(lengthLine[1:]))
		if err != nil || !strings.HasPrefix(lengthLine, "$") {
			return nil, fmt.Errorf("bulk line = %q", lengthLine)
		}
		value := make([]byte, length+2)
		if _, err := io.ReadFull(reader, value); err != nil {
			return nil, err
		}
		command = append(command, string(value[:length]))
	}
	return command, nil
}

func TestCustomDeckRunnerAppliesAuthorizationBeforeProvider(t *testing.T) {
	provider := &countingCustomDeckProvider{catalog: CustomDeckCatalog{Decks: []CustomDeck{{Cards: sequentialCustomDeckBytes(), BankerSeat: 0}}}}
	runtime := &customDeckRecordingRuntime{sent: make(chan ProvideCustomDeckRequest, 1)}
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
		t.Fatalf("unauthorized provision = %#v", result)
	case <-time.After(20 * time.Millisecond):
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
		if result.BattleID != 403 || len(result.Catalog.Decks) != 1 {
			t.Fatalf("authorized provision = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("authorized provision not sent")
	}
	if provider.loads != 1 {
		t.Fatalf("provider loads = %d, want 1", provider.loads)
	}
}

func TestCustomDeckRunnerTimeoutFallsBackWithoutBlockingBattle(t *testing.T) {
	runtime := &customDeckRecordingRuntime{sent: make(chan ProvideCustomDeckRequest, 1)}
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
		t.Fatalf("timeout provision = %#v", result)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCustomDeckRunnerLoadAndProvideUsesPublicCommand(t *testing.T) {
	runtime := &customDeckRecordingRuntime{sent: make(chan ProvideCustomDeckRequest, 1)}
	runner, err := NewCustomDeckRunner(runtime, &countingCustomDeckProvider{catalog: CustomDeckCatalog{Decks: []CustomDeck{{Cards: sequentialCustomDeckBytes(), BankerSeat: 1}}}}, CustomDeckRunnerConfig{Enabled: true, AllowAnyAccount: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	request := CustomDeckLoadRequest{Target: gsr.ServiceRef{Node: "test", ID: 1}, BattleID: 405, GameNum: 1, SubgameNum: 1, Lookup: CustomDeckLookup{UserIDs: []uint32{1}}}
	if err := runner.LoadAndProvide(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	select {
	case provision := <-runtime.sent:
		if provision.BattleID != request.BattleID || provision.GameNum != request.GameNum || provision.SubgameNum != request.SubgameNum || len(provision.Catalog.Decks) != 1 {
			t.Fatalf("provision = %#v", provision)
		}
	case <-time.After(time.Second):
		t.Fatal("public provision not sent")
	}
}

func TestBattleAcceptsExternalCustomDeckProvision(t *testing.T) {
	const battleID game.BattleID = 403
	battle, err := NewBattleService(NHSKBattleConfig{ID: battleID, MatchID: 1, ProductID: NHSKDescriptor.GameID, Random: rand.New(rand.NewSource(8)), Clock: &nhskTestClock{now: time.Unix(1, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := battle.Init(&recordingBattleTestServiceContext{}); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	players := []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}
	for _, command := range []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: battleID, ProductID: NHSKDescriptor.GameID, MatchID: 1}}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: players}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
	} {
		if err := battle.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	cards := sequentialCustomDeckBytes()
	if err := battle.Handle(ctx, gsr.Command{ID: ProvideCustomDeckCommand, Payload: ProvideCustomDeckRequest{
		BattleID: battleID, GameNum: 1, SubgameNum: 1,
		Catalog: CustomDeckCatalog{Decks: []CustomDeck{{Cards: cards, BankerSeat: 2}}},
	}}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(CommandResult); !result.Accepted {
		t.Fatalf("provide result = %#v", result)
	}
	cards[0] = 0xff
	if err := battle.Handle(ctx, gsr.Command{ID: StartSubgameCommand, Payload: struct{}{}}); err != nil {
		t.Fatal(err)
	}
	if battle.activeSeat != 2 || battle.hands[battle.bySeat[2]][0] != 0x01 || battle.hands[battle.bySeat[3]][0] != 0x1b {
		t.Fatalf("external custom deal banker=%d hands=%#v", battle.activeSeat, battle.hands)
	}
	if err := battle.Handle(ctx, gsr.Command{ID: ProvideCustomDeckCommand, Payload: ProvideCustomDeckRequest{
		BattleID: battleID, GameNum: 1, SubgameNum: 1,
		Catalog: CustomDeckCatalog{Decks: []CustomDeck{{Cards: sequentialCustomDeckBytes(), BankerSeat: 3}}},
	}}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(CommandResult); result.Accepted {
		t.Fatalf("late provision result = %#v", result)
	}
	if battle.activeSeat != 2 || battle.hands[battle.bySeat[2]][0] != 0x01 {
		t.Fatal("late provision changed the running subgame")
	}
}

func TestBattleRejectsMismatchedExternalCustomDeckProvision(t *testing.T) {
	const battleID game.BattleID = 404
	battle, err := NewBattleService(NHSKBattleConfig{ID: battleID, MatchID: 1, ProductID: NHSKDescriptor.GameID, Random: rand.New(rand.NewSource(8)), Clock: &nhskTestClock{now: time.Unix(1, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := battle.Init(&recordingBattleTestServiceContext{}); err != nil {
		t.Fatal(err)
	}
	ctx := &battleTestCommandContext{}
	players := []BattlePlayer{{Player: "1", UserID: 1, SeatID: 0}, {Player: "2", UserID: 2, SeatID: 1}, {Player: "3", UserID: 3, SeatID: 2}, {Player: "4", UserID: 4, SeatID: 3}}
	for _, command := range []gsr.Command{
		{ID: InitializeBattleCommand, Payload: InitializeBattleRequest{Identity: BattleIdentity{BattleID: battleID, ProductID: NHSKDescriptor.GameID, MatchID: 1}}},
		{ID: UpdatePlayersCommand, Payload: UpdatePlayersRequest{Players: players}},
		{ID: PrepareSubgameCommand, Payload: PrepareSubgameRequest{GameNum: 1, SubgameNum: 1}},
	} {
		if err := battle.Handle(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	if err := battle.Handle(ctx, gsr.Command{ID: ProvideCustomDeckCommand, Payload: ProvideCustomDeckRequest{
		BattleID: battleID + 1, GameNum: 1, SubgameNum: 1,
		Catalog: CustomDeckCatalog{Decks: []CustomDeck{{Cards: sequentialCustomDeckBytes(), BankerSeat: 2}}},
	}}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(CommandResult); result.Accepted || result.Rejection == "" {
		t.Fatalf("mismatched provision result = %#v", result)
	}
	if battle.customDeckCatalog.valid() {
		t.Fatal("mismatched provision changed the catalog")
	}
}

type countingCustomDeckProvider struct {
	loads   int
	catalog CustomDeckCatalog
}

type redisStringValue struct {
	value string
	found bool
	err   error
}

type recordingRedisStringGetter struct {
	values map[string]redisStringValue
	keys   []string
}

func (getter *recordingRedisStringGetter) Get(_ context.Context, key string) (string, bool, error) {
	getter.keys = append(getter.keys, key)
	value := getter.values[key]
	return value.value, value.found, value.err
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
	sent chan ProvideCustomDeckRequest
}

func (runtime *customDeckRecordingRuntime) Send(_ gsr.ServiceRef, command gsr.CommandID, payload any) error {
	if command != ProvideCustomDeckCommand {
		return errInvalidCustomDeckRequest
	}
	runtime.sent <- payload.(ProvideCustomDeckRequest)
	return nil
}

func (*customDeckRecordingRuntime) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, errInvalidCustomDeckRequest
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
