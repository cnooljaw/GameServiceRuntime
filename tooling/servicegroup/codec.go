package servicegroup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type codec struct {
	fallback gsr.ClusterCodec
}

// NewCodec creates a ClusterCodec that handles ServiceGroup Commands and delegates all others.
func NewCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec {
	return &codec{fallback: fallback}
}

func (c *codec) Encode(command gsr.CommandID, response bool, value any) ([]byte, error) {
	if command == commandSweepExpiredWatches {
		return nil, ErrUnsupportedCommand
	}
	prototype, handled := serviceGroupPayload(command, response)
	if !handled {
		if c.fallback == nil {
			return nil, ErrUnsupportedCommand
		}
		return c.fallback.Encode(command, response, value)
	}
	if reflect.TypeOf(value) != reflect.TypeOf(prototype) {
		return nil, fmt.Errorf("%w: command %d response=%t has payload %T, want %T", ErrInvalidResponse, command, response, value, prototype)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return payload, nil
}

func (c *codec) Decode(command gsr.CommandID, response bool, payload []byte) (any, error) {
	if command == commandSweepExpiredWatches {
		return nil, ErrUnsupportedCommand
	}
	prototype, handled := serviceGroupPayload(command, response)
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
	if response && !validWireResponse(value) {
		return nil, ErrInvalidResponse
	}
	return value, nil
}

func serviceGroupPayload(command gsr.CommandID, response bool) (any, bool) {
	switch command {
	case commandPublishServiceSet:
		if response {
			return serviceSetResponse{}, true
		}
		return publishServiceSetRequest{}, true
	case commandGetServiceSet:
		if response {
			return serviceSetResponse{}, true
		}
		return getServiceSetRequest{}, true
	default:
		return nil, false
	}
}

func validWireResponse(value any) bool {
	response, ok := value.(serviceSetResponse)
	if !ok || !validResponseCode(response.Error) {
		return false
	}
	return response.Error != responseOK || validWireServiceSet(response.Set)
}

func validResponseCode(code errorCode) bool {
	switch code {
	case responseOK,
		responseInvalidGroup,
		responseInvalidServiceSet,
		responseGroupNotFound,
		responseVersionConflict,
		responseVersionExhausted,
		responseUnauthorized,
		responseInvalidRequest:
		return true
	default:
		return false
	}
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%w: trailing JSON value", ErrInvalidResponse)
		}
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return nil
}
