package legacywire

import "encoding/binary"

const askOutCardFrameSize = headerSize + 4 + 4 + 4

// AskOutCard is the normalized input for one Legacy NHSK ASK_OUT_CARD packet.
type AskOutCard struct {
	UserID             uint32
	VerifyCode         uint32
	ActionMilliseconds uint32
}

// EncodeAskOutCard encodes one exact Legacy 0x7603 NHSK ASK_OUT_CARD packet.
func EncodeAskOutCard(ask AskOutCard) []byte {
	data := make([]byte, askOutCardFrameSize)
	encodeHeader(data, bsHeader{Type: messageNHSKAskOutCard, Length: askOutCardFrameSize})
	offset := headerSize
	binary.LittleEndian.PutUint32(data[offset:offset+4], ask.UserID)
	binary.LittleEndian.PutUint32(data[offset+4:offset+8], ask.VerifyCode)
	binary.LittleEndian.PutUint32(data[offset+8:offset+12], ask.ActionMilliseconds)
	return data
}
