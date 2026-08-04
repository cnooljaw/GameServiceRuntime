package legacywire

import (
	"encoding/binary"
	"errors"
)

const (
	showCardsPlayerCount = 4
	showCardsCardCount   = 26
	showCardsFrameSize   = headerSize + showCardsPlayerCount*4 + showCardsPlayerCount*showCardsCardCount + showCardsPlayerCount
)

var errInvalidShowCards = errors.New("legacywire: invalid SHOW_CARDS")

// ShowCards is the normalized input for one Legacy NHSK SHOW_CARDS packet.
type ShowCards struct {
	UserIDs    [showCardsPlayerCount]uint32
	Cards      [showCardsPlayerCount][showCardsCardCount]byte
	CardCounts [showCardsPlayerCount]uint8
}

// EncodeShowCards encodes one exact Legacy 0x7606 NHSK SHOW_CARDS packet.
func EncodeShowCards(show ShowCards) ([]byte, error) {
	for seat, count := range show.CardCounts {
		if count > showCardsCardCount {
			return nil, errInvalidShowCards
		}
		for _, card := range show.Cards[seat][count:] {
			if card != 0 {
				return nil, errInvalidShowCards
			}
		}
	}

	data := make([]byte, showCardsFrameSize)
	encodeHeader(data, bsHeader{Type: messageNHSKShowCards, Length: showCardsFrameSize})
	offset := headerSize
	for _, userID := range show.UserIDs {
		binary.LittleEndian.PutUint32(data[offset:offset+4], userID)
		offset += 4
	}
	for _, cards := range show.Cards {
		copy(data[offset:offset+showCardsCardCount], cards[:])
		offset += showCardsCardCount
	}
	copy(data[offset:], show.CardCounts[:])
	return data, nil
}
