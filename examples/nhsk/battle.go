package nhsk

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	mathrand "math/rand"
	"sort"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	nhskBattleTimerCommand     gsr.CommandID = 0x0410f003
	applyReplayResultCommand   gsr.CommandID = 0x0410f006
	applyAIResultCommand       gsr.CommandID = 0x0410f007
	deleteBattleBarrierCommand gsr.CommandID = 0x0410f008
)

const (
	settlementFlagSeal  int32 = 0x00000100
	settlementFlagBreak int32 = 0x00000200
)

var (
	errInvalidBattleConfig  = errors.New("nhsk: invalid Battle config")
	errBattleNotInitialized = errors.New("nhsk: battle not initialized")
	errBattleStateConflict  = errors.New("nhsk: battle state conflict")
	errBattleInvalidRequest = errors.New("nhsk: invalid Battle request")
	errBattleRandomFailure  = errors.New("nhsk: Battle random source failure")
)

// NHSKRandomSource supplies the Battle-owned random operations used by one
// NHSK subgame. *rand.Rand satisfies this interface and is suitable for tests
// when constructed with a fixed seed.
type NHSKRandomSource interface {
	Intn(n int) int
	Shuffle(n int, swap func(i, j int))
}

// NHSKClock supplies wall-clock reads to one NHSK Battle. Tests can inject a
// fixed or advancing implementation; production uses the system clock when
// the configuration leaves it nil.
type NHSKClock interface {
	Now() time.Time
}

type systemNHSKClock struct{}

func (systemNHSKClock) Now() time.Time { return time.Now() }

var readRandomSeed = cryptorand.Read

// NHSKBattlePhase is the business lifecycle of one NHSK Battle.
type NHSKBattlePhase string

const (
	// NHSKBattleAwaitingInit accepts identity and player setup Commands.
	NHSKBattleAwaitingInit NHSKBattlePhase = "awaiting_init"
	// NHSKBattlePreparing accepts the next subgame preparation Commands.
	NHSKBattlePreparing NHSKBattlePhase = "preparing"
	// NHSKBattlePlaying accepts card actions.
	NHSKBattlePlaying NHSKBattlePhase = "playing"
	// NHSKBattleAwaitingSettlement waits for the external settlement result.
	NHSKBattleAwaitingSettlement NHSKBattlePhase = "awaiting_settlement"
	// NHSKBattleFinalizingReplay waits for one writer result or local deadline.
	NHSKBattleFinalizingReplay NHSKBattlePhase = "finalizing_replay"
	// NHSKBattleFinished is the terminal phase for the example Battle.
	NHSKBattleFinished NHSKBattlePhase = "finished"
	// NHSKBattleStopping has crossed the DEL_GAME Mailbox barrier and cannot emit.
	NHSKBattleStopping NHSKBattlePhase = "stopping"
)

// NHSKBattleConfig configures one Battle Service before it is created by a factory.
type NHSKBattleConfig struct {
	ID        game.BattleID
	OutputRef gsr.ServiceRef
	MatchID   uint32
	ProductID uint32
	// IsNewbie selects the legacy new-player deal adjustment for this Battle.
	IsNewbie             bool
	ConnectionGeneration ConnectionGeneration
	OutputReporter       ConnectionFailureReporter
	// Random optionally injects the Battle-owned random source. When nil,
	// NewBattleService seeds a private source from crypto/rand.
	Random NHSKRandomSource
	// RandomSeed identifies an injected deterministic Random for diagnostics.
	// Production-generated random sources always retain their generated seed.
	RandomSeed *int64
	// Clock optionally injects the Battle-owned wall clock. When nil,
	// NewBattleService uses the process system clock.
	Clock NHSKClock
	// ReplaySubmitter is required by the production composition root. Tests may
	// leave it nil to use deterministic in-memory completion.
	ReplaySubmitter ReplaySubmitter
	ReplayTimeout   time.Duration
	// AISubmitter is provided by the process composition root. A nil submitter
	// keeps direct unit tests on the deterministic local automation path.
	AISubmitter AISubmitter
}

// NHSKBattleService owns one Battle's state and serializes all game mutations in its Mailbox.
type NHSKBattleService struct {
	id                   game.BattleID
	outputRef            gsr.ServiceRef
	matchID              uint32
	productID            uint32
	connectionGeneration ConnectionGeneration
	reporter             ConnectionFailureReporter
	isNewbie             bool
	service              gsr.ServiceContext
	phase                NHSKBattlePhase
	identity             BattleIdentity
	initialized          bool
	players              map[game.PlayerID]BattlePlayer
	bySeat               [4]game.PlayerID
	hands                map[game.PlayerID][]byte
	auto                 map[game.PlayerID]bool
	offline              map[game.PlayerID]bool
	clientReady          map[game.PlayerID]bool
	activeSeat           int
	verifyCode           uint32
	lastCards            []byte
	lastRank             int
	lastCount            int
	preOutSeat           int
	passCount            int
	scoreCards           []byte
	capturedPoints       [4]uint16
	moveCount            [4]uint32
	autoCount            [4]uint32
	moveMilliseconds     [4]uint32
	actionCounts         [4][6]uint32
	cardDetails          [4][]replayCardDetail
	lastPlayedCards      [4][8]byte
	lastPlayCounts       [4]int8
	revision             uint64
	turnRevision         uint64
	actionStartedAt      time.Time
	deadlineAt           time.Time
	gameNum              uint16
	subgameNum           uint16
	fee                  int32
	maxGameNum           uint16
	maxSubgameNum        uint16
	scoreBase            int32
	scoreDenominator     int32
	replayMetadata       ReplayMetadata
	replayRules          ReplayRuleSnapshot
	initialization       battleInitialization
	finished             [4]bool
	ranks                [4]uint8
	settlementFailed     bool
	pendingResult        subgameResultSnapshot
	nextRound            UpdateRoundContextRequest
	currentRound         UpdateRoundContextRequest
	startedDresses       [4]string
	replayDocument       ReplayDocument
	replaySubmitter      ReplaySubmitter
	replayTimeout        time.Duration
	replayFinalized      bool
	noticeAfterReplay    bool
	aiSubmitter          AISubmitter
	random               NHSKRandomSource
	clock                NHSKClock
	rules                NHSKConfig
	customDeckCatalog    CustomDeckCatalog
	outedCards           [][]byte
	deleting             bool
	randomSeed           int64
	randomSeedKnown      bool
}

type battleInitialization struct {
	identity         BattleIdentity
	maxGameNum       uint16
	maxSubgameNum    uint16
	fee              int32
	scoreBase        int32
	scoreDenominator int32
	replayMetadata   ReplayMetadata
	replayRules      ReplayRuleSnapshot
	rules            NHSKConfig
}

// NewBattleService creates an NHSK Battle Service with no initialized business state.
func NewBattleService(config NHSKBattleConfig) (*NHSKBattleService, error) {
	if config.ID == 0 {
		return nil, errInvalidBattleConfig
	}
	random := config.Random
	randomSeed, randomSeedKnown := int64(0), false
	if random == nil {
		var err error
		random, randomSeed, err = newBattleRandom()
		if err != nil {
			return nil, err
		}
		randomSeedKnown = true
	} else if config.RandomSeed != nil {
		randomSeed, randomSeedKnown = *config.RandomSeed, true
	}
	clock := config.Clock
	if clock == nil {
		clock = systemNHSKClock{}
	}
	if config.ReplayTimeout <= 0 {
		config.ReplayTimeout = 5 * time.Second
	}
	productID := config.ProductID
	if productID == 0 {
		productID = NHSKDescriptor.GameID
	}
	return &NHSKBattleService{
		id: config.ID, outputRef: config.OutputRef, matchID: config.MatchID, productID: productID,
		connectionGeneration: config.ConnectionGeneration, reporter: config.OutputReporter,
		isNewbie:        config.IsNewbie,
		random:          random,
		randomSeed:      randomSeed,
		randomSeedKnown: randomSeedKnown,
		clock:           clock,
		replaySubmitter: config.ReplaySubmitter,
		replayTimeout:   config.ReplayTimeout,
		aiSubmitter:     config.AISubmitter,
		rules:           DefaultNHSKConfig(),
		phase:           NHSKBattleAwaitingInit, players: make(map[game.PlayerID]BattlePlayer),
		hands: make(map[game.PlayerID][]byte), auto: make(map[game.PlayerID]bool), offline: make(map[game.PlayerID]bool), clientReady: make(map[game.PlayerID]bool),
		preOutSeat:     -1,
		lastPlayCounts: [4]int8{-1, -1, -1, -1},
	}, nil
}

func newBattleRandom() (NHSKRandomSource, int64, error) {
	var seedBytes [8]byte
	if n, err := readRandomSeed(seedBytes[:]); err != nil {
		return nil, 0, fmt.Errorf("%w: %v", errBattleRandomFailure, err)
	} else if n != len(seedBytes) {
		return nil, 0, fmt.Errorf("%w: short seed read", errBattleRandomFailure)
	}
	seed := int64(binary.LittleEndian.Uint64(seedBytes[:]))
	return mathrand.New(mathrand.NewSource(seed)), seed, nil
}

