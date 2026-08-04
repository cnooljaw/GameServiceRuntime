package nhsk

import (
	"context"
	"errors"
	"fmt"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// RouteLegacyGameplaySend resolves the Host binding and sends one normalized Legacy gameplay Command to Battle.
func RouteLegacyGameplaySend(ctx context.Context, runtime game.CommandRuntime, host gsr.ServiceRef, frame []byte) error {
	if runtime == nil || host.Node == "" || host.ID == 0 {
		return errInvalidLegacyInboundGameplayRelay
	}
	command, err := mapLegacyInboundGameplayRelay(frame)
	if err != nil {
		return err
	}
	ref, err := resolveLegacyBattle(ctx, runtime, host, command.BattleID)
	if err != nil {
		return err
	}
	return runtime.Send(ref, command.Command.ID, command.Command.Payload)
}

// RouteLegacyGameplayCall resolves the Host binding and calls one normalized Legacy gameplay Command on Battle.
func RouteLegacyGameplayCall(ctx context.Context, runtime game.CommandRuntime, host gsr.ServiceRef, frame []byte) (any, error) {
	if runtime == nil || host.Node == "" || host.ID == 0 {
		return nil, errInvalidLegacyInboundGameplayRelay
	}
	command, err := mapLegacyInboundGameplayRelay(frame)
	if err != nil {
		return nil, err
	}
	ref, err := resolveLegacyBattle(ctx, runtime, host, command.BattleID)
	if err != nil {
		return nil, err
	}
	return runtime.Call(ctx, ref, command.Command.ID, command.Command.Payload)
}

func resolveLegacyBattle(ctx context.Context, runtime game.CommandRuntime, host gsr.ServiceRef, id game.BattleID) (gsr.ServiceRef, error) {
	value, err := runtime.Call(ctx, host, ResolveBattleCommand, ResolveBattleRequest{BattleID: id})
	if err != nil {
		return gsr.ServiceRef{}, err
	}
	result, ok := value.(ResolveBattleResult)
	if !ok || result.BattleID != id || result.Ref.Node == "" || result.Ref.ID == 0 {
		return gsr.ServiceRef{}, fmt.Errorf("%w: invalid Host resolve reply", errInvalidLegacyInboundGameplayRelay)
	}
	return result.Ref, nil
}

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
