package legacywire

import (
	"encoding/binary"
	"errors"
)

const (
	headerSize = 24

	originGameMaster uint16 = 100
	originGameLogic  uint16 = 107

	messageOrigin            uint32 = 0x0600
	messageGameToAgentRelay  uint32 = 0x7400
	messageAgentToGameRelay  uint32 = 0x7402
	messageGMToGLGame        uint32 = 0x8605
	messageGLToGMGame        uint32 = 0x8644
	messageGLToGMGameStarted uint32 = 0x8654

	messageNHSKBase            uint32 = 0x7600
	messageGameUserStateChange uint32 = 0x720a
	messageGameUserReconnect   uint32 = 0x7208
	messageGameScene           uint32 = 0x720d
	messageGameStart           uint32 = 0x7205
	messageNHSKGameInfo               = messageNHSKBase + 0x001
	messageNHSKDeal                   = messageNHSKBase + 0x002
	messageNHSKAskOutCard             = messageNHSKBase + 0x003
	messageNHSKOutCardInfo            = messageNHSKBase + 0x004
	messageNHSKTurnEnd                = messageNHSKBase + 0x005
	messageNHSKShowCards              = messageNHSKBase + 0x006
	messageNHSKGameResult             = messageNHSKBase + 0x007
	messageNHSKGameScene              = messageNHSKBase + 0x008
	messageNHSKOutCardResult          = messageNHSKBase + 0x009
	messageNHSKCardActionWatch        = messageNHSKBase + 0x011
	messageNHSKOutCard                = messageNHSKBase + 0x101
	messageNHSKCardAction             = messageNHSKBase + 0x102
)

type bsHeader struct {
	Magic   uint32
	Serial  uint32
	Origin  uint16
	Reserve uint16
	Type    uint32
	Param   uint32
	Length  uint32
}

func encodeOrigin(origin uint16) []byte {
	data := make([]byte, headerSize)
	encodeHeader(data, bsHeader{Origin: origin, Type: messageOrigin, Length: headerSize})
	return data
}

func encodeHeader(target []byte, header bsHeader) {
	binary.LittleEndian.PutUint32(target[0:4], header.Magic)
	binary.LittleEndian.PutUint32(target[4:8], header.Serial)
	binary.LittleEndian.PutUint16(target[8:10], header.Origin)
	binary.LittleEndian.PutUint16(target[10:12], header.Reserve)
	binary.LittleEndian.PutUint32(target[12:16], header.Type)
	binary.LittleEndian.PutUint32(target[16:20], header.Param)
	binary.LittleEndian.PutUint32(target[20:24], header.Length)
}

func decodeHeader(data []byte) (bsHeader, error) {
	if len(data) < headerSize {
		return bsHeader{}, errors.New("legacywire: short BSHeader")
	}
	return bsHeader{
		Magic:   binary.LittleEndian.Uint32(data[0:4]),
		Serial:  binary.LittleEndian.Uint32(data[4:8]),
		Origin:  binary.LittleEndian.Uint16(data[8:10]),
		Reserve: binary.LittleEndian.Uint16(data[10:12]),
		Type:    binary.LittleEndian.Uint32(data[12:16]),
		Param:   binary.LittleEndian.Uint32(data[16:20]),
		Length:  binary.LittleEndian.Uint32(data[20:24]),
	}, nil
}
