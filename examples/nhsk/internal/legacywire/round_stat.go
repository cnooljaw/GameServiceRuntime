package legacywire

import "encoding/binary"

const (
	messageGameRoundStat uint32 = 0x7246
	gameRoundStatSize           = headerSize + 4 + suffixIndexSize
)

// EncodeRoundStat encodes the empty-player-stat projection used by the first
// NHSK migration slice. The old client still receives the packet, while the
// abandoned cross-subgame statistics module contributes no entries.
func EncodeRoundStat() []byte {
	data := make([]byte, gameRoundStatSize)
	encodeHeader(data, bsHeader{Type: messageGameRoundStat, Length: uint32(len(data))})
	// PlayerCount is zero. The suffix index still points immediately after the
	// fixed header, matching the reference formatter's SetSizeAndOffset call.
	binary.LittleEndian.PutUint32(data[headerSize:headerSize+4], 0)
	binary.LittleEndian.PutUint32(data[headerSize+4:headerSize+8], gameRoundStatSize)
	binary.LittleEndian.PutUint32(data[headerSize+8:gameRoundStatSize], 0)
	return data
}
