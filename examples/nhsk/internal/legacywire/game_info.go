package legacywire

import "encoding/binary"

const gameInfoFrameSize = headerSize + 4 + 4 + 4*4 + 2

// GameInfo is the normalized input for one Legacy NHSK GAME_INFO packet.
type GameInfo struct {
	OutCardSeconds uint32
	ServiceFee     int32
	Scores         [4]int32
	GameNum        uint16
}

// EncodeGameInfo encodes one exact Legacy 0x7601 NHSK GAME_INFO packet.
func EncodeGameInfo(info GameInfo) []byte {
	data := make([]byte, gameInfoFrameSize)
	encodeHeader(data, bsHeader{Type: messageNHSKGameInfo, Length: gameInfoFrameSize})
	offset := headerSize
	binary.LittleEndian.PutUint32(data[offset:offset+4], info.OutCardSeconds)
	offset += 4
	binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(info.ServiceFee))
	offset += 4
	for _, score := range info.Scores {
		binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(score))
		offset += 4
	}
	binary.LittleEndian.PutUint16(data[offset:offset+2], info.GameNum)
	return data
}
