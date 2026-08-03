package legacywire

// EncodeGameStart encodes the bodyless Legacy 0x7205 GAME_START packet.
func EncodeGameStart() []byte {
	data := make([]byte, headerSize)
	encodeHeader(data, bsHeader{Type: messageGameStart, Length: headerSize})
	return data
}
