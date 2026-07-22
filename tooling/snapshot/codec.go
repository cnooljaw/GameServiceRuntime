package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type codec struct {
	fallback gsr.ClusterCodec
}

type wireKey struct {
	Namespace string `json:"namespace"`
	ID        string `json:"id"`
}

type wireCaptureRequest struct {
	Key wireKey `json:"key"`
}

type wireState struct {
	Schema   string `json:"schema"`
	Version  uint32 `json:"version"`
	Revision uint64 `json:"revision"`
	Payload  []byte `json:"payload"`
}

type wireCaptureResponse struct {
	Key   wireKey   `json:"key"`
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
		request, ok := value.(CaptureRequest)
		if !ok {
			return nil, fmt.Errorf("%w: command %d response=false has payload %T, want snapshot.CaptureRequest", ErrInvalidResponse, command, value)
		}
		if err := validateKey(request.Key); err != nil {
			return nil, fmt.Errorf("%w: request key: %v", ErrInvalidResponse, err)
		}
		return json.Marshal(wireCaptureRequest{Key: toWireKey(request.Key)})
	}
	capture, ok := value.(CaptureResponse)
	if !ok {
		return nil, fmt.Errorf("%w: command %d response=true has payload %T, want snapshot.CaptureResponse", ErrInvalidResponse, command, value)
	}
	if err := validateKey(capture.Key); err != nil {
		return nil, fmt.Errorf("%w: response key: %v", ErrInvalidResponse, err)
	}
	if err := validateState(capture.State, 0); err != nil {
		return nil, fmt.Errorf("%w: response state: %v", ErrInvalidResponse, err)
	}
	wire := wireCaptureResponse{Key: toWireKey(capture.Key), State: wireState{
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
		key := fromWireKey(request.Key)
		if err := validateKey(key); err != nil {
			return nil, fmt.Errorf("%w: request key: %v", ErrInvalidResponse, err)
		}
		return CaptureRequest{Key: key}, nil
	}
	var responsePayload wireCaptureResponse
	if err := decodeJSONObject(payload, &responsePayload); err != nil {
		return nil, err
	}
	result := CaptureResponse{Key: fromWireKey(responsePayload.Key), State: State{
		Schema:   responsePayload.State.Schema,
		Version:  responsePayload.State.Version,
		Revision: responsePayload.State.Revision,
		Payload:  append([]byte(nil), responsePayload.State.Payload...),
	}}
	if err := validateKey(result.Key); err != nil {
		return nil, fmt.Errorf("%w: response key: %v", ErrInvalidResponse, err)
	}
	if err := validateState(result.State, 0); err != nil {
		return nil, fmt.Errorf("%w: response state: %v", ErrInvalidResponse, err)
	}
	return result, nil
}

func decodeJSONObject(payload []byte, target any) error {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' || !utf8.Valid(trimmed) {
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

func toWireKey(key Key) wireKey {
	return wireKey{Namespace: key.Namespace, ID: key.ID}
}

func fromWireKey(key wireKey) Key {
	return Key{Namespace: key.Namespace, ID: key.ID}
}
