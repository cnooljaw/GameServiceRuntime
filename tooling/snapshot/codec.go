package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type codec struct {
	fallback gsr.ClusterCodec
}

type wireCaptureRequest struct{}

type wireState struct {
	Schema   string `json:"schema"`
	Version  uint32 `json:"version"`
	Revision uint64 `json:"revision"`
	Payload  []byte `json:"payload"`
}

type wireCaptureResponse struct {
	State wireState `json:"state"`
}

// NewCodec creates a ClusterCodec for CaptureCommand and delegates all others.
func NewCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec {
	return &codec{fallback: fallback}
}

func (c *codec) Encode(command gsr.CommandID, response bool, value any) ([]byte, error) {
	if command != CaptureCommand {
		if isNil(c.fallback) {
			return nil, ErrUnsupportedCommand
		}
		return c.fallback.Encode(command, response, value)
	}
	if !response {
		if _, ok := value.(CaptureRequest); !ok {
			return nil, fmt.Errorf("%w: command %d response=false has payload %T, want snapshot.CaptureRequest", ErrInvalidResponse, command, value)
		}
		return json.Marshal(wireCaptureRequest{})
	}
	capture, ok := value.(CaptureResponse)
	if !ok {
		return nil, fmt.Errorf("%w: command %d response=true has payload %T, want snapshot.CaptureResponse", ErrInvalidResponse, command, value)
	}
	wire := wireCaptureResponse{State: wireState{
		Schema:   capture.State.Schema,
		Version:  capture.State.Version,
		Revision: capture.State.Revision,
		Payload:  capture.State.Payload,
	}}
	payload, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return payload, nil
}

func (c *codec) Decode(command gsr.CommandID, response bool, payload []byte) (any, error) {
	if command != CaptureCommand {
		if isNil(c.fallback) {
			return nil, ErrUnsupportedCommand
		}
		return c.fallback.Decode(command, response, payload)
	}
	if !response {
		var request wireCaptureRequest
		if err := decodeJSONObject(payload, &request); err != nil {
			return nil, err
		}
		return CaptureRequest{}, nil
	}
	var responsePayload wireCaptureResponse
	if err := decodeJSONObject(payload, &responsePayload); err != nil {
		return nil, err
	}
	return CaptureResponse{State: State{
		Schema:   responsePayload.State.Schema,
		Version:  responsePayload.State.Version,
		Revision: responsePayload.State.Revision,
		Payload:  append([]byte(nil), responsePayload.State.Payload...),
	}}, nil
}

func decodeJSONObject(payload []byte, target any) error {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ErrInvalidResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%w: trailing JSON value", ErrInvalidResponse)
		}
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return nil
}
