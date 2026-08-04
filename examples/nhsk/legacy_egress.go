package nhsk

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
	"github.com/lijiawang/GameServiceRuntime/game"
)

var errInvalidLegacyGameOutput = errors.New("nhsk: invalid Legacy GameOutput")

func encodeLegacyGameOutputBatch(batch GameOutputBatch) ([][]byte, error) {
	if !validGameOutputBatch(batch) {
		return nil, errInvalidLegacyGameOutput
	}
	var frames [][]byte
	for _, gameOutput := range batch.Outputs {
		switch output := gameOutput.(type) {
		case ClientGameOutput:
			payload, err := encodeLegacyClientPayload(output)
			if err != nil {
				return nil, err
			}
			targets, err := legacyTargetUserIDs(output.Targets)
			if err != nil {
				return nil, err
			}
			for _, userID := range targets {
				frame, err := legacywire.EncodeOutboundGameRelay(legacywire.OutboundGameRelay{
					BattleID:  uint32(batch.BattleID),
					UserID:    userID,
					MatchID:   batch.MatchID,
					ProductID: batch.ProductID,
					Payload:   payload,
				})
				if err != nil {
					return nil, fmt.Errorf("%w: relay: %v", errInvalidLegacyGameOutput, err)
				}
				frames = append(frames, frame)
			}
		case GameStartedOutput:
			if output.ReplayName == "" {
				return nil, fmt.Errorf("%w: empty GAME_STARTED replay name", errInvalidLegacyGameOutput)
			}
			frames = append(frames, legacywire.EncodeGameStarted(uint32(batch.BattleID), output.ReplayName))
		default:
			return nil, fmt.Errorf("%w: unsupported output type %T", errInvalidLegacyGameOutput, gameOutput)
		}
	}
	return frames, nil
}

func encodeLegacyClientPayload(output ClientGameOutput) ([]byte, error) {
	switch output.Kind {
	case OutputGameStart:
		if _, ok := output.Payload.(GameStartPayload); !ok {
			return nil, fmt.Errorf("%w: %s payload %T", errInvalidLegacyGameOutput, output.Kind, output.Payload)
		}
		return legacywire.EncodeGameStart(), nil
	case OutputGameInfo:
		payload, ok := output.Payload.(GameInfoPayload)
		if !ok {
			return nil, fmt.Errorf("%w: %s payload %T", errInvalidLegacyGameOutput, output.Kind, output.Payload)
		}
		return legacywire.EncodeGameInfo(legacywire.GameInfo{
			OutCardSeconds: payload.OutCardSeconds,
			ServiceFee:     payload.ServiceFee,
			Scores:         payload.Scores,
			GameNum:        payload.GameNum,
		}), nil
	case OutputDeal:
		payload, ok := output.Payload.(DealPayload)
		if !ok {
			return nil, fmt.Errorf("%w: %s payload %T", errInvalidLegacyGameOutput, output.Kind, output.Payload)
		}
		return encodeLegacyDeal(output, payload)
	case OutputAskOutCard:
		payload, ok := output.Payload.(AskOutCardPayload)
		if !ok {
			return nil, fmt.Errorf("%w: %s payload %T", errInvalidLegacyGameOutput, output.Kind, output.Payload)
		}
		return encodeLegacyAskOutCard(payload)
	case OutputOutCardInfo:
		payload, ok := output.Payload.(OutCardInfoPayload)
		if !ok {
			return nil, fmt.Errorf("%w: %s payload %T", errInvalidLegacyGameOutput, output.Kind, output.Payload)
		}
		return encodeLegacyOutCardInfo(payload)
	case OutputTurnEnd:
		payload, ok := output.Payload.(TurnEndPayload)
		if !ok {
			return nil, fmt.Errorf("%w: %s payload %T", errInvalidLegacyGameOutput, output.Kind, output.Payload)
		}
		return encodeLegacyTurnEnd(payload)
	case OutputShowCards:
		payload, ok := output.Payload.(ShowCardsPayload)
		if !ok {
			return nil, fmt.Errorf("%w: %s payload %T", errInvalidLegacyGameOutput, output.Kind, output.Payload)
		}
		return encodeLegacyShowCards(payload)
	case OutputGameResult:
		payload, ok := output.Payload.(GameResultPayload)
		if !ok {
			return nil, fmt.Errorf("%w: %s payload %T", errInvalidLegacyGameOutput, output.Kind, output.Payload)
		}
		return encodeLegacyGameResult(payload)
	case OutputGameScene:
		payload, ok := output.Payload.(GameScenePayload)
		if !ok {
			return nil, fmt.Errorf("%w: %s payload %T", errInvalidLegacyGameOutput, output.Kind, output.Payload)
		}
		return encodeLegacyGameScene(output, payload)
	case OutputOutCardRejection:
		payload, ok := output.Payload.(OutCardRejectionPayload)
		if !ok {
			return nil, fmt.Errorf("%w: %s payload %T", errInvalidLegacyGameOutput, output.Kind, output.Payload)
		}
		return encodeLegacyOutCardRejection(output, payload)
	case OutputCardSelectionPreview:
		payload, ok := output.Payload.(CardSelectionPreviewPayload)
		if !ok {
			return nil, fmt.Errorf("%w: %s payload %T", errInvalidLegacyGameOutput, output.Kind, output.Payload)
		}
		return encodeLegacyCardSelectionPreview(payload)
	default:
		return nil, fmt.Errorf("%w: output kind %q", errInvalidLegacyGameOutput, output.Kind)
	}
}

