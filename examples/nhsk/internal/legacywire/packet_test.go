package legacywire

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestReadFrameReturnsOneExactFrameAtATime(t *testing.T) {
	first := testFrame(t, messageOrigin, []byte{1, 2, 3})
	second := testFrame(t, messageGMToGLGame, []byte{4, 5})
	reader := bytes.NewReader(append(append([]byte(nil), first...), second...))

	gotFirst, err := readFrame(reader)
	if err != nil {
		t.Fatalf("read first frame: %v", err)
	}
	gotSecond, err := readFrame(reader)
	if err != nil {
		t.Fatalf("read second frame: %v", err)
	}
	if !reflect.DeepEqual(gotFirst.bytes, first) || !reflect.DeepEqual(gotSecond.bytes, second) {
		t.Fatalf("frames = (%x, %x), want (%x, %x)", gotFirst.bytes, gotSecond.bytes, first, second)
	}
	if gotFirst.header.Type != messageOrigin || gotSecond.header.Type != messageGMToGLGame {
		t.Fatalf("headers = (%#x, %#x)", gotFirst.header.Type, gotSecond.header.Type)
	}
	if _, err := readFrame(reader); !errors.Is(err, io.EOF) {
		t.Fatalf("read after frames error = %v, want EOF", err)
	}
}

func TestReadFrameRejectsInvalidLength(t *testing.T) {
	tests := []struct {
		name   string
		length uint32
	}{
		{name: "zero", length: 0},
		{name: "below header", length: headerSize - 1},
		{name: "above maximum", length: maxFrameSize + 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := make([]byte, headerSize)
			encodeHeader(header, bsHeader{Type: messageGMToGLGame, Length: test.length})
			_, err := readFrame(bytes.NewReader(header))
			if !errors.Is(err, errInvalidFrameLength) {
				t.Fatalf("read frame error = %v, want errInvalidFrameLength", err)
			}
		})
	}
}

func TestReadFrameRejectsTruncatedHeaderAndBody(t *testing.T) {
	complete := testFrame(t, messageGMToGLGame, []byte{1, 2, 3, 4})
	tests := []struct {
		name string
		data []byte
	}{
		{name: "header", data: complete[:headerSize-1]},
		{name: "body", data: complete[:len(complete)-1]},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := readFrame(bytes.NewReader(test.data))
			if !errors.Is(err, errTruncatedFrame) {
				t.Fatalf("read frame error = %v, want errTruncatedFrame", err)
			}
		})
	}
}

func TestReadFrameReturnsIndependentStorage(t *testing.T) {
	data := testFrame(t, messageOrigin, []byte{1, 2, 3})
	frame, err := readFrame(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	data[0] = 0xff
	if frame.bytes[0] == 0xff {
		t.Fatal("read frame retained caller storage")
	}
}

func testFrame(t *testing.T, messageID uint32, body []byte) []byte {
	t.Helper()
	length := headerSize + len(body)
	data := make([]byte, length)
	encodeHeader(data, bsHeader{Type: messageID, Length: uint32(length)})
	copy(data[headerSize:], body)
	return data
}
