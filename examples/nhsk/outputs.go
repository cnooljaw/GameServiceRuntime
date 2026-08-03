package main

import (
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// ConnectionGeneration identifies one physical Legacy GameMaster connection.
type ConnectionGeneration uint64

// OutputKind identifies one protocol-independent NHSK output variant.
type OutputKind string

// OutputPayload is the closed set of typed NHSK client payload values.
type OutputPayload interface {
	isNHSKOutputPayload()
}

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

// GameOutputBatch is one Battle's immutable, ordered output commit.
type GameOutputBatch struct {
	BattleID             game.BattleID
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
