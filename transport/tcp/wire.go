package tcp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	protocolVersion       uint16 = 1
	wireVersion           byte   = 1
	defaultMaxFrameSize   uint32 = 16 << 20
	maxHandshakeFrameSize        = 1024
	maxNodeIDLength              = 255
	maxCallPathLength            = 64
	maxErrorCodeLength           = 128
	maxErrorMessageLength        = 4096
)

var (
	// ErrFrameTooLarge indicates that a frame or bounded field exceeded its configured limit.
	ErrFrameTooLarge = errors.New("gsr tcp: frame too large")
	// ErrInvalidFrame indicates that a handshake or WireEnvelope frame is malformed.
	ErrInvalidFrame = errors.New("gsr tcp: invalid frame")
	// ErrProtocolVersion indicates that peer protocol versions do not match.
	ErrProtocolVersion = errors.New("gsr tcp: protocol version mismatch")
	// ErrPeerIdentity indicates that a peer declared an unexpected NodeID.
	ErrPeerIdentity = errors.New("gsr tcp: peer identity mismatch")
)

var handshakeMagic = [4]byte{'G', 'S', 'R', 'H'}

func encodeWireEnvelope(envelope gsr.WireEnvelope, maxFrameSize uint32) ([]byte, error) {
	if maxFrameSize == 0 {
		return nil, ErrFrameTooLarge
	}
	size, err := wireEnvelopeSize(envelope)
	if err != nil {
		return nil, err
	}
	maxInt := uint64(^uint(0) >> 1)
	if size > uint64(maxFrameSize) || size > maxInt {
		return nil, ErrFrameTooLarge
	}
	var output bytes.Buffer
	output.Grow(int(size))
	output.WriteByte(wireVersion)
	var flags byte
	if envelope.Response {
		flags = 1
	}
	output.WriteByte(flags)
	if err := writeServiceRef(&output, envelope.Source); err != nil {
		return nil, err
	}
	if err := writeServiceRef(&output, envelope.Target); err != nil {
		return nil, err
	}
	writeUint64(&output, uint64(envelope.Session))
	writeUint32(&output, uint32(envelope.Command))
	writeUint16(&output, uint16(len(envelope.CallPath)))
	for _, ref := range envelope.CallPath {
		if err := writeServiceRef(&output, ref); err != nil {
			return nil, err
		}
	}
	if err := writeString16(&output, envelope.ErrorCode, maxErrorCodeLength); err != nil {
		return nil, err
	}
	if err := writeString16(&output, envelope.ErrorMessage, maxErrorMessageLength); err != nil {
		return nil, err
	}
	writeUint32(&output, uint32(len(envelope.Payload)))
	output.Write(envelope.Payload)
	return output.Bytes(), nil
}

func wireEnvelopeSize(envelope gsr.WireEnvelope) (uint64, error) {
	if len(envelope.CallPath) > maxCallPathLength || len(envelope.ErrorCode) > maxErrorCodeLength || len(envelope.ErrorMessage) > maxErrorMessageLength {
		return 0, ErrFrameTooLarge
	}
	if uint64(len(envelope.Payload)) > uint64(^uint32(0)) {
		return 0, ErrFrameTooLarge
	}
	sourceSize, err := serviceRefWireSize(envelope.Source)
	if err != nil {
		return 0, err
	}
	targetSize, err := serviceRefWireSize(envelope.Target)
	if err != nil {
		return 0, err
	}
	size := uint64(2) + sourceSize + targetSize + 8 + 4 + 2
	for _, ref := range envelope.CallPath {
		refSize, err := serviceRefWireSize(ref)
		if err != nil {
			return 0, err
		}
		size += refSize
	}
	size += 2 + uint64(len(envelope.ErrorCode))
	size += 2 + uint64(len(envelope.ErrorMessage))
	size += 4 + uint64(len(envelope.Payload))
	return size, nil
}

func serviceRefWireSize(ref gsr.ServiceRef) (uint64, error) {
	if len(ref.Node) > maxNodeIDLength || len(ref.Node) > int(^uint16(0)) {
		return 0, ErrFrameTooLarge
	}
	return 2 + uint64(len(ref.Node)) + 8, nil
}

