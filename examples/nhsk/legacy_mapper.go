package nhsk

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

var errInvalidLegacyGameplayCommand = errors.New("nhsk: invalid Legacy gameplay command")

const legacyPlayerStateAuto uint32 = 1 << 0

func mapLegacyGameplayCommand(userID uint32, payload []byte) (gsr.Command, error) {
	if userID == 0 {
		return gsr.Command{}, fmt.Errorf("%w: zero user ID", errInvalidLegacyGameplayCommand)
	}
	message, err := legacywire.DecodeClientGameplayMessage(payload)
	if err != nil {
		return gsr.Command{}, fmt.Errorf("%w: %v", errInvalidLegacyGameplayCommand, err)
	}
	player := game.PlayerID(strconv.FormatUint(uint64(userID), 10))

	switch message {
	case legacywire.ClientGameplayOutCard:
		request, err := legacywire.DecodeOutCardRequest(payload)
		if err != nil {
			return gsr.Command{}, fmt.Errorf("%w: %v", errInvalidLegacyGameplayCommand, err)
		}
		return gsr.Command{
			ID: PlayCardsCommand,
			Payload: PlayCardsRequest{
				Player:     player,
				Cards:      copyLegacyGameplayCards(request.Cards[:], request.CardCount),
				VerifyCode: request.VerifyCode,
			},
		}, nil
	case legacywire.ClientGameplayCardAction:
		request, err := legacywire.DecodeCardActionRequest(payload)
		if err != nil {
			return gsr.Command{}, fmt.Errorf("%w: %v", errInvalidLegacyGameplayCommand, err)
		}
		return gsr.Command{
			ID: PreviewCardSelectionCommand,
			Payload: PreviewCardSelectionRequest{
				Player: player,
				Cards:  copyLegacyGameplayCards(request.Cards[:], request.CardCount),
			},
		}, nil
	case legacywire.ClientGameplayUserStateChange:
		request, err := legacywire.DecodeUserStateChange(payload)
		if err != nil {
			return gsr.Command{}, fmt.Errorf("%w: %v", errInvalidLegacyGameplayCommand, err)
		}
		if request.UserID != userID {
			return gsr.Command{}, fmt.Errorf(
				"%w: outer UserID %d differs from payload UserID %d",
				errInvalidLegacyGameplayCommand,
				userID,
				request.UserID,
			)
		}
		return gsr.Command{
			ID: SetPlayerAutoStateCommand,
			Payload: SetPlayerAutoStateRequest{
				Player:  player,
				Enabled: request.State&legacyPlayerStateAuto != 0,
			},
		}, nil
	case legacywire.ClientGameplayReconnect:
		request, err := legacywire.DecodeUserReconnect(payload)
		if err != nil {
			return gsr.Command{}, fmt.Errorf("%w: %v", errInvalidLegacyGameplayCommand, err)
		}
		if request.UserID != userID {
			return gsr.Command{}, fmt.Errorf("%w: reconnect user %d differs from outer user %d", errInvalidLegacyGameplayCommand, request.UserID, userID)
		}
		return gsr.Command{ID: ReconnectPlayerCommand, Payload: ReconnectPlayerRequest{Player: player}}, nil
	case legacywire.ClientGameplayScene:
		request, err := legacywire.DecodeGameSceneRequest(payload)
		if err != nil {
			return gsr.Command{}, fmt.Errorf("%w: %v", errInvalidLegacyGameplayCommand, err)
		}
		if request.UserID != userID {
			return gsr.Command{}, fmt.Errorf("%w: scene user %d differs from outer user %d", errInvalidLegacyGameplayCommand, request.UserID, userID)
		}
		return gsr.Command{ID: RequestGameSceneCommand, Payload: ReconnectPlayerRequest{Player: player}}, nil
	case legacywire.ClientGameplayPropUse:
		request, err := legacywire.DecodePropUse(payload)
		if err != nil {
			return gsr.Command{}, fmt.Errorf("%w: %v", errInvalidLegacyGameplayCommand, err)
		}
		if request.SenderID != userID {
			return gsr.Command{}, fmt.Errorf("%w: prop sender %d differs from outer user %d", errInvalidLegacyGameplayCommand, request.SenderID, userID)
		}
		return gsr.Command{ID: RecordPropUseCommand, Payload: RecordPropUseRequest{SenderID: request.SenderID, PropID: request.PropID, SendCount: request.SendCount, TargetIDs: append([]uint32(nil), request.TargetIDs...)}}, nil
	default:
		return gsr.Command{}, fmt.Errorf("%w: unsupported client MessageID %#x", errInvalidLegacyGameplayCommand, message)
	}
}

func copyLegacyGameplayCards(cards []byte, count uint8) []byte {
	result := make([]byte, int(count))
	copy(result, cards[:count])
	return result
}
