package nhsk

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const nhskBattleTimerCommand gsr.CommandID = 0x0410f003

var (
	errInvalidBattleConfig  = errors.New("nhsk: invalid Battle config")
	errBattleNotInitialized = errors.New("nhsk: battle not initialized")
	errBattleStateConflict  = errors.New("nhsk: battle state conflict")
	errBattleInvalidRequest = errors.New("nhsk: invalid Battle request")
)

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
	// NHSKBattleFinished is the terminal phase for the example Battle.
	NHSKBattleFinished NHSKBattlePhase = "finished"
)

// NHSKBattleConfig configures one Battle Service before it is created by a factory.
type NHSKBattleConfig struct {
	ID                   game.BattleID
	OutputRef            gsr.ServiceRef
	MatchID              uint32
	ProductID            uint32
	ConnectionGeneration ConnectionGeneration
	OutputReporter       ConnectionFailureReporter
}

// NHSKBattleService owns one Battle's state and serializes all game mutations in its Mailbox.
type NHSKBattleService struct {
	id                   game.BattleID
	outputRef            gsr.ServiceRef
	matchID              uint32
	productID            uint32
	connectionGeneration ConnectionGeneration
	reporter             ConnectionFailureReporter
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
	revision             uint64
	turnRevision         uint64
	deadlineAt           time.Time
	gameNum              uint16
	subgameNum           uint16
	fee                  int32
	finished             [4]bool
	ranks                [4]uint8
	nextRound            UpdateRoundContextRequest
}

