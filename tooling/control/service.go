package control

import (
	"context"
	"errors"
	"sort"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const defaultCallTimeout = 3 * time.Second

type controlService struct {
	config  ObserverConfig
	context gsr.ServiceContext
	nodes   map[gsr.NodeID]NodeDetail
}

// NewClusterObserverService creates a ClusterObserverService with frozen static node configuration.
func NewClusterObserverService(config ObserverConfig) (gsr.Service, error) {
	if config.CallTimeout < 0 {
		return nil, ErrInvalidConfig
	}
	if config.CallTimeout == 0 {
		config.CallTimeout = defaultCallTimeout
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	seen := make(map[gsr.NodeID]struct{}, len(config.Nodes))
	config.Nodes = append([]NodeTarget(nil), config.Nodes...)
	nodes := make(map[gsr.NodeID]NodeDetail, len(config.Nodes))
	for _, target := range config.Nodes {
		if !validTarget(target) {
			return nil, ErrInvalidConfig
		}
		if _, exists := seen[target.Config.ID]; exists {
			return nil, ErrInvalidConfig
		}
		seen[target.Config.ID] = struct{}{}
		status := NodeUnknown
		if !target.Config.Enabled {
			status = NodeDisabled
		}
		nodes[target.Config.ID] = NodeDetail{Config: target.Config, Observed: NodeObservedState{ID: target.Config.ID, Status: status}}
	}
	return &controlService{config: config, nodes: nodes}, nil
}

func (*controlService) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandListNodes, commandGetNodeDetail, commandRefreshNode}
}

func (s *controlService) Init(serviceContext gsr.ServiceContext) error {
	s.context = serviceContext
	return nil
}

func (s *controlService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case commandListNodes:
		if _, ok := command.Payload.(listNodesRequest); !ok {
			return commandContext.Reply(nodesResponse{Error: responseInvalidRequest})
		}
		return commandContext.Reply(nodesResponse{Nodes: s.listNodes()})
	case commandGetNodeDetail:
		request, ok := command.Payload.(getNodeDetailRequest)
		if !ok || !validNode(request.Node) {
			return commandContext.Reply(nodeDetailResponse{Error: responseInvalidNode})
		}
		detail, exists := s.nodes[request.Node]
		if !exists {
			return commandContext.Reply(nodeDetailResponse{Error: responseNodeNotFound})
		}
		return commandContext.Reply(nodeDetailResponse{Detail: cloneNodeDetail(detail)})
	case commandRefreshNode:
		request, ok := command.Payload.(refreshNodeRequest)
		if !ok || !validNode(request.Node) {
			return commandContext.Reply(nodeDetailResponse{Error: responseInvalidNode})
		}
		detail, err := s.refreshNode(request.Node)
		if err != nil {
			return commandContext.Reply(nodeDetailResponse{Error: codeFromError(err)})
		}
		return commandContext.Reply(nodeDetailResponse{Detail: cloneNodeDetail(detail)})
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (s *controlService) Stop(context.Context) error {
	s.nodes = make(map[gsr.NodeID]NodeDetail)
	return nil
}

func (s *controlService) Close() error {
	s.context = nil
	s.nodes = nil
	return nil
}

func (s *controlService) listNodes() []NodeDetail {
	nodes := make([]NodeDetail, 0, len(s.nodes))
	for _, detail := range s.nodes {
		nodes = append(nodes, cloneNodeDetail(detail))
	}
	sort.Slice(nodes, func(left, right int) bool { return nodes[left].Config.ID < nodes[right].Config.ID })
	return nodes
}

func (s *controlService) refreshNode(node gsr.NodeID) (NodeDetail, error) {
	detail, exists := s.nodes[node]
	if !exists {
		return NodeDetail{}, ErrNodeNotFound
	}
	if !detail.Config.Enabled {
		return NodeDetail{}, ErrNodeDisabled
	}
	started := s.config.Now()
	callContext, cancel := context.WithTimeout(context.Background(), s.config.CallTimeout)
	value, err := s.context.Call(callContext, s.agentFor(node), commandGetNodeReport, getNodeReportRequest{})
	cancel()
	completed := s.config.Now()
	latency := completed.Sub(started)
	if latency < 0 {
		latency = 0
	}
	if err == nil {
		err = s.applyReport(&detail, value, completed, latency)
	}
	if err != nil {
		detail.Observed = NodeObservedState{ID: node, Status: NodeUnavailable, CapturedAt: completed, Latency: latency, LastError: refreshError(err)}
		detail.Report = NodeDetail{}.Report
		detail.HasReport = false
		s.nodes[node] = detail
		s.recordRefresh(node, latency, false, detail.Observed.Status)
		return detail, nil
	}
	s.nodes[node] = detail
	s.recordRefresh(node, latency, true, detail.Observed.Status)
	return detail, nil
}

func (s *controlService) agentFor(node gsr.NodeID) gsr.ServiceRef {
	for _, target := range s.config.Nodes {
		if target.Config.ID == node {
			return target.Agent
		}
	}
	return gsr.ServiceRef{}
}

func (s *controlService) applyReport(detail *NodeDetail, value any, completed time.Time, latency time.Duration) error {
	response, ok := value.(nodeReportResponse)
	if !ok {
		return ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return err
	}
	if !validNode(response.Report.Node) || response.Report.Node != detail.Config.ID {
		return ErrInvalidResponse
	}
	detail.Observed = NodeObservedState{ID: detail.Config.ID, Status: NodeHealthy, CapturedAt: completed, Latency: latency}
	detail.Report = cloneReport(response.Report)
	detail.HasReport = true
	return nil
}

func (s *controlService) recordRefresh(node gsr.NodeID, latency time.Duration, succeeded bool, status NodeStatus) {
	metrics := s.context.Metrics()
	if succeeded {
		metrics.Inc("control_refresh_succeeded_total")
	} else {
		metrics.Inc("control_refresh_failed_total")
	}
	metrics.SetGauge("control_node_status."+string(node), nodeStatusMetric(status))
	metrics.Observe("control_refresh_latency", latency)
}

func cloneNodeDetails(source []NodeDetail) []NodeDetail {
	copy := make([]NodeDetail, len(source))
	for index, detail := range source {
		copy[index] = cloneNodeDetail(detail)
	}
	return copy
}

func nodeStatusMetric(status NodeStatus) int64 {
	switch status {
	case NodeHealthy:
		return 1
	case NodeUnavailable:
		return 2
	case NodeDisabled:
		return 3
	default:
		return 0
	}
}

func refreshError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, gsr.ErrTimeout):
		return "timeout"
	case errors.Is(err, ErrInvalidResponse):
		return "invalid_response"
	default:
		return "remote_unavailable"
	}
}