// Init captures the Service capability used for timers and output delivery.
func (battle *NHSKBattleService) Init(service gsr.ServiceContext) error {
	if service == nil {
		return errInvalidBattleConfig
	}
	battle.service = service
	return nil
}

// Handle applies one Command inside the Battle Mailbox.
func (battle *NHSKBattleService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	if command.ID == deleteBattleBarrierCommand {
		return battle.deleteBarrier(ctx, command.Payload)
	}
	if battle.deleting {
		return nil
	}
	switch command.ID {
	case InitializeBattleCommand:
		return battle.initialize(ctx, command.Payload)
	case UpdatePlayersCommand:
		return battle.updatePlayers(ctx, command.Payload)
	case PrepareSubgameCommand:
		return battle.prepare(ctx, command.Payload)
	case StartSubgameCommand:
		return battle.start(ctx, command.Payload)
	case ProvideCustomDeckCommand:
		return battle.provideCustomDeck(ctx, command.Payload)
	case UpdateRoundContextCommand:
		return battle.updateRoundContext(ctx, command.Payload)
	case ExitPlayerCommand:
		return battle.exit(ctx, command.Payload)
	case UpdatePlayerDressCommand:
		return battle.updateDress(ctx, command.Payload)
	case SetPlayerAutoStateCommand:
		return battle.setAuto(ctx, command.Payload)
	case SetPlayerOfflineCommand:
		return battle.setOffline(ctx, command.Payload)
	case ReconnectPlayerCommand:
		return battle.reconnect(ctx, command.Payload)
	case PlayCardsCommand:
		return battle.play(ctx, command.Payload)
	case PreviewCardSelectionCommand:
		return battle.preview(ctx, command.Payload)
	case RequestGameSceneCommand:
		return battle.scene(ctx, command.Payload)
	case RecordPropUseCommand:
		return battle.recordPropUse(command.Payload)
	case ForceFinishSubgameCommand:
		return battle.forceFinish(ctx, command.Payload)
	case CompleteSettlementCommand:
		return battle.completeSettlement(ctx, command.Payload)
	case GetNHSKBattleSnapshotCommand:
		return battle.snapshotReply(ctx)
	case nhskBattleTimerCommand:
		return battle.timer(ctx, command.Payload)
	case applyReplayResultCommand:
		return battle.applyReplayResult(ctx, command.Payload)
	case applyAIResultCommand:
		return battle.applyAIResult(ctx, command.Payload)
	default:
		return gsr.ErrUnknownCommand
	}
}

type deleteBattleBarrier struct{ BattleID game.BattleID }

func (battle *NHSKBattleService) deleteBarrier(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(deleteBattleBarrier)
	if !ok || request.BattleID != battle.id {
		return gsr.ErrInvalidClusterEnvelope
	}
	if !battle.deleting {
		battle.deleting = true
		battle.phase = NHSKBattleStopping
		battle.turnRevision++
		battle.deadlineAt = time.Time{}
		battle.noticeAfterReplay = false
	}
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) recordPropUse(payload any) error {
	request, ok := payload.(RecordPropUseRequest)
	if !ok || (battle.phase != NHSKBattlePlaying && battle.phase != NHSKBattleAwaitingSettlement) || battle.replayDocument.start.BattleID == 0 {
		battle.observeDroppedProp("state")
		return nil
	}
	found := false
	for _, player := range battle.players {
		if player.UserID == request.SenderID {
			found = true
			break
		}
	}
	if !found {
		battle.observeDroppedProp("sender")
		return nil
	}
	battle.replayDocument.appendMove(ReplayMove{Kind: ReplayMoveProp, UserID: request.SenderID, PropID: request.PropID, PropCount: request.SendCount, TargetIDs: append([]uint32(nil), request.TargetIDs...), Source: ReplayMoveSourceUnknown})
	return nil
}

func (battle *NHSKBattleService) observeDroppedProp(cause string) {
	if battle.service != nil {
		battle.service.Metrics().Inc("nhsk_prop_fact_dropped_total")
		battle.service.Logger().Warn("NHSK prop fact dropped", "battle_id", battle.id, "cause", cause)
	}
}

// Stop cancels lifecycle-local timers without changing business state.
func (battle *NHSKBattleService) Stop(context.Context) error { return nil }

// Close releases the Service capability after Runtime has stopped the Battle.
func (battle *NHSKBattleService) Close() error { battle.service = nil; return nil }

func (battle *NHSKBattleService) initialize(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(InitializeBattleRequest)
	if !ok || request.Identity.BattleID == 0 || request.Identity.BattleID != battle.id || request.Identity.ProductID == 0 || request.Identity.MatchID == 0 {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	rules := DefaultNHSKConfig()
	if request.Rules != nil {
		rules = *request.Rules
	}
	initialization := battleInitialization{
		identity: request.Identity, maxGameNum: request.MaxGameNum, maxSubgameNum: request.MaxSubgameNum,
		fee: request.Fee, scoreBase: request.ScoreBase, scoreDenominator: request.ScoreDenominator,
		replayMetadata: request.ReplayMetadata, replayRules: request.ReplayRules, rules: rules,
	}
	if battle.initialized {
		if battle.initialization == initialization {
			return battle.reply(ctx, CommandResult{Accepted: true})
		}
		return battle.reject(ctx, errBattleStateConflict)
	}
	battle.identity = request.Identity
	battle.matchID = request.Identity.MatchID
	battle.productID = request.Identity.ProductID
	battle.fee = request.Fee
	battle.maxGameNum = request.MaxGameNum
	battle.maxSubgameNum = request.MaxSubgameNum
	battle.scoreBase = request.ScoreBase
	battle.scoreDenominator = request.ScoreDenominator
	battle.replayMetadata = request.ReplayMetadata
	battle.replayRules = request.ReplayRules
	battle.rules = rules
	battle.initialization = initialization
	battle.initialized = true
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) updatePlayers(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(UpdatePlayersRequest)
	if !ok || !battle.initialized || (battle.phase != NHSKBattleAwaitingInit && battle.phase != NHSKBattlePreparing) || len(request.Players) == 0 {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	seenPlayers := make(map[game.PlayerID]struct{}, len(request.Players))
	seenSeats := make(map[uint8]struct{}, len(request.Players))
	for _, player := range request.Players {
		if player.Player == "" || player.UserID == 0 || player.SeatID >= 4 {
			return battle.reject(ctx, errBattleInvalidRequest)
		}
		if _, exists := seenPlayers[player.Player]; exists {
			return battle.reject(ctx, errBattleInvalidRequest)
		}
		if _, exists := seenSeats[player.SeatID]; exists {
			return battle.reject(ctx, errBattleInvalidRequest)
		}
		seenPlayers[player.Player] = struct{}{}
		seenSeats[player.SeatID] = struct{}{}
	}
	if battle.phase == NHSKBattlePlaying {
		return battle.reject(ctx, errBattleStateConflict)
	}
	candidate := make(map[game.PlayerID]BattlePlayer, len(battle.players)+len(request.Players))
	candidateReady := make(map[game.PlayerID]bool, len(battle.players)+len(request.Players))
	for playerID, player := range battle.players {
		candidate[playerID] = player
		candidateReady[playerID] = battle.clientReady[playerID]
	}
	for _, player := range request.Players {
		settlementFlags := candidate[player.Player]
		player.IsBreak = settlementFlags.IsBreak
		player.IsSeal = settlementFlags.IsSeal
		player.Dress = settlementFlags.Dress
		candidate[player.Player] = player
		if _, exists := candidateReady[player.Player]; !exists {
			// Legacy GameLogic marks a newly admitted player client-ready before
			// the first subgame starts. Reconnect/scene commands can refresh it.
			candidateReady[player.Player] = true
		}
	}
	candidateBySeat := [4]game.PlayerID{}
	for _, player := range candidate {
		if candidateBySeat[player.SeatID] != "" && candidateBySeat[player.SeatID] != player.Player {
			return battle.reject(ctx, errBattleInvalidRequest)
		}
		candidateBySeat[player.SeatID] = player.Player
	}
	battle.players = candidate
	battle.bySeat = candidateBySeat
	battle.clientReady = candidateReady
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) prepare(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(PrepareSubgameRequest)
	if !ok || !battle.initialized || request.GameNum == 0 || request.SubgameNum == 0 || !battle.hasFourPlayers() {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	if battle.phase == NHSKBattlePreparing && battle.gameNum == request.GameNum && battle.subgameNum == request.SubgameNum {
		return battle.reply(ctx, CommandResult{Accepted: true})
	}
	if battle.phase != NHSKBattleAwaitingInit && battle.phase != NHSKBattleFinished && battle.phase != NHSKBattlePreparing {
		return battle.reject(ctx, errBattleStateConflict)
	}
	battle.gameNum, battle.subgameNum = request.GameNum, request.SubgameNum
	battle.phase = NHSKBattlePreparing
	battle.customDeckCatalog = CustomDeckCatalog{}
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) start(ctx gsr.CommandContext, payload any) error {
	if payload != nil {
		if _, ok := payload.(struct{}); !ok {
			return battle.reject(ctx, errBattleInvalidRequest)
		}
	}
	if battle.phase != NHSKBattlePreparing || !battle.hasFourPlayers() {
		return battle.reject(ctx, errBattleStateConflict)
	}
	bankerSeat := battle.random.Intn(len(battle.bySeat))
	if bankerSeat < 0 || bankerSeat >= len(battle.bySeat) {
		return battle.reject(ctx, errBattleRandomFailure)
	}
	customDeck := battle.customDeckForSubgame()
	if customDeck != nil && customDeck.BankerSeat >= 0 {
		bankerSeat = customDeck.BankerSeat
	}
	battle.deal(bankerSeat, customDeck)
	battle.phase = NHSKBattlePlaying
	battle.currentRound = battle.nextRound
	for seat, player := range battle.bySeat {
		if player != "" {
			battle.startedDresses[seat] = battle.players[player].Dress
		}
	}
	battle.activeSeat = bankerSeat
	battle.verifyCode = 1
	startedAt := battle.clock.Now()
	battle.resetTrick()
	battle.finished = [4]bool{}
	battle.noticeAfterReplay = false
	battle.ranks = [4]uint8{}
	battle.capturedPoints = [4]uint16{}
	battle.moveCount = [4]uint32{}
	battle.autoCount = [4]uint32{}
	battle.moveMilliseconds = [4]uint32{}
	battle.actionCounts = [4][6]uint32{}
	battle.cardDetails = [4][]replayCardDetail{}
	battle.outedCards = nil
	battle.pendingResult = subgameResultSnapshot{}
	battle.replayFinalized = false
	battle.settlementFailed = false
	for playerID, player := range battle.players {
		player.IsBreak = false
		player.IsSeal = false
		battle.players[playerID] = player
	}
	replayStart := battle.buildReplayStartSnapshot(startedAt)
	battle.replayDocument = NewReplayDocument(replayStart)
	battle.revision++
	if err := battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputGameStart, Payload: GameStartPayload{}}); err != nil {
		return err
	}
	if err := battle.emit(ctx, GameStartedOutput{ReplayName: replayStart.ReplayName}); err != nil {
		return err
	}
	if err := battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputGameInfo, Payload: battle.gameInfoPayload()}); err != nil {
		return err
	}
	players := battle.bySeat
	for seat, player := range players {
		if err := battle.emit(ctx, ClientGameOutput{Targets: []game.PlayerID{player}, Kind: OutputDeal, Payload: DealPayload{Players: players, SeatID: uint8(seat), Cards: toFixedCards(battle.hands[player])}}); err != nil {
			return err
		}
	}
	if err := battle.startAction(ctx, battle.rules.firstOutCardTimeout()); err != nil {
		return err
	}
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) buildReplayStartSnapshot(startedAt time.Time) ReplayStartSnapshot {
	var snapshot ReplayStartSnapshot
	snapshot.BattleID = battle.id
	snapshot.Identity = battle.identity
	snapshot.GameNum = battle.gameNum
	snapshot.SubgameNum = battle.subgameNum
	snapshot.StartedAt = startedAt
	snapshot.ReplayName, snapshot.ReplayUID, snapshot.RelativePath = battle.replayIdentity(startedAt)
	snapshot.MaxGameNum = battle.maxGameNum
	snapshot.MaxSubgameNum = battle.maxSubgameNum
	snapshot.Fee = battle.fee
	snapshot.ScoreBase = battle.scoreBase
	snapshot.ScoreDenominator = battle.scoreDenominator
	snapshot.ReplayMetadata = battle.replayMetadata
	snapshot.ReplayRules = battle.replayRules
	snapshot.Rules = battle.rules
	snapshot.RoundContext = battle.currentRound
	snapshot.BankerSeat = uint8(battle.activeSeat)
	for seat, playerID := range battle.bySeat {
		player := battle.players[playerID]
		snapshot.Players[seat] = ReplayPlayerSnapshot{
			SeatID:    player.SeatID,
			Player:    player.Player,
			UserID:    player.UserID,
			Nickname:  player.Nickname,
			InitScore: player.Score,
			Platform:  player.ClientID,
			Dress:     battle.startedDresses[seat],
			Automated: player.Automated,
		}
		snapshot.Hands[seat] = append([]byte(nil), battle.hands[playerID]...)
	}
	return snapshot
}

