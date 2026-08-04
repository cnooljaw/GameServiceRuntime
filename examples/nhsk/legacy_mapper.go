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
	default:
		return gsr.Command{}, fmt.Errorf("%w: unsupported client MessageID %#x", errInvalidLegacyGameplayCommand, message)
	}
}

func copyLegacyGameplayCards(cards []byte, count uint8) []byte {
	result := make([]byte, int(count))
	copy(result, cards[:count])
	return result
}
