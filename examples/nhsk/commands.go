package nhsk

import (
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	// PlayCardsCommand submits one player play or pass to an NHSK Battle.
	PlayCardsCommand gsr.CommandID = 0x04100301
	// PreviewCardSelectionCommand publishes one non-authoritative player selection.
	PreviewCardSelectionCommand gsr.CommandID = 0x04100302
	// SetPlayerAutoStateCommand changes one NHSK player's auto-play state.
	SetPlayerAutoStateCommand gsr.CommandID = 0x04100303
)

// PlayCardsRequest submits one player play or pass candidate.
type PlayCardsRequest struct {
	Player     game.PlayerID
	Cards      []byte
	VerifyCode uint32
}

// PreviewCardSelectionRequest submits one non-authoritative player selection.
type PreviewCardSelectionRequest struct {
	Player game.PlayerID
	Cards  []byte
}

// SetPlayerAutoStateRequest changes one NHSK player's auto-play state.
type SetPlayerAutoStateRequest struct {
	Player  game.PlayerID
	Enabled bool
}
