package legacywire

import (
	"encoding/binary"
	"errors"
)

const messageGLToGMNoticeRoundOver uint32 = 0x864e

// NoticeRoundOver is the fixed old GameLogic→GameMaster force-round-over
// notification. It carries no suffix or player list.
type NoticeRoundOver struct {
	BattleID       uint32
	YueJuEndReason int32
	YueJuEndPlayer uint32
}

// EncodeNoticeRoundOver encodes one exact 42-byte NOTICE_ROUND_OVER frame.
func EncodeNoticeRoundOver(value NoticeRoundOver) ([]byte, error) {
	if value.BattleID == 0 {
		return nil, errors.New("legacywire: invalid NOTICE_ROUND_OVER")
	}
	data := make([]byte, 42)
	encodeHeader(data, bsHeader{Type: messageGLToGMNoticeRoundOver, Length: uint32(len(data))})
	binary.LittleEndian.PutUint16(data[24:26], glHeaderSize)
	binary.LittleEndian.PutUint32(data[26:30], value.BattleID)
	binary.LittleEndian.PutUint32(data[34:38], uint32(value.YueJuEndReason))
	binary.LittleEndian.PutUint32(data[38:42], value.YueJuEndPlayer)
	return data, nil
}
