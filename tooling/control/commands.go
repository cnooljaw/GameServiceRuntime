package control

import (
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/monitor"
)

const (
	commandGetNodeReport         gsr.CommandID = 0x02500101
	commandRegisterNodeLease     gsr.CommandID = 0x025001fe
	commandHeartbeatNodeLease    gsr.CommandID = 0x025001ff
	commandListNodes             gsr.CommandID = 0x02500201
	commandGetNodeDetail         gsr.CommandID = 0x02500202
	commandRefreshNode           gsr.CommandID = 0x02500203
	commandStartDrainOperation   gsr.CommandID = 0x02500301
	commandResolveDrainOperation gsr.CommandID = 0x02500302
	commandGetDrainOperation     gsr.CommandID = 0x02500303
	commandListDrainAudit        gsr.CommandID = 0x02500304
)

type errorCode string

const (
	responseOK                     errorCode = ""
	responseInvalidNode            errorCode = "invalid_node"
	responseNodeNotFound           errorCode = "node_not_found"
	responseNodeDisabled           errorCode = "node_disabled"
	responseUnauthorized           errorCode = "unauthorized"
	responseInvalidRequest         errorCode = "invalid_response"
	responseInvalidPrincipal       errorCode = "invalid_principal"
	responseInvalidRequestID       errorCode = "invalid_request_id"
	responseInvalidDrainRequest    errorCode = "invalid_drain_request"
	responseRequestConflict        errorCode = "request_conflict"
	responseOperationNotFound      errorCode = "operation_not_found"
	responseOperationOwnerMismatch errorCode = "operation_owner_mismatch"
)

type getNodeReportRequest struct{}

type listNodesRequest struct{}

type getNodeDetailRequest struct {
	Node gsr.NodeID `json:"node"`
}

type refreshNodeRequest struct {
	Node gsr.NodeID `json:"node"`
}

type nodeReportResponse struct {
	Report monitor.Report `json:"report"`
	Error  errorCode      `json:"error"`
}

type nodeDetailResponse struct {
	Detail NodeDetail `json:"detail"`
	Error  errorCode  `json:"error"`
}

type nodesResponse struct {
	Nodes []NodeDetail `json:"nodes"`
	Error errorCode    `json:"error"`
}

type startDrainOperationRequest struct {
	Request StartDrainRequest `json:"request"`
}

type resolveDrainOperationRequest struct {
	RequestID RequestID `json:"request_id"`
	Principal Principal `json:"principal"`
}

type getDrainOperationRequest struct {
	RequestID RequestID `json:"request_id"`
	Principal Principal `json:"principal"`
}

type listDrainAuditRequest struct {
	Principal Principal `json:"principal"`
}

type drainOperationResponse struct {
	Operation DrainOperation `json:"operation"`
	Error     errorCode      `json:"error"`
}

type drainAuditsResponse struct {
	Audits []DrainAudit `json:"audits"`
	Error  errorCode    `json:"error"`
}
