package legacywire

import (
	"encoding/binary"
	"errors"
)

const outCardResultFrameSize = headerSize + 4

var errInvalidOutCardResult = errors.New("legacywire: invalid OUT_CARD_RESULT")

// EncodeOutCardResult encodes one exact Legacy 0x7609 NHSK OUT_CARD_RESULT packet.
func EncodeOutCardResult(reason uint32) ([]byte, error) {
	if reason < 1 || reason > 5 {
		return nil, errInvalidOutCardResult
	}
	data := make([]byte, outCardResultFrameSize)
	encodeHeader(data, bsHeader{Type: messageNHSKOutCardResult, Length: outCardResultFrameSize})
	binary.LittleEndian.PutUint32(data[headerSize:outCardResultFrameSize], reason)
	return data, nil
}
