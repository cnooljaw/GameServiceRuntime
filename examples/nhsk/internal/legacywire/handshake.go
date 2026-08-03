package legacywire

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

var errInvalidOrigin = errors.New("legacywire: invalid GameMaster origin")

func performOriginHandshake(connection net.Conn, timeout time.Duration) error {
	if connection == nil {
		return errors.New("legacywire: origin handshake requires a connection")
	}
	if timeout <= 0 {
		return errors.New("legacywire: origin handshake timeout must be positive")
	}
	if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("legacywire: set origin deadline: %w", err)
	}
	if err := writeAll(connection, encodeOrigin(originGameLogic)); err != nil {
		return fmt.Errorf("legacywire: write GameLogic origin: %w", err)
	}
	frame, err := readFrame(connection)
	if err != nil {
		return fmt.Errorf("legacywire: read GameMaster origin: %w", err)
	}
	if frame.header.Type != messageOrigin || frame.header.Origin != originGameMaster || len(frame.bytes) != headerSize {
		return fmt.Errorf("%w: type=%#x origin=%d length=%d", errInvalidOrigin, frame.header.Type, frame.header.Origin, len(frame.bytes))
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("legacywire: clear origin deadline: %w", err)
	}
	return nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(data) {
			return io.ErrNoProgress
		}
		data = data[written:]
	}
	return nil
}