// NewBattleService creates an NHSK Battle Service with no initialized business state.
func NewBattleService(config NHSKBattleConfig) (*NHSKBattleService, error) {
	if config.ID == 0 {
		return nil, errInvalidBattleConfig
	}
	productID := config.ProductID
	if productID == 0 {
		productID = NHSKDescriptor.GameID
	}
	return &NHSKBattleService{
		id: config.ID, outputRef: config.OutputRef, matchID: config.MatchID, productID: productID,
		connectionGeneration: config.ConnectionGeneration, reporter: config.OutputReporter,
		phase: NHSKBattleAwaitingInit, players: make(map[game.PlayerID]BattlePlayer),
		hands: make(map[game.PlayerID][]byte), auto: make(map[game.PlayerID]bool), offline: make(map[game.PlayerID]bool), clientReady: make(map[game.PlayerID]bool),
	}, nil
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
	switch command.ID {
	case InitializeBattleCommand:
		return battle.initialize(ctx, command.Payload)
	case UpdatePlayersCommand:
		return battle.updatePlayers(ctx, command.Payload)
	case PrepareSubgameCommand:
		return battle.prepare(ctx, command.Payload)
	case StartSubgameCommand:
		return battle.start(ctx, command.Payload)
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
	case ForceFinishSubgameCommand:
		return battle.forceFinish(ctx, command.Payload)
	case CompleteSettlementCommand:
		return battle.completeSettlement(ctx, command.Payload)
	case GetNHSKBattleSnapshotCommand:
		return battle.snapshotReply(ctx)
	case nhskBattleTimerCommand:
		return battle.timer(ctx, command.Payload)
	default:
		return gsr.ErrUnknownCommand
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
	if battle.initialized {
		if battle.identity == request.Identity {
			return battle.reply(ctx, CommandResult{Accepted: true})
		}
		return battle.reject(ctx, errBattleStateConflict)
	}
	battle.identity = request.Identity
	battle.matchID = request.Identity.MatchID
	battle.productID = request.Identity.ProductID
	battle.fee = request.Fee
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
	battle.deal()
	battle.phase = NHSKBattlePlaying
	battle.activeSeat = 0
	battle.verifyCode = 1
	battle.turnRevision++
	battle.deadlineAt = battle.service.Now().Add(15 * time.Second)
	if battle.service != nil {
		if _, err := battle.service.After(15*time.Second, nhskBattleTimerCommand, battle.turnRevision); err != nil {
			return err
		}
	}
	battle.lastCards = nil
	battle.lastRank = -1
	battle.lastCount = 0
	battle.finished = [4]bool{}
	battle.ranks = [4]uint8{}
	battle.revision++
	if err := battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputGameStart, Payload: GameStartPayload{}}); err != nil {
		return err
	}
	if err := battle.emit(ctx, GameStartedOutput{ReplayName: battle.replayName()}); err != nil {
		return err
	}
	return battle.reply(ctx, CommandResult{Accepted: true})
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
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) setAuto(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(SetPlayerAutoStateRequest)
	if !ok || battle.players[request.Player].Player == "" {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	battle.auto[request.Player] = request.Enabled
	if request.Enabled && battle.phase == NHSKBattlePlaying && request.Player == battle.bySeat[battle.activeSeat] && battle.service != nil {
		battle.deadlineAt = battle.service.Now().Add(time.Second)
		if _, err := battle.service.After(time.Second, nhskBattleTimerCommand, battle.turnRevision); err != nil {
			return err
		}
	}
	if err := battle.emit(ctx, ClientGameOutput{Targets: []game.PlayerID{request.Player}, Kind: OutputGameScene, Payload: battle.scenePayload(request.Player)}); err != nil {
		return err
	}
	return battle.reply(ctx, CommandResult{Accepted: true})
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
		battle.auto[request.Player] = false
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
	if battle.phase != NHSKBattlePlaying {
		return battle.actionReject(ctx, request.Player, "not_playing")
	}
	if request.Player != battle.bySeat[battle.activeSeat] {
		return battle.actionReject(ctx, request.Player, "not_active")
	}
	if request.VerifyCode == 0 || request.VerifyCode != battle.verifyCode {
		return battle.actionReject(ctx, request.Player, "stale_verify_code")
	}
	if len(request.Cards) > 8 {
		return battle.actionReject(ctx, request.Player, "card_count")
	}
	if len(request.Cards) == 0 && len(battle.lastCards) == 0 {
		return battle.actionReject(ctx, request.Player, "first_play_cannot_pass")
	}
	rank, valid := battle.validateCards(request.Player, request.Cards)
	if !valid || (len(request.Cards) > 0 && len(battle.lastCards) > 0 && (len(request.Cards) != battle.lastCount || rank <= battle.lastRank)) {
		return battle.actionReject(ctx, request.Player, "card_type")
	}
	battle.removeCards(request.Player, request.Cards)
	if len(request.Cards) > 0 {
		battle.lastCards = append(battle.lastCards[:0], request.Cards...)
		battle.lastRank, battle.lastCount = rank, len(request.Cards)
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
			battle.phase = NHSKBattleAwaitingSettlement
			battle.deadlineAt = time.Time{}
			if err := battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputShowCards, Payload: battle.showCardsPayload(-1, true)}); err != nil {
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
	return battle.reply(ctx, ActionResult{Accepted: true})
}

func (battle *NHSKBattleService) scene(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(ReconnectPlayerRequest)
	if !ok || battle.players[request.Player].Player == "" || battle.gameNum == 0 || battle.subgameNum == 0 {
		return battle.reject(ctx, errBattleInvalidRequest)
	}
	battle.auto[request.Player] = false
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
		battle.phase = NHSKBattleFinished
		battle.revision++
		// The old GameLogic emits GAME_OVER before the force-round-over notice.
		// Keep the two typed outputs in the same Mailbox order; the output owner
		// then serializes them onto the single GM connection.
		if err := battle.emit(ctx, GameOverOutput{
			Reason:     int32(GameOverReasonSuccess),
			ReplayName: battle.replayName(),
			IsGameOver: false,
		}); err != nil {
			return err
		}
		if err := battle.emit(ctx, NoticeRoundOverOutput{EndReason: int32(GameOverReasonSuccess)}); err != nil {
			return err
		}
	}
	return battle.reply(ctx, CommandResult{Accepted: true})
}

func (battle *NHSKBattleService) completeSettlement(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(CompleteSettlementRequest)
	if !ok || battle.phase != NHSKBattleAwaitingSettlement {
		return battle.reject(ctx, errBattleStateConflict)
	}
	if !request.Success {
		request.Scores = [4]int32{}
	}
	for seat, player := range battle.bySeat {
		if player == "" || battle.players[player].Player == "" {
			return battle.reject(ctx, errBattleInvalidRequest)
		}
		value := battle.players[player]
		value.Score = request.Scores[seat]
		battle.players[player] = value
	}
	battle.phase = NHSKBattleFinished
	battle.revision++
	if err := battle.emit(ctx, GameOverOutput{Reason: 0, ReplayName: battle.replayName(), IsGameOver: true}); err != nil {
		return err
	}
	return battle.reply(ctx, SettlementCommandResult{Accepted: true})
}

