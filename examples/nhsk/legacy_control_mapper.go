package nhsk

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// LegacyControlTarget identifies which named Service receives a normalized
// GameMaster control Command.
type LegacyControlTarget uint8

const (
	// LegacyControlTargetHost sends the Command to `.nhsk-game-host`.
	LegacyControlTargetHost LegacyControlTarget = iota + 1
	// LegacyControlTargetBattle resolves BattleID through Host first.
	LegacyControlTargetBattle
)

// LegacyControlRoute is the protocol-independent result of decoding one old
// GameMaster control frame.
type LegacyControlRoute struct {
	Kind     legacywire.ControlKind
	Target   LegacyControlTarget
	BattleID game.BattleID
	Command  gsr.Command
}

var (
	errInvalidLegacyControl     = errors.New("nhsk: invalid Legacy control")
	errUnsupportedLegacyControl = errors.New("nhsk: unsupported Legacy control")
	errLegacyCreateTimeout      = errors.New("nhsk: Legacy NEW_GAME completion timeout")
)

const (
	legacyRoundStart     int32 = 0
	legacyRoundStop      int32 = 1
	legacyRoundPause     int32 = 2
	legacyRoundContinue  int32 = 3
	legacyRoundMatchStop int32 = 4
	legacyRoundException int32 = 5
)

// MapLegacyControl normalizes one decoded old GameMaster control message.
// ConnectionGeneration is copied into NEW_GAME so a later output service can
// fence writes to the same physical TCP connection.
func MapLegacyControl(control legacywire.LegacyControl, generation ConnectionGeneration) (LegacyControlRoute, error) {
	if control.BattleID == 0 && control.Kind != legacywire.ControlNewGame {
		return LegacyControlRoute{}, fmt.Errorf("%w: zero BattleID", errInvalidLegacyControl)
	}
	battleID := game.BattleID(control.BattleID)
	switch control.Kind {
	case legacywire.ControlNewGame:
		if control.BattleID == 0 || control.ProductID == 0 || control.GameID == 0 {
			return LegacyControlRoute{}, fmt.Errorf("%w: NEW_GAME identity", errInvalidLegacyControl)
		}
		if control.GameID != NHSKDescriptor.GameID {
			return LegacyControlRoute{}, fmt.Errorf("%w: unsupported game id %d", errUnsupportedLegacyControl, control.GameID)
		}
		return LegacyControlRoute{Kind: control.Kind, Target: LegacyControlTargetHost, BattleID: battleID, Command: gsr.Command{ID: BeginCreateBattleCommand, Payload: CreateBattleRequest{BattleID: battleID, IsNewbie: control.IsNewbie, ConnectionGeneration: generation}}}, nil
	case legacywire.ControlDeleteGame:
		return LegacyControlRoute{Kind: control.Kind, Target: LegacyControlTargetHost, BattleID: battleID, Command: gsr.Command{ID: RequestDeleteBattleCommand, Payload: RequestDeleteBattleRequest{BattleID: battleID, ConnectionGeneration: generation}}}, nil
	case legacywire.ControlInitGame:
		if control.ProductID == 0 || control.MatchID == 0 || control.RoundID == 0 || control.MaxGameNum > 65535 || control.MaxSubgameNum > 65535 {
			return LegacyControlRoute{}, fmt.Errorf("%w: INIT_GAME identity or limits", errInvalidLegacyControl)
		}
		rules := normalizeNHSKConfig(control.BaseRule, control.GameRule)
		return battleRoute(control.Kind, battleID, InitializeBattleCommand, InitializeBattleRequest{
			Identity:   BattleIdentity{BattleID: battleID, ProductID: control.ProductID, MatchID: control.MatchID, RoundID: control.RoundID, RoundUniCode: control.RoundUniCode},
			MaxGameNum: uint16(control.MaxGameNum), MaxSubgameNum: uint16(control.MaxSubgameNum), Fee: control.Fee, ScoreBase: control.ScoreBase, ScoreDenominator: control.ScoreDenominator,
			ReplayMetadata: ReplayMetadata{MatchName: control.MatchName, GameType: control.GameType, ScoreType: control.ScoreType, ScoreMode: control.ScoreMode, RoomID: control.RoomID, CreatorID: control.CreatorID},
			ReplayRules:    normalizeReplayRuleSnapshot(control.BaseRule), Rules: &rules,
		}), nil
	case legacywire.ControlUpdatePlayers:
		players := make([]BattlePlayer, 0, len(control.Players))
		for _, player := range control.Players {
			if player.UserID == 0 || player.SeatID >= 4 {
				return LegacyControlRoute{}, fmt.Errorf("%w: UPDATE_PLAYER record", errInvalidLegacyControl)
			}
			players = append(players, BattlePlayer{Player: game.PlayerID(strconv.FormatUint(uint64(player.UserID), 10)), UserID: player.UserID, SeatID: player.SeatID, Score: player.Score, Exp: player.Exp, PlayerState: player.PlayerState, Nickname: player.Nickname, ClientID: player.ClientID, Automated: player.IsAI})
		}
		if len(players) == 0 {
			return LegacyControlRoute{}, fmt.Errorf("%w: empty UPDATE_PLAYER", errInvalidLegacyControl)
		}
		return battleRoute(control.Kind, battleID, UpdatePlayersCommand, UpdatePlayersRequest{Players: players}), nil
	case legacywire.ControlUpdateGame:
		if control.GameNum == 0 || control.SubgameNum == 0 || control.GameNum > 65535 || control.SubgameNum > 65535 {
			return LegacyControlRoute{}, fmt.Errorf("%w: UPDATE_GAME numbers", errInvalidLegacyControl)
		}
		return battleRoute(control.Kind, battleID, PrepareSubgameCommand, PrepareSubgameRequest{GameNum: uint16(control.GameNum), SubgameNum: uint16(control.SubgameNum)}), nil
	case legacywire.ControlStartNewGame:
		return battleRoute(control.Kind, battleID, UpdateRoundContextCommand, UpdateRoundContextRequest{SecRoundTotal: control.SecRoundTotal, SecRoundUsed: control.SecRoundUsed, RoomInfo: control.RoomInfo}), nil
	case legacywire.ControlCommand:
		commandID, err := mapLegacyRoundCommand(control.Command)
		if err != nil {
			return LegacyControlRoute{}, err
		}
		return battleRoute(control.Kind, battleID, commandID, struct{}{}), nil
	case legacywire.ControlDress:
		if control.UserID == 0 {
			return LegacyControlRoute{}, fmt.Errorf("%w: DRESS user", errInvalidLegacyControl)
		}
		return battleRoute(control.Kind, battleID, UpdatePlayerDressCommand, UpdatePlayerDressRequest{Player: playerID(control.UserID), Dress: control.Dress}), nil
	case legacywire.ControlPlayerExit:
		if control.UserID == 0 {
			return LegacyControlRoute{}, fmt.Errorf("%w: PLAYER_EXIT user", errInvalidLegacyControl)
		}
		return battleRoute(control.Kind, battleID, ExitPlayerCommand, ExitPlayerRequest{Player: playerID(control.UserID)}), nil
	case legacywire.ControlSettlementAck:
		gains := make([]SettlementGain, 0, len(control.ResultDetails))
		for _, gain := range control.ResultDetails {
			gains = append(gains, SettlementGain{PayTeamID: gain.PayTeamID, GainTeamID: gain.GainTeamID, Score: gain.Score})
		}
		players := make([]SettlementPlayerResult, 0, len(control.PlayerResults))
		for _, player := range control.PlayerResults {
			players = append(players, SettlementPlayerResult{PlayerID: player.PlayerID, Flag: player.Flag, Score: player.Score, Exp: player.Exp, TeamID: player.TeamID})
		}
		return battleRoute(control.Kind, battleID, CompleteSettlementCommand, CompleteSettlementRequest{
			Success:    control.SettlementSuccess,
			ResultType: control.ResultType,
			TeamCount:  control.TeamCount,
			Gains:      gains,
			Players:    players,
		}), nil
	case legacywire.ControlUnsupported:
		return LegacyControlRoute{Kind: control.Kind, BattleID: battleID}, nil
	default:
		return LegacyControlRoute{}, fmt.Errorf("%w: kind %d", errUnsupportedLegacyControl, control.Kind)
	}
}

