package main

import (
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// ConnectionGeneration identifies one physical Legacy GameMaster connection.
type ConnectionGeneration uint64

// OutputKind identifies one protocol-independent NHSK output variant.
type OutputKind string

const (
	// OutputGameStart tells clients that one NHSK subgame is starting.
	OutputGameStart OutputKind = "game_start"
	// OutputGameInfo publishes the current NHSK subgame configuration and seat scores.
	OutputGameInfo OutputKind = "game_info"
	// OutputDeal privately delivers one seat's initial hand.
	OutputDeal OutputKind = "deal"
	// OutputAskOutCard publishes the current player's action opportunity.
	OutputAskOutCard OutputKind = "ask_out_card"
	// OutputOutCardInfo publishes one committed play or pass.
	OutputOutCardInfo OutputKind = "out_card_info"
	// OutputTurnEnd publishes one completed trick and its captured points.
	OutputTurnEnd OutputKind = "turn_end"
	// OutputShowCards publishes one receiver-specific four-seat hand view.
	OutputShowCards OutputKind = "show_cards"
	// OutputGameResult publishes one applied NHSK subgame result.
	OutputGameResult OutputKind = "game_result"
	// OutputGameScene publishes one receiver-specific NHSK scene snapshot.
	OutputGameScene OutputKind = "game_scene"
)

// GameSceneState identifies one client-visible NHSK scene state.
type GameSceneState uint8

const (
	// GameSceneStatePlaying is the active out-card scene.
	GameSceneStatePlaying GameSceneState = 3
	// GameSceneStateShowingResult is the terminal hand-reveal scene.
	GameSceneStateShowingResult GameSceneState = 4
)

// GameOverReason identifies why one NHSK subgame ended.
type GameOverReason uint32

const (
	// GameOverReasonSuccess means the subgame ended by normal play.
	GameOverReasonSuccess GameOverReason = iota
	// GameOverReasonEscape means a player escaped the subgame.
	GameOverReasonEscape
	// GameOverReasonOffline means the subgame ended because a player was offline.
	GameOverReasonOffline
	// GameOverReasonException means the subgame ended because of an exception.
	GameOverReasonException
	// GameOverReasonDissolve means the subgame was dissolved.
	GameOverReasonDissolve
)

// SubgameResult identifies the NHSK single/double/peace result category.
type SubgameResult uint8

const (
	// SubgameResultSingle is a single win.
	SubgameResultSingle SubgameResult = iota
	// SubgameResultDouble is a double win.
	SubgameResultDouble
	// SubgameResultPeace is a tied subgame.
	SubgameResultPeace
)

// PlayerOutcome identifies one player's NHSK result.
type PlayerOutcome uint8

const (
	// PlayerOutcomeWin marks a winning player.
	PlayerOutcomeWin PlayerOutcome = iota
	// PlayerOutcomeLoss marks a losing player.
	PlayerOutcomeLoss
	// PlayerOutcomePeace marks a tied player.
	PlayerOutcomePeace
)

// OutputPayload is the closed set of typed NHSK client payload values.
type OutputPayload interface {
	isNHSKOutputPayload()
}

// GameStartPayload is the bodyless client GAME_START fact.
type GameStartPayload struct{}

func (GameStartPayload) isNHSKOutputPayload() {}

// GameInfoPayload is the protocol-independent NHSK game information snapshot.
type GameInfoPayload struct {
	OutCardSeconds uint32
	ServiceFee     int32
	Scores         [4]int32
	GameNum        uint16
}

func (GameInfoPayload) isNHSKOutputPayload() {}

// DealPayload is one seat's private immutable initial hand.
type DealPayload struct {
	Players [4]game.PlayerID
	SeatID  uint8
	Cards   [26]byte
}

func (DealPayload) isNHSKOutputPayload() {}

// AskOutCardPayload is one current NHSK action opportunity snapshot.
type AskOutCardPayload struct {
	ActivePlayer       game.PlayerID
	VerifyCode         uint32
	ActionMilliseconds uint32
}

func (AskOutCardPayload) isNHSKOutputPayload() {}

// OutCardInfoPayload is one committed NHSK play or pass fact.
type OutCardInfoPayload struct {
	Player    game.PlayerID
	Cards     [8]byte
	CardCount uint8
}

func (OutCardInfoPayload) isNHSKOutputPayload() {}

// TurnEndPayload is one completed NHSK trick fact.
type TurnEndPayload struct {
	Winner         game.PlayerID
	CapturedPoints uint32
}

func (TurnEndPayload) isNHSKOutputPayload() {}

// ShowCardsPayload is one receiver-specific NHSK four-seat hand view.
type ShowCardsPayload struct {
	Players    [4]game.PlayerID
	HandCounts [4]uint8
	Cards      [4][26]byte
}

func (ShowCardsPayload) isNHSKOutputPayload() {}

// GameResultPayload is one applied NHSK subgame result snapshot.
type GameResultPayload struct {
	Reason         GameOverReason
	Players        [4]game.PlayerID
	Automated      [4]bool
	Scores         [4]int32
	Outcomes       [4]PlayerOutcome
	CapturedPoints [4]uint16
	Ranks          [4]uint8
	Result         SubgameResult
	WinningTeam    uint8
	ReplayUID      string
}

func (GameResultPayload) isNHSKOutputPayload() {}

// GameScenePlayer is one seat in a receiver-specific NHSK scene snapshot.
type GameScenePlayer struct {
	Player          game.PlayerID
	Automated       bool
	Offline         bool
	HandCards       [26]byte
	HandCount       uint8
	LastPlayedCards [8]byte
	LastPlayCount   int8
	CapturedPoints  uint16
	Rank            uint8
}

// GameScenePayload is one complete receiver-specific NHSK scene snapshot.
type GameScenePayload struct {
	State               GameSceneState
	ActiveSeat          int8
	PreviousPlayerSeat  int8
	RemainingSeconds    uint32
	TrickScoreCards     [24]byte
	TrickScoreCardCount uint8
	FinishedPlayerCount uint8
	Players             [4]GameScenePlayer
}

func (GameScenePayload) isNHSKOutputPayload() {}

// GameOutput is the closed set of outputs produced by NHSK Battle logic.
type GameOutput interface {
	isNHSKGameOutput()
}

// ClientGameOutput delivers one typed payload to a frozen player target list.
type ClientGameOutput struct {
	Targets []game.PlayerID
	Kind    OutputKind
	Payload OutputPayload
}

func (ClientGameOutput) isNHSKGameOutput() {}

// GameStartedOutput tells the game coordinator that one subgame has started.
type GameStartedOutput struct {
	ReplayName string
}

func (GameStartedOutput) isNHSKGameOutput() {}

// GameOutputBatch is one Battle's immutable, ordered output commit.
type GameOutputBatch struct {
	BattleID             game.BattleID
	MatchID              uint32
	ProductID            uint32
	Ref                  gsr.ServiceRef
	ConnectionGeneration ConnectionGeneration
	Outputs              []GameOutput
}

// ConnectionFailureKind classifies a stable output-path connection failure.
type ConnectionFailureKind string

const (
	// ConnectionFailureOutputSendRejected means a Battle could not enter the OutputService mailbox.
	ConnectionFailureOutputSendRejected ConnectionFailureKind = "output_send_rejected"
	// ConnectionFailureOutputSinkRejected means OutputService could not submit an accepted batch to its sink.
	ConnectionFailureOutputSinkRejected ConnectionFailureKind = "output_sink_rejected"
)

// ConnectionFailureReporter reports an output-path failure for one connection generation.
type ConnectionFailureReporter interface {
	FailConnection(ConnectionGeneration, ConnectionFailureKind)
}
