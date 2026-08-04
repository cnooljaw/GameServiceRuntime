package legacywire

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const userStateChangeFrameSize = headerSize + 4 + 4

var errInvalidUserStateChange = errors.New("legacywire: invalid USER_STATE_CHANGE request")

// UserStateChangeRequest is one decoded Legacy USER_STATE_CHANGE request.
type UserStateChangeRequest struct {
	UserID uint32
	State  uint32
}

// DecodeUserStateChange decodes one exact Legacy 0x720A USER_STATE_CHANGE packet.
func DecodeUserStateChange(data []byte) (UserStateChangeRequest, error) {
	if len(data) != userStateChangeFrameSize {
		return UserStateChangeRequest{}, invalidUserStateChange("frame length %d is not %d", len(data), userStateChangeFrameSize)
	}
	header, err := decodeHeader(data)
	if err != nil {
		return UserStateChangeRequest{}, invalidUserStateChange("header: %v", err)
	}
	if header.Type != messageGameUserStateChange {
		return UserStateChangeRequest{}, invalidUserStateChange("message type %#x", header.Type)
	}
	if header.Length != userStateChangeFrameSize {
		return UserStateChangeRequest{}, invalidUserStateChange("header length %d is not %d", header.Length, userStateChangeFrameSize)
	}
	return UserStateChangeRequest{
		UserID: binary.LittleEndian.Uint32(data[headerSize : headerSize+4]),
		State:  binary.LittleEndian.Uint32(data[headerSize+4 : userStateChangeFrameSize]),
	}, nil
}

func invalidUserStateChange(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidUserStateChange, fmt.Sprintf(format, args...))
}