func (battle *NHSKBattleService) timer(ctx gsr.CommandContext, payload any) error {
	value, ok := payload.(uint64)
	if !ok || value != battle.turnRevision || battle.phase != NHSKBattlePlaying {
		return nil
	}
	player := battle.bySeat[battle.activeSeat]
	if player == "" || !battle.auto[player] || len(battle.hands[player]) == 0 {
		return nil
	}
	request := PlayCardsRequest{Player: player, Cards: []byte{battle.hands[player][0]}, VerifyCode: battle.verifyCode}
	if err := battle.play(ctx, request); err != nil {
		return err
	}
	return nil
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
	payload.PreviousPlayerSeat = -1
	payload.RemainingSeconds = battle.remainingActionMilliseconds() / 1000
	payload.FinishedPlayerCount = uint8(battle.finishedPlayerCount())
	targetSeat := battle.seatOf(target)
	for seat, player := range battle.bySeat {
		payload.Players[seat] = GameScenePlayer{Player: player, Automated: battle.auto[player], Offline: battle.offline[player], HandCount: uint8(len(battle.hands[player])), Rank: battle.ranks[seat]}
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
	return GameInfoPayload{OutCardSeconds: 15, ServiceFee: battle.fee, Scores: scores, GameNum: battle.gameNum}
}

func (battle *NHSKBattleService) askOutCardPayload() AskOutCardPayload {
	return AskOutCardPayload{ActivePlayer: battle.bySeat[battle.activeSeat], VerifyCode: battle.verifyCode, ActionMilliseconds: battle.remainingActionMilliseconds()}
}

func (battle *NHSKBattleService) remainingActionMilliseconds() uint32 {
	if battle.deadlineAt.IsZero() {
		return 0
	}
	now := time.Now()
	if battle.service != nil {
		now = battle.service.Now()
	}
	remaining := battle.deadlineAt.Sub(now)
	if remaining <= 0 {
		return 0
	}
	return uint32(remaining / time.Millisecond)
}

func (battle *NHSKBattleService) replayName() string {
	return fmt.Sprintf("nhsk-%d-%d-%d.xml", battle.id, battle.gameNum, battle.subgameNum)
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
	battle.verifyCode++
	battle.turnRevision++
	if battle.service != nil {
		battle.deadlineAt = battle.service.Now().Add(15 * time.Second)
		if _, err := battle.service.After(15*time.Second, nhskBattleTimerCommand, battle.turnRevision); err != nil {
			return err
		}
	} else {
		battle.deadlineAt = time.Time{}
	}
	if battle.phase != NHSKBattlePlaying || battle.bySeat[battle.activeSeat] == "" {
		return nil
	}
	return battle.emit(ctx, ClientGameOutput{Targets: battle.activePlayers(), Kind: OutputAskOutCard, Payload: battle.askOutCardPayload()})
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

func (battle *NHSKBattleService) deal() {
	battle.hands = make(map[game.PlayerID][]byte, 4)
	for seat, player := range battle.bySeat {
		cards := make([]byte, 26)
		for index := range cards {
			cards[index] = byte((seat*26 + index) % 52)
		}
		battle.hands[player] = cards
	}
}

func (battle *NHSKBattleService) advanceSeat() {
	for step := 1; step <= 4; step++ {
		seat := (battle.activeSeat + step) % 4
		if player := battle.bySeat[seat]; player != "" && len(battle.hands[player]) > 0 && !battle.players[player].Exited {
			battle.activeSeat = seat
			return
		}
	}
}

func (battle *NHSKBattleService) validateCards(player game.PlayerID, cards []byte) (int, bool) {
	if len(cards) == 0 {
		return 0, true
	}
	hand := append([]byte(nil), battle.hands[player]...)
	seen := make(map[byte]bool, len(cards))
	rank := int(cards[0] % 13)
	for _, card := range cards {
		if seen[card] || int(card%13) != rank {
			return 0, false
		}
		seen[card] = true
		found := false
		for index, held := range hand {
			if held == card {
				hand = append(hand[:index], hand[index+1:]...)
				found = true
				break
			}
		}
		if !found {
			return 0, false
		}
	}
	return rank, true
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
	if battle.service == nil || battle.outputRef.ID == 0 || battle.matchID == 0 || battle.productID == 0 {
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
