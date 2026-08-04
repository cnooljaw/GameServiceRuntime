package legacywire

import (
	"errors"
	"fmt"
)

const (
	cardActionRequestCardCount = 26
	cardActionRequestFrameSize = headerSize + cardActionRequestCardCount + 1
)

var errInvalidCardActionRequest = errors.New("legacywire: invalid CARD_ACTION request")

// CardActionRequest is one decoded Legacy NHSK CARD_ACTION selection.
type CardActionRequest struct {
	Cards     [cardActionRequestCardCount]byte
	CardCount uint8
}

// DecodeCardActionRequest decodes one exact Legacy 0x7702 NHSK CARD_ACTION packet.
func DecodeCardActionRequest(data []byte) (CardActionRequest, error) {
	if len(data) != cardActionRequestFrameSize {
		return CardActionRequest{}, invalidCardActionRequest("frame length %d is not %d", len(data), cardActionRequestFrameSize)
	}
	header, err := decodeHeader(data)
	if err != nil {
		return CardActionRequest{}, invalidCardActionRequest("header: %v", err)
	}
	if header.Type != messageNHSKCardAction {
		return CardActionRequest{}, invalidCardActionRequest("message type %#x", header.Type)
	}
	if header.Length != cardActionRequestFrameSize {
		return CardActionRequest{}, invalidCardActionRequest("header length %d is not %d", header.Length, cardActionRequestFrameSize)
	}

	request := CardActionRequest{CardCount: data[cardActionRequestFrameSize-1]}
	if request.CardCount > cardActionRequestCardCount {
		return CardActionRequest{}, invalidCardActionRequest("card count %d exceeds %d", request.CardCount, cardActionRequestCardCount)
	}
	cardData := data[headerSize : headerSize+cardActionRequestCardCount]
	if cardActionRequestHasNonzero(cardData[request.CardCount:]) {
		return CardActionRequest{}, invalidCardActionRequest("card data after count %d is not zero", request.CardCount)
	}
	copy(request.Cards[:], cardData[:request.CardCount])
	return request, nil
}

func invalidCardActionRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidCardActionRequest, fmt.Sprintf(format, args...))
}

func cardActionRequestHasNonzero(cards []byte) bool {
	for _, card := range cards {
		if card != 0 {
			return true
		}
	}
	return false
}
