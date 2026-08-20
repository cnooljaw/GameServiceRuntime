package legacywire

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const propUseFixedSize = 52

// PropUse is the retained replay-only projection of 0x7218.
type PropUse struct {
	SenderID  uint32
	SendCount uint32
	PropID    string
	TargetIDs []uint32
}

// DecodePropUse decodes the two exact suffixes of BROADCAST_USE_PROP.
func DecodePropUse(data []byte) (PropUse, error) {
	if len(data) < propUseFixedSize || len(data) > maxFrameSize {
		return PropUse{}, fmt.Errorf("legacywire: invalid prop frame length %d", len(data))
	}
	header, err := decodeHeader(data)
	if err != nil || header.Type != messageGameBroadcastUseProp || header.Length != uint32(len(data)) {
		return PropUse{}, fmt.Errorf("legacywire: invalid prop header")
	}
	propOffset, propSize := binary.LittleEndian.Uint32(data[32:36]), binary.LittleEndian.Uint32(data[36:40])
	targetCount := binary.LittleEndian.Uint32(data[40:44])
	targetOffset, targetSize := binary.LittleEndian.Uint32(data[44:48]), binary.LittleEndian.Uint32(data[48:52])
	propEnd, targetEnd := uint64(propOffset)+uint64(propSize), uint64(targetOffset)+uint64(targetSize)
	if propOffset != propUseFixedSize || propEnd > uint64(len(data)) || uint64(targetOffset) != propEnd || uint64(targetSize) != uint64(targetCount)*4 || targetEnd != uint64(len(data)) {
		return PropUse{}, fmt.Errorf("legacywire: invalid prop suffixes")
	}
	targets := make([]uint32, targetCount)
	for index := range targets {
		offset := int(targetOffset) + index*4
		targets[index] = binary.LittleEndian.Uint32(data[offset : offset+4])
	}
	return PropUse{SenderID: binary.LittleEndian.Uint32(data[28:32]), SendCount: binary.LittleEndian.Uint32(data[24:28]), PropID: string(bytes.TrimRight(data[propOffset:propEnd], "\x00")), TargetIDs: targets}, nil
}
