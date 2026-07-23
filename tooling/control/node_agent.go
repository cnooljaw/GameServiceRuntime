package control

import (
	"context"
	"errors"
	"strings"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
)

const defaultHeartbeatInterval = 10 * time.Second

type nodeLeaseClient interface {
	RegisterNode(context.Context, gsr.NodeID, string) (discovery.NodeLease, error)
	Heartbeat(context.Context, discovery.NodeLease) (discovery.NodeLease, error)
	UnregisterNode(context.Context, discovery.NodeLease) error
}

type nodeAgent struct {
	config           NodeAgentConfig
	context          gsr.ServiceContext
	leaseClient      nodeLeaseClient
	newLeaseClient   func(gsr.ServiceContext, gsr.ServiceRef) (nodeLeaseClient, error)
	lease            discovery.NodeLease
	hasLease         bool
	stopReceipts     map[nodeStopKey]nodeStopRecord
	recoveryReceipts map[nodeRecoveryKey]nodeRecoveryRecord
}

type nodeStopKey struct {
	requestID RequestID
	target    gsr.ServiceRef
}

type nodeStopRecord struct {
	task    NodeStopTask
	receipt NodeStopReceipt
}

type nodeRecoveryKey struct {
	requestID RequestID
	removed   gsr.ServiceRef
}

type nodeRecoveryRecord struct {
	task    RecoveryCreateTask
	receipt RecoveryReceipt
}

// NewNodeAgentService creates a NodeAgentService that maintains its local Discovery lease.
func NewNodeAgentService(config NodeAgentConfig) (gsr.Service, error) {
	if !validNodeAgentConfig(config) {
		return nil, ErrInvalidConfig
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = defaultHeartbeatInterval
	}
	if config.CallTimeout == 0 {
		config.CallTimeout = defaultCallTimeout
	}
	return &nodeAgent{config: config, newLeaseClient: newNodeLeaseClient, stopReceipts: make(map[nodeStopKey]nodeStopRecord), recoveryReceipts: make(map[nodeRecoveryKey]nodeRecoveryRecord)}, nil
}

func (*nodeAgent) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandGetNodeReport, commandBeginNodeStop, commandGetNodeStopReceipt, commandBeginRecoveryCreate, commandGetRecoveryReceipt, commandRecordRecoveryCreate, commandRecordNodeStopResult, commandRegisterNodeLease, commandHeartbeatNodeLease}
}

// StartupCommand starts registration only after Runtime has entered Running.
func (*nodeAgent) StartupCommand() (gsr.Command, bool) {
	return gsr.Command{ID: commandRegisterNodeLease}, true
}

func (a *nodeAgent) Init(serviceContext gsr.ServiceContext) error {
	client, err := a.newLeaseClient(serviceContext, a.config.Discovery)
	if err != nil {
		return err
	}
	a.context = serviceContext
	a.leaseClient = client
	return nil
}

func (a *nodeAgent) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case commandGetNodeReport:
		return a.handleGetNodeReport(commandContext, command)
	case commandBeginNodeStop:
		return a.handleBeginNodeStop(commandContext, command)
	case commandGetNodeStopReceipt:
		return a.handleGetNodeStopReceipt(commandContext, command)
	case commandRecordNodeStopResult:
		return a.handleNodeStopResult(commandContext, command)
	case commandBeginRecoveryCreate:
		return a.handleBeginRecoveryCreate(commandContext, command)
	case commandGetRecoveryReceipt:
		return a.handleGetRecoveryReceipt(commandContext, command)
	case commandRecordRecoveryCreate:
		return a.handleRecoveryCreateResult(commandContext, command)
	case commandRegisterNodeLease:
		if command.Payload != nil || !a.allowedLeaseCommandSource(commandContext.Source()) {
			return ErrUnauthorized
		}
		return a.registerLease()
	case commandHeartbeatNodeLease:
		if command.Payload != nil || !a.runtimeNodeSource(commandContext.Source()) {
			return ErrUnauthorized
		}
		return a.heartbeatLease()
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (a *nodeAgent) allowedLeaseCommandSource(source gsr.ServiceRef) bool {
	return source == a.context.Self() || a.runtimeNodeSource(source)
}

