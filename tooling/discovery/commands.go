package discovery

import gsr "github.com/lijiawang/GameServiceRuntime/runtime"

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
	errorNone         errorCode = ""
	errorUnknown      errorCode = "unknown"
	errorInvalidNode  errorCode = "invalid_node"
	errorNodeNotFound errorCode = "node_not_found"
	errorLeaseExpired errorCode = "lease_expired"
	errorInvalidName  errorCode = "invalid_name"
	errorNameNotFound errorCode = "name_not_found"
	errorNameConflict errorCode = "name_conflict"
)

type registerNodeRequest struct {
	Node    gsr.NodeID
	Address string
}

type heartbeatRequest struct {
	Lease NodeLease
}

type unregisterNodeRequest struct {
	Lease NodeLease
}

type getNodeRequest struct {
	Node gsr.NodeID
}

type listNodesRequest struct{}

type registerNameRequest struct {
	Lease NodeLease
	Name  gsr.ServiceName
	Ref   gsr.ServiceRef
}

type unregisterNameRequest struct {
	Lease NodeLease
	Name  gsr.ServiceName
	Ref   gsr.ServiceRef
}

type resolveNameRequest struct {
	Name gsr.ServiceName
}

type leaseResponse struct {
	Lease NodeLease
	Error errorCode
}

type nodeResponse struct {
	Node  NodeRecord
	Error errorCode
}

type nodesResponse struct {
	Nodes []NodeRecord
	Error errorCode
}

type emptyResponse struct {
	Error errorCode
}

type refResponse struct {
	Ref   gsr.ServiceRef
	Error errorCode
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

func codeFromError(err error) errorCode {
	switch err {
	case nil:
		return errorNone
	case ErrInvalidNode:
		return errorInvalidNode
	case ErrNodeNotFound:
		return errorNodeNotFound
	case ErrLeaseExpired:
		return errorLeaseExpired
	case ErrInvalidName:
		return errorInvalidName
	case ErrNameNotFound:
		return errorNameNotFound
	case ErrNameConflict:
		return errorNameConflict
	default:
		return errorUnknown
	}
}
