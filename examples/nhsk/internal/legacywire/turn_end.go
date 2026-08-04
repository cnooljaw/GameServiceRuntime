package legacywire

import "encoding/binary"

const turnEndFrameSize = headerSize + 4 + 4

// TurnEnd is the normalized input for one Legacy NHSK TURN_END packet.
type TurnEnd struct {
	WinnerUserID   uint32
	CapturedPoints uint32
}

// EncodeTurnEnd encodes one exact Legacy 0x7605 NHSK TURN_END packet.
func EncodeTurnEnd(turn TurnEnd) []byte {
	data := make([]byte, turnEndFrameSize)
	encodeHeader(data, bsHeader{Type: messageNHSKTurnEnd, Length: turnEndFrameSize})
	binary.LittleEndian.PutUint32(data[headerSize:headerSize+4], turn.WinnerUserID)
	binary.LittleEndian.PutUint32(data[headerSize+4:headerSize+8], turn.CapturedPoints)
	return data
}
