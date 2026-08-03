package legacywire

import (
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
