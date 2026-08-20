package legacywire

import (
	"encoding/binary"
	"errors"
)

const (
	messageGLToGMSettlementRequest uint32 = 0x8650
	settlementRequestFixedSize            = 71
)

// SettlementGain is one directed positive score transfer in a Legacy
// GameLogic→GameMaster comprehensive settlement request.
type SettlementGain struct {
	PayTeamID  uint32
	GainTeamID uint32
	Score      int32
}

// SettlementPlayer is one seat-indexed player fact in a Legacy comprehensive
// settlement request.
type SettlementPlayer struct {
	PlayerID uint32
	Flag     int32
	Score    int32
	Exp      int32
	TeamID   uint32
}

// SettlementRequest is the normalized input for one Legacy 0x8650 request.
type SettlementRequest struct {
	BattleID       uint32
	ResultType     int32
	TeamCount      int32
	Gains          []SettlementGain
	Players        []SettlementPlayer
	NoScoreBase    bool
	NoCheckSeal    bool
	NoRecharge     bool
	LevelScoreType uint8
}

// EncodeSettlementRequest encodes the exact old 0x8650 fixed body followed by
// its contiguous gain and player suffixes.
func EncodeSettlementRequest(request SettlementRequest) ([]byte, error) {
	if request.BattleID == 0 || request.TeamCount != 4 || len(request.Players) != 4 {
		return nil, errors.New("legacywire: invalid settlement request")
	}
	for _, gain := range request.Gains {
		if gain.PayTeamID >= 4 || gain.GainTeamID >= 4 || gain.PayTeamID == gain.GainTeamID || gain.Score <= 0 {
			return nil, errors.New("legacywire: invalid settlement gain")
		}
	}
	for seat, player := range request.Players {
		if player.PlayerID == 0 || player.TeamID != uint32(seat) {
			return nil, errors.New("legacywire: invalid settlement player")
		}
	}
	gainSize, playerSize := len(request.Gains)*settlementGainSize, len(request.Players)*settlementPlayerResultSize
	data := make([]byte, settlementRequestFixedSize+gainSize+playerSize)
	encodeHeader(data, bsHeader{Type: messageGLToGMSettlementRequest, Length: uint32(len(data))})
	binary.LittleEndian.PutUint16(data[24:26], glHeaderSize)
	binary.LittleEndian.PutUint32(data[26:30], request.BattleID)
	binary.LittleEndian.PutUint32(data[35:39], uint32(request.ResultType))
	binary.LittleEndian.PutUint32(data[39:43], uint32(len(request.Gains)))
	binary.LittleEndian.PutUint32(data[43:47], uint32(len(request.Players)))
	binary.LittleEndian.PutUint32(data[47:51], uint32(request.TeamCount))
	putSuffixIndex(data, 51, settlementRequestFixedSize, gainSize)
	putSuffixIndex(data, 59, settlementRequestFixedSize+gainSize, playerSize)
	if request.NoScoreBase {
		data[67] = 1
	}
	if request.NoCheckSeal {
		data[68] = 1
	}
	if request.NoRecharge {
		data[69] = 1
	}
	data[70] = request.LevelScoreType
	offset := settlementRequestFixedSize
	for _, gain := range request.Gains {
		binary.LittleEndian.PutUint32(data[offset:offset+4], gain.PayTeamID)
		binary.LittleEndian.PutUint32(data[offset+4:offset+8], gain.GainTeamID)
		binary.LittleEndian.PutUint32(data[offset+8:offset+12], uint32(gain.Score))
		offset += settlementGainSize
	}
	for _, player := range request.Players {
		binary.LittleEndian.PutUint32(data[offset:offset+4], player.PlayerID)
		binary.LittleEndian.PutUint32(data[offset+4:offset+8], uint32(player.Flag))
		binary.LittleEndian.PutUint32(data[offset+8:offset+12], uint32(player.Score))
		binary.LittleEndian.PutUint32(data[offset+12:offset+16], uint32(player.Exp))
		binary.LittleEndian.PutUint32(data[offset+16:offset+20], player.TeamID)
		offset += settlementPlayerResultSize
	}
	return data, nil
}

func putSuffixIndex(data []byte, offset, absoluteOffset, size int) {
	binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(absoluteOffset))
	binary.LittleEndian.PutUint32(data[offset+4:offset+8], uint32(size))
}
