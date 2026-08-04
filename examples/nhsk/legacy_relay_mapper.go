package nhsk

import (
	"errors"
	"fmt"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

var errInvalidLegacyInboundGameplayRelay = errors.New("nhsk: invalid Legacy inbound gameplay relay")

type legacyInboundGameplayCommand struct {
	BattleID  game.BattleID
	MatchID   uint32
	ProductID uint32
	Command   gsr.Command
}

func mapLegacyInboundGameplayRelay(frame []byte) (legacyInboundGameplayCommand, error) {
	relay, err := legacywire.DecodeInboundGameRelay(frame)
	if err != nil {
		return legacyInboundGameplayCommand{}, fmt.Errorf("%w: %v", errInvalidLegacyInboundGameplayRelay, err)
	}
	if relay.BattleID == 0 {
		return legacyInboundGameplayCommand{}, fmt.Errorf("%w: zero BattleID", errInvalidLegacyInboundGameplayRelay)
	}
	if relay.UserID == 0 {
		return legacyInboundGameplayCommand{}, fmt.Errorf("%w: zero outer UserID", errInvalidLegacyInboundGameplayRelay)
	}
	if relay.GameHeader.UserID != relay.UserID {
		return legacyInboundGameplayCommand{}, fmt.Errorf(
			"%w: outer UserID %d differs from inner UserID %d",
			errInvalidLegacyInboundGameplayRelay,
			relay.UserID,
			relay.GameHeader.UserID,
		)
	}
	command, err := mapLegacyGameplayCommand(relay.UserID, relay.Payload)
	if err != nil {
		return legacyInboundGameplayCommand{}, fmt.Errorf("%w: %v", errInvalidLegacyInboundGameplayRelay, err)
	}
	return legacyInboundGameplayCommand{
		BattleID:  game.BattleID(relay.BattleID),
		MatchID:   relay.GameHeader.MatchID,
		ProductID: relay.GameHeader.ProductID,
		Command:   command,
	}, nil
}
