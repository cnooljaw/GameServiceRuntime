package legacywire

import (
	"encoding/binary"
	"errors"
)

const (
	dealPlayerCount = 4
	dealCardCount   = 26
	dealFrameSize   = headerSize + dealPlayerCount*4 + dealPlayerCount*dealCardCount
)

var errInvalidDealSeat = errors.New("legacywire: invalid DEAL seat")

// Deal is the normalized input for one private Legacy NHSK DEAL packet.
type Deal struct {
	UserIDs [dealPlayerCount]uint32
	SeatID  uint8
	Cards   [dealCardCount]byte
}

// EncodeDeal encodes one exact Legacy 0x7602 NHSK DEAL packet.
func EncodeDeal(deal Deal) ([]byte, error) {
	if deal.SeatID >= dealPlayerCount {
		return nil, errInvalidDealSeat
	}
	data := make([]byte, dealFrameSize)
	encodeHeader(data, bsHeader{Type: messageNHSKDeal, Length: dealFrameSize})
	offset := headerSize
	for _, userID := range deal.UserIDs {
		binary.LittleEndian.PutUint32(data[offset:offset+4], userID)
		offset += 4
	}
	cardsOffset := offset + int(deal.SeatID)*dealCardCount
	copy(data[cardsOffset:cardsOffset+dealCardCount], deal.Cards[:])
	return data, nil
}
