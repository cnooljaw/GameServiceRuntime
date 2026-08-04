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

// GameOver is the minimal old GameLogic→GameMaster terminal notification.
// The optional player suffix is intentionally omitted until the settlement
// adapter owns the reference PlayerData projection.
type GameOver struct {
	BattleID       uint32
	Reason         int32
	ReplayName     string
	IsGameOver     bool
	YueJuEndReason int32
	YueJuEndPlayer uint32
}

// EncodeGameOver encodes one fixed empty-player-data GAME_OVER frame.
func EncodeGameOver(value GameOver) ([]byte, error) {
	if value.BattleID == 0 || value.ReplayName == "" {
		return nil, errors.New("legacywire: invalid GAME_OVER")
	}
	data := make([]byte, gameOverFixedSize)
	encodeHeader(data, bsHeader{Type: messageGLToGMGameOver, Length: uint32(len(data))})
	binary.LittleEndian.PutUint16(data[24:26], glHeaderSize)
	binary.LittleEndian.PutUint32(data[26:30], value.BattleID)
	binary.LittleEndian.PutUint32(data[34:38], uint32(value.Reason))
	copy(data[38:38+gameOverReplaySize], value.ReplayName)
	if value.IsGameOver {
		data[118] = 1
	}
	// PlayerCount=0; the suffix index is absolute and empty.
	binary.LittleEndian.PutUint32(data[123:127], gameOverFixedSize)
	binary.LittleEndian.PutUint32(data[127:131], 0)
	binary.LittleEndian.PutUint32(data[131:135], uint32(value.YueJuEndReason))
	binary.LittleEndian.PutUint32(data[135:139], value.YueJuEndPlayer)
	return data, nil
}
