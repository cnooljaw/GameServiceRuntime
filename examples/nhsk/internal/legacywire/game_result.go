package legacywire

import (
	"encoding/binary"
	"errors"
)

const (
	gameResultPlayerCount = 4
	gameResultReplaySize  = 64
	gameResultDetailSize  = 122
	gameResultHeaderSize  = headerSize + 8
	gameResultFrameSize   = gameResultHeaderSize + gameResultDetailSize
)

var errInvalidGameResult = errors.New("legacywire: invalid GAME_RESULT")

// GameResult is the normalized input for one Legacy NHSK GAME_RESULT packet.
type GameResult struct {
	Reason         uint32
	UserIDs        [gameResultPlayerCount]uint32
	Automated      [gameResultPlayerCount]bool
	Scores         [gameResultPlayerCount]int32
	Outcomes       [gameResultPlayerCount]uint8
	CapturedPoints [gameResultPlayerCount]uint16
	Ranks          [gameResultPlayerCount]uint8
	Result         uint8
	WinningTeam    uint8
	ReplayUID      string
}

// EncodeGameResult encodes one exact Legacy 0x7607 NHSK GAME_RESULT packet.
func EncodeGameResult(result GameResult) ([]byte, error) {
	if result.Reason > 4 || result.Result > 2 || result.WinningTeam > 1 || result.Result == 2 && result.WinningTeam != 0 {
		return nil, errInvalidGameResult
	}
	for seat := 0; seat < gameResultPlayerCount; seat++ {
		if result.Outcomes[seat] > 2 || result.Ranks[seat] < 1 || result.Ranks[seat] > 4 {
			return nil, errInvalidGameResult
		}
	}

	data := make([]byte, gameResultFrameSize)
	encodeHeader(data, bsHeader{Type: messageNHSKGameResult, Length: gameResultFrameSize})
	binary.LittleEndian.PutUint32(data[headerSize:headerSize+4], gameResultHeaderSize)
	binary.LittleEndian.PutUint32(data[headerSize+4:gameResultHeaderSize], gameResultDetailSize)

	offset := gameResultHeaderSize
	binary.LittleEndian.PutUint32(data[offset:offset+4], result.Reason)
	offset += 4
	for _, userID := range result.UserIDs {
		binary.LittleEndian.PutUint32(data[offset:offset+4], userID)
		offset += 4
	}
	for _, automated := range result.Automated {
		if automated {
			data[offset] = 1
		}
		offset++
	}
	for _, score := range result.Scores {
		binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(score))
		offset += 4
	}
	copy(data[offset:offset+gameResultPlayerCount], result.Outcomes[:])
	offset += gameResultPlayerCount
	for _, points := range result.CapturedPoints {
		binary.LittleEndian.PutUint16(data[offset:offset+2], points)
		offset += 2
	}
	copy(data[offset:offset+gameResultPlayerCount], result.Ranks[:])
	offset += gameResultPlayerCount
	data[offset] = result.Result
	data[offset+1] = result.WinningTeam
	offset += 2
	copy(data[offset:offset+gameResultReplaySize], result.ReplayUID)
	return data, nil
}
