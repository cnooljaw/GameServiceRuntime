package legacywire

import (
	"bytes"
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestPerformOriginHandshakeWritesGameLogicBeforeReadingGameMaster(t *testing.T) {
	response := encodeOrigin(originGameMaster)
	header, err := decodeHeader(response)
	if err != nil {
		t.Fatalf("decode response header: %v", err)
	}
	header.Magic = 0x11223344
	header.Serial = 0x55667788
	header.Reserve = 0x99aa
	header.Param = 0xbbccddee
	encodeHeader(response, header)

	connection := &handshakeConn{
		reader:             bytes.NewReader(response),
		maxWrite:           5,
		requireWrittenRead: headerSize,
	}
	if err := performOriginHandshake(connection, time.Second); err != nil {
		t.Fatalf("perform origin handshake: %v", err)
	}
	if got, want := connection.written.Bytes(), encodeOrigin(originGameLogic); !reflect.DeepEqual(got, want) {
		t.Fatalf("written origin = %x, want %x", got, want)
	}
	if len(connection.deadlines) != 2 || connection.deadlines[0].IsZero() || !connection.deadlines[1].IsZero() {
		t.Fatalf("deadlines = %v, want non-zero then cleared", connection.deadlines)
	}
}

func TestPerformOriginHandshakeRejectsInvalidGameMasterOrigin(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "wrong origin", mutate: func(data []byte) []byte {
			header, _ := decodeHeader(data)
			header.Origin = originGameLogic
			encodeHeader(data, header)
			return data
		}},
		{name: "wrong type", mutate: func(data []byte) []byte {
			header, _ := decodeHeader(data)
			header.Type = messageGMToGLGame
			encodeHeader(data, header)
			return data
		}},
		{name: "body attached", mutate: func(data []byte) []byte {
			data = append(data, 1)
			header, _ := decodeHeader(data)
			header.Length = uint32(len(data))
			encodeHeader(data, header)
			return data
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := test.mutate(encodeOrigin(originGameMaster))
			connection := &handshakeConn{reader: bytes.NewReader(response)}
			if err := performOriginHandshake(connection, time.Second); !errors.Is(err, errInvalidOrigin) {
				t.Fatalf("handshake error = %v, want errInvalidOrigin", err)
			}
		})
	}
}

func TestPerformOriginHandshakePropagatesWriteAndReadFailures(t *testing.T) {
	t.Run("write", func(t *testing.T) {
		connection := &handshakeConn{reader: bytes.NewReader(nil), writeErr: io.ErrClosedPipe}
		if err := performOriginHandshake(connection, time.Second); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("handshake error = %v, want closed pipe", err)
		}
	})

	t.Run("write no progress", func(t *testing.T) {
		connection := &handshakeConn{reader: bytes.NewReader(nil), zeroWrite: true}
		if err := performOriginHandshake(connection, time.Second); !errors.Is(err, io.ErrNoProgress) {
			t.Fatalf("handshake error = %v, want no progress", err)
		}
	})

	t.Run("read", func(t *testing.T) {
		connection := &handshakeConn{reader: bytes.NewReader(encodeOrigin(originGameMaster)[:headerSize-1])}
		if err := performOriginHandshake(connection, time.Second); !errors.Is(err, errTruncatedFrame) {
			t.Fatalf("handshake error = %v, want truncated frame", err)
		}
	})
}

func TestPerformOriginHandshakeRejectsNonPositiveTimeout(t *testing.T) {
	connection := &handshakeConn{reader: bytes.NewReader(encodeOrigin(originGameMaster))}
	if err := performOriginHandshake(connection, 0); err == nil {
		t.Fatal("handshake with zero timeout succeeded")
	}
	if connection.written.Len() != 0 || len(connection.deadlines) != 0 {
		t.Fatal("invalid timeout touched connection")
	}
}

type handshakeConn struct {
	reader             *bytes.Reader
	written            bytes.Buffer
	maxWrite           int
	requireWrittenRead int
	writeErr           error
	zeroWrite          bool
	deadlines          []time.Time
}

func (connection *handshakeConn) Read(target []byte) (int, error) {
	if connection.written.Len() < connection.requireWrittenRead {
		return 0, errors.New("read attempted before complete origin write")
	}
	return connection.reader.Read(target)
}

func (connection *handshakeConn) Write(source []byte) (int, error) {
	if connection.writeErr != nil {
		return 0, connection.writeErr
	}
	if connection.zeroWrite {
		return 0, nil
	}
	if connection.maxWrite > 0 && len(source) > connection.maxWrite {
		source = source[:connection.maxWrite]
	}
	return connection.written.Write(source)
}

func (connection *handshakeConn) Close() error         { return nil }
func (connection *handshakeConn) LocalAddr() net.Addr  { return handshakeAddr("local") }
func (connection *handshakeConn) RemoteAddr() net.Addr { return handshakeAddr("remote") }
func (connection *handshakeConn) SetDeadline(value time.Time) error {
	connection.deadlines = append(connection.deadlines, value)
	return nil
}
func (connection *handshakeConn) SetReadDeadline(time.Time) error  { return nil }
func (connection *handshakeConn) SetWriteDeadline(time.Time) error { return nil }

type handshakeAddr string

func (address handshakeAddr) Network() string { return "test" }
func (address handshakeAddr) String() string  { return string(address) }
