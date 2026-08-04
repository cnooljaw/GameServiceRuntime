package legacywire

import "encoding/binary"

const messageGLToGMNewGameAck uint32 = 0x800086c0

// EncodeNewGameAck encodes the old GameLogic response consumed by GameMaster
// after NEW_GAME. The response is a BSHeader, GameInnerID and one result byte.
func EncodeNewGameAck(battleID uint32, accepted bool) []byte {
	data := make([]byte, 29)
	encodeHeader(data, bsHeader{Type: messageGLToGMNewGameAck, Length: uint32(len(data))})
	binary.LittleEndian.PutUint32(data[24:28], battleID)
	if accepted {
		data[28] = 1
	}
	return data
}
