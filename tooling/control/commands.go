package control

import (
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/monitor"
)

const (
	commandGetNodeReport         gsr.CommandID = 0x02500101
	commandBeginNodeStop         gsr.CommandID = 0x02500102
	commandGetNodeStopReceipt    gsr.CommandID = 0x02500103
	commandBeginRecoveryCreate   gsr.CommandID = 0x02500104
	commandGetRecoveryReceipt    gsr.CommandID = 0x02500105
	commandRecordRecoveryCreate  gsr.CommandID = 0x025001fc
	commandRecordNodeStopResult  gsr.CommandID = 0x025001fd
	commandRegisterNodeLease     gsr.CommandID = 0x025001fe
	commandHeartbeatNodeLease    gsr.CommandID = 0x025001ff
	commandListNodes             gsr.CommandID = 0x02500201
	commandGetNodeDetail         gsr.CommandID = 0x02500202
	commandRefreshNode           gsr.CommandID = 0x02500203
	commandStartDrainOperation   gsr.CommandID = 0x02500301
	commandResolveDrainOperation gsr.CommandID = 0x02500302
	commandGetDrainOperation     gsr.CommandID = 0x02500303
	commandListDrainAudit        gsr.CommandID = 0x02500304
	commandBeginDrainStop        gsr.CommandID = 0x02500305
	commandResolveDrainStop      gsr.CommandID = 0x02500306
	commandGetDrainStop          gsr.CommandID = 0x02500307
	commandBeginRecovery         gsr.CommandID = 0x02500308
	commandConfirmRecovery       gsr.CommandID = 0x02500309
	commandResolveRecovery       gsr.CommandID = 0x0250030a
	commandGetRecovery           gsr.CommandID = 0x0250030b
	commandAbandonRecovery       gsr.CommandID = 0x0250030c
)

type errorCode string

const (
	responseOK                      errorCode = ""
	responseInvalidNode             errorCode = "invalid_node"
	responseNodeNotFound            errorCode = "node_not_found"
	responseNodeDisabled            errorCode = "node_disabled"
	responseUnauthorized            errorCode = "unauthorized"
	responseInvalidRequest          errorCode = "invalid_response"
	responseInvalidPrincipal        errorCode = "invalid_principal"
	responseInvalidRequestID        errorCode = "invalid_request_id"
	responseInvalidDrainRequest     errorCode = "invalid_drain_request"
	responseRequestConflict         errorCode = "request_conflict"
	responseOperationNotFound       errorCode = "operation_not_found"
	responseOperationOwnerMismatch  errorCode = "operation_owner_mismatch"
	responseInvalidStopRequest      errorCode = "invalid_stop_request"
	responseStopOperationNotFound   errorCode = "stop_operation_not_found"
	responseStopDisabled            errorCode = "stop_disabled"
	responseStopRequestConflict     errorCode = "stop_request_conflict"
	responseStopNotReady            errorCode = "stop_not_ready"
	responseStopTargetMismatch      errorCode = "stop_target_mismatch"
	responseInvalidRecoveryRequest  errorCode = "invalid_recovery_request"
	responseRecoveryNotFound        errorCode = "recovery_not_found"
	responseRecoveryRequestConflict errorCode = "recovery_request_conflict"
	responseRecoveryNotReady        errorCode = "recovery_not_ready"
	responseRecoveryDisabled        errorCode = "recovery_disabled"
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

type beginNodeStopRequest struct {
	Task NodeStopTask `json:"task"`
}

type getNodeStopReceiptRequest struct {
	RequestID RequestID      `json:"request_id"`
	Target    gsr.ServiceRef `json:"target"`
}

type nodeStopReceiptResponse struct {
	Receipt NodeStopReceipt `json:"receipt"`
	Error   errorCode       `json:"error"`
}

type beginRecoveryCreateRequest struct {
	Task RecoveryCreateTask `json:"task"`
}

type getRecoveryReceiptRequest struct {
	RequestID RequestID      `json:"request_id"`
	Removed   gsr.ServiceRef `json:"removed"`
}

type recoveryReceiptResponse struct {
	Receipt RecoveryReceipt `json:"receipt"`
	Error   errorCode       `json:"error"`
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

type beginDrainStopRequest struct {
	Request BeginStopRequest `json:"request"`
}

type resolveDrainStopRequest struct {
	RequestID RequestID `json:"request_id"`
	Principal Principal `json:"principal"`
}

type getDrainStopRequest struct {
	RequestID RequestID `json:"request_id"`
	Principal Principal `json:"principal"`
}

type stopOperationResponse struct {
	Operation StopOperation `json:"operation"`
	Error     errorCode     `json:"error"`
}

type beginRecoveryRequest struct {
	Request BeginRecoveryRequest `json:"request"`
}

type recoveryOperationRequest struct {
	RequestID RequestID `json:"request_id"`
	Principal Principal `json:"principal"`
}

type recoveryOperationResponse struct {
	Operation RecoveryOperation `json:"operation"`
	Error     errorCode         `json:"error"`
}
