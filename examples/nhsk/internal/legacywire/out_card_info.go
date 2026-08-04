package legacywire

import (
	"encoding/binary"
	"errors"
)

const (
	outCardInfoWireCardCount = 26
	outCardInfoFrameSize     = headerSize + 4 + outCardInfoWireCardCount + 1
)

var errTooManyOutCards = errors.New("legacywire: too many OUT_CARD_INFO cards")

// OutCardInfo is the normalized input for one Legacy NHSK OUT_CARD_INFO packet.
type OutCardInfo struct {
	UserID uint32
	Cards  []byte
}

// EncodeOutCardInfo encodes one exact Legacy 0x7604 NHSK OUT_CARD_INFO packet.
func EncodeOutCardInfo(info OutCardInfo) ([]byte, error) {
	if len(info.Cards) > outCardInfoWireCardCount {
		return nil, errTooManyOutCards
	}
	data := make([]byte, outCardInfoFrameSize)
	encodeHeader(data, bsHeader{Type: messageNHSKOutCardInfo, Length: outCardInfoFrameSize})
	binary.LittleEndian.PutUint32(data[headerSize:headerSize+4], info.UserID)
	copy(data[headerSize+4:headerSize+4+outCardInfoWireCardCount], info.Cards)
	data[outCardInfoFrameSize-1] = byte(len(info.Cards))
	return data, nil
}
