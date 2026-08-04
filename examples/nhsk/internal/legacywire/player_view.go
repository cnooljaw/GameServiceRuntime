package legacywire

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const playerViewRequestFrameSize = headerSize + gameHeaderSize

var errInvalidPlayerViewRequest = errors.New("legacywire: invalid player view request")

// PlayerViewRequest is one Legacy reconnect or scene request carrying the
// player's redundant GameHeader identity.
type PlayerViewRequest struct {
	UserID    uint32
	MatchID   uint32
	ProductID uint32
}

// DecodeUserReconnect decodes one exact Legacy 0x7208 USER_RECONNECT packet.
func DecodeUserReconnect(data []byte) (PlayerViewRequest, error) {
	return decodePlayerViewRequest(data, messageGameUserReconnect)
}

// DecodeGameSceneRequest decodes one exact Legacy 0x720D GAME_SCENE packet.
func DecodeGameSceneRequest(data []byte) (PlayerViewRequest, error) {
	return decodePlayerViewRequest(data, messageGameScene)
}

func decodePlayerViewRequest(data []byte, message uint32) (PlayerViewRequest, error) {
	if len(data) != playerViewRequestFrameSize {
		return PlayerViewRequest{}, invalidPlayerViewRequest("frame length %d is not %d", len(data), playerViewRequestFrameSize)
	}
	header, err := decodeHeader(data)
	if err != nil {
		return PlayerViewRequest{}, invalidPlayerViewRequest("header: %v", err)
	}
	if header.Type != message {
		return PlayerViewRequest{}, invalidPlayerViewRequest("message type %#x", header.Type)
	}
	if header.Length != playerViewRequestFrameSize {
		return PlayerViewRequest{}, invalidPlayerViewRequest("header length %d is not %d", header.Length, playerViewRequestFrameSize)
	}
	return PlayerViewRequest{
		UserID:    binary.LittleEndian.Uint32(data[headerSize : headerSize+4]),
		MatchID:   binary.LittleEndian.Uint32(data[headerSize+12 : headerSize+16]),
		ProductID: binary.LittleEndian.Uint32(data[headerSize+16 : headerSize+20]),
	}, nil
}

func invalidPlayerViewRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidPlayerViewRequest, fmt.Sprintf(format, args...))
}