func (a *nodeAgent) runtimeNodeSource(source gsr.ServiceRef) bool {
	return source.Node == a.context.Self().Node && source.ID == 0
}

func (a *nodeAgent) handleGetNodeReport(commandContext gsr.CommandContext, command gsr.Command) error {
	if _, ok := command.Payload.(getNodeReportRequest); !ok {
		return commandContext.Reply(nodeReportResponse{Error: responseInvalidRequest})
	}
	source := commandContext.Source()
	if source.Node != a.config.ObserverNode || source.ID == 0 {
		return commandContext.Reply(nodeReportResponse{Error: responseUnauthorized})
	}
	return commandContext.Reply(nodeReportResponse{Report: cloneReport(a.config.Reporter.Capture())})
}

func (a *nodeAgent) handleBeginNodeStop(commandContext gsr.CommandContext, command gsr.Command) error {
	if !a.stopEnabled() {
		return a.replyNodeStopReceipt(commandContext, NodeStopReceipt{}, ErrStopDisabled)
	}
	if commandContext.Source() != a.config.StopCoordinator {
		return a.replyNodeStopReceipt(commandContext, NodeStopReceipt{}, ErrUnauthorized)
	}
	request, ok := command.Payload.(beginNodeStopRequest)
	if !ok || !a.validNodeStopTask(request.Task) {
		return a.replyNodeStopReceipt(commandContext, NodeStopReceipt{}, ErrInvalidStopRequest)
	}
	key := nodeStopKey{requestID: request.Task.RequestID, target: request.Task.Target}
	if record, exists := a.stopReceipts[key]; exists {
		if !sameNodeStopTask(record.task, request.Task) {
			return a.replyNodeStopReceipt(commandContext, NodeStopReceipt{}, ErrInvalidStopRequest)
		}
		if record.receipt.State != StopTargetPending {
			return a.replyNodeStopReceipt(commandContext, record.receipt, nil)
		}
	}
	return a.submitNodeStop(commandContext, key, request.Task)
}

func (a *nodeAgent) handleGetNodeStopReceipt(commandContext gsr.CommandContext, command gsr.Command) error {
	if !a.stopEnabled() {
		return a.replyNodeStopReceipt(commandContext, NodeStopReceipt{}, ErrStopDisabled)
	}
	if commandContext.Source() != a.config.StopCoordinator {
		return a.replyNodeStopReceipt(commandContext, NodeStopReceipt{}, ErrUnauthorized)
	}
	request, ok := command.Payload.(getNodeStopReceiptRequest)
	if !ok || !validRequestID(request.RequestID) || !validServiceRef(request.Target) || request.Target.Node != a.context.Self().Node || request.Target == a.context.Self() {
		return a.replyNodeStopReceipt(commandContext, NodeStopReceipt{}, ErrInvalidStopRequest)
	}
	record, exists := a.stopReceipts[nodeStopKey{requestID: request.RequestID, target: request.Target}]
	if !exists {
		return a.replyNodeStopReceipt(commandContext, NodeStopReceipt{}, ErrStopOperationNotFound)
	}
	return a.replyNodeStopReceipt(commandContext, record.receipt, nil)
}