func errorFromCode(code errorCode) error {
	switch code {
	case responseOK:
		return nil
	case responseInvalidNode:
		return ErrInvalidNode
	case responseNodeNotFound:
		return ErrNodeNotFound
	case responseNodeDisabled:
		return ErrNodeDisabled
	case responseUnauthorized:
		return ErrUnauthorized
	case responseInvalidPrincipal:
		return ErrInvalidPrincipal
	case responseInvalidRequestID:
		return ErrInvalidRequestID
	case responseInvalidDrainRequest:
		return ErrInvalidDrainRequest
	case responseRequestConflict:
		return ErrRequestConflict
	case responseOperationNotFound:
		return ErrDrainOperationNotFound
	case responseOperationOwnerMismatch:
		return ErrOperationOwnerMismatch
	case responseInvalidStopRequest:
		return ErrInvalidStopRequest
	case responseStopOperationNotFound:
		return ErrStopOperationNotFound
	case responseStopDisabled:
		return ErrStopDisabled
	case responseStopRequestConflict:
		return ErrStopRequestConflict
	case responseStopNotReady:
		return ErrStopNotReady
	case responseStopTargetMismatch:
		return ErrStopTargetMismatch
	case responseInvalidRequest:
		return ErrInvalidResponse
	default:
		return ErrInvalidResponse
	}
}

func codeFromError(err error) errorCode {
	switch {
	case err == nil:
		return responseOK
	case errors.Is(err, ErrInvalidNode):
		return responseInvalidNode
	case errors.Is(err, ErrNodeNotFound):
		return responseNodeNotFound
	case errors.Is(err, ErrNodeDisabled):
		return responseNodeDisabled
	case errors.Is(err, ErrUnauthorized):
		return responseUnauthorized
	case errors.Is(err, ErrInvalidPrincipal):
		return responseInvalidPrincipal
	case errors.Is(err, ErrInvalidRequestID):
		return responseInvalidRequestID
	case errors.Is(err, ErrInvalidDrainRequest):
		return responseInvalidDrainRequest
	case errors.Is(err, ErrRequestConflict):
		return responseRequestConflict
	case errors.Is(err, ErrDrainOperationNotFound):
		return responseOperationNotFound
	case errors.Is(err, ErrOperationOwnerMismatch):
		return responseOperationOwnerMismatch
	case errors.Is(err, ErrInvalidStopRequest):
		return responseInvalidStopRequest
	case errors.Is(err, ErrStopOperationNotFound):
		return responseStopOperationNotFound
	case errors.Is(err, ErrStopDisabled):
		return responseStopDisabled
	case errors.Is(err, ErrStopRequestConflict):
		return responseStopRequestConflict
	case errors.Is(err, ErrStopNotReady):
		return responseStopNotReady
	case errors.Is(err, ErrStopTargetMismatch):
		return responseStopTargetMismatch
	default:
		return responseInvalidRequest
	}
}
