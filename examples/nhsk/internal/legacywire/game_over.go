package legacywire

import (
	"encoding/binary"
	"errors"
)

const (
	messageGLToGMGameOver uint32 = 0x8641
	gameOverReplaySize           = 80
	gameOverFixedSize            = 139
)

// GameOver is the old GameLogic→GameMaster terminal notification.
// Players is either empty for the reference force-stop shape or contains the
// four seat-indexed settlement records used by the normal terminal path.
type GameOver struct {
	BattleID       uint32
	Reason         int32
	ReplayName     string
	IsGameOver     bool
	YueJuEndReason int32
	YueJuEndPlayer uint32
	Players        []GameOverPlayer
}

// GameOverPlayer is one seat-indexed 9-byte GL→GM terminal player record.
type GameOverPlayer struct {
	Score     int32
	Exp       int32
	Automated bool
}

// EncodeGameOver encodes one GAME_OVER frame and its optional player suffix.
func EncodeGameOver(value GameOver) ([]byte, error) {
	if value.BattleID == 0 || value.ReplayName == "" || len(value.Players) != 0 && len(value.Players) != 4 {
		return nil, errors.New("legacywire: invalid GAME_OVER")
	}
	data := make([]byte, gameOverFixedSize+len(value.Players)*9)
	encodeHeader(data, bsHeader{Type: messageGLToGMGameOver, Length: uint32(len(data))})
	binary.LittleEndian.PutUint16(data[24:26], glHeaderSize)
	binary.LittleEndian.PutUint32(data[26:30], value.BattleID)
	binary.LittleEndian.PutUint32(data[34:38], uint32(value.Reason))
	copy(data[38:38+gameOverReplaySize], value.ReplayName)
	if value.IsGameOver {
		data[118] = 1
	}
	binary.LittleEndian.PutUint32(data[119:123], uint32(len(value.Players)))
	binary.LittleEndian.PutUint32(data[123:127], gameOverFixedSize)
	binary.LittleEndian.PutUint32(data[127:131], uint32(len(value.Players)*9))
	binary.LittleEndian.PutUint32(data[131:135], uint32(value.YueJuEndReason))
	binary.LittleEndian.PutUint32(data[135:139], value.YueJuEndPlayer)
	offset := gameOverFixedSize
	for _, player := range value.Players {
		binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(player.Score))
		binary.LittleEndian.PutUint32(data[offset+4:offset+8], uint32(player.Exp))
		if player.Automated {
			data[offset+8] = 1
		}
		offset += 9
	}
	return data, nil
}