func decodeWireEnvelope(data []byte) (gsr.WireEnvelope, error) {
	reader := wireReader{data: data}
	version, err := reader.byte()
	if err != nil || version != wireVersion {
		return gsr.WireEnvelope{}, ErrInvalidFrame
	}
	flags, err := reader.byte()
	if err != nil || flags&^byte(1) != 0 {
		return gsr.WireEnvelope{}, ErrInvalidFrame
	}
	source, err := reader.serviceRef()
	if err != nil {
		return gsr.WireEnvelope{}, err
	}
	target, err := reader.serviceRef()
	if err != nil {
		return gsr.WireEnvelope{}, err
	}
	session, err := reader.uint64()
	if err != nil {
		return gsr.WireEnvelope{}, err
	}
	command, err := reader.uint32()
	if err != nil {
		return gsr.WireEnvelope{}, err
	}
	pathLength, err := reader.uint16()
	if err != nil || pathLength > maxCallPathLength {
		return gsr.WireEnvelope{}, ErrInvalidFrame
	}
	var path []gsr.ServiceRef
	if pathLength > 0 {
		path = make([]gsr.ServiceRef, 0, pathLength)
	}
	for range int(pathLength) {
		ref, err := reader.serviceRef()
		if err != nil {
			return gsr.WireEnvelope{}, err
		}
		path = append(path, ref)
	}
	errorCode, err := reader.string16(maxErrorCodeLength)
	if err != nil {
		return gsr.WireEnvelope{}, err
	}
	errorMessage, err := reader.string16(maxErrorMessageLength)
	if err != nil {
		return gsr.WireEnvelope{}, err
	}
	payloadLength, err := reader.uint32()
	if err != nil {
		return gsr.WireEnvelope{}, err
	}
	payload, err := reader.bytes(payloadLength)
	if err != nil || reader.remaining() != 0 {
		return gsr.WireEnvelope{}, ErrInvalidFrame
	}
	return gsr.WireEnvelope{
		Source:       source,
		Target:       target,
		Session:      gsr.SessionID(session),
		Command:      gsr.CommandID(command),
		Payload:      append([]byte(nil), payload...),
		Response:     flags&1 != 0,
		CallPath:     path,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
	}, nil
}

func writeFrame(writer io.Writer, body []byte, maxFrameSize uint32) error {
	if len(body) == 0 {
		return ErrInvalidFrame
	}
	if uint64(len(body)) > uint64(maxFrameSize) {
		return ErrFrameTooLarge
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(body)))
	if err := writeAll(writer, header[:]); err != nil {
		return err
	}
	return writeAll(writer, body)
}

func readFrame(reader io.Reader, maxFrameSize uint32) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 {
		return nil, ErrInvalidFrame
	}
	if length > maxFrameSize {
		return nil, ErrFrameTooLarge
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	return body, nil
}

func initiateHandshake(conn io.ReadWriter, local, target gsr.NodeID, version uint16, timeout time.Duration) (gsr.NodeID, error) {
	reset := setDeadline(conn, timeout)
	defer reset()
	if err := writeHandshake(conn, local, version); err != nil {
		return "", err
	}
	peer, peerVersion, err := readHandshake(conn)
	if err != nil {
		return "", err
	}
	if peerVersion != version {
		return "", ErrProtocolVersion
	}
	if peer != target || peer == local || peer == "" {
		return "", ErrPeerIdentity
	}
	return peer, nil
}

func acceptHandshake(conn io.ReadWriter, local gsr.NodeID, version uint16, timeout time.Duration) (gsr.NodeID, error) {
	reset := setDeadline(conn, timeout)
	defer reset()
	peer, peerVersion, err := readHandshake(conn)
	if err != nil {
		return "", err
	}
	if err := writeHandshake(conn, local, version); err != nil {
		return "", err
	}
	if peerVersion != version {
		return "", ErrProtocolVersion
	}
	if peer == "" || peer == local {
		return "", ErrPeerIdentity
	}
	return peer, nil
}

