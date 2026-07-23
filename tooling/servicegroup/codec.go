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
	if command == ServiceSetChangedCommand {
		if response {
			return nil, ErrUnsupportedCommand
		}
		change, ok := value.(ServiceSetChanged)
		if !ok || !validServiceSet(change.Set) {
			return nil, ErrInvalidResponse
		}
		payload, err := json.Marshal(wireServiceSetChanged{Set: newWireServiceSet(change.Set)})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
		}
		return payload, nil
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
	if response && !validWireResponse(command, value) {
		return nil, ErrInvalidResponse
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
	if command == ServiceSetChangedCommand {
		if response {
			return nil, ErrUnsupportedCommand
		}
		var changed wireServiceSetChanged
		if err := decodeJSON(payload, &changed); err != nil {
			return nil, err
		}
		if !validWireServiceSet(changed.Set) {
			return nil, ErrInvalidResponse
		}
		return ServiceSetChanged{Set: changed.Set.serviceSet()}, nil
	}
	prototype, handled := serviceGroupPayload(command, response)
	if !handled {
		if c.fallback == nil {
			return nil, ErrUnsupportedCommand
		}
		return c.fallback.Decode(command, response, payload)
	}
	target := reflect.New(reflect.TypeOf(prototype))
	if err := decodeJSON(payload, target.Interface()); err != nil {
		return nil, err
	}
	value := target.Elem().Interface()
	if response && !validWireResponse(command, value) {
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
	case commandWatchServiceGroup:
		if response {
			return watchResultResponse{}, true
		}
		return watchServiceGroupRequest{}, true
	case commandRenewServiceGroupWatch:
		if response {
			return watchLeaseResponse{}, true
		}
		return renewServiceGroupWatchRequest{}, true
	case commandUnwatchServiceGroup:
		if response {
			return emptyResponse{}, true
		}
		return unwatchServiceGroupRequest{}, true
	default:
		return nil, false
	}
}

func validWireResponse(command gsr.CommandID, value any) bool {
	switch command {
	case commandPublishServiceSet, commandGetServiceSet:
		response, ok := value.(serviceSetResponse)
		return ok &&
			validResponseCode(response.Error) &&
			(response.Error != responseOK || validWireServiceSet(response.Set))
	case commandWatchServiceGroup:
		response, ok := value.(watchResultResponse)
		return ok &&
			validResponseCode(response.Error) &&
			(response.Error != responseOK || validWireWatchResult(response))
	case commandRenewServiceGroupWatch:
		response, ok := value.(watchLeaseResponse)
		return ok &&
			validResponseCode(response.Error) &&
			(response.Error != responseOK || validWireWatchLease(response.Lease))
	case commandUnwatchServiceGroup:
		response, ok := value.(emptyResponse)
		return ok && validResponseCode(response.Error)
	default:
		return false
	}
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
		responseInvalidWatch,
		responseWatchExpired,
		responseWatchOwnerMismatch,
		responseInvalidRequest:
		return true
	default:
		return false
	}
}

func decodeJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return requireJSONEnd(decoder)
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