func (battle *NHSKBattleService) provideCustomDeck(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(ProvideCustomDeckRequest)
	if !ok || request.BattleID != battle.id || request.GameNum == 0 || request.SubgameNum == 0 || !request.Catalog.valid() {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	if !battle.initialized || battle.phase != NHSKBattlePreparing || battle.gameNum != request.GameNum || battle.subgameNum != request.SubgameNum {
		return battle.reject(ctx, errBattleStateConflict)
	}
	battle.customDeckCatalog = request.Catalog.clone()
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) customDeckForSubgame() *CustomDeck {
	if !battle.customDeckCatalog.valid() {
		return nil
	}
	index := int(battle.subgameNum-1) % len(battle.customDeckCatalog.Decks)
	deck := battle.customDeckCatalog.Decks[index]
	return &deck
}

func (battle *NHSKBattleService) updateRoundContext(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(UpdateRoundContextRequest)
	if !ok || !battle.initialized {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	battle.nextRound = request
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) exit(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(ExitPlayerRequest)
	player, exists := battle.players[request.Player]
	if !ok || !exists {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	player.Exited = true
	battle.players[player.Player] = player
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) updateDress(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(UpdatePlayerDressRequest)
	if !ok || request.Player == "" || battle.players[request.Player].Player == "" {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	player := battle.players[request.Player]
	player.Dress = request.Dress
	battle.players[request.Player] = player
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) setAuto(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(SetPlayerAutoStateRequest)
	if !ok || battle.players[request.Player].Player == "" || battle.phase != NHSKBattlePlaying {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	if _, err := battle.changeAutoState(ctx, request.Player, request.Enabled); err != nil {
		return err
	}
	if err := battle.emit(ctx, ClientGameOutput{Targets: []game.PlayerID{request.Player}, Kind: OutputPlayerAutoState, Payload: PlayerAutoStatePayload{Player: request.Player, Enabled: request.Enabled}}); err != nil {
		return err
	}
	if request.Enabled && request.Player == battle.bySeat[battle.activeSeat] && battle.deadlineAt.Sub(battle.clock.Now()) > 100*time.Millisecond {
		battle.turnRevision++
		battle.deadlineAt = time.Time{}
		if err := battle.playWithSource(ctx, PlayCardsRequest{Player: request.Player, Cards: battle.localAutoCards(request.Player), VerifyCode: battle.verifyCode}, ReplayMoveSourceAuto, false); err != nil {
			return err
		}
	}
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) changeAutoState(ctx gsr.CommandContext, player game.PlayerID, enabled bool) (bool, error) {
	if battle.auto[player] == enabled {
		return false, nil
	}
	battle.auto[player] = enabled
	return true, battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputPlayerAutoState, Payload: PlayerAutoStatePayload{Player: player, Enabled: enabled}})
}

func (battle *NHSKBattleService) setOffline(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(SetPlayerOfflineRequest)
	if !ok || battle.players[request.Player].Player == "" {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	battle.offline[request.Player] = request.Offline
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) reconnect(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(ReconnectPlayerRequest)
	if !ok || battle.players[request.Player].Player == "" {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	battle.offline[request.Player] = false
	if battle.phase == NHSKBattlePlaying {
		if _, err := battle.changeAutoState(ctx, request.Player, false); err != nil {
			return err
		}
		if err := battle.restorePlayerView(ctx, request.Player); err != nil {
			return err
		}
	}
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) preview(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(PreviewCardSelectionRequest)
	if !ok || len(request.Cards) > 26 || battle.phase != NHSKBattlePlaying || request.Player != battle.bySeat[battle.activeSeat] {
		return battle.reply(ctx, ActionResult{Rejection: "invalid_preview"})
	}
	copyCards := append([]byte(nil), request.Cards...)
	if err := battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputCardSelectionPreview, Payload: CardSelectionPreviewPayload{Player: request.Player, Cards: toFixedCards(copyCards), CardCount: uint8(len(copyCards))}}); err != nil {
		return err
	}
	return battle.reply(ctx, ActionResult{Accepted: true})
}

func (battle *NHSKBattleService) play(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(PlayCardsRequest)
	if !ok {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	return battle.playWithSource(ctx, request, ReplayMoveSourcePlayer, true)
}

func (battle *NHSKBattleService) playWithSource(ctx gsr.CommandContext, request PlayCardsRequest, source ReplayMoveSource, notifyRejection bool) error {
	if battle.phase != NHSKBattlePlaying {
		return battle.actionFailure(ctx, request.Player, "not_playing", notifyRejection)
	}
	if request.Player != battle.bySeat[battle.activeSeat] {
		return battle.actionFailure(ctx, request.Player, "not_active", notifyRejection)
	}
	if request.VerifyCode == 0 || request.VerifyCode != battle.verifyCode {
		return battle.actionFailure(ctx, request.Player, "stale_verify_code", notifyRejection)
	}
	if len(request.Cards) > 8 {
		return battle.actionFailure(ctx, request.Player, "card_count", notifyRejection)
	}
	if len(request.Cards) == 0 && len(battle.lastCards) == 0 {
		return battle.actionFailure(ctx, request.Player, "first_play_cannot_pass", notifyRejection)
	}
	pattern, valid := battle.validateCards(request.Player, request.Cards)
	if !valid || (len(request.Cards) > 0 && len(battle.lastCards) > 0 && compareCardSets(request.Cards, battle.lastCards) <= 0) {
		return battle.actionFailure(ctx, request.Player, "card_type", notifyRejection)
	}
	if source == ReplayMoveSourcePlayer {
		if _, err := battle.changeAutoState(ctx, request.Player, false); err != nil {
			return err
		}
	}
	moveMilliseconds := battle.currentActionMilliseconds()
	battle.removeCards(request.Player, request.Cards)
	if len(request.Cards) > 0 {
		battle.lastCards = append(battle.lastCards[:0], request.Cards...)
		battle.lastRank, battle.lastCount = pattern.rank, pattern.count
		battle.preOutSeat = battle.activeSeat
		battle.passCount = 0
		_, scoreCards := scoreCardsIn(request.Cards)
		battle.scoreCards = append(battle.scoreCards, scoreCards...)
		currentScore, _ := scoreCardsIn(battle.scoreCards)
		battle.replayDocument.appendMove(ReplayMove{Kind: ReplayMoveCurrentPoint, Cards: battle.scoreCards, Point: currentScore, Source: ReplayMoveSourceSystem})
		battle.lastPlayCounts[battle.activeSeat] = int8(len(request.Cards))
		copy(battle.lastPlayedCards[battle.activeSeat][:], request.Cards)
	} else {
		battle.passCount++
		battle.lastPlayCounts[battle.activeSeat] = 0
		battle.lastPlayedCards[battle.activeSeat] = [8]byte{}
	}
	player := battle.players[request.Player]
	battle.replayDocument.appendMove(ReplayMove{Kind: ReplayMoveOutCard, SeatID: uint8(battle.activeSeat), UserID: player.UserID, Cards: request.Cards, CardType: replayCardTypeName(pattern, len(request.Cards)), Source: source, MoveMilliseconds: moveMilliseconds})
	battle.moveCount[battle.activeSeat]++
	battle.moveMilliseconds[battle.activeSeat] += moveMilliseconds
	battle.recordReplayAction(battle.activeSeat, pattern, request.Cards)
	battle.outedCards = append(battle.outedCards, append([]byte(nil), request.Cards...))
	if !player.Automated && source != ReplayMoveSourcePlayer {
		battle.autoCount[battle.activeSeat]++
	}
	battle.revision++
	if err := battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputOutCardInfo, Payload: OutCardInfoPayload{Player: request.Player, Cards: toFixedEight(request.Cards), CardCount: uint8(len(request.Cards))}}); err != nil {
		return err
	}
	if len(battle.hands[request.Player]) == 0 && !battle.finished[battle.activeSeat] {
		finishedSeat := battle.activeSeat
		battle.finished[finishedSeat] = true
		battle.ranks[finishedSeat] = uint8(battle.finishedPlayerCount())
		partnerSeat := battle.partnerSeat(finishedSeat)
		if len(battle.hands[battle.bySeat[partnerSeat]]) == 0 {
			battle.awardFinalScoreCards()
			battle.phase = NHSKBattleAwaitingSettlement
			battle.deadlineAt = time.Time{}
			if err := battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputShowCards, Payload: battle.showCardsPayload(-1, true)}); err != nil {
				return err
			}
			battle.pendingResult = calculateSubgameResult(GameOverReasonSuccess, battle.ranks, battle.capturedPoints, battle.settlementAutomated())
			if err := battle.emit(ctx, buildSettlementRequest(battle.pendingResult, battle.playersBySeat())); err != nil {
				return err
			}
		} else {
			if err := battle.emit(ctx, ClientGameOutput{Targets: []game.PlayerID{request.Player}, Kind: OutputShowCards, Payload: battle.showCardsPayload(finishedSeat, false)}); err != nil {
				return err
			}
			if err := battle.advanceTurn(ctx); err != nil {
				return err
			}
		}
	} else {
		if err := battle.advanceTurn(ctx); err != nil {
			return err
		}
	}
	if notifyRejection {
		return battle.reply(ctx, ActionResult{Accepted: true})
	}
	return nil
}