func battleRoute(kind legacywire.ControlKind, battleID game.BattleID, id gsr.CommandID, payload any) LegacyControlRoute {
	return LegacyControlRoute{Kind: kind, Target: LegacyControlTargetBattle, BattleID: battleID, Command: gsr.Command{ID: id, Payload: payload}}
}

func mapLegacyRoundCommand(value int32) (gsr.CommandID, error) {
	switch value {
	case legacyRoundStart:
		return StartSubgameCommand, nil
	case legacyRoundMatchStop:
		return ForceFinishSubgameCommand, nil
	case legacyRoundStop, legacyRoundPause, legacyRoundContinue, legacyRoundException:
		return 0, fmt.Errorf("%w: round command %d", errUnsupportedLegacyControl, value)
	default:
		return 0, fmt.Errorf("%w: round command %d", errUnsupportedLegacyControl, value)
	}
}

// RouteLegacyControlSend decodes and sends one old GameMaster control frame.
// NEW_GAME is completed before returning so the following ordered INIT_GAME
// frame can resolve the newly-created Battle without a race.
func RouteLegacyControlSend(ctx context.Context, runtime game.CommandRuntime, host gsr.ServiceRef, frame []byte, generation ConnectionGeneration) error {
	if runtime == nil || host.Node == "" || host.ID == 0 {
		return errInvalidLegacyControl
	}
	control, err := legacywire.DecodeControl(frame)
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidLegacyControl, err)
	}
	route, err := MapLegacyControl(control, generation)
	if err != nil {
		return err
	}
	if route.Target == 0 || route.Kind == legacywire.ControlUnsupported {
		return nil
	}
	if route.Target == LegacyControlTargetHost {
		return sendLegacyHostControl(ctx, runtime, host, route)
	}
	ref, err := resolveLegacyBattle(ctx, runtime, host, route.BattleID)
	if err != nil {
		return err
	}
	return runtime.Send(ref, route.Command.ID, route.Command.Payload)
}