func (a *nodeAgent) handleNodeStopResult(commandContext gsr.CommandContext, command gsr.Command) error {
	if !a.stopEnabled() || !a.runtimeNodeSource(commandContext.Source()) {
		return ErrUnauthorized
	}
	result, ok := command.Payload.(nodeStopResult)
	if !ok || !validNodeStopResult(result) {
		return ErrInvalidStopRequest
	}
	key := nodeStopKey{requestID: result.RequestID, target: result.Target}
	record, exists := a.stopReceipts[key]
	if !exists || record.receipt.State != StopTargetQueued {
		return ErrInvalidStopRequest
	}
	record.receipt.State = result.State
	record.receipt.Failure = result.Failure
	record.receipt.UpdatedAt = a.context.Now()
	a.stopReceipts[key] = record
	switch result.State {
	case StopTargetStopped:
		a.context.Metrics().Inc("node_stop_completed_total")
	case StopTargetSuperseded:
		a.context.Metrics().Inc("node_stop_superseded_total")
	case StopTargetFailed:
		a.context.Metrics().Inc("node_stop_failed_total")
	}
	return nil
}

func (a *nodeAgent) submitNodeStop(commandContext gsr.CommandContext, key nodeStopKey, task NodeStopTask) error {
	now := a.context.Now()
	receipt := NodeStopReceipt{RequestID: task.RequestID, Target: task.Target, State: StopTargetQueued, UpdatedAt: now}
	err := a.config.StopExecutor.Submit(task)
	switch {
	case err == nil:
		a.stopReceipts[key] = nodeStopRecord{task: cloneNodeStopTask(task), receipt: receipt}
		a.context.Metrics().Inc("node_stop_queued_total")
	case errors.Is(err, ErrNodeStopQueueFull):
		receipt.State = StopTargetPending
		receipt.Failure = StopFailureQueueFull
		a.stopReceipts[key] = nodeStopRecord{task: cloneNodeStopTask(task), receipt: receipt}
	case errors.Is(err, ErrNodeStopRunnerClosed):
		receipt.State = StopTargetFailed
		receipt.Failure = StopFailureRunnerClosed
		a.stopReceipts[key] = nodeStopRecord{task: cloneNodeStopTask(task), receipt: receipt}
		a.context.Metrics().Inc("node_stop_failed_total")
	default:
		return a.replyNodeStopReceipt(commandContext, NodeStopReceipt{}, ErrInvalidResponse)
	}
	return a.replyNodeStopReceipt(commandContext, receipt, nil)
}

func (a *nodeAgent) stopEnabled() bool {
	return validServiceRef(a.config.StopCoordinator) && !isNil(a.config.StopExecutor)
}

func (a *nodeAgent) handleBeginRecoveryCreate(commandContext gsr.CommandContext, command gsr.Command) error {
	if !a.recoveryEnabled() {
		return a.replyRecoveryReceipt(commandContext, RecoveryReceipt{}, ErrRecoveryNotReady)
	}
	if commandContext.Source() != a.config.RecoveryCoordinator {
		return a.replyRecoveryReceipt(commandContext, RecoveryReceipt{}, ErrUnauthorized)
	}
	request, ok := command.Payload.(beginRecoveryCreateRequest)
	if !ok || !a.validRecoveryTask(request.Task) {
		return a.replyRecoveryReceipt(commandContext, RecoveryReceipt{}, ErrInvalidRecoveryRequest)
	}
	key := nodeRecoveryKey{requestID: request.Task.RequestID, removed: request.Task.Removed}
	if record, exists := a.recoveryReceipts[key]; exists {
		if !sameRecoveryCreateTask(record.task, request.Task) {
			return a.replyRecoveryReceipt(commandContext, RecoveryReceipt{}, ErrInvalidRecoveryRequest)
		}
		if record.receipt.State != RecoveryTargetFailed || record.receipt.Failure != RecoveryFailureQueueFull {
			return a.replyRecoveryReceipt(commandContext, record.receipt, nil)
		}
	}
	return a.submitRecovery(commandContext, key, request.Task)
}

