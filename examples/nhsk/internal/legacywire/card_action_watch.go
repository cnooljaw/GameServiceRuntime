package legacywire

import (
	"encoding/binary"
	"errors"
)

const (
	cardActionWatchCardCount = 26
	cardActionWatchFrameSize = headerSize + 4 + cardActionWatchCardCount + 1
)

var errInvalidCardActionWatch = errors.New("legacywire: invalid CARD_ACTION_WATCH")

// CardActionWatch is the normalized input for one Legacy NHSK CARD_ACTION_WATCH packet.
type CardActionWatch struct {
	UserID    uint32
	Cards     [cardActionWatchCardCount]byte
	CardCount uint8
}

// EncodeCardActionWatch encodes one exact Legacy 0x7611 NHSK CARD_ACTION_WATCH packet.
func EncodeCardActionWatch(watch CardActionWatch) ([]byte, error) {
	if watch.CardCount > cardActionWatchCardCount || cardActionWatchHasNonzero(watch.Cards[watch.CardCount:]) {
		return nil, errInvalidCardActionWatch
	}
	data := make([]byte, cardActionWatchFrameSize)
	encodeHeader(data, bsHeader{Type: messageNHSKCardActionWatch, Length: cardActionWatchFrameSize})
	binary.LittleEndian.PutUint32(data[headerSize:headerSize+4], watch.UserID)
	copy(data[headerSize+4:headerSize+4+cardActionWatchCardCount], watch.Cards[:])
	data[cardActionWatchFrameSize-1] = watch.CardCount
	return data, nil
}

func cardActionWatchHasNonzero(cards []byte) bool {
	for _, card := range cards {
		if card != 0 {
			return true
		}
	}
	return false
}
