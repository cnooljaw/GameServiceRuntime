package legacywire

import (
	"encoding/binary"
	"errors"
)

const (
	gameScenePlayerCount     = 4
	gameSceneHandCardCount   = 26
	gameSceneLastCardCount   = 8
	gameSceneScoreCardCount  = 24
	gameSceneHeaderSize      = headerSize + 8 + 4 + 8
	gameSceneBodySize        = 42
	gameScenePlayerSize      = 49
	gameScenePlayersBodySize = gameScenePlayerCount * gameScenePlayerSize
	gameSceneFrameSize       = gameSceneHeaderSize + gameSceneBodySize + gameScenePlayersBodySize
)

var errInvalidGameScene = errors.New("legacywire: invalid GAME_SCENE")

// GameScenePlayer is one seat in a receiver-specific Legacy NHSK scene.
type GameScenePlayer struct {
	UserID          uint32
	State           uint16
	HandCards       [gameSceneHandCardCount]byte
	HandCount       uint8
	LastPlayedCards [gameSceneLastCardCount]byte
	LastPlayCount   int8
	CapturedPoints  uint16
	Rank            uint8
}

// GameScene is the normalized input for one Legacy NHSK GAME_SCENE packet.
type GameScene struct {
	State               uint8
	ActiveSeat          int8
	PreviousPlayerSeat  int8
	RemainingSeconds    uint32
	TrickScoreCards     [gameSceneScoreCardCount]byte
	TrickScoreCardCount uint8
	FinishedPlayerCount uint8
	Players             [gameScenePlayerCount]GameScenePlayer
}

// EncodeGameScene encodes one exact Legacy 0x7608 NHSK GAME_SCENE packet.
func EncodeGameScene(scene GameScene) ([]byte, error) {
	if scene.State != 3 && scene.State != 4 || !validGameSceneSeat(scene.ActiveSeat) || !validGameSceneSeat(scene.PreviousPlayerSeat) {
		return nil, errInvalidGameScene
	}
	if scene.TrickScoreCardCount > gameSceneScoreCardCount || scene.FinishedPlayerCount > gameScenePlayerCount || hasNonzero(scene.TrickScoreCards[scene.TrickScoreCardCount:]) {
		return nil, errInvalidGameScene
	}
	for _, player := range scene.Players {
		if player.State&^3 != 0 || player.HandCount > gameSceneHandCardCount || hasNonzero(player.HandCards[player.HandCount:]) || player.Rank > 4 {
			return nil, errInvalidGameScene
		}
		if player.LastPlayCount < -1 || player.LastPlayCount > gameSceneLastCardCount {
			return nil, errInvalidGameScene
		}
		if player.LastPlayCount <= 0 && hasNonzero(player.LastPlayedCards[:]) || player.LastPlayCount > 0 && hasNonzero(player.LastPlayedCards[player.LastPlayCount:]) {
			return nil, errInvalidGameScene
		}
	}

	data := make([]byte, gameSceneFrameSize)
	encodeHeader(data, bsHeader{Type: messageNHSKGameScene, Length: gameSceneFrameSize})
	binary.LittleEndian.PutUint32(data[headerSize:headerSize+4], gameSceneHeaderSize)
	binary.LittleEndian.PutUint32(data[headerSize+4:headerSize+8], gameSceneBodySize)
	binary.LittleEndian.PutUint32(data[headerSize+8:headerSize+12], gameScenePlayerCount)
	binary.LittleEndian.PutUint32(data[headerSize+12:headerSize+16], gameSceneHeaderSize+gameSceneBodySize)
	binary.LittleEndian.PutUint32(data[headerSize+16:gameSceneHeaderSize], gameScenePlayersBodySize)

	offset := gameSceneHeaderSize
	binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(scene.State))
	offset += 4
	binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(int32(scene.ActiveSeat)))
	offset += 4
	binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(int32(scene.PreviousPlayerSeat)))
	offset += 4
	binary.LittleEndian.PutUint32(data[offset:offset+4], scene.RemainingSeconds)
	offset += 4
	copy(data[offset:offset+gameSceneScoreCardCount], scene.TrickScoreCards[:])
	offset += gameSceneScoreCardCount
	data[offset] = scene.TrickScoreCardCount
	data[offset+1] = scene.FinishedPlayerCount
	offset += 2

	for _, player := range scene.Players {
		binary.LittleEndian.PutUint32(data[offset:offset+4], player.UserID)
		offset += 4
		binary.LittleEndian.PutUint16(data[offset:offset+2], player.State)
		offset += 2
		copy(data[offset:offset+gameSceneHandCardCount], player.HandCards[:])
		offset += gameSceneHandCardCount
		data[offset] = player.HandCount
		offset++
		copy(data[offset:offset+gameSceneLastCardCount], player.LastPlayedCards[:])
		offset += gameSceneLastCardCount
		binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(int32(player.LastPlayCount)))
		offset += 4
		binary.LittleEndian.PutUint16(data[offset:offset+2], player.CapturedPoints)
		offset += 2
		binary.LittleEndian.PutUint16(data[offset:offset+2], uint16(player.Rank))
		offset += 2
	}
	return data, nil
}

func validGameSceneSeat(seat int8) bool {
	return seat >= -1 && seat < gameScenePlayerCount
}

func hasNonzero(values []byte) bool {
	for _, value := range values {
		if value != 0 {
			return true
		}
	}
	return false
}
