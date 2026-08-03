package legacywire

import "encoding/binary"

const (
	gameStartedReplayNameSize = 80
	gameStartedFrameSize      = glHeaderSize + 1 + gameStartedReplayNameSize
)

// EncodeGameStarted encodes one successful Legacy 0x8654 GAME_STARTED control frame.
func EncodeGameStarted(battleID uint32, replayName string) []byte {
	data := make([]byte, gameStartedFrameSize)
	encodeHeader(data, bsHeader{Type: messageGLToGMGameStarted, Length: gameStartedFrameSize})
	binary.LittleEndian.PutUint16(data[headerSize:26], glHeaderSize)
	binary.LittleEndian.PutUint32(data[26:30], battleID)
	data[glHeaderSize] = 1
	copy(data[glHeaderSize+1:], replayName)
	return data
}
