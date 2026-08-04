package legacywire

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	outCardRequestCardCount       = 26
	outCardRequestCardCountOffset = headerSize + outCardRequestCardCount
	outCardRequestVerifyOffset    = outCardRequestCardCountOffset + 1
	outCardRequestFrameSize       = outCardRequestVerifyOffset + 4
)

var errInvalidOutCardRequest = errors.New("legacywire: invalid OUT_CARD request")

// OutCardRequest is one decoded Legacy NHSK OUT_CARD action.
type OutCardRequest struct {
	Cards      [outCardRequestCardCount]byte
	CardCount  uint8
	VerifyCode uint32
}

// DecodeOutCardRequest decodes one exact Legacy 0x7701 NHSK OUT_CARD packet.
func DecodeOutCardRequest(data []byte) (OutCardRequest, error) {
	if len(data) != outCardRequestFrameSize {
		return OutCardRequest{}, invalidOutCardRequest("frame length %d is not %d", len(data), outCardRequestFrameSize)
	}
	header, err := decodeHeader(data)
	if err != nil {
		return OutCardRequest{}, invalidOutCardRequest("header: %v", err)
	}
	if header.Type != messageNHSKOutCard {
		return OutCardRequest{}, invalidOutCardRequest("message type %#x", header.Type)
	}
	if header.Length != outCardRequestFrameSize {
		return OutCardRequest{}, invalidOutCardRequest("header length %d is not %d", header.Length, outCardRequestFrameSize)
	}

	request := OutCardRequest{
		CardCount:  data[outCardRequestCardCountOffset],
		VerifyCode: binary.LittleEndian.Uint32(data[outCardRequestVerifyOffset:outCardRequestFrameSize]),
	}
	if request.CardCount > outCardRequestCardCount {
		return OutCardRequest{}, invalidOutCardRequest("card count %d exceeds %d", request.CardCount, outCardRequestCardCount)
	}
	cardData := data[headerSize:outCardRequestCardCountOffset]
	if outCardRequestHasNonzero(cardData[request.CardCount:]) {
		return OutCardRequest{}, invalidOutCardRequest("card data after count %d is not zero", request.CardCount)
	}
	copy(request.Cards[:], cardData[:request.CardCount])
	return request, nil
}

func invalidOutCardRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidOutCardRequest, fmt.Sprintf(format, args...))
}

func outCardRequestHasNonzero(cards []byte) bool {
	for _, card := range cards {
		if card != 0 {
			return true
		}
	}
	return false
}
