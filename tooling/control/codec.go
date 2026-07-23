package control

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

// NewCodec creates a ClusterCodec that handles Control Plane commands and delegates all others.
func NewCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec {
	return &codec{fallback: fallback}
}

func (c *codec) Encode(command gsr.CommandID, response bool, value any) ([]byte, error) {
	prototype, handled := controlPayload(command, response)
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
	prototype, handled := controlPayload(command, response)
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
	if response && !validWireResponse(command, value) {
		return nil, ErrInvalidResponse
	}
	return value, nil
}

func controlPayload(command gsr.CommandID, response bool) (any, bool) {
	switch command {
	case commandGetNodeReport:
		if response {
			return nodeReportResponse{}, true
		}
		return getNodeReportRequest{}, true
	case commandBeginNodeStop:
		if response {
			return nodeStopReceiptResponse{}, true
		}
		return beginNodeStopRequest{}, true
	case commandGetNodeStopReceipt:
		if response {
			return nodeStopReceiptResponse{}, true
		}
		return getNodeStopReceiptRequest{}, true
	case commandListNodes:
		if response {
			return nodesResponse{}, true
		}
		return listNodesRequest{}, true
	case commandGetNodeDetail:
		if response {
			return nodeDetailResponse{}, true
		}
		return getNodeDetailRequest{}, true
	case commandRefreshNode:
		if response {
			return nodeDetailResponse{}, true
		}
		return refreshNodeRequest{}, true
	case commandStartDrainOperation:
		if response {
			return drainOperationResponse{}, true
		}
		return startDrainOperationRequest{}, true
	case commandResolveDrainOperation:
		if response {
			return drainOperationResponse{}, true
		}
		return resolveDrainOperationRequest{}, true
	case commandGetDrainOperation:
		if response {
			return drainOperationResponse{}, true
		}
		return getDrainOperationRequest{}, true
	case commandListDrainAudit:
		if response {
			return drainAuditsResponse{}, true
		}
		return listDrainAuditRequest{}, true
	case commandBeginDrainStop:
		if response {
			return stopOperationResponse{}, true
		}
		return beginDrainStopRequest{}, true
	case commandResolveDrainStop:
		if response {
			return stopOperationResponse{}, true
		}
		return resolveDrainStopRequest{}, true
	case commandGetDrainStop:
		if response {
			return stopOperationResponse{}, true
		}
		return getDrainStopRequest{}, true
	default:
		return nil, false
	}
}

func validWireResponse(command gsr.CommandID, value any) bool {
	switch command {
	case commandGetNodeReport:
		response, ok := value.(nodeReportResponse)
		return ok && validResponseCode(response.Error) && (response.Error != responseOK || validNode(response.Report.Node))
	case commandBeginNodeStop, commandGetNodeStopReceipt:
		response, ok := value.(nodeStopReceiptResponse)
		return ok && validResponseCode(response.Error) && (response.Error != responseOK || validNodeStopReceipt(response.Receipt))
	case commandListNodes:
		response, ok := value.(nodesResponse)
		if !ok || !validResponseCode(response.Error) {
			return false
		}
		for _, detail := range response.Nodes {
			if !validDetail(detail) {
				return false
			}
		}
		return true
	case commandGetNodeDetail, commandRefreshNode:
		response, ok := value.(nodeDetailResponse)
		return ok && validResponseCode(response.Error) && (response.Error != responseOK || validDetail(response.Detail))
	case commandStartDrainOperation, commandResolveDrainOperation, commandGetDrainOperation:
		response, ok := value.(drainOperationResponse)
		return ok && validResponseCode(response.Error) && (response.Error != responseOK || validDrainOperation(response.Operation))
	case commandListDrainAudit:
		response, ok := value.(drainAuditsResponse)
		return ok && validResponseCode(response.Error) && (response.Error != responseOK || validDrainAudits(response.Audits))
	case commandBeginDrainStop, commandResolveDrainStop, commandGetDrainStop:
		response, ok := value.(stopOperationResponse)
		return ok && validResponseCode(response.Error) && (response.Error != responseOK || validStopOperation(response.Operation))
	default:
		return false
	}
}

func validResponseCode(code errorCode) bool {
	switch code {
	case responseOK,
		responseInvalidNode,
		responseNodeNotFound,
		responseNodeDisabled,
		responseUnauthorized,
		responseInvalidRequest,
		responseInvalidPrincipal,
		responseInvalidRequestID,
		responseInvalidDrainRequest,
		responseRequestConflict,
		responseOperationNotFound,
		responseOperationOwnerMismatch,
		responseInvalidStopRequest,
		responseStopOperationNotFound,
		responseStopDisabled,
		responseStopRequestConflict,
		responseStopNotReady,
		responseStopTargetMismatch:
		return true
	default:
		return false
	}
}

func validDetail(detail NodeDetail) bool {
	if !validNodeConfig(detail.Config) || detail.Observed.ID != detail.Config.ID {
		return false
	}
	switch detail.Observed.Status {
	case NodeUnknown, NodeHealthy, NodeUnavailable, NodeDisabled:
	default:
		return false
	}
	if detail.HasReport && !validNode(detail.Report.Node) {
		return false
	}
	return !detail.HasReport || detail.Report.Node == detail.Config.ID
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
