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
