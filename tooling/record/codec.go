package record

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type clusterCodec struct{ fallback gsr.ClusterCodec }

// NewClusterCodec creates a Recorder protocol codec that delegates unrelated Commands to fallback.
func NewClusterCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec {
	return &clusterCodec{fallback: fallback}
}

func (c *clusterCodec) Encode(command gsr.CommandID, response bool, value any) ([]byte, error) {
	prototype, handled := recorderPayload(command, response)
	if !handled {
		if c.fallback == nil {
			return nil, ErrUnsupportedCommand
		}
		return c.fallback.Encode(command, response, value)
	}
	if reflect.TypeOf(value) != reflect.TypeOf(prototype) {
		return nil, fmt.Errorf("%w: command %d response=%t has payload %T, want %T", ErrInvalidResponse, command, response, value, prototype)
	}
	if err := validateRecorderPayload(command, response, value); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return payload, nil
}

func (c *clusterCodec) Decode(command gsr.CommandID, response bool, payload []byte) (any, error) {
	prototype, handled := recorderPayload(command, response)
	if !handled {
		if c.fallback == nil {
			return nil, ErrUnsupportedCommand
		}
		return c.fallback.Decode(command, response, payload)
	}
	target := reflect.New(reflect.TypeOf(prototype))
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(target.Interface()); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return nil, err
	}
	value := target.Elem().Interface()
	if err := validateRecorderPayload(command, response, value); err != nil {
		return nil, err
	}
	return value, nil
}

func recorderPayload(command gsr.CommandID, response bool) (any, bool) {
	switch command {
	case AppendRecordCommand:
		if response {
			return emptyResponse{}, true
		}
		return appendRecordRequest{}, true
	case ListRecordsCommand:
		if response {
			return listRecordsResponse{}, true
		}
		return listRecordsRequest{}, true
	case ClearRecordsCommand:
		if response {
			return emptyResponse{}, true
		}
		return clearRecordsRequest{}, true
	default:
		return nil, false
	}
}

func validateRecorderPayload(command gsr.CommandID, response bool, value any) error {
	switch typed := value.(type) {
	case appendRecordRequest:
		if response || validateEntry(typed.Entry) != nil {
			return ErrInvalidResponse
		}
	case listRecordsRequest:
		if response || validateKey(typed.Key) != nil || typed.Limit <= 0 {
			return ErrInvalidResponse
		}
	case clearRecordsRequest:
		if response || validateKey(typed.Key) != nil {
			return ErrInvalidResponse
		}
	case emptyResponse:
		if !response || !validResponseCode(typed.Error) {
			return ErrInvalidResponse
		}
	case listRecordsResponse:
		if !response || !validResponseCode(typed.Error) {
			return ErrInvalidResponse
		}
		for _, entry := range typed.Entries {
			if validateEntry(entry) != nil {
				return ErrInvalidResponse
			}
		}
	default:
		return ErrInvalidResponse
	}
	return nil
}

func validResponseCode(code responseCode) bool {
	return code >= responseOK && code <= responseSequenceConflict
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return ErrInvalidResponse
		}
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return nil
}
