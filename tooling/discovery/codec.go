package discovery

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

// NewCodec creates a ClusterCodec that handles Discovery Commands and delegates all others.
func NewCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec {
	return &codec{fallback: fallback}
}

func (c *codec) Encode(command gsr.CommandID, response bool, value any) ([]byte, error) {
	if command == commandSweepExpired {
		return nil, ErrUnsupportedCommand
	}
	prototype, handled := discoveryPayload(command, response)
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
	if command == commandSweepExpired {
		return nil, ErrUnsupportedCommand
	}
	prototype, handled := discoveryPayload(command, response)
	if !handled {
		if c.fallback == nil {
			return nil, ErrUnsupportedCommand
		}
		return c.fallback.Decode(command, response, payload)
	}
	target := reflect.New(reflect.TypeOf(prototype))
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target.Interface()); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return nil, err
	}
	return target.Elem().Interface(), nil
}

func discoveryPayload(command gsr.CommandID, response bool) (any, bool) {
	switch command {
	case commandRegisterNode:
		if response {
			return leaseResponse{}, true
		}
		return registerNodeRequest{}, true
	case commandHeartbeat:
		if response {
			return leaseResponse{}, true
		}
		return heartbeatRequest{}, true
	case commandUnregisterNode:
		if response {
			return emptyResponse{}, true
		}
		return unregisterNodeRequest{}, true
	case commandGetNode:
		if response {
			return nodeResponse{}, true
		}
		return getNodeRequest{}, true
	case commandListNodes:
		if response {
			return nodesResponse{}, true
		}
		return listNodesRequest{}, true
	case commandRegisterName:
		if response {
			return emptyResponse{}, true
		}
		return registerNameRequest{}, true
	case commandUnregisterName:
		if response {
			return emptyResponse{}, true
		}
		return unregisterNameRequest{}, true
	case commandResolveName:
		if response {
			return refResponse{}, true
		}
		return resolveNameRequest{}, true
	default:
		return nil, false
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