func (battle *NHSKBattleService) localAutoCards(player game.PlayerID) []byte {
	if len(battle.lastCards) != 0 || len(battle.hands[player]) == 0 {
		return nil
	}
	card := battle.hands[player][0]
	for _, candidate := range battle.hands[player][1:] {
		candidateValue, cardValue := cardLogicValue(int(candidate&0x0f)), cardLogicValue(int(card&0x0f))
		if candidateValue < cardValue || candidateValue == cardValue && candidate < card {
			card = candidate
		}
	}
	return []byte{card}
}

func (battle *NHSKBattleService) awardFinalScoreCards() {
	if battle.preOutSeat < 0 || battle.preOutSeat >= len(battle.bySeat) {
		battle.scoreCards = nil
		return
	}
	points, cards := scoreCardsIn(battle.scoreCards)
	if points > 0 {
		battle.capturedPoints[battle.preOutSeat] += uint16(points)
		player := battle.players[battle.bySeat[battle.preOutSeat]]
		battle.replayDocument.appendMove(ReplayMove{Kind: ReplayMoveCatchPoint, SeatID: uint8(battle.preOutSeat), UserID: player.UserID, Cards: cards, Point: points, Source: ReplayMoveSourceSystem})
	}
	battle.scoreCards = nil
}

func (battle *NHSKBattleService) playersBySeat() [4]BattlePlayer {
	var players [4]BattlePlayer
	for seat, playerID := range battle.bySeat {
		players[seat] = battle.players[playerID]
	}
	return players
}

func (battle *NHSKBattleService) settlementAutomated() [4]bool {
	var automated [4]bool
	for seat := range automated {
		countEnabled := battle.rules.AutoSettlementMinCount >= 0
		ratioEnabled := battle.rules.AutoSettlementRatioFactor >= 0
		switch {
		case countEnabled && ratioEnabled:
			automated[seat] = battle.autoCount[seat] >= uint32(battle.rules.AutoSettlementMinCount) && int(battle.autoCount[seat])*battle.rules.AutoSettlementRatioFactor >= int(battle.moveCount[seat])
		case countEnabled:
			automated[seat] = battle.autoCount[seat] >= uint32(battle.rules.AutoSettlementMinCount)
		case ratioEnabled:
			automated[seat] = int(battle.autoCount[seat])*battle.rules.AutoSettlementRatioFactor >= int(battle.moveCount[seat])
		}
	}
	return automated
}

func (battle *NHSKBattleService) scene(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(ReconnectPlayerRequest)
	if !ok || battle.players[request.Player].Player == "" || battle.gameNum == 0 || battle.subgameNum == 0 {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	if _, err := battle.changeAutoState(ctx, request.Player, false); err != nil {
		return err
	}
	if battle.phase == NHSKBattlePlaying || battle.phase == NHSKBattleAwaitingSettlement {
		if err := battle.restorePlayerView(ctx, request.Player); err != nil {
			return err
		}
	}
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) forceFinish(ctx gsr.CommandContext, payload any) error {
	if payload != nil {
		if _, ok := payload.(struct{}); !ok {
			return battle.reject(ctx, errBattleInvalidRequest)
		}
	}
	if battle.phase == NHSKBattlePlaying || battle.phase == NHSKBattleAwaitingSettlement {
		battle.turnRevision++
		battle.deadlineAt = time.Time{}
		battle.phase = NHSKBattleFinished
		battle.revision++
		if err := battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputShowCards, Payload: battle.showCardsPayload(-1, true)}); err != nil {
			return err
		}
		result := calculateSubgameResult(GameOverReasonSuccess, battle.ranks, battle.capturedPoints, battle.settlementAutomated())
		scores := result.multiples
		for seat, playerID := range battle.bySeat {
			player := battle.players[playerID]
			player.Score = scores[seat]
			battle.players[playerID] = player
		}
		if err := battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputGameResult, Payload: battle.gameResultPayload(GameOverReasonSuccess, scores, result)}); err != nil {
			return err
		}
		battle.noticeAfterReplay = true
		battle.finalizeReplay(result, scores)
		if err := battle.beginReplayFinalization(ctx); err != nil {
			return err
		}
	}
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) completeSettlement(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(CompleteSettlementRequest)
	if !ok || battle.phase != NHSKBattleAwaitingSettlement {
		return battle.settlementReject(ctx, errBattleStateConflict)
	}
	plan, err := battle.settlementPlan(request)
	if err != nil {
		return battle.settlementReject(ctx, err)
	}
	for seat, player := range battle.bySeat {
		if player == "" || battle.players[player].Player == "" {
			return battle.settlementReject(ctx, errBattleInvalidRequest)
		}
		value := battle.players[player]
		value.Score = plan.scores[seat]
		value.IsBreak = plan.isBreak[seat]
		value.IsSeal = plan.isSeal[seat]
		battle.players[player] = value
	}
	battle.settlementFailed = plan.failed
	result := battle.pendingResult
	if result.ranks == ([4]uint8{}) {
		result = calculateSubgameResult(GameOverReasonSuccess, battle.ranks, battle.capturedPoints, battle.settlementAutomated())
	}
	battle.phase = NHSKBattleFinished
	battle.revision++
	reason := GameOverReasonSuccess
	if plan.failed {
		reason = GameOverReasonDissolve
		result = calculateSubgameResult(reason, battle.ranks, battle.capturedPoints, battle.settlementAutomated())
	}
	if err := battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputGameResult, Payload: battle.gameResultPayload(reason, plan.scores, result)}); err != nil {
		return err
	}
	battle.finalizeReplay(result, plan.scores)
	if err := battle.beginReplayFinalization(ctx); err != nil {
		return err
	}
	return battle.reply(ctx, SettlementCommandResult{Accepted: true})
}

