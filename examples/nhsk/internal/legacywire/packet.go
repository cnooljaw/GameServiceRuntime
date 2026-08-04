package legacywire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const maxFrameSize = 8 * 1024

var (
	errInvalidFrameLength = errors.New("legacywire: invalid frame length")
	errTruncatedFrame     = errors.New("legacywire: truncated frame")
)

type wireFrame struct {
	header bsHeader
	bytes  []byte
}

// Frame is one complete Legacy TCP frame. Bytes owns an independent copy.
type Frame struct {
	Type   uint32
	Origin uint16
	Bytes  []byte
}

// ReadFrame reads exactly one bounded Legacy TCP frame.
func ReadFrame(reader io.Reader) (Frame, error) {
	frame, err := readFrame(reader)
	if err != nil {
		return Frame{}, err
	}
	return Frame{Type: frame.header.Type, Origin: frame.header.Origin, Bytes: frame.bytes}, nil
}

// WriteFrame writes one complete bounded Legacy TCP frame.
func WriteFrame(writer io.Writer, data []byte) error {
	if len(data) < headerSize || len(data) > maxFrameSize {
		return fmt.Errorf("%w: %d", errInvalidFrameLength, len(data))
	}
	if binary.LittleEndian.Uint32(data[20:24]) != uint32(len(data)) {
		return fmt.Errorf("%w: header length %d does not match %d", errInvalidFrameLength, binary.LittleEndian.Uint32(data[20:24]), len(data))
	}
	return writeAll(writer, data)
}

func readFrame(reader io.Reader) (wireFrame, error) {
	headerBytes := make([]byte, headerSize)
	if _, err := io.ReadFull(reader, headerBytes); err != nil {
		if errors.Is(err, io.EOF) {
			return wireFrame{}, io.EOF
		}
		return wireFrame{}, fmt.Errorf("%w: header: %v", errTruncatedFrame, err)
	}
	header, err := decodeHeader(headerBytes)
	if err != nil {
		return wireFrame{}, err
	}
	if header.Length < headerSize || header.Length > maxFrameSize {
		return wireFrame{}, fmt.Errorf("%w: %d", errInvalidFrameLength, header.Length)
	}

	data := make([]byte, int(header.Length))
	copy(data, headerBytes)
	if _, err := io.ReadFull(reader, data[headerSize:]); err != nil {
		return wireFrame{}, fmt.Errorf("%w: body: %v", errTruncatedFrame, err)
	}
	return wireFrame{header: header, bytes: data}, nil
}