func (a *nodeAgent) handleGetRecoveryReceipt(commandContext gsr.CommandContext, command gsr.Command) error {
	if !a.recoveryEnabled() {
		return a.replyRecoveryReceipt(commandContext, RecoveryReceipt{}, ErrRecoveryNotReady)
	}
	if commandContext.Source() != a.config.RecoveryCoordinator {
		return a.replyRecoveryReceipt(commandContext, RecoveryReceipt{}, ErrUnauthorized)
	}
	request, ok := command.Payload.(getRecoveryReceiptRequest)
	if !ok || !validRequestID(request.RequestID) || !validServiceRef(request.Removed) || request.Removed.Node != a.context.Self().Node || request.Removed == a.context.Self() {
		return a.replyRecoveryReceipt(commandContext, RecoveryReceipt{}, ErrInvalidRecoveryRequest)
	}
	record, exists := a.recoveryReceipts[nodeRecoveryKey{requestID: request.RequestID, removed: request.Removed}]
	if !exists {
		return a.replyRecoveryReceipt(commandContext, RecoveryReceipt{}, ErrRecoveryOperationNotFound)
	}
	return a.replyRecoveryReceipt(commandContext, record.receipt, nil)
}

func (a *nodeAgent) handleRecoveryCreateResult(commandContext gsr.CommandContext, command gsr.Command) error {
	if !a.recoveryEnabled() || !a.runtimeNodeSource(commandContext.Source()) {
		return ErrUnauthorized
	}
	result, ok := command.Payload.(recoveryCreateResult)
	if !ok || !validRecoveryCreateResult(result) || (result.Created != (gsr.ServiceRef{}) && result.Created.Node != a.context.Self().Node) || result.Removed.Node != a.context.Self().Node {
		return ErrInvalidRecoveryRequest
	}
	key := nodeRecoveryKey{requestID: result.RequestID, removed: result.Removed}
	record, exists := a.recoveryReceipts[key]
	if !exists || record.receipt.State != RecoveryTargetCreating || record.task.Blueprint != result.Blueprint {
		return ErrInvalidRecoveryRequest
	}
	record.receipt.Created = result.Created
	record.receipt.State = result.State
	record.receipt.Failure = result.Failure
	record.receipt.UpdatedAt = a.context.Now()
	a.recoveryReceipts[key] = record
	switch result.State {
	case RecoveryTargetCreated:
		a.context.Metrics().Inc("node_recovery_created_total")
	case RecoveryTargetFailed:
		a.context.Metrics().Inc("node_recovery_failed_total")
	}
	return nil
}

func (a *nodeAgent) submitRecovery(commandContext gsr.CommandContext, key nodeRecoveryKey, task RecoveryCreateTask) error {
	now := a.context.Now()
	receipt := RecoveryReceipt{RequestID: task.RequestID, Removed: task.Removed, Blueprint: task.Blueprint, State: RecoveryTargetCreating, UpdatedAt: now}
	err := a.config.RecoveryExecutor.Submit(task)
	switch {
	case err == nil:
		a.recoveryReceipts[key] = nodeRecoveryRecord{task: task, receipt: receipt}
		a.context.Metrics().Inc("node_recovery_queued_total")
	case errors.Is(err, ErrRecoveryQueueFull):
		receipt.State = RecoveryTargetFailed
		receipt.Failure = RecoveryFailureQueueFull
		a.recoveryReceipts[key] = nodeRecoveryRecord{task: task, receipt: receipt}
	case errors.Is(err, ErrRecoveryRunnerClosed):
		receipt.State = RecoveryTargetFailed
		receipt.Failure = RecoveryFailureRunnerClosed
		a.recoveryReceipts[key] = nodeRecoveryRecord{task: task, receipt: receipt}
		a.context.Metrics().Inc("node_recovery_failed_total")
	default:
		return a.replyRecoveryReceipt(commandContext, RecoveryReceipt{}, ErrInvalidResponse)
	}
	return a.replyRecoveryReceipt(commandContext, receipt, nil)
}

func (a *nodeAgent) recoveryEnabled() bool {
	return validServiceRef(a.config.RecoveryCoordinator) && !isNil(a.config.RecoveryExecutor)
}