type replayWriteResult struct {
	BattleID   game.BattleID
	GameNum    uint16
	SubgameNum uint16
	ReplayName string
	Error      string
}

type replayFinalizeDeadline struct {
	GameNum    uint16
	SubgameNum uint16
}

type actionDeadline struct {
	TurnRevision uint64
	Source       ReplayMoveSource
	Cards        []byte
}

func (battle *NHSKBattleService) beginReplayFinalization(ctx gsr.CommandContext) error {
	battle.phase = NHSKBattleFinalizingReplay
	xmlBytes, err := marshalReplayDocumentXML(battle.replayDocument.Clone())
	if err != nil {
		battle.observeReplayFailure("serialize")
		return battle.finishReplay(ctx)
	}
	if battle.replaySubmitter == nil {
		return battle.finishReplay(ctx)
	}
	start := battle.replayDocument.start
	artifact := ReplayArtifact{
		Target: ctx.Self(), BattleID: battle.id, GameNum: battle.gameNum, SubgameNum: battle.subgameNum,
		ReplayName: start.ReplayName, RelativePath: start.RelativePath, XML: xmlBytes,
	}
	if err := battle.replaySubmitter.SubmitReplay(artifact); err != nil {
		battle.observeReplayFailure("submit")
		return battle.finishReplay(ctx)
	}
	if battle.service == nil {
		return battle.finishReplay(ctx)
	}
	if _, err := battle.service.After(battle.replayTimeout, nhskBattleTimerCommand, replayFinalizeDeadline{GameNum: battle.gameNum, SubgameNum: battle.subgameNum}); err != nil {
		battle.observeReplayFailure("timer")
		return battle.finishReplay(ctx)
	}
	return nil
}

func (battle *NHSKBattleService) applyReplayResult(ctx gsr.CommandContext, payload any) error {
	result, ok := payload.(replayWriteResult)
	if !ok || result.BattleID != battle.id || result.GameNum != battle.gameNum || result.SubgameNum != battle.subgameNum || result.ReplayName != battle.currentReplayName() || battle.phase != NHSKBattleFinalizingReplay {
		return nil
	}
	if result.Error != "" {
		battle.observeReplayFailure("write")
	}
	return battle.finishReplay(ctx)
}

func (battle *NHSKBattleService) observeReplayFailure(cause string) {
	if battle.service == nil {
		return
	}
	battle.service.Metrics().Inc("nhsk_replay_finalize_failure_total")
	battle.service.Logger().Warn("NHSK replay finalization failed", "battle_id", battle.id, "game_num", battle.gameNum, "subgame_num", battle.subgameNum, "cause", cause)
}

func (battle *NHSKBattleService) finishReplay(ctx gsr.CommandContext) error {
	if battle.phase != NHSKBattleFinalizingReplay || battle.replayFinalized || battle.replayDocument.end == nil {
		return nil
	}
	battle.replayFinalized = true
	if targets := battle.roundStatPlayers(); len(targets) > 0 {
		if err := battle.emit(ctx, ClientGameOutput{Targets: targets, Kind: OutputRoundStat, Payload: RoundStatPayload{}}); err != nil {
			return err
		}
	}
	end := battle.replayDocument.end
	players := make([]GameOverPlayerData, 4)
	for seat, player := range end.players {
		players[seat] = GameOverPlayerData{Score: end.finalScores[seat], Exp: player.Exp, Automated: end.result.automated[seat]}
	}
	if err := battle.emit(ctx, GameOverOutput{Reason: int32(end.result.reason), ReplayName: battle.currentReplayName(), IsGameOver: false, Players: players}); err != nil {
		return err
	}
	if battle.noticeAfterReplay {
		battle.noticeAfterReplay = false
		if err := battle.emit(ctx, NoticeRoundOverOutput{EndReason: int32(GameOverReasonSuccess)}); err != nil {
			return err
		}
	}
	battle.phase = NHSKBattleFinished
	return nil
}

func (battle *NHSKBattleService) finalizeReplay(result subgameResultSnapshot, finalScores [4]int32) {
	var actions [4]replayActionSummary
	for seat := range actions {
		actions[seat].counts = battle.actionCounts[seat]
	}
	battle.replayDocument.finalize(replayEndSnapshot{
		endedAt: battle.clock.Now(), result: result, finalScores: finalScores,
		players: battle.playersBySeat(), moveCount: battle.moveCount, autoCount: battle.autoCount,
		moveMilliseconds: battle.moveMilliseconds, actions: actions, cardDetails: battle.cardDetails,
	})
}

func (battle *NHSKBattleService) recordReplayAction(seat int, pattern cardPattern, cards []byte) {
	index := 0
	if len(cards) > 0 {
		switch pattern.kind {
		case cardPatternSingle:
			index = 1
		case cardPatternPair:
			index = 2
		case cardPatternTriple:
			index = 3
		case cardPatternThreeTwo:
			index = 4
		case cardPatternBomb:
			index = 5
		}
	}
	battle.actionCounts[seat][index]++
	if pattern.kind == cardPatternBomb && len(cards) > 0 {
		battle.cardDetails[seat] = append(battle.cardDetails[seat], replayCardDetail{cardType: "ZhaDan", cards: append([]byte(nil), cards...)})
	}
}

func (battle *NHSKBattleService) gameResultPayload(reason GameOverReason, scores [4]int32, result subgameResultSnapshot) GameResultPayload {
	var players [4]game.PlayerID
	for seat, player := range battle.bySeat {
		players[seat] = player
	}
	return GameResultPayload{
		Reason: reason, Players: players, Automated: result.automated, Scores: scores,
		Outcomes: result.outcomes, CapturedPoints: result.points, Ranks: result.ranks,
		Result: result.result, WinningTeam: result.winningTeam, ReplayUID: battle.replayDocument.start.ReplayUID,
	}
}

type settlementPlan struct {
	scores  [4]int32
	isBreak [4]bool
	isSeal  [4]bool
	failed  bool
}

func (battle *NHSKBattleService) settlementPlan(request CompleteSettlementRequest) (settlementPlan, error) {
	if !request.Success {
		return settlementPlan{failed: true}, nil
	}
	if request.TeamCount != 4 || len(request.Players) != 4 {
		return settlementPlan{}, fmt.Errorf("%w: settlement player matrix", errBattleInvalidRequest)
	}
	playerSeats := make(map[uint32]int, len(battle.bySeat))
	for seat, playerID := range battle.bySeat {
		if playerID == "" || battle.players[playerID].Player == "" {
			return settlementPlan{}, fmt.Errorf("%w: settlement Battle player", errBattleInvalidRequest)
		}
		userID := battle.players[playerID].UserID
		if userID == 0 {
			return settlementPlan{}, fmt.Errorf("%w: settlement player identity", errBattleInvalidRequest)
		}
		if _, exists := playerSeats[userID]; exists {
			return settlementPlan{}, fmt.Errorf("%w: duplicate settlement identity", errBattleInvalidRequest)
		}
		playerSeats[userID] = seat
	}
	seenPlayers := make(map[uint32]struct{}, len(request.Players))
	var plan settlementPlan
	for _, result := range request.Players {
		seat, ok := playerSeats[result.PlayerID]
		if !ok || result.TeamID != uint32(seat) {
			return settlementPlan{}, fmt.Errorf("%w: settlement player/team", errBattleInvalidRequest)
		}
		if _, exists := seenPlayers[result.PlayerID]; exists {
			return settlementPlan{}, fmt.Errorf("%w: duplicate settlement player", errBattleInvalidRequest)
		}
		seenPlayers[result.PlayerID] = struct{}{}
		plan.isSeal[seat] = result.Flag&settlementFlagSeal != 0
		plan.isBreak[seat] = result.Flag&settlementFlagBreak != 0
	}
	if len(seenPlayers) != len(playerSeats) {
		return settlementPlan{}, fmt.Errorf("%w: incomplete settlement players", errBattleInvalidRequest)
	}
	var values [4]int64
	seenGains := make(map[[2]uint32]struct{}, len(request.Gains))
	for _, gain := range request.Gains {
		if gain.Score <= 0 || gain.PayTeamID >= 4 || gain.GainTeamID >= 4 || gain.PayTeamID == gain.GainTeamID {
			return settlementPlan{}, fmt.Errorf("%w: settlement gain", errBattleInvalidRequest)
		}
		key := [2]uint32{gain.PayTeamID, gain.GainTeamID}
		if _, exists := seenGains[key]; exists {
			return settlementPlan{}, fmt.Errorf("%w: duplicate settlement gain", errBattleInvalidRequest)
		}
		seenGains[key] = struct{}{}
		values[gain.PayTeamID] -= int64(gain.Score)
		values[gain.GainTeamID] += int64(gain.Score)
	}
	for seat, value := range values {
		if value < -1<<31 || value > 1<<31-1 {
			return settlementPlan{}, fmt.Errorf("%w: settlement score overflow", errBattleInvalidRequest)
		}
		plan.scores[seat] = int32(value)
	}
	return plan, nil
}

