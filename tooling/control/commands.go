package control

import (
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/monitor"
)

const (
	commandGetNodeReport      gsr.CommandID = 0x02500101
	commandRegisterNodeLease  gsr.CommandID = 0x025001fe
	commandHeartbeatNodeLease gsr.CommandID = 0x025001ff
	commandListNodes          gsr.CommandID = 0x02500201
	commandGetNodeDetail      gsr.CommandID = 0x02500202
	commandRefreshNode        gsr.CommandID = 0x02500203
)

type errorCode string

const (
	responseOK             errorCode = ""
	responseInvalidNode    errorCode = "invalid_node"
	responseNodeNotFound   errorCode = "node_not_found"
	responseNodeDisabled   errorCode = "node_disabled"
	responseUnauthorized   errorCode = "unauthorized"
	responseInvalidRequest errorCode = "invalid_response"
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

type controlRefreshResult struct {
	Detail    NodeDetail
	Completed time.Time
}
