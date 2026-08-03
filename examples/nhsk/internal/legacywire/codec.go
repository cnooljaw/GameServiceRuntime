package legacywire

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	glHeaderSize    = 34
	gameHeaderSize  = 24
	suffixIndexSize = 8
	relayFixedSize  = headerSize + gameHeaderSize + suffixIndexSize
	gameFrameBase   = glHeaderSize + relayFixedSize
)

var errMalformedRelay = errors.New("legacywire: malformed relay")

// GameHeader contains the complete identity metadata carried by a Legacy relay.
// The bridge decides which redundant fields enter a normalized game Command.
type GameHeader struct {
	UserID      uint32
	ConnectType uint32
	PlatformID  uint32
	MatchID     uint32
	ProductID   uint32
	Reserved    uint32
}

// InboundGameRelay is a decoded GameMaster-to-GameLogic relay without wire headers.
type InboundGameRelay struct {
	BattleID   uint32
	UserID     uint32
	GameHeader GameHeader
	Payload    []byte
}

// OutboundGameRelay describes one user-targeted GameLogic-to-GameMaster relay.
// Connection and platform fields are deliberately encoded as zero.
type OutboundGameRelay struct {
	BattleID  uint32
	UserID    uint32
	MatchID   uint32
	ProductID uint32
	Payload   []byte
}

// DecodeInboundGameRelay decodes one exact 0x8605 plus 0x7402 frame.
func DecodeInboundGameRelay(data []byte) (InboundGameRelay, error) {
	if len(data) < gameFrameBase {
		return InboundGameRelay{}, malformedRelay("frame length %d is below %d", len(data), gameFrameBase)
	}
	if len(data) > maxFrameSize {
		return InboundGameRelay{}, malformedRelay("frame length %d exceeds %d", len(data), maxFrameSize)
	}

	outer, err := decodeHeader(data)
	if err != nil {
		return InboundGameRelay{}, malformedRelay("outer header: %v", err)
	}
	if outer.Type != messageGMToGLGame {
		return InboundGameRelay{}, malformedRelay("outer message type %#x", outer.Type)
	}
	if outer.Length == 0 || uint64(outer.Length) != uint64(len(data)) {
		return InboundGameRelay{}, malformedRelay("outer length %d does not match %d", outer.Length, len(data))
	}
	if binary.LittleEndian.Uint16(data[headerSize:glHeaderSize]) != glHeaderSize {
		return InboundGameRelay{}, malformedRelay("outer header length is not %d", glHeaderSize)
	}

	inner := data[glHeaderSize:]
	innerHeader, err := decodeHeader(inner)
	if err != nil {
		return InboundGameRelay{}, malformedRelay("inner header: %v", err)
	}
	if innerHeader.Type != messageAgentToGameRelay {
		return InboundGameRelay{}, malformedRelay("inner message type %#x", innerHeader.Type)
	}
	if innerHeader.Length == 0 || uint64(innerHeader.Length) != uint64(len(inner)) {
		return InboundGameRelay{}, malformedRelay("inner length %d does not match %d", innerHeader.Length, len(inner))
	}

	suffixOffset := binary.LittleEndian.Uint32(inner[headerSize+gameHeaderSize : headerSize+gameHeaderSize+4])
	suffixSize := binary.LittleEndian.Uint32(inner[headerSize+gameHeaderSize+4 : relayFixedSize])
	if suffixOffset != relayFixedSize {
		return InboundGameRelay{}, malformedRelay("suffix offset %d is not %d", suffixOffset, relayFixedSize)
	}
	suffixEnd := uint64(suffixOffset) + uint64(suffixSize)
	if suffixEnd != uint64(len(inner)) {
		return InboundGameRelay{}, malformedRelay("suffix end %d does not match inner length %d", suffixEnd, len(inner))
	}

	gameOffset := headerSize
	gameHeader := GameHeader{
		UserID:      binary.LittleEndian.Uint32(inner[gameOffset : gameOffset+4]),
		ConnectType: binary.LittleEndian.Uint32(inner[gameOffset+4 : gameOffset+8]),
		PlatformID:  binary.LittleEndian.Uint32(inner[gameOffset+8 : gameOffset+12]),
		MatchID:     binary.LittleEndian.Uint32(inner[gameOffset+12 : gameOffset+16]),
		ProductID:   binary.LittleEndian.Uint32(inner[gameOffset+16 : gameOffset+20]),
		Reserved:    binary.LittleEndian.Uint32(inner[gameOffset+20 : gameOffset+24]),
	}
	payload := append([]byte(nil), inner[int(suffixOffset):int(suffixEnd)]...)
	return InboundGameRelay{
		BattleID:   binary.LittleEndian.Uint32(data[26:30]),
		UserID:     binary.LittleEndian.Uint32(data[30:34]),
		GameHeader: gameHeader,
		Payload:    payload,
	}, nil
}

// EncodeOutboundGameRelay encodes one exact 0x8644 plus 0x7400 frame.
func EncodeOutboundGameRelay(message OutboundGameRelay) ([]byte, error) {
	if len(message.Payload) > maxFrameSize-gameFrameBase {
		return nil, malformedRelay("payload length %d exceeds frame capacity", len(message.Payload))
	}
	totalLength := gameFrameBase + len(message.Payload)
	innerLength := relayFixedSize + len(message.Payload)
	data := make([]byte, totalLength)

	encodeHeader(data, bsHeader{Type: messageGLToGMGame, Length: uint32(totalLength)})
	binary.LittleEndian.PutUint16(data[headerSize:26], glHeaderSize)
	binary.LittleEndian.PutUint32(data[26:30], message.BattleID)
	binary.LittleEndian.PutUint32(data[30:34], message.UserID)

	inner := data[glHeaderSize:]
	encodeHeader(inner, bsHeader{Type: messageGameToAgentRelay, Length: uint32(innerLength)})
	gameOffset := headerSize
	binary.LittleEndian.PutUint32(inner[gameOffset:gameOffset+4], message.UserID)
	binary.LittleEndian.PutUint32(inner[gameOffset+12:gameOffset+16], message.MatchID)
	binary.LittleEndian.PutUint32(inner[gameOffset+16:gameOffset+20], message.ProductID)
	binary.LittleEndian.PutUint32(inner[headerSize+gameHeaderSize:headerSize+gameHeaderSize+4], relayFixedSize)
	binary.LittleEndian.PutUint32(inner[headerSize+gameHeaderSize+4:relayFixedSize], uint32(len(message.Payload)))
	copy(inner[relayFixedSize:], message.Payload)
	return data, nil
}

func malformedRelay(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errMalformedRelay, fmt.Sprintf(format, args...))
}
