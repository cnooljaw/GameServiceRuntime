package drain

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

// NewCodec creates a ClusterCodec that handles Visitor Registry Commands and delegates all others.
func NewCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec {
	return &codec{fallback: fallback}
}

func (c *codec) Encode(command gsr.CommandID, response bool, value any) ([]byte, error) {
	if command == commandSweepVisitors {
		return nil, ErrUnsupportedCommand
	}
	prototype, handled := visitorPayload(command, response)
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
	if command == commandSweepVisitors {
		return nil, ErrUnsupportedCommand
	}
	prototype, handled := visitorPayload(command, response)
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

func visitorPayload(command gsr.CommandID, response bool) (any, bool) {
	switch command {
	case commandAcquireVisitorLease:
		if response {
			return leaseResponse{}, true
		}
		return acquireVisitorLeaseRequest{}, true
	case commandRenewVisitorLease:
		if response {
			return leaseResponse{}, true
		}
		return renewVisitorLeaseRequest{}, true
	case commandReleaseVisitorLease:
		if response {
			return emptyResponse{}, true
		}
		return releaseVisitorLeaseRequest{}, true
	case commandListVisitors:
		if response {
			return listVisitorsResponse{}, true
		}
		return listVisitorsRequest{}, true
	default:
		return nil, false
	}
}

func validWireResponse(command gsr.CommandID, value any) bool {
	switch command {
	case commandAcquireVisitorLease, commandRenewVisitorLease:
		response, ok := value.(leaseResponse)
		return ok &&
			validResponseCode(response.Error) &&
			(response.Error != responseOK || validWireLease(response.Lease))
	case commandReleaseVisitorLease:
		response, ok := value.(emptyResponse)
		return ok && validResponseCode(response.Error)
	case commandListVisitors:
		response, ok := value.(listVisitorsResponse)
		return ok &&
			validResponseCode(response.Error) &&
			(response.Error != responseOK || validWireVisitorRefs(response.Visitors))
	default:
		return false
	}
}

func validResponseCode(code errorCode) bool {
	switch code {
	case responseOK,
		responseInvalidLease,
		responseInvalidTarget,
		responseInvalidVisitor,
		responseLeaseExpired,
		responseLeaseOwnerMismatch,
		responseLeaseExhausted,
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