func (battle *NHSKBattleService) settlementReject(ctx gsr.CommandContext, err error) error {
	return battle.reply(ctx, SettlementCommandResult{Rejection: err.Error()})
}

func (battle *NHSKBattleService) timer(ctx gsr.CommandContext, payload any) error {
	if deadline, ok := payload.(replayFinalizeDeadline); ok {
		if battle.phase == NHSKBattleFinalizingReplay && deadline.GameNum == battle.gameNum && deadline.SubgameNum == battle.subgameNum {
			battle.observeReplayFailure("timeout")
			return battle.finishReplay(ctx)
		}
		return nil
	}
	deadline, ok := payload.(actionDeadline)
	if !ok || deadline.TurnRevision != battle.turnRevision || battle.phase != NHSKBattlePlaying {
		return nil
	}
	player := battle.bySeat[battle.activeSeat]
	if player == "" || len(battle.hands[player]) == 0 {
		return nil
	}
	battle.deadlineAt = time.Time{}
	source := deadline.Source
	if source == ReplayMoveSourceTimeout && !battle.auto[player] && !battle.players[player].Automated {
		if !battle.rules.TimeoutAutoMove {
			return nil
		}
		if _, err := battle.changeAutoState(ctx, player, true); err != nil {
			return err
		}
	}
	cards := deadline.Cards
	if source != ReplayMoveSourceAI {
		cards = battle.localAutoCards(player)
	}
	return battle.playWithSource(ctx, PlayCardsRequest{Player: player, Cards: cards, VerifyCode: battle.verifyCode}, source, false)
}

func (battle *NHSKBattleService) snapshotReply(ctx gsr.CommandContext) error {
	return battle.reply(ctx, battle.snapshot())
}

func (battle *NHSKBattleService) snapshot() NHSKBattleSnapshot {
	players := make([]BattlePlayer, 0, len(battle.players))
	for _, player := range battle.players {
		players = append(players, player)
	}
	sort.Slice(players, func(i, j int) bool { return players[i].SeatID < players[j].SeatID })
	hands := make(map[game.PlayerID][]byte, len(battle.hands))
	for player, cards := range battle.hands {
		hands[player] = append([]byte(nil), cards...)
	}
	auto := make(map[game.PlayerID]bool, len(battle.auto))
	for player, enabled := range battle.auto {
		auto[player] = enabled
	}
	deadlineUnix := int64(0)
	if !battle.deadlineAt.IsZero() {
		deadlineUnix = battle.deadlineAt.Unix()
	}
	return NHSKBattleSnapshot{BattleID: battle.id, Phase: string(battle.phase), Identity: battle.identity, Players: players, ActivePlayer: battle.bySeat[battle.activeSeat], VerifyCode: battle.verifyCode, Hands: hands, Auto: auto, Revision: battle.revision, TurnRevision: battle.turnRevision, DeadlineUnix: deadlineUnix}
}

func (battle *NHSKBattleService) scenePayload(target game.PlayerID) GameScenePayload {
	var payload GameScenePayload
	payload.State = GameSceneStatePlaying
	if battle.phase == NHSKBattleAwaitingSettlement {
		payload.State = GameSceneStateShowingResult
	}
	payload.ActiveSeat = int8(battle.activeSeat)
	payload.PreviousPlayerSeat = int8(battle.preOutSeat)
	payload.RemainingSeconds = battle.remainingActionMilliseconds() / 1000
	payload.FinishedPlayerCount = uint8(battle.finishedPlayerCount())
	payload.TrickScoreCardCount = uint8(len(battle.scoreCards))
	copy(payload.TrickScoreCards[:], battle.scoreCards)
	targetSeat := battle.seatOf(target)
	for seat, player := range battle.bySeat {
		payload.Players[seat] = GameScenePlayer{
			Player:          player,
			Automated:       battle.auto[player],
			Offline:         battle.offline[player],
			HandCount:       uint8(len(battle.hands[player])),
			LastPlayedCards: battle.lastPlayedCards[seat],
			LastPlayCount:   battle.lastPlayCounts[seat],
			CapturedPoints:  battle.capturedPoints[seat],
			Rank:            battle.ranks[seat],
		}
		if player == target || (targetSeat >= 0 && len(battle.hands[target]) == 0 && seat == battle.partnerSeat(targetSeat)) {
			copy(payload.Players[seat].HandCards[:], battle.hands[player])
		}
	}
	return payload
}

func (battle *NHSKBattleService) showCardsPayload(finishedSeat int, revealAll bool) ShowCardsPayload {
	var payload ShowCardsPayload
	partnerSeat := -1
	if finishedSeat >= 0 && finishedSeat < len(battle.bySeat) {
		partnerSeat = battle.partnerSeat(finishedSeat)
	}
	for seat, player := range battle.bySeat {
		payload.Players[seat] = player
		payload.HandCounts[seat] = uint8(len(battle.hands[player]))
		if revealAll || seat == partnerSeat {
			copy(payload.Cards[seat][:], battle.hands[player])
		}
	}
	return payload
}

func (battle *NHSKBattleService) restorePlayerView(ctx gsr.CommandContext, player game.PlayerID) error {
	battle.clientReady[player] = true
	if err := battle.emit(ctx, ClientGameOutput{Targets: []game.PlayerID{player}, Kind: OutputGameInfo, Payload: battle.gameInfoPayload()}); err != nil {
		return err
	}
	if err := battle.emit(ctx, ClientGameOutput{Targets: []game.PlayerID{player}, Kind: OutputGameScene, Payload: battle.scenePayload(player)}); err != nil {
		return err
	}
	if battle.phase == NHSKBattlePlaying && battle.bySeat[battle.activeSeat] == player {
		if err := battle.emit(ctx, ClientGameOutput{Targets: []game.PlayerID{player}, Kind: OutputAskOutCard, Payload: battle.askOutCardPayload()}); err != nil {
			return err
		}
	}
	return nil
}

func (battle *NHSKBattleService) gameInfoPayload() GameInfoPayload {
	var scores [4]int32
	for seat, player := range battle.bySeat {
		if player != "" {
			scores[seat] = battle.players[player].Score
		}
	}
	return GameInfoPayload{OutCardSeconds: uint32(battle.rules.outCardTimeout() / time.Second), ServiceFee: battle.fee, Scores: scores, GameNum: battle.gameNum}
}

func (battle *NHSKBattleService) askOutCardPayload() AskOutCardPayload {
	return AskOutCardPayload{ActivePlayer: battle.bySeat[battle.activeSeat], VerifyCode: battle.verifyCode, ActionMilliseconds: uint32(battle.rules.outCardTimeout() / time.Millisecond)}
}

func (battle *NHSKBattleService) remainingActionMilliseconds() uint32 {
	if battle.deadlineAt.IsZero() {
		return 0
	}
	now := battle.clock.Now()
	remaining := battle.deadlineAt.Sub(now)
	if remaining <= 0 {
		return 0
	}
	return uint32(remaining / time.Millisecond)
}

func (battle *NHSKBattleService) currentActionMilliseconds() uint32 {
	if battle.actionStartedAt.IsZero() {
		return 0
	}
	elapsed := battle.clock.Now().Sub(battle.actionStartedAt)
	if elapsed <= 0 {
		return 0
	}
	return uint32(elapsed / time.Millisecond)
}

func (battle *NHSKBattleService) replayIdentity(startedAt time.Time) (name, uid, relativePath string) {
	local := startedAt.In(time.FixedZone("UTC+8", 8*60*60))
	date, clockHour := local.Format("20060102"), local.Format("15")
	seatZeroUserID := battle.players[battle.bySeat[0]].UserID
	name = fmt.Sprintf("NHSK_M%dR%d_%s_%s_%d.xml", battle.identity.ProductID, battle.identity.RoundID, date, local.Format("150405"), seatZeroUserID)
	uid = fmt.Sprintf("%d%d", startedAt.Unix(), battle.replayMetadata.CreatorID)
	return name, uid, fmt.Sprintf("FuPan/%s/%s", date, clockHour)
}