func encodeLegacyCardSelectionPreview(payload CardSelectionPreviewPayload) ([]byte, error) {
	players, err := legacyTargetUserIDs([]game.PlayerID{payload.Player})
	if err != nil {
		return nil, err
	}
	frame, err := legacywire.EncodeCardActionWatch(legacywire.CardActionWatch{
		UserID:    players[0],
		Cards:     payload.Cards,
		CardCount: payload.CardCount,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: CARD_ACTION_WATCH: %v", errInvalidLegacyGameOutput, err)
	}
	return frame, nil
}

func encodeLegacyOutCardRejection(output ClientGameOutput, payload OutCardRejectionPayload) ([]byte, error) {
	if len(output.Targets) != 1 {
		return nil, fmt.Errorf("%w: OUT_CARD_RESULT target count %d", errInvalidLegacyGameOutput, len(output.Targets))
	}
	frame, err := legacywire.EncodeOutCardResult(uint32(payload.Reason))
	if err != nil {
		return nil, fmt.Errorf("%w: OUT_CARD_RESULT: %v", errInvalidLegacyGameOutput, err)
	}
	return frame, nil
}

func encodeLegacyGameScene(output ClientGameOutput, payload GameScenePayload) ([]byte, error) {
	if len(output.Targets) != 1 {
		return nil, fmt.Errorf("%w: GAME_SCENE target count %d", errInvalidLegacyGameOutput, len(output.Targets))
	}
	players := make([]game.PlayerID, len(payload.Players))
	for seat, player := range payload.Players {
		players[seat] = player.Player
	}
	userIDs, err := legacyTargetUserIDs(players)
	if err != nil {
		return nil, err
	}
	targets, err := legacyTargetUserIDs(output.Targets)
	if err != nil {
		return nil, err
	}
	var targetFound bool
	for _, userID := range userIDs {
		if targets[0] == userID {
			targetFound = true
			break
		}
	}
	if !targetFound {
		return nil, fmt.Errorf("%w: GAME_SCENE target %d is not a player", errInvalidLegacyGameOutput, targets[0])
	}

	var wirePlayers [4]legacywire.GameScenePlayer
	for seat, player := range payload.Players {
		var state uint16
		if player.Automated {
			state |= 1
		}
		if player.Offline {
			state |= 2
		}
		wirePlayers[seat] = legacywire.GameScenePlayer{
			UserID:          userIDs[seat],
			State:           state,
			HandCards:       player.HandCards,
			HandCount:       player.HandCount,
			LastPlayedCards: player.LastPlayedCards,
			LastPlayCount:   player.LastPlayCount,
			CapturedPoints:  player.CapturedPoints,
			Rank:            player.Rank,
		}
	}
	frame, err := legacywire.EncodeGameScene(legacywire.GameScene{
		State:               uint8(payload.State),
		ActiveSeat:          payload.ActiveSeat,
		PreviousPlayerSeat:  payload.PreviousPlayerSeat,
		RemainingSeconds:    payload.RemainingSeconds,
		TrickScoreCards:     payload.TrickScoreCards,
		TrickScoreCardCount: payload.TrickScoreCardCount,
		FinishedPlayerCount: payload.FinishedPlayerCount,
		Players:             wirePlayers,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: GAME_SCENE: %v", errInvalidLegacyGameOutput, err)
	}
	return frame, nil
}

func encodeLegacyGameResult(payload GameResultPayload) ([]byte, error) {
	players, err := legacyTargetUserIDs(payload.Players[:])
	if err != nil {
		return nil, err
	}
	var outcomes [4]uint8
	for seat, outcome := range payload.Outcomes {
		outcomes[seat] = uint8(outcome)
	}
	frame, err := legacywire.EncodeGameResult(legacywire.GameResult{
		Reason:         uint32(payload.Reason),
		UserIDs:        [4]uint32{players[0], players[1], players[2], players[3]},
		Automated:      payload.Automated,
		Scores:         payload.Scores,
		Outcomes:       outcomes,
		CapturedPoints: payload.CapturedPoints,
		Ranks:          payload.Ranks,
		Result:         uint8(payload.Result),
		WinningTeam:    payload.WinningTeam,
		ReplayUID:      payload.ReplayUID,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: GAME_RESULT: %v", errInvalidLegacyGameOutput, err)
	}
	return frame, nil
}

func encodeLegacyShowCards(payload ShowCardsPayload) ([]byte, error) {
	players, err := legacyTargetUserIDs(payload.Players[:])
	if err != nil {
		return nil, err
	}
	frame, err := legacywire.EncodeShowCards(legacywire.ShowCards{
		UserIDs:    [4]uint32{players[0], players[1], players[2], players[3]},
		Cards:      payload.Cards,
		CardCounts: payload.HandCounts,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: SHOW_CARDS: %v", errInvalidLegacyGameOutput, err)
	}
	return frame, nil
}

func encodeLegacyTurnEnd(payload TurnEndPayload) ([]byte, error) {
	winners, err := legacyTargetUserIDs([]game.PlayerID{payload.Winner})
	if err != nil {
		return nil, err
	}
	return legacywire.EncodeTurnEnd(legacywire.TurnEnd{
		WinnerUserID:   winners[0],
		CapturedPoints: payload.CapturedPoints,
	}), nil
}

func encodeLegacyOutCardInfo(payload OutCardInfoPayload) ([]byte, error) {
	if int(payload.CardCount) > len(payload.Cards) {
		return nil, fmt.Errorf("%w: OUT_CARD_INFO card count %d", errInvalidLegacyGameOutput, payload.CardCount)
	}
	players, err := legacyTargetUserIDs([]game.PlayerID{payload.Player})
	if err != nil {
		return nil, err
	}
	frame, err := legacywire.EncodeOutCardInfo(legacywire.OutCardInfo{
		UserID: players[0],
		Cards:  payload.Cards[:payload.CardCount],
	})
	if err != nil {
		return nil, fmt.Errorf("%w: OUT_CARD_INFO: %v", errInvalidLegacyGameOutput, err)
	}
	return frame, nil
}

func encodeLegacyAskOutCard(payload AskOutCardPayload) ([]byte, error) {
	if payload.VerifyCode == 0 {
		return nil, fmt.Errorf("%w: ASK_OUT_CARD zero verify code", errInvalidLegacyGameOutput)
	}
	active, err := legacyTargetUserIDs([]game.PlayerID{payload.ActivePlayer})
	if err != nil {
		return nil, err
	}
	return legacywire.EncodeAskOutCard(legacywire.AskOutCard{
		UserID:             active[0],
		VerifyCode:         payload.VerifyCode,
		ActionMilliseconds: payload.ActionMilliseconds,
	}), nil
}

func encodeLegacyDeal(output ClientGameOutput, payload DealPayload) ([]byte, error) {
	if len(output.Targets) != 1 {
		return nil, fmt.Errorf("%w: DEAL target count %d", errInvalidLegacyGameOutput, len(output.Targets))
	}
	if payload.SeatID >= uint8(len(payload.Players)) {
		return nil, fmt.Errorf("%w: DEAL seat %d", errInvalidLegacyGameOutput, payload.SeatID)
	}
	userIDs, err := legacyTargetUserIDs(payload.Players[:])
	if err != nil {
		return nil, err
	}
	targets, err := legacyTargetUserIDs(output.Targets)
	if err != nil {
		return nil, err
	}
	if targets[0] != userIDs[payload.SeatID] {
		return nil, fmt.Errorf("%w: DEAL target %d does not match seat %d user %d", errInvalidLegacyGameOutput, targets[0], payload.SeatID, userIDs[payload.SeatID])
	}
	frame, err := legacywire.EncodeDeal(legacywire.Deal{
		UserIDs: [4]uint32{userIDs[0], userIDs[1], userIDs[2], userIDs[3]},
		SeatID:  payload.SeatID,
		Cards:   payload.Cards,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: DEAL: %v", errInvalidLegacyGameOutput, err)
	}
	return frame, nil
}

func legacyTargetUserIDs(targets []game.PlayerID) ([]uint32, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("%w: empty targets", errInvalidLegacyGameOutput)
	}
	result := make([]uint32, len(targets))
	seen := make(map[uint32]struct{}, len(targets))
	for index, target := range targets {
		value, err := strconv.ParseUint(string(target), 10, 32)
		if err != nil || value == 0 {
			return nil, fmt.Errorf("%w: target %q", errInvalidLegacyGameOutput, target)
		}
		userID := uint32(value)
		if _, exists := seen[userID]; exists {
			return nil, fmt.Errorf("%w: duplicate target %d", errInvalidLegacyGameOutput, userID)
		}
		seen[userID] = struct{}{}
		result[index] = userID
	}
	return result, nil
}
