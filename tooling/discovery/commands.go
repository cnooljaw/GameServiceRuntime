package discovery

import (
	"errors"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	commandRegisterNode   gsr.CommandID = 0x02000101
	commandHeartbeat      gsr.CommandID = 0x02000102
	commandUnregisterNode gsr.CommandID = 0x02000103
	commandGetNode        gsr.CommandID = 0x02000104
	commandListNodes      gsr.CommandID = 0x02000105
	commandRegisterName   gsr.CommandID = 0x02000106
	commandUnregisterName gsr.CommandID = 0x02000107
	commandResolveName    gsr.CommandID = 0x02000108
	commandSweepExpired   gsr.CommandID = 0x020001ff
)

type errorCode string

const (
	errorNone               errorCode = ""
	errorInvalidNode        errorCode = "invalid_node"
	errorNodeNotFound       errorCode = "node_not_found"
	errorLeaseExpired       errorCode = "lease_expired"
	errorLeaseOwnerMismatch errorCode = "lease_owner_mismatch"
	errorInvalidName        errorCode = "invalid_name"
	errorNameNotFound       errorCode = "name_not_found"
	errorNameConflict       errorCode = "name_conflict"
)

type registerNodeRequest struct {
	Node    gsr.NodeID `json:"node"`
	Address string     `json:"address"`
}

type heartbeatRequest struct {
	Lease NodeLease `json:"lease"`
}

type unregisterNodeRequest struct {
	Lease NodeLease `json:"lease"`
}

type getNodeRequest struct {
	Node gsr.NodeID `json:"node"`
}

type listNodesRequest struct{}

type wireServiceRef struct {
	Node gsr.NodeID    `json:"node"`
	ID   gsr.ServiceID `json:"id"`
}

func newWireServiceRef(ref gsr.ServiceRef) wireServiceRef {
	return wireServiceRef{Node: ref.Node, ID: ref.ID}
}

func (ref wireServiceRef) serviceRef() gsr.ServiceRef {
	return gsr.ServiceRef{Node: ref.Node, ID: ref.ID}
}

type registerNameRequest struct {
	Lease NodeLease       `json:"lease"`
	Name  gsr.ServiceName `json:"name"`
	Ref   wireServiceRef  `json:"ref"`
}

type unregisterNameRequest struct {
	Lease NodeLease       `json:"lease"`
	Name  gsr.ServiceName `json:"name"`
	Ref   wireServiceRef  `json:"ref"`
}

type resolveNameRequest struct {
	Name gsr.ServiceName `json:"name"`
}

type leaseResponse struct {
	Lease NodeLease `json:"lease"`
	Error errorCode `json:"error"`
}

type nodeResponse struct {
	Node  NodeRecord `json:"node"`
	Error errorCode  `json:"error"`
}

type nodesResponse struct {
	Nodes []NodeRecord `json:"nodes"`
	Error errorCode    `json:"error"`
}

type emptyResponse struct {
	Error errorCode `json:"error"`
}

type refResponse struct {
	Ref   wireServiceRef `json:"ref"`
	Error errorCode      `json:"error"`
}

func errorFromCode(code errorCode) error {
	switch code {
	case errorNone:
		return nil
	case errorInvalidNode:
		return ErrInvalidNode
	case errorNodeNotFound:
		return ErrNodeNotFound
	case errorLeaseExpired:
		return ErrLeaseExpired
	case errorLeaseOwnerMismatch:
		return ErrLeaseOwnerMismatch
	case errorInvalidName:
		return ErrInvalidName
	case errorNameNotFound:
		return ErrNameNotFound
	case errorNameConflict:
		return ErrNameConflict
	default:
		return ErrInvalidResponse
	}
}

func codeFromError(err error) (errorCode, bool) {
	switch {
	case err == nil:
		return errorNone, true
	case errors.Is(err, ErrInvalidNode):
		return errorInvalidNode, true
	case errors.Is(err, ErrNodeNotFound):
		return errorNodeNotFound, true
	case errors.Is(err, ErrLeaseExpired):
		return errorLeaseExpired, true
	case errors.Is(err, ErrLeaseOwnerMismatch):
		return errorLeaseOwnerMismatch, true
	case errors.Is(err, ErrInvalidName):
		return errorInvalidName, true
	case errors.Is(err, ErrNameNotFound):
		return errorNameNotFound, true
	case errors.Is(err, ErrNameConflict):
		return errorNameConflict, true
	default:
		return errorNone, false
	}
}