// RouteLegacyControlCall is the Call form of RouteLegacyControlSend. It is
// useful to Cluster callers and tests that need the normalized Command reply;
// old TCP control frames themselves are normally Send-style.
func RouteLegacyControlCall(ctx context.Context, runtime game.CommandRuntime, host gsr.ServiceRef, frame []byte, generation ConnectionGeneration) (any, error) {
	if runtime == nil || host.Node == "" || host.ID == 0 {
		return nil, errInvalidLegacyControl
	}
	control, err := legacywire.DecodeControl(frame)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidLegacyControl, err)
	}
	route, err := MapLegacyControl(control, generation)
	if err != nil {
		return nil, err
	}
	if route.Target == 0 || route.Kind == legacywire.ControlUnsupported {
		return CommandResult{Accepted: true}, nil
	}
	if route.Target == LegacyControlTargetHost {
		return callLegacyHostControl(ctx, runtime, host, route)
	}
	ref, err := resolveLegacyBattle(ctx, runtime, host, route.BattleID)
	if err != nil {
		return nil, err
	}
	return runtime.Call(ctx, ref, route.Command.ID, route.Command.Payload)
}

func sendLegacyHostControl(ctx context.Context, runtime game.CommandRuntime, host gsr.ServiceRef, route LegacyControlRoute) error {
	if route.Kind != legacywire.ControlNewGame {
		return runtime.Send(host, route.Command.ID, route.Command.Payload)
	}
	_, err := completeLegacyCreate(ctx, runtime, host, route)
	return err
}

func completeLegacyCreate(ctx context.Context, runtime game.CommandRuntime, host gsr.ServiceRef, route LegacyControlRoute) (CreateBattleOperation, error) {
	value, err := runtime.Call(ctx, host, route.Command.ID, route.Command.Payload)
	if err != nil {
		return CreateBattleOperation{}, err
	}
	operation, ok := value.(CreateBattleOperation)
	if !ok {
		return CreateBattleOperation{}, fmt.Errorf("%w: NEW_GAME reply %T", errInvalidLegacyControl, value)
	}
	operation, err = waitLegacyCreateResult(ctx, runtime, host, operation)
	if err != nil {
		return operation, err
	}
	if operation.BattleID != route.BattleID || operation.Ref.Node == "" || operation.Ref.ID == 0 {
		return operation, fmt.Errorf("%w: invalid completed NEW_GAME operation", errInvalidLegacyControl)
	}
	return operation, nil
}

func callLegacyHostControl(ctx context.Context, runtime game.CommandRuntime, host gsr.ServiceRef, route LegacyControlRoute) (any, error) {
	if route.Kind != legacywire.ControlNewGame {
		return runtime.Call(ctx, host, route.Command.ID, route.Command.Payload)
	}
	return completeLegacyCreate(ctx, runtime, host, route)
}

func waitLegacyCreate(ctx context.Context, runtime game.CommandRuntime, host gsr.ServiceRef, operation CreateBattleOperation) error {
	_, err := waitLegacyCreateResult(ctx, runtime, host, operation)
	return err
}

func waitLegacyCreateResult(ctx context.Context, runtime game.CommandRuntime, host gsr.ServiceRef, operation CreateBattleOperation) (CreateBattleOperation, error) {
	if operation.Phase == HostOperationFailed {
		return operation, errors.New(operation.Rejection)
	}
	if operation.Phase == HostOperationCompleted {
		return operation, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return operation, context.Cause(ctx)
		case <-deadline.C:
			return operation, errLegacyCreateTimeout
		case <-ticker.C:
			value, err := runtime.Call(ctx, host, GetCreateBattleOperationCommand, GetCreateBattleOperationRequest{OperationID: operation.OperationID})
			if err != nil {
				return operation, err
			}
			current, ok := value.(CreateBattleOperation)
			if !ok {
				return operation, fmt.Errorf("%w: operation reply %T", errInvalidLegacyControl, value)
			}
			operation = current
			if current.Phase == HostOperationFailed {
				return current, errors.New(current.Rejection)
			}
			if current.Phase == HostOperationCompleted {
				return current, nil
			}
		}
	}
}

func playerID(userID uint32) game.PlayerID {
	return game.PlayerID(strconv.FormatUint(uint64(userID), 10))
}