func (battle *NHSKBattleService) currentReplayName() string {
	return battle.replayDocument.start.ReplayName
}

func (battle *NHSKBattleService) hasFourPlayers() bool {
	for seat := range battle.bySeat {
		if battle.bySeat[seat] == "" {
			return false
		}
	}
	return len(battle.players) >= 4
}

func (battle *NHSKBattleService) activePlayers() []game.PlayerID {
	players := make([]game.PlayerID, 0, len(battle.players))
	for seat := range battle.bySeat {
		player := battle.bySeat[seat]
		if player != "" && !battle.players[player].Exited {
			players = append(players, player)
		}
	}
	return players
}

func (battle *NHSKBattleService) advanceTurn(ctx gsr.CommandContext) error {
	battle.advanceSeat()
	if battle.preOutSeat >= 0 && battle.passCount >= 3 {
		return battle.finishTrick(ctx)
	}
	return battle.startTurn(ctx)
}

func (battle *NHSKBattleService) startTurn(ctx gsr.CommandContext) error {
	return battle.startAction(ctx, battle.rules.outCardTimeout())
}

func (battle *NHSKBattleService) startAction(ctx gsr.CommandContext, timeout time.Duration) error {
	battle.verifyCode += 2
	battle.turnRevision++
	startedAt := battle.clock.Now()
	actionTimeout := timeout
	battle.actionStartedAt = startedAt
	if battle.service != nil {
		source := ReplayMoveSourceTimeout
		player := battle.bySeat[battle.activeSeat]
		useAI := player != "" && battle.aiSubmitter != nil && (battle.players[player].Automated || battle.offline[player] && battle.auto[player] && battle.rules.OfflineAutoUsesAI)
		if useAI {
			if battle.rules.MsAITimeout > 0 {
				timeout = battle.rules.MsAITimeout
			}
		} else if player != "" && (battle.auto[player] || battle.players[player].Automated) {
			source = ReplayMoveSourceAuto
			if battle.rules.MsOutCardRobot > 0 {
				timeout = battle.rules.MsOutCardRobot
			}
		}
		battle.deadlineAt = startedAt.Add(timeout)
		if _, err := battle.service.After(timeout, nhskBattleTimerCommand, actionDeadline{TurnRevision: battle.turnRevision, Source: source}); err != nil {
			return err
		}
		if useAI {
			if err := battle.aiSubmitter.SubmitAI(battle.buildAIRequest(ctx.Self(), player, startedAt, actionTimeout)); err != nil {
				battle.service.Metrics().Inc("nhsk_ai_submit_failure_total")
				battle.service.Logger().Warn("NHSK AI request submit failed", "battle_id", battle.id, "game_num", battle.gameNum, "subgame_num", battle.subgameNum)
			}
		}
	} else {
		battle.deadlineAt = time.Time{}
	}
	if battle.phase != NHSKBattlePlaying || battle.bySeat[battle.activeSeat] == "" {
		return nil
	}
	return battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputAskOutCard, Payload: battle.askOutCardPayload()})
}

func (battle *NHSKBattleService) buildAIRequest(target gsr.ServiceRef, player game.PlayerID, startedAt time.Time, actionTimeout time.Duration) AIRequest {
	var handCounts [4]uint8
	for seat, playerID := range battle.bySeat {
		handCounts[seat] = uint8(len(battle.hands[playerID]))
	}
	trickPoint, _ := scoreCardsIn(battle.scoreCards)
	request := AIRequest{
		Target: target, BattleID: battle.id, ProductID: battle.identity.ProductID, MatchID: battle.identity.MatchID, RoundID: battle.identity.RoundID,
		GameNum: battle.gameNum, SubgameNum: battle.subgameNum, UserID: battle.players[player].UserID, SeatID: uint8(battle.activeSeat),
		TurnRevision: battle.turnRevision, VerifyCode: battle.verifyCode, StartedAt: startedAt, MoveMS: uint32(battle.rules.MsOutCardRobot / time.Millisecond),
		ActionMS: uint32(actionTimeout / time.Millisecond),
		Scene:    AIScene{ActiveSeat: uint8(battle.activeSeat), FirstOutSeat: battle.replayDocument.start.BankerSeat, HandCounts: handCounts, Hand: append([]byte(nil), battle.hands[player]...), CapturedPoints: battle.capturedPoints, Ranks: battle.ranks, TrickPoint: trickPoint, Leading: len(battle.lastCards) == 0},
	}
	request.Scene.OutedCards = make([][]byte, len(battle.outedCards))
	for index, cards := range battle.outedCards {
		request.Scene.OutedCards[index] = append([]byte(nil), cards...)
	}
	return request
}

func (battle *NHSKBattleService) applyAIResult(ctx gsr.CommandContext, payload any) error {
	result, ok := payload.(aiResult)
	if !ok || result.Error != "" || battle.phase != NHSKBattlePlaying || result.BattleID != battle.id || result.GameNum != battle.gameNum || result.SubgameNum != battle.subgameNum || result.TurnRevision != battle.turnRevision || result.VerifyCode != battle.verifyCode || !result.StartedAt.Equal(battle.actionStartedAt) || int(result.SeatID) != battle.activeSeat {
		return nil
	}
	player := battle.bySeat[battle.activeSeat]
	if player == "" || battle.players[player].UserID != result.UserID || !battle.validActionCandidate(player, result.Cards) {
		return nil
	}
	battle.turnRevision++
	if !battle.players[player].Automated && battle.rules.MsOutCardRobot > 0 {
		now := battle.clock.Now()
		elapsed := now.Sub(result.StartedAt)
		if remaining := battle.rules.MsOutCardRobot - elapsed; remaining > 0 {
			battle.deadlineAt = now.Add(remaining)
			if battle.service == nil {
				battle.deadlineAt = time.Time{}
				return battle.playWithSource(ctx, PlayCardsRequest{Player: player, Cards: result.Cards, VerifyCode: battle.verifyCode}, ReplayMoveSourceAI, false)
			}
			_, err := battle.service.After(remaining, nhskBattleTimerCommand, actionDeadline{TurnRevision: battle.turnRevision, Source: ReplayMoveSourceAI, Cards: append([]byte(nil), result.Cards...)})
			return err
		}
	}
	battle.deadlineAt = time.Time{}
	return battle.playWithSource(ctx, PlayCardsRequest{Player: player, Cards: result.Cards, VerifyCode: battle.verifyCode}, ReplayMoveSourceAI, false)
}

func (battle *NHSKBattleService) validActionCandidate(player game.PlayerID, cards []byte) bool {
	if len(cards) > 8 || len(cards) == 0 && len(battle.lastCards) == 0 {
		return false
	}
	_, valid := battle.validateCards(player, cards)
	return valid && (len(cards) == 0 || len(battle.lastCards) == 0 || compareCardSets(cards, battle.lastCards) > 0)
}

func (battle *NHSKBattleService) finishTrick(ctx gsr.CommandContext) error {
	if battle.preOutSeat < 0 || battle.preOutSeat >= len(battle.bySeat) || battle.bySeat[battle.preOutSeat] == "" {
		return nil
	}
	winnerSeat := battle.preOutSeat
	winner := battle.bySeat[winnerSeat]
	points, _ := scoreCardsIn(battle.scoreCards)
	battle.capturedPoints[winnerSeat] += uint16(points)
	if points > 0 {
		battle.replayDocument.appendMove(ReplayMove{Kind: ReplayMoveCatchPoint, SeatID: uint8(winnerSeat), UserID: battle.players[winner].UserID, Cards: battle.scoreCards, Point: points, Source: ReplayMoveSourceSystem})
	}
	battle.replayDocument.appendMove(ReplayMove{Kind: ReplayMoveTurnEnd, Scores: battle.capturedPoints, Source: ReplayMoveSourceSystem})
	if err := battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputTurnEnd, Payload: TurnEndPayload{Winner: winner, CapturedPoints: points}}); err != nil {
		return err
	}
	if len(battle.hands[winner]) == 0 {
		battle.activeSeat = battle.partnerSeat(winnerSeat)
	} else {
		battle.activeSeat = winnerSeat
	}
	battle.resetTrick()
	return battle.startTurn(ctx)
}

func (battle *NHSKBattleService) seatOf(player game.PlayerID) int {
	for seat, candidate := range battle.bySeat {
		if candidate == player {
			return seat
		}
	}
	return -1
}

func (battle *NHSKBattleService) partnerSeat(seat int) int {
	return (seat + 2) % len(battle.bySeat)
}

func (battle *NHSKBattleService) finishedPlayerCount() int {
	count := 0
	for _, finished := range battle.finished {
		if finished {
			count++
		}
	}
	return count
}