func (a *nodeAgent) validRecoveryTask(task RecoveryCreateTask) bool {
	return validRecoveryCreateTask(task) && task.Agent == a.context.Self()
}

func (a *nodeAgent) replyRecoveryReceipt(commandContext gsr.CommandContext, receipt RecoveryReceipt, err error) error {
	return commandContext.Reply(recoveryReceiptResponse{Receipt: receipt, Error: codeFromError(err)})
}

func (a *nodeAgent) validNodeStopTask(task NodeStopTask) bool {
	return validNodeStopTask(task) && task.Agent == a.context.Self()
}

func (a *nodeAgent) replyNodeStopReceipt(commandContext gsr.CommandContext, receipt NodeStopReceipt, err error) error {
	return commandContext.Reply(nodeStopReceiptResponse{Receipt: receipt, Error: codeFromError(err)})
}

func (a *nodeAgent) registerLease() error {
	callContext, cancel := context.WithTimeout(context.Background(), a.config.CallTimeout)
	lease, err := a.leaseClient.RegisterNode(callContext, a.context.Self().Node, a.config.Address)
	cancel()
	if err == nil {
		a.lease = lease
		a.hasLease = true
		return a.schedule(commandHeartbeatNodeLease)
	}
	return a.schedule(commandRegisterNodeLease)
}

func (a *nodeAgent) heartbeatLease() error {
	if !a.hasLease {
		return a.registerLease()
	}
	callContext, cancel := context.WithTimeout(context.Background(), a.config.CallTimeout)
	lease, err := a.leaseClient.Heartbeat(callContext, a.lease)
	cancel()
	if err == nil {
		a.lease = lease
		return a.schedule(commandHeartbeatNodeLease)
	}
	if errors.Is(err, discovery.ErrLeaseExpired) {
		return a.registerLease()
	}
	return a.schedule(commandHeartbeatNodeLease)
}

func (a *nodeAgent) schedule(command gsr.CommandID) error {
	_, err := a.context.After(a.config.HeartbeatInterval, command, nil)
	return err
}

func (a *nodeAgent) Stop(stopContext context.Context) error {
	a.stopReceipts = nil
	a.recoveryReceipts = nil
	if !a.hasLease || a.leaseClient == nil {
		return nil
	}
	lease := a.lease
	a.hasLease = false
	callContext, cancel := context.WithTimeout(stopContext, a.config.CallTimeout)
	err := a.leaseClient.UnregisterNode(callContext, lease)
	cancel()
	if errors.Is(err, discovery.ErrLeaseExpired) {
		return nil
	}
	return nil
}

func (a *nodeAgent) Close() error {
	a.context = nil
	a.leaseClient = nil
	a.lease = discovery.NodeLease{}
	a.hasLease = false
	a.recoveryReceipts = nil
	return nil
}

func newNodeLeaseClient(caller gsr.ServiceContext, target gsr.ServiceRef) (nodeLeaseClient, error) {
	return discovery.NewClient(caller, target)
}

func validNodeAgentConfig(config NodeAgentConfig) bool {
	stopConfigured := config.StopCoordinator != (gsr.ServiceRef{}) || !isNil(config.StopExecutor)
	recoveryConfigured := config.RecoveryCoordinator != (gsr.ServiceRef{}) || !isNil(config.RecoveryExecutor)
	return !isNil(config.Reporter) && validNode(config.ObserverNode) && validServiceRef(config.Discovery) && strings.TrimSpace(config.Address) != "" && config.HeartbeatInterval >= 0 && config.CallTimeout >= 0 && (!stopConfigured || validServiceRef(config.StopCoordinator) && !isNil(config.StopExecutor)) && (!recoveryConfigured || validServiceRef(config.RecoveryCoordinator) && !isNil(config.RecoveryExecutor))
}

var _ gsr.Service = (*nodeAgent)(nil)
var _ gsr.StartupCommandDeclarer = (*nodeAgent)(nil)