func writeHandshake(writer io.Writer, node gsr.NodeID, version uint16) error {
	if node == "" || len(node) > maxNodeIDLength {
		return ErrPeerIdentity
	}
	var body bytes.Buffer
	body.Write(handshakeMagic[:])
	writeUint16(&body, version)
	if err := writeString16(&body, string(node), maxNodeIDLength); err != nil {
		return err
	}
	return writeFrame(writer, body.Bytes(), maxHandshakeFrameSize)
}

func readHandshake(reader io.Reader) (gsr.NodeID, uint16, error) {
	body, err := readFrame(reader, maxHandshakeFrameSize)
	if err != nil {
		return "", 0, err
	}
	wire := wireReader{data: body}
	magic, err := wire.bytes(uint32(len(handshakeMagic)))
	if err != nil || !bytes.Equal(magic, handshakeMagic[:]) {
		return "", 0, ErrInvalidFrame
	}
	version, err := wire.uint16()
	if err != nil {
		return "", 0, err
	}
	node, err := wire.string16(maxNodeIDLength)
	if err != nil || node == "" || wire.remaining() != 0 {
		return "", 0, ErrInvalidFrame
	}
	return gsr.NodeID(node), version, nil
}

func setDeadline(conn io.ReadWriter, timeout time.Duration) func() {
	deadlineConn, ok := conn.(interface{ SetDeadline(time.Time) error })
	if !ok || timeout <= 0 {
		return func() {}
	}
	_ = deadlineConn.SetDeadline(time.Now().Add(timeout))
	return func() { _ = deadlineConn.SetDeadline(time.Time{}) }
}

func writeServiceRef(output *bytes.Buffer, ref gsr.ServiceRef) error {
	if err := writeString16(output, string(ref.Node), maxNodeIDLength); err != nil {
		return err
	}
	writeUint64(output, uint64(ref.ID))
	return nil
}

func writeString16(output *bytes.Buffer, value string, maximum int) error {
	if len(value) > maximum || len(value) > int(^uint16(0)) {
		return ErrFrameTooLarge
	}
	writeUint16(output, uint16(len(value)))
	output.WriteString(value)
	return nil
}

func writeUint16(output *bytes.Buffer, value uint16) {
	var data [2]byte
	binary.BigEndian.PutUint16(data[:], value)
	output.Write(data[:])
}

func writeUint32(output *bytes.Buffer, value uint32) {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], value)
	output.Write(data[:])
}

func writeUint64(output *bytes.Buffer, value uint64) {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], value)
	output.Write(data[:])
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrUnexpectedEOF
		}
		data = data[written:]
	}
	return nil
}

type wireReader struct {
	data []byte
	pos  int
}

func (r *wireReader) remaining() int { return len(r.data) - r.pos }

func (r *wireReader) byte() (byte, error) {
	data, err := r.bytes(1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (r *wireReader) bytes(length uint32) ([]byte, error) {
	if uint64(length) > uint64(r.remaining()) {
		return nil, ErrInvalidFrame
	}
	start := r.pos
	r.pos += int(length)
	return r.data[start:r.pos], nil
}

func (r *wireReader) uint16() (uint16, error) {
	data, err := r.bytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(data), nil
}

func (r *wireReader) uint32() (uint32, error) {
	data, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(data), nil
}

func (r *wireReader) uint64() (uint64, error) {
	data, err := r.bytes(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(data), nil
}

func (r *wireReader) string16(maximum int) (string, error) {
	length, err := r.uint16()
	if err != nil {
		return "", err
	}
	if int(length) > maximum {
		return "", ErrFrameTooLarge
	}
	data, err := r.bytes(uint32(length))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *wireReader) serviceRef() (gsr.ServiceRef, error) {
	node, err := r.string16(maxNodeIDLength)
	if err != nil {
		return gsr.ServiceRef{}, err
	}
	id, err := r.uint64()
	if err != nil {
		return gsr.ServiceRef{}, err
	}
	return gsr.ServiceRef{Node: gsr.NodeID(node), ID: gsr.ServiceID(id)}, nil
}