func (battle *NHSKBattleService) roundStatPlayers() []game.PlayerID {
	players := make([]game.PlayerID, 0, len(battle.players))
	for seat := range battle.bySeat {
		player := battle.bySeat[seat]
		if player != "" && !battle.players[player].Exited && battle.clientReady[player] {
			players = append(players, player)
		}
	}
	return players
}

func (battle *NHSKBattleService) deal(bankerSeat int, custom *CustomDeck) {
	battle.hands = make(map[game.PlayerID][]byte, 4)
	if custom != nil && len(custom.Cards) == customDeckCardCount {
		battle.dealDeck(bankerSeat, custom.Cards)
		return
	}
	deck := make([]byte, 0, 104)
	for copyIndex := 0; copyIndex < 2; copyIndex++ {
		for suit := 0; suit < 4; suit++ {
			for value := 1; value <= 13; value++ {
				deck = append(deck, byte(suit<<4|value))
			}
		}
	}
	battle.random.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
	if battle.isNewbie {
		newPlayerSeat := -1
		for seat, player := range battle.bySeat {
			if player != "" && !battle.players[player].Automated {
				newPlayerSeat = seat
				break
			}
		}
		if newPlayerSeat >= 0 {
			adjustNewbieCards(deck, len(battle.bySeat), 26, newPlayerSeat)
		}
	} else {
		swapSingleCards(deck, len(battle.bySeat), 26, battle.rules.SingleCountToSwap)
	}
	battle.dealDeck(bankerSeat, deck)
}

func (battle *NHSKBattleService) dealDeck(bankerSeat int, deck []byte) {
	for offset := 0; offset < len(battle.bySeat); offset++ {
		seat := (bankerSeat + offset) % len(battle.bySeat)
		player := battle.bySeat[seat]
		start := offset * 26
		battle.hands[player] = append([]byte(nil), deck[start:start+26]...)
	}
}

// adjustNewbieCards applies the legacy new-player retry: first reduce single
// cards to three, then retry with four if the selected seat became worse after
// the whole four-seat exchange. The helper intentionally allows exchanges to
// affect all seats, matching the old Logic.RandCardListByNewPlayer behavior.
func adjustNewbieCards(cards []byte, playerNum, handCardCount, newPlayerSeat int) {
	if playerNum <= 0 || handCardCount <= 0 || newPlayerSeat < 0 || newPlayerSeat >= playerNum {
		return
	}
	start := newPlayerSeat * handCardCount
	end := start + handCardCount
	if end > len(cards) {
		return
	}
	before := len(singleCards(cards[start:end]))
	swapSingleCards(cards, playerNum, handCardCount, 3)
	if after := len(singleCards(cards[start:end])); after > before {
		swapSingleCards(cards, playerNum, handCardCount, 4)
	}
}

// swapSingleCards applies the NHSK ordinary-deal adjustment used by the
// legacy Logic.SwapSingleCard implementation. The deck is laid out in seat
// order, and the operation deliberately walks seats and cards in their
// original order so the resulting fixed-seed deal remains compatible.
func swapSingleCards(cards []byte, playerNum, handCardCount, singleCountToSwap int) {
	if playerNum <= 1 || handCardCount <= 0 || singleCountToSwap <= 0 {
		return
	}
	for seat := 0; seat < playerNum; seat++ {
		start := seat * handCardCount
		end := start + handCardCount
		if end > len(cards) {
			return
		}
		for len(singleCards(cards[start:end])) > singleCountToSwap {
			singles := singleCards(cards[start:end])
			swapped := false
			for _, single := range singles {
				singleValue := cardValue(single)
				for otherSeat := 0; otherSeat < playerNum && !swapped; otherSeat++ {
					if otherSeat == seat {
						continue
					}
					otherStart := otherSeat * handCardCount
					otherEnd := otherStart + handCardCount
					if otherEnd > len(cards) {
						return
					}
					for index := otherStart; index < otherEnd; index++ {
						candidate := cards[index]
						candidateValue := cardValue(candidate)
						if candidateValue == singleValue || !containsCardValue(cards[start:end], candidateValue) {
							continue
						}
						singleIndex := findCardIndex(cards[start:end], single)
						if singleIndex < 0 {
							continue
						}
						cards[start+singleIndex], cards[index] = cards[index], cards[start+singleIndex]
						swapped = true
						break
					}
				}
				if swapped {
					break
				}
			}
			if !swapped {
				break
			}
		}
	}
}

func singleCards(cards []byte) []byte {
	counts := [16]int{}
	first := [16]byte{}
	for _, card := range cards {
		value := cardValue(card)
		if value >= byte(len(counts)) {
			continue
		}
		if counts[value] == 0 {
			first[value] = card
		}
		counts[value]++
	}
	singles := make([]byte, 0, 13)
	for value := byte(1); value < 14; value++ {
		if counts[value] == 1 {
			singles = append(singles, first[value])
		}
	}
	return singles
}

func containsCardValue(cards []byte, value byte) bool {
	return valueCount(cards, value) > 0
}

func valueCount(cards []byte, value byte) int {
	count := 0
	for _, card := range cards {
		if cardValue(card) == value {
			count++
		}
	}
	return count
}

func findCardIndex(cards []byte, card byte) int {
	for index, candidate := range cards {
		if candidate == card {
			return index
		}
	}
	return -1
}

func cardValue(card byte) byte { return card & 0x0f }

func (battle *NHSKBattleService) advanceSeat() {
	for step := 1; step <= 4; step++ {
		seat := (battle.activeSeat + step) % 4
		if player := battle.bySeat[seat]; player != "" && len(battle.hands[player]) > 0 && !battle.players[player].Exited {
			battle.activeSeat = seat
			return
		}
		if battle.preOutSeat >= 0 {
			battle.passCount++
		}
	}
}

func (battle *NHSKBattleService) resetTrick() {
	battle.lastCards = nil
	battle.lastRank = -1
	battle.lastCount = 0
	battle.preOutSeat = -1
	battle.passCount = 0
	battle.scoreCards = nil
	battle.lastPlayedCards = [4][8]byte{}
	battle.lastPlayCounts = [4]int8{-1, -1, -1, -1}
}

func (battle *NHSKBattleService) validateCards(player game.PlayerID, cards []byte) (cardPattern, bool) {
	if len(cards) == 0 {
		return cardPattern{}, true
	}
	pattern, valid := classifyCards(cards)
	if !valid {
		return cardPattern{}, false
	}
	hand := append([]byte(nil), battle.hands[player]...)
	for _, card := range cards {
		found := false
		for index, held := range hand {
			if held == card {
				hand = append(hand[:index], hand[index+1:]...)
				found = true
				break
			}
		}
		if !found {
			return cardPattern{}, false
		}
	}
	return pattern, true
}

func (battle *NHSKBattleService) removeCards(player game.PlayerID, cards []byte) {
	hand := battle.hands[player]
	for _, card := range cards {
		for index, held := range hand {
			if held == card {
				hand = append(hand[:index], hand[index+1:]...)
				break
			}
		}
	}
	battle.hands[player] = hand
}

func (battle *NHSKBattleService) actionReject(ctx gsr.CommandContext, player game.PlayerID, reason string) error {
	_ = battle.emit(ctx, ClientGameOutput{Targets: []game.PlayerID{player}, Kind: OutputOutCardRejection, Payload: OutCardRejectionPayload{Reason: OutCardRejectionReason(1)}})
	return battle.reply(ctx, ActionResult{Rejection: reason})
}
func (battle *NHSKBattleService) actionFailure(ctx gsr.CommandContext, player game.PlayerID, reason string, notify bool) error {
	if notify {
		return battle.actionReject(ctx, player, reason)
	}
	return nil
}
func (battle *NHSKBattleService) reject(ctx gsr.CommandContext, err error) error {
	return battle.reply(ctx, CommandResult{Rejection: err.Error()})
}
func (battle *NHSKBattleService) reply(ctx gsr.CommandContext, value any) error {
	if err := ctx.Reply(value); err != nil && !errors.Is(err, gsr.ErrReplyUnavailable) {
		return err
	}
	return nil
}

func (battle *NHSKBattleService) emit(ctx gsr.CommandContext, output GameOutput) error {
	if battle.deleting || battle.service == nil || battle.outputRef.ID == 0 || battle.matchID == 0 || battle.productID == 0 {
		return nil
	}
	batch := GameOutputBatch{BattleID: battle.id, MatchID: battle.matchID, ProductID: battle.productID, Ref: ctx.Self(), ConnectionGeneration: battle.connectionGeneration, Outputs: []GameOutput{output}}
	if err := battle.service.Send(battle.outputRef, deliverGameOutputBatchCommand, batch); err != nil {
		if battle.reporter != nil && battle.connectionGeneration != 0 {
			battle.reporter.FailConnection(battle.connectionGeneration, ConnectionFailureOutputSendRejected)
		}
		return fmt.Errorf("nhsk: deliver output: %w", err)
	}
	return nil
}

func toFixedCards(cards []byte) (result [26]byte) { copy(result[:], cards); return result }
func toFixedEight(cards []byte) (result [8]byte)  { copy(result[:], cards); return result }
