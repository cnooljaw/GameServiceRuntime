package control

import (
	"context"
	"errors"
	"fmt"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/drain"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

const defaultDrainAuditLimit = 1024

type drainOperationRecord struct {
	request   StartDrainRequest
	operation DrainOperation
}

type stopOperationRecord struct {
	request   BeginStopRequest
	operation StopOperation
}

type recoveryOperationRecord struct {
	request   BeginRecoveryRequest
	operation RecoveryOperation
}

type drainCoordinatorService struct {
	config     DrainCoordinatorConfig
	context    gsr.ServiceContext
	directory  *servicegroup.Client
	visitors   *drain.Client
	operations map[RequestID]drainOperationRecord
	stops      map[RequestID]stopOperationRecord
	recoveries map[RequestID]recoveryOperationRecord
	audits     []DrainAudit
	sequence   uint64
}

// NewDrainCoordinatorService creates a Mailbox-owned, Gateway-authorized Drain operation coordinator.
func NewDrainCoordinatorService(config DrainCoordinatorConfig) (gsr.Service, error) {
	if !validDrainCoordinatorConfig(config) {
		return nil, ErrInvalidConfig
	}
	if config.CallTimeout == 0 {
		config.CallTimeout = defaultCallTimeout
	}
	if config.AuditLimit == 0 {
		config.AuditLimit = defaultDrainAuditLimit
	}
	config.AllowedPrincipals = append([]Principal(nil), config.AllowedPrincipals...)
	return &drainCoordinatorService{config: config, operations: make(map[RequestID]drainOperationRecord), stops: make(map[RequestID]stopOperationRecord), recoveries: make(map[RequestID]recoveryOperationRecord)}, nil
}

func (*drainCoordinatorService) Commands() []gsr.CommandID {
	return []gsr.CommandID{
		commandStartDrainOperation,
		commandResolveDrainOperation,
		commandGetDrainOperation,
		commandListDrainAudit,
		commandBeginDrainStop,
		commandResolveDrainStop,
		commandGetDrainStop,
		commandBeginRecovery,
		commandConfirmRecovery,
		commandResolveRecovery,
		commandGetRecovery,
		commandAbandonRecovery,
	}
}

func (s *drainCoordinatorService) Init(serviceContext gsr.ServiceContext) error {
	directory, err := servicegroup.NewClient(serviceContext, s.config.Directory)
	if err != nil {
		return fmt.Errorf("control: create Directory client: %w", err)
	}
	visitors, err := drain.NewClient(serviceContext, s.config.VisitorRegistry)
	if err != nil {
		return fmt.Errorf("control: create Visitor client: %w", err)
	}
	s.context = serviceContext
	s.directory = directory
	s.visitors = visitors
	return nil
}

func (s *drainCoordinatorService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case commandStartDrainOperation:
		request, ok := command.Payload.(startDrainOperationRequest)
		if !ok {
			return s.replyOperation(commandContext, DrainOperation{}, ErrInvalidDrainRequest)
		}
		return s.handleStart(commandContext, request.Request)
	case commandResolveDrainOperation:
		request, ok := command.Payload.(resolveDrainOperationRequest)
		if !ok {
			return s.replyOperation(commandContext, DrainOperation{}, ErrInvalidDrainRequest)
		}
		return s.handleResolve(commandContext, request.RequestID, request.Principal)
	case commandGetDrainOperation:
		request, ok := command.Payload.(getDrainOperationRequest)
		if !ok {
			return s.replyOperation(commandContext, DrainOperation{}, ErrInvalidDrainRequest)
		}
		return s.handleGet(commandContext, request.RequestID, request.Principal)
	case commandListDrainAudit:
		request, ok := command.Payload.(listDrainAuditRequest)
		if !ok {
			return s.replyAudits(commandContext, nil, ErrInvalidDrainRequest)
		}
		return s.handleListAudit(commandContext, request.Principal)
	case commandBeginDrainStop:
		request, ok := command.Payload.(beginDrainStopRequest)
		if !ok {
			return s.replyStopOperation(commandContext, StopOperation{}, ErrInvalidStopRequest)
		}
		return s.handleBeginStop(commandContext, request.Request)
	case commandResolveDrainStop:
		request, ok := command.Payload.(resolveDrainStopRequest)
		if !ok {
			return s.replyStopOperation(commandContext, StopOperation{}, ErrInvalidStopRequest)
		}
		return s.handleResolveStop(commandContext, request.RequestID, request.Principal)
	case commandGetDrainStop:
		request, ok := command.Payload.(getDrainStopRequest)
		if !ok {
			return s.replyStopOperation(commandContext, StopOperation{}, ErrInvalidStopRequest)
		}
		return s.handleGetStop(commandContext, request.RequestID, request.Principal)
	case commandBeginRecovery:
		request, ok := command.Payload.(beginRecoveryRequest)
		if !ok {
			return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrInvalidRecoveryRequest)
		}
		return s.handleBeginRecovery(commandContext, request.Request)
	case commandConfirmRecovery:
		request, ok := command.Payload.(recoveryOperationRequest)
		if !ok {
			return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrInvalidRecoveryRequest)
		}
		return s.handleConfirmRecovery(commandContext, request.RequestID, request.Principal)
	case commandResolveRecovery:
		request, ok := command.Payload.(recoveryOperationRequest)
		if !ok {
			return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrInvalidRecoveryRequest)
		}
		return s.handleResolveRecovery(commandContext, request.RequestID, request.Principal)
	case commandGetRecovery:
		request, ok := command.Payload.(recoveryOperationRequest)
		if !ok {
			return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrInvalidRecoveryRequest)
		}
		return s.handleGetRecovery(commandContext, request.RequestID, request.Principal)
	case commandAbandonRecovery:
		request, ok := command.Payload.(recoveryOperationRequest)
		if !ok {
			return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrInvalidRecoveryRequest)
		}
		return s.handleAbandonRecovery(commandContext, request.RequestID, request.Principal)
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (s *drainCoordinatorService) Stop(context.Context) error {
	s.operations = make(map[RequestID]drainOperationRecord)
	s.stops = make(map[RequestID]stopOperationRecord)
	s.recoveries = make(map[RequestID]recoveryOperationRecord)
	s.audits = nil
	s.sequence = 0
	return nil
}

func (s *drainCoordinatorService) Close() error {
	s.context = nil
	s.directory = nil
	s.visitors = nil
	s.operations = nil
	s.stops = nil
	s.recoveries = nil
	s.audits = nil
	s.sequence = 0
	return nil
}

func (s *drainCoordinatorService) handleStart(commandContext gsr.CommandContext, request StartDrainRequest) error {
	if !s.gatewaySource(commandContext.Source()) {
		return s.replyOperation(commandContext, DrainOperation{}, ErrUnauthorized)
	}
	normalized, err := normalizeStartDrainRequest(request)
	if err != nil {
		return s.replyOperation(commandContext, DrainOperation{}, err)
	}
	if !s.allowed(normalized.Principal) {
		s.appendAudit(normalized.RequestID, normalized.Principal, "start", "denied")
		s.context.Metrics().Inc("drain_operations_denied_total")
		return s.replyOperation(commandContext, DrainOperation{}, ErrUnauthorized)
	}
	if existing, exists := s.operations[normalized.RequestID]; exists {
		if !sameStartDrainRequest(existing.request, normalized) {
			s.context.Metrics().Inc("drain_operations_conflict_total")
			return s.replyOperation(commandContext, DrainOperation{}, ErrRequestConflict)
		}
		s.appendAudit(normalized.RequestID, normalized.Principal, "start", "duplicate")
		s.context.Metrics().Inc("drain_operations_duplicate_total")
		return s.replyOperation(commandContext, existing.operation, nil)
	}
	now := s.context.Now()
	record := drainOperationRecord{
		request: normalized,
		operation: DrainOperation{
			RequestID: normalized.RequestID,
			Principal: normalized.Principal,
			Group:     normalized.Group,
			Expected:  normalized.Expected,
			Phase:     DrainPreparing,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	s.operations[normalized.RequestID] = record
	s.advance(&record)
	s.operations[normalized.RequestID] = record
	s.appendAudit(normalized.RequestID, normalized.Principal, "start", string(record.operation.Phase))
	s.context.Metrics().Inc("drain_operations_started_total")
	return s.replyOperation(commandContext, record.operation, nil)
}

func (s *drainCoordinatorService) handleResolve(commandContext gsr.CommandContext, requestID RequestID, principal Principal) error {
	if !s.gatewaySource(commandContext.Source()) {
		return s.replyOperation(commandContext, DrainOperation{}, ErrUnauthorized)
	}
	if !validRequestID(requestID) {
		return s.replyOperation(commandContext, DrainOperation{}, ErrInvalidRequestID)
	}
	if !validPrincipal(principal) {
		return s.replyOperation(commandContext, DrainOperation{}, ErrInvalidPrincipal)
	}
	if !s.allowed(principal) {
		s.appendAudit(requestID, principal, "resolve", "denied")
		s.context.Metrics().Inc("drain_operations_denied_total")
		return s.replyOperation(commandContext, DrainOperation{}, ErrUnauthorized)
	}
	record, exists := s.operations[requestID]
	if !exists {
		return s.replyOperation(commandContext, DrainOperation{}, ErrDrainOperationNotFound)
	}
	if record.operation.Principal != principal {
		return s.replyOperation(commandContext, DrainOperation{}, ErrOperationOwnerMismatch)
	}
	s.advance(&record)
	s.operations[requestID] = record
	s.appendAudit(requestID, principal, "resolve", string(record.operation.Phase))
	return s.replyOperation(commandContext, record.operation, nil)
}

func (s *drainCoordinatorService) handleGet(commandContext gsr.CommandContext, requestID RequestID, principal Principal) error {
	if !s.gatewaySource(commandContext.Source()) {
		return s.replyOperation(commandContext, DrainOperation{}, ErrUnauthorized)
	}
	if !validRequestID(requestID) {
		return s.replyOperation(commandContext, DrainOperation{}, ErrInvalidRequestID)
	}
	if !validPrincipal(principal) {
		return s.replyOperation(commandContext, DrainOperation{}, ErrInvalidPrincipal)
	}
	if !s.allowed(principal) {
		s.appendAudit(requestID, principal, "get", "denied")
		s.context.Metrics().Inc("drain_operations_denied_total")
		return s.replyOperation(commandContext, DrainOperation{}, ErrUnauthorized)
	}
	record, exists := s.operations[requestID]
	if !exists {
		return s.replyOperation(commandContext, DrainOperation{}, ErrDrainOperationNotFound)
	}
	if record.operation.Principal != principal {
		return s.replyOperation(commandContext, DrainOperation{}, ErrOperationOwnerMismatch)
	}
	return s.replyOperation(commandContext, record.operation, nil)
}

func (s *drainCoordinatorService) handleListAudit(commandContext gsr.CommandContext, principal Principal) error {
	if !s.gatewaySource(commandContext.Source()) {
		return s.replyAudits(commandContext, nil, ErrUnauthorized)
	}
	if !validPrincipal(principal) {
		return s.replyAudits(commandContext, nil, ErrInvalidPrincipal)
	}
	if !s.allowed(principal) {
		s.appendAudit("denied", principal, "list_audit", "denied")
		s.context.Metrics().Inc("drain_operations_denied_total")
		return s.replyAudits(commandContext, nil, ErrUnauthorized)
	}
	return s.replyAudits(commandContext, s.audits, nil)
}

func (s *drainCoordinatorService) handleBeginStop(commandContext gsr.CommandContext, request BeginStopRequest) error {
	if !s.gatewaySource(commandContext.Source()) {
		return s.replyStopOperation(commandContext, StopOperation{}, ErrUnauthorized)
	}
	normalized, err := normalizeBeginStopRequest(request)
	if err != nil {
		return s.replyStopOperation(commandContext, StopOperation{}, err)
	}
	if !s.allowed(normalized.Principal) {
		s.appendAudit(normalized.RequestID, normalized.Principal, "begin_stop", "denied")
		s.context.Metrics().Inc("drain_stop_operations_denied_total")
		return s.replyStopOperation(commandContext, StopOperation{}, ErrUnauthorized)
	}
	if existing, exists := s.stops[normalized.RequestID]; exists {
		if !sameBeginStopRequest(existing.request, normalized) {
			s.appendAudit(normalized.RequestID, normalized.Principal, "begin_stop", "conflict")
			return s.replyStopOperation(commandContext, StopOperation{}, ErrStopRequestConflict)
		}
		s.appendAudit(normalized.RequestID, normalized.Principal, "begin_stop", "duplicate")
		s.context.Metrics().Inc("drain_stop_operations_duplicate_total")
		return s.replyStopOperation(commandContext, existing.operation, nil)
	}
	drainRecord, exists := s.operations[normalized.RequestID]
	if !exists || drainRecord.operation.Phase != DrainReadyToStop {
		s.appendAudit(normalized.RequestID, normalized.Principal, "begin_stop", "not_ready")
		return s.replyStopOperation(commandContext, StopOperation{}, ErrStopNotReady)
	}
	if drainRecord.operation.Principal != normalized.Principal {
		s.appendAudit(normalized.RequestID, normalized.Principal, "begin_stop", "owner_mismatch")
		return s.replyStopOperation(commandContext, StopOperation{}, ErrOperationOwnerMismatch)
	}
	if !matchesDrainStopTargets(normalized.Targets, drainRecord.operation.Targets) {
		s.appendAudit(normalized.RequestID, normalized.Principal, "begin_stop", "target_mismatch")
		return s.replyStopOperation(commandContext, StopOperation{}, ErrStopTargetMismatch)
	}
	matches, err := s.matchesStopDirectory(drainRecord.operation.Published)
	if err != nil {
		s.appendAudit(normalized.RequestID, normalized.Principal, "begin_stop", "directory_unavailable")
		return s.replyStopOperation(commandContext, StopOperation{}, ErrInvalidResponse)
	}
	if !matches {
		s.appendAudit(normalized.RequestID, normalized.Principal, "begin_stop", "superseded")
		return s.replyStopOperation(commandContext, StopOperation{}, ErrStopNotReady)
	}
	now := s.context.Now()
	record := stopOperationRecord{
		request: normalized,
		operation: StopOperation{
			RequestID: normalized.RequestID,
			Principal: normalized.Principal,
			Group:     drainRecord.operation.Group,
			Published: cloneDrainServiceSet(drainRecord.operation.Published),
			Targets:   stopTargetsFromRequest(normalized.Targets),
			Phase:     StopDispatching,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	s.advanceStop(&record)
	s.stops[normalized.RequestID] = record
	s.appendAudit(normalized.RequestID, normalized.Principal, "begin_stop", string(record.operation.Phase))
	s.context.Metrics().Inc("drain_stop_operations_started_total")
	return s.replyStopOperation(commandContext, record.operation, nil)
}

func (s *drainCoordinatorService) handleResolveStop(commandContext gsr.CommandContext, requestID RequestID, principal Principal) error {
	if !s.gatewaySource(commandContext.Source()) {
		return s.replyStopOperation(commandContext, StopOperation{}, ErrUnauthorized)
	}
	if !validRequestID(requestID) {
		return s.replyStopOperation(commandContext, StopOperation{}, ErrInvalidRequestID)
	}
	if !validPrincipal(principal) {
		return s.replyStopOperation(commandContext, StopOperation{}, ErrInvalidPrincipal)
	}
	if !s.allowed(principal) {
		s.appendAudit(requestID, principal, "resolve_stop", "denied")
		s.context.Metrics().Inc("drain_stop_operations_denied_total")
		return s.replyStopOperation(commandContext, StopOperation{}, ErrUnauthorized)
	}
	record, exists := s.stops[requestID]
	if !exists {
		s.appendAudit(requestID, principal, "resolve_stop", "not_found")
		return s.replyStopOperation(commandContext, StopOperation{}, ErrStopOperationNotFound)
	}
	if record.operation.Principal != principal {
		s.appendAudit(requestID, principal, "resolve_stop", "owner_mismatch")
		return s.replyStopOperation(commandContext, StopOperation{}, ErrOperationOwnerMismatch)
	}
	if !stopTerminal(record.operation.Phase) {
		s.advanceStop(&record)
		s.stops[requestID] = record
	}
	s.appendAudit(requestID, principal, "resolve_stop", string(record.operation.Phase))
	return s.replyStopOperation(commandContext, record.operation, nil)
}

func (s *drainCoordinatorService) handleGetStop(commandContext gsr.CommandContext, requestID RequestID, principal Principal) error {
	if !s.gatewaySource(commandContext.Source()) {
		return s.replyStopOperation(commandContext, StopOperation{}, ErrUnauthorized)
	}
	if !validRequestID(requestID) {
		return s.replyStopOperation(commandContext, StopOperation{}, ErrInvalidRequestID)
	}
	if !validPrincipal(principal) {
		return s.replyStopOperation(commandContext, StopOperation{}, ErrInvalidPrincipal)
	}
	if !s.allowed(principal) {
		s.appendAudit(requestID, principal, "get_stop", "denied")
		s.context.Metrics().Inc("drain_stop_operations_denied_total")
		return s.replyStopOperation(commandContext, StopOperation{}, ErrUnauthorized)
	}
	record, exists := s.stops[requestID]
	if !exists {
		return s.replyStopOperation(commandContext, StopOperation{}, ErrStopOperationNotFound)
	}
	if record.operation.Principal != principal {
		return s.replyStopOperation(commandContext, StopOperation{}, ErrOperationOwnerMismatch)
	}
	return s.replyStopOperation(commandContext, record.operation, nil)
}

func (s *drainCoordinatorService) handleBeginRecovery(commandContext gsr.CommandContext, request BeginRecoveryRequest) error {
	if !s.gatewaySource(commandContext.Source()) {
		return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrUnauthorized)
	}
	normalized, err := normalizeBeginRecoveryRequest(request)
	if err != nil {
		return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, err)
	}
	if !s.allowed(normalized.Principal) {
		s.appendAudit(normalized.RequestID, normalized.Principal, "begin_recovery", "denied")
		return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrUnauthorized)
	}
	if existing, exists := s.recoveries[normalized.RequestID]; exists {
		if !sameBeginRecoveryRequest(existing.request, normalized) {
			s.appendAudit(normalized.RequestID, normalized.Principal, "begin_recovery", "conflict")
			return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrRecoveryRequestConflict)
		}
		return s.replyRecoveryOperation(commandContext, existing.operation, nil)
	}
	stop, exists := s.stops[normalized.RequestID]
	if !exists {
		return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrStopOperationNotFound)
	}
	if stop.operation.Principal != normalized.Principal {
		return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrOperationOwnerMismatch)
	}
	if !stopTerminal(stop.operation.Phase) || !matchesStopRecoveryTargets(stop.operation.Targets, normalized.Targets) {
		return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrRecoveryNotReady)
	}
	for _, target := range normalized.Targets {
		if containsRecoveryRef(normalized.Expected.Refs, target.Removed) {
			return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrInvalidRecoveryRequest)
		}
	}
	current, err := s.getDirectory(normalized.Group)
	if err != nil || !sameDrainServiceSet(current, normalized.Expected) {
		return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, ErrRecoveryNotReady)
	}
	now := s.context.Now()
	record := recoveryOperationRecord{
		request: normalized,
		operation: RecoveryOperation{
			RequestID: normalized.RequestID, Principal: normalized.Principal, Group: normalized.Group,
			Expected: cloneDrainServiceSet(normalized.Expected), Targets: recoveryTargetsFromRequest(normalized.Targets),
			Phase: RecoveryCreating, CreatedAt: now, UpdatedAt: now,
		},
	}
	s.advanceRecovery(&record)
	s.recoveries[normalized.RequestID] = record
	s.appendAudit(normalized.RequestID, normalized.Principal, "begin_recovery", string(record.operation.Phase))
	s.context.Metrics().Inc("recovery_operations_started_total")
	return s.replyRecoveryOperation(commandContext, record.operation, nil)
}

func (s *drainCoordinatorService) handleResolveRecovery(commandContext gsr.CommandContext, requestID RequestID, principal Principal) error {
	record, err := s.authorizedRecovery(commandContext, requestID, principal, "resolve_recovery")
	if err != nil {
		return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, err)
	}
	if !recoveryTerminal(record.operation.Phase) {
		s.advanceRecovery(&record)
		s.recoveries[requestID] = record
	}
	s.appendAudit(requestID, principal, "resolve_recovery", string(record.operation.Phase))
	return s.replyRecoveryOperation(commandContext, record.operation, nil)
}

func (s *drainCoordinatorService) handleGetRecovery(commandContext gsr.CommandContext, requestID RequestID, principal Principal) error {
	record, err := s.authorizedRecovery(commandContext, requestID, principal, "get_recovery")
	if err != nil {
		return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, err)
	}
	return s.replyRecoveryOperation(commandContext, record.operation, nil)
}

func (s *drainCoordinatorService) handleConfirmRecovery(commandContext gsr.CommandContext, requestID RequestID, principal Principal) error {
	record, err := s.authorizedRecovery(commandContext, requestID, principal, "confirm_recovery")
	if err != nil {
		return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, err)
	}
	if record.operation.Phase != RecoveryAwaitingConfirmation {
		return s.replyRecoveryOperation(commandContext, record.operation, ErrRecoveryNotReady)
	}
	current, getErr := s.getDirectory(record.operation.Group)
	if getErr != nil || !sameDrainServiceSet(current, record.operation.Expected) {
		s.setRecoveryPhase(&record, RecoveryFailed)
		s.recoveries[requestID] = record
		return s.replyRecoveryOperation(commandContext, record.operation, ErrRecoveryNotReady)
	}
	candidate := recoveryPublishedSet(record.operation)
	s.setRecoveryPhase(&record, RecoveryPublishing)
	published, publishErr := s.publishRecoveryDirectory(record.operation, candidate)
	if publishErr != nil {
		s.recoveries[requestID] = record
		s.appendAudit(requestID, principal, "confirm_recovery", "publishing")
		return s.replyRecoveryOperation(commandContext, record.operation, nil)
	}
	s.completeRecoveryPublish(&record, published)
	s.recoveries[requestID] = record
	s.appendAudit(requestID, principal, "confirm_recovery", string(record.operation.Phase))
	return s.replyRecoveryOperation(commandContext, record.operation, nil)
}

func (s *drainCoordinatorService) handleAbandonRecovery(commandContext gsr.CommandContext, requestID RequestID, principal Principal) error {
	record, err := s.authorizedRecovery(commandContext, requestID, principal, "abandon_recovery")
	if err != nil {
		return s.replyRecoveryOperation(commandContext, RecoveryOperation{}, err)
	}
	if record.operation.Phase == RecoveryCompleted || record.operation.Phase == RecoveryPublishing {
		return s.replyRecoveryOperation(commandContext, record.operation, ErrRecoveryNotReady)
	}
	for index := range record.operation.Targets {
		record.operation.Targets[index].State = RecoveryTargetAbandoned
		record.operation.Targets[index].Failure = RecoveryFailureNone
	}
	s.setRecoveryPhase(&record, RecoveryAbandoned)
	s.recoveries[requestID] = record
	s.appendAudit(requestID, principal, "abandon_recovery", string(record.operation.Phase))
	return s.replyRecoveryOperation(commandContext, record.operation, nil)
}

func (s *drainCoordinatorService) authorizedRecovery(commandContext gsr.CommandContext, requestID RequestID, principal Principal, action string) (recoveryOperationRecord, error) {
	if !s.gatewaySource(commandContext.Source()) {
		return recoveryOperationRecord{}, ErrUnauthorized
	}
	if !validRequestID(requestID) {
		return recoveryOperationRecord{}, ErrInvalidRequestID
	}
	if !validPrincipal(principal) {
		return recoveryOperationRecord{}, ErrInvalidPrincipal
	}
	if !s.allowed(principal) {
		s.appendAudit(requestID, principal, action, "denied")
		return recoveryOperationRecord{}, ErrUnauthorized
	}
	record, exists := s.recoveries[requestID]
	if !exists {
		return recoveryOperationRecord{}, ErrRecoveryOperationNotFound
	}
	if record.operation.Principal != principal {
		return recoveryOperationRecord{}, ErrOperationOwnerMismatch
	}
	return record, nil
}

func (s *drainCoordinatorService) advanceRecovery(record *recoveryOperationRecord) {
	if record.operation.Phase == RecoveryPublishing {
		s.resolveRecoveryPublish(record)
		return
	}
	if record.operation.Phase != RecoveryCreating {
		return
	}
	current, err := s.getDirectory(record.operation.Group)
	if err != nil {
		s.touchRecovery(record)
		return
	}
	if !sameDrainServiceSet(current, record.operation.Expected) {
		s.setRecoveryPhase(record, RecoveryFailed)
		return
	}
	for index := range record.operation.Targets {
		target := record.operation.Targets[index]
		if target.State == RecoveryTargetCreated || target.State == RecoveryTargetFailed {
			continue
		}
		var receipt RecoveryReceipt
		if target.State == RecoveryTargetPending {
			receipt, err = s.beginNodeRecovery(record.operation, target)
		} else {
			receipt, err = s.getNodeRecoveryReceipt(record.operation, target)
		}
		if err == nil {
			s.applyNodeRecoveryReceipt(record, index, receipt)
		}
	}
	allCreated := true
	allTerminal := true
	anyFailed := false
	for _, target := range record.operation.Targets {
		allCreated = allCreated && target.State == RecoveryTargetCreated
		allTerminal = allTerminal && target.State != RecoveryTargetPending && target.State != RecoveryTargetCreating
		anyFailed = anyFailed || target.State == RecoveryTargetFailed
	}
	switch {
	case allCreated:
		s.setRecoveryPhase(record, RecoveryAwaitingConfirmation)
	case allTerminal && anyFailed:
		s.setRecoveryPhase(record, RecoveryFailed)
	default:
		s.touchRecovery(record)
	}
}

func (s *drainCoordinatorService) beginNodeRecovery(operation RecoveryOperation, target RecoveryTarget) (RecoveryReceipt, error) {
	callContext, cancel := context.WithTimeout(context.Background(), s.config.CallTimeout)
	value, err := s.context.Call(callContext, target.Agent, commandBeginRecoveryCreate, beginRecoveryCreateRequest{Task: RecoveryCreateTask{Agent: target.Agent, RequestID: operation.RequestID, Removed: target.Removed, Blueprint: target.Blueprint}})
	cancel()
	return recoveryReceiptFromResponse(value, err, operation.RequestID, target)
}

func (s *drainCoordinatorService) getNodeRecoveryReceipt(operation RecoveryOperation, target RecoveryTarget) (RecoveryReceipt, error) {
	callContext, cancel := context.WithTimeout(context.Background(), s.config.CallTimeout)
	value, err := s.context.Call(callContext, target.Agent, commandGetRecoveryReceipt, getRecoveryReceiptRequest{RequestID: operation.RequestID, Removed: target.Removed})
	cancel()
	return recoveryReceiptFromResponse(value, err, operation.RequestID, target)
}

func recoveryReceiptFromResponse(value any, callErr error, requestID RequestID, target RecoveryTarget) (RecoveryReceipt, error) {
	if callErr != nil {
		return RecoveryReceipt{}, callErr
	}
	response, ok := value.(recoveryReceiptResponse)
	if !ok {
		return RecoveryReceipt{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return RecoveryReceipt{}, err
	}
	if !validRecoveryReceipt(response.Receipt) || response.Receipt.RequestID != requestID || response.Receipt.Removed != target.Removed || response.Receipt.Blueprint != target.Blueprint {
		return RecoveryReceipt{}, ErrInvalidResponse
	}
	return response.Receipt, nil
}

func (s *drainCoordinatorService) applyNodeRecoveryReceipt(record *recoveryOperationRecord, index int, receipt RecoveryReceipt) {
	target := &record.operation.Targets[index]
	target.Created = receipt.Created
	target.State = receipt.State
	target.Failure = receipt.Failure
	s.touchRecovery(record)
}

func (s *drainCoordinatorService) resolveRecoveryPublish(record *recoveryOperationRecord) {
	current, err := s.getDirectory(record.operation.Group)
	if err != nil {
		s.touchRecovery(record)
		return
	}
	candidate := recoveryPublishedSet(record.operation)
	if sameDrainServiceSet(current, candidate) {
		s.completeRecoveryPublish(record, current)
		return
	}
	if !sameDrainServiceSet(current, record.operation.Expected) {
		s.setRecoveryPhase(record, RecoveryFailed)
	}
}

func (s *drainCoordinatorService) completeRecoveryPublish(record *recoveryOperationRecord, published servicegroup.ServiceSet) {
	record.operation.Published = cloneDrainServiceSet(published)
	for index := range record.operation.Targets {
		record.operation.Targets[index].State = RecoveryTargetPublished
		record.operation.Targets[index].Failure = RecoveryFailureNone
	}
	s.setRecoveryPhase(record, RecoveryCompleted)
}

func (s *drainCoordinatorService) publishRecoveryDirectory(operation RecoveryOperation, candidate servicegroup.ServiceSet) (servicegroup.ServiceSet, error) {
	callContext, cancel := context.WithTimeout(context.Background(), s.config.CallTimeout)
	set, err := s.directory.Publish(callContext, operation.Group, operation.Expected.Version, candidate.Refs, candidate.Tags)
	cancel()
	return set, err
}

func (s *drainCoordinatorService) setRecoveryPhase(record *recoveryOperationRecord, phase RecoveryPhase) {
	if record.operation.Phase == phase {
		return
	}
	record.operation.Phase = phase
	s.touchRecovery(record)
	switch phase {
	case RecoveryCompleted:
		s.context.Metrics().Inc("recovery_operations_completed_total")
	case RecoveryFailed:
		s.context.Metrics().Inc("recovery_operations_failed_total")
	case RecoveryAbandoned:
		s.context.Metrics().Inc("recovery_operations_abandoned_total")
	}
}

func (s *drainCoordinatorService) touchRecovery(record *recoveryOperationRecord) {
	now := s.context.Now()
	if !now.Before(record.operation.UpdatedAt) {
		record.operation.UpdatedAt = now
	}
}

func recoveryTerminal(phase RecoveryPhase) bool {
	return phase == RecoveryCompleted || phase == RecoveryFailed || phase == RecoveryAbandoned
}

func matchesStopRecoveryTargets(stops []StopTarget, targets []RecoveryTargetRequest) bool {
	if len(stops) != len(targets) {
		return false
	}
	for index := range stops {
		if stops[index].Target != targets[index].Removed {
			return false
		}
	}
	return true
}

func recoveryTargetsFromRequest(requests []RecoveryTargetRequest) []RecoveryTarget {
	targets := make([]RecoveryTarget, len(requests))
	for index, request := range requests {
		targets[index] = RecoveryTarget{Removed: request.Removed, Agent: request.Agent, Blueprint: request.Blueprint, State: RecoveryTargetPending}
	}
	return targets
}

func recoveryPublishedSet(operation RecoveryOperation) servicegroup.ServiceSet {
	refs := append([]gsr.ServiceRef(nil), operation.Expected.Refs...)
	for _, target := range operation.Targets {
		if !containsRecoveryRef(refs, target.Created) {
			refs = append(refs, target.Created)
		}
	}
	for left := 0; left < len(refs); left++ {
		for right := left + 1; right < len(refs); right++ {
			if refs[right].Node < refs[left].Node || (refs[right].Node == refs[left].Node && refs[right].ID < refs[left].ID) {
				refs[left], refs[right] = refs[right], refs[left]
			}
		}
	}
	return servicegroup.ServiceSet{Name: operation.Group, Version: servicegroup.ServiceSetVersion{AuthorityEpoch: operation.Expected.Version.AuthorityEpoch, Revision: operation.Expected.Version.Revision + 1}, Refs: refs, Tags: cloneDrainTags(operation.Expected.Tags)}
}

func (s *drainCoordinatorService) advanceStop(record *stopOperationRecord) {
	if stopTerminal(record.operation.Phase) {
		return
	}
	matches, err := s.matchesStopDirectory(record.operation.Published)
	if err != nil {
		s.setStopPhase(record, StopWaiting)
		return
	}
	if !matches {
		s.setStopPhase(record, StopSuperseded)
		return
	}
	for index := range record.operation.Targets {
		if receipt, err := s.getNodeStopReceipt(record.operation, record.operation.Targets[index]); err == nil {
			s.applyNodeStopReceipt(record, index, receipt)
		}
	}
	for index := range record.operation.Targets {
		if record.operation.Targets[index].State != StopTargetPending {
			continue
		}
		matches, err := s.matchesStopDirectory(record.operation.Published)
		if err != nil {
			s.setStopPhase(record, StopWaiting)
			return
		}
		if !matches {
			s.setStopPhase(record, StopSuperseded)
			return
		}
		if receipt, err := s.beginNodeStop(record.operation, record.operation.Targets[index]); err == nil {
			s.applyNodeStopReceipt(record, index, receipt)
		}
	}
	s.concludeStop(record)
}

func (s *drainCoordinatorService) matchesStopDirectory(published servicegroup.ServiceSet) (bool, error) {
	current, err := s.getDirectory(published.Name)
	if err != nil {
		return false, err
	}
	return sameDrainServiceSet(current, published), nil
}

func (s *drainCoordinatorService) beginNodeStop(operation StopOperation, target StopTarget) (NodeStopReceipt, error) {
	callContext, cancel := context.WithTimeout(context.Background(), s.config.CallTimeout)
	value, err := s.context.Call(callContext, target.Agent, commandBeginNodeStop, beginNodeStopRequest{Task: NodeStopTask{
		Agent: target.Agent, RequestID: operation.RequestID, Target: target.Target, Group: operation.Group, Published: cloneDrainServiceSet(operation.Published),
	}})
	cancel()
	return nodeStopReceiptFromResponse(value, err, operation.RequestID, target.Target)
}

func (s *drainCoordinatorService) getNodeStopReceipt(operation StopOperation, target StopTarget) (NodeStopReceipt, error) {
	callContext, cancel := context.WithTimeout(context.Background(), s.config.CallTimeout)
	value, err := s.context.Call(callContext, target.Agent, commandGetNodeStopReceipt, getNodeStopReceiptRequest{RequestID: operation.RequestID, Target: target.Target})
	cancel()
	return nodeStopReceiptFromResponse(value, err, operation.RequestID, target.Target)
}

func nodeStopReceiptFromResponse(value any, callErr error, requestID RequestID, target gsr.ServiceRef) (NodeStopReceipt, error) {
	if callErr != nil {
		return NodeStopReceipt{}, callErr
	}
	response, ok := value.(nodeStopReceiptResponse)
	if !ok {
		return NodeStopReceipt{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return NodeStopReceipt{}, err
	}
	if !validNodeStopReceipt(response.Receipt) || response.Receipt.RequestID != requestID || response.Receipt.Target != target {
		return NodeStopReceipt{}, ErrInvalidResponse
	}
	return response.Receipt, nil
}

func (s *drainCoordinatorService) applyNodeStopReceipt(record *stopOperationRecord, index int, receipt NodeStopReceipt) {
	record.operation.Targets[index].State = receipt.State
	record.operation.Targets[index].Failure = receipt.Failure
	s.touchStop(record)
}

func (s *drainCoordinatorService) concludeStop(record *stopOperationRecord) {
	allStopped := true
	allTerminal := true
	anyFailed := false
	for _, target := range record.operation.Targets {
		switch target.State {
		case StopTargetSuperseded:
			s.setStopPhase(record, StopSuperseded)
			return
		case StopTargetStopped:
		case StopTargetFailed:
			allStopped = false
			anyFailed = true
		case StopTargetPending, StopTargetQueued:
			allStopped = false
			allTerminal = false
		}
	}
	switch {
	case allStopped:
		s.setStopPhase(record, StopCompleted)
	case allTerminal && anyFailed:
		s.setStopPhase(record, StopFailed)
	default:
		s.setStopPhase(record, StopWaiting)
	}
}

func (s *drainCoordinatorService) setStopPhase(record *stopOperationRecord, phase StopPhase) {
	if record.operation.Phase == phase {
		return
	}
	record.operation.Phase = phase
	s.touchStop(record)
	switch phase {
	case StopCompleted:
		s.context.Metrics().Inc("drain_stop_operations_completed_total")
	case StopFailed:
		s.context.Metrics().Inc("drain_stop_operations_failed_total")
	case StopSuperseded:
		s.context.Metrics().Inc("drain_stop_operations_superseded_total")
	}
}

func (s *drainCoordinatorService) touchStop(record *stopOperationRecord) {
	now := s.context.Now()
	if !now.Before(record.operation.UpdatedAt) {
		record.operation.UpdatedAt = now
	}
}

func stopTerminal(phase StopPhase) bool {
	return phase == StopCompleted || phase == StopFailed || phase == StopSuperseded
}

func matchesDrainStopTargets(targets []StopTargetRequest, drained []DrainTarget) bool {
	if len(targets) != len(drained) {
		return false
	}
	for index := range targets {
		if targets[index].Target != drained[index].Ref {
			return false
		}
	}
	return true
}

func stopTargetsFromRequest(requests []StopTargetRequest) []StopTarget {
	targets := make([]StopTarget, len(requests))
	for index, request := range requests {
		targets[index] = StopTarget{Target: request.Target, Agent: request.Agent, State: StopTargetPending}
	}
	return targets
}

func (s *drainCoordinatorService) advance(record *drainOperationRecord) {
	for {
		switch record.operation.Phase {
		case DrainPreparing:
			if !s.prepare(record) {
				return
			}
		case DrainPublishUnknown:
			if !s.confirmPublished(record) {
				return
			}
		case DrainGuarding:
			if !s.guard(record) {
				return
			}
		case DrainWaitingVisitors:
			if !s.waitForVisitors(record) {
				return
			}
		default:
			return
		}
	}
}

func (s *drainCoordinatorService) prepare(record *drainOperationRecord) bool {
	set, err := s.getDirectory(record.request.Group)
	if err != nil {
		return false
	}
	if set.Version != record.request.Expected {
		s.setPhase(record, DrainConflict)
		s.context.Metrics().Inc("drain_operations_conflict_total")
		return false
	}
	record.operation.Original = cloneDrainServiceSet(set)
	published, err := s.publishDirectory(record.request)
	if err != nil {
		if errors.Is(err, servicegroup.ErrVersionConflict) {
			s.setPhase(record, DrainConflict)
			s.context.Metrics().Inc("drain_operations_conflict_total")
			return false
		}
		s.setPhase(record, DrainPublishUnknown)
		s.context.Metrics().Inc("drain_operations_publish_unknown_total")
		return false
	}
	s.confirmPublish(record, published)
	return true
}

func (s *drainCoordinatorService) confirmPublished(record *drainOperationRecord) bool {
	set, err := s.getDirectory(record.request.Group)
	if err != nil {
		return false
	}
	if sameDrainServiceSet(set, expectedPublishedSet(record.request, set.Version)) && isExpectedNextVersion(record.request.Expected, set.Version) {
		s.confirmPublish(record, set)
		return true
	}
	if isVersionAfter(record.request.Expected, set.Version) {
		s.setPhase(record, DrainSuperseded)
		return false
	}
	return false
}

func (s *drainCoordinatorService) confirmPublish(record *drainOperationRecord, published servicegroup.ServiceSet) {
	record.operation.Published = cloneDrainServiceSet(published)
	record.operation.Targets = drainTargets(record.operation.Original, published)
	s.setPhase(record, DrainGuarding)
}

func (s *drainCoordinatorService) guard(record *drainOperationRecord) bool {
	if !s.directoryStillPublished(record) {
		return false
	}
	allGuarded := true
	for index := range record.operation.Targets {
		target := &record.operation.Targets[index]
		if target.Guarded {
			continue
		}
		guard, err := drain.NewGuardClient(s.context, target.Ref)
		if err != nil {
			allGuarded = false
			continue
		}
		status, beginErr := s.beginGuard(guard)
		if beginErr == nil && status.Draining {
			target.Guarded = true
			continue
		}
		status, statusErr := s.guardStatus(guard)
		if statusErr == nil && status.Draining {
			target.Guarded = true
			continue
		}
		allGuarded = false
		s.context.Metrics().Inc("drain_operations_guard_unknown_total")
	}
	if !allGuarded {
		s.touch(record)
		return false
	}
	s.setPhase(record, DrainWaitingVisitors)
	return true
}

func (s *drainCoordinatorService) waitForVisitors(record *drainOperationRecord) bool {
	if !s.directoryStillPublished(record) {
		return false
	}
	allClear := true
	for index := range record.operation.Targets {
		visitors, err := s.listVisitors(record.operation.Targets[index].Ref)
		if err != nil {
			allClear = false
			continue
		}
		strong := 0
		for _, visitor := range visitors {
			if !visitor.Weak {
				strong++
			}
		}
		record.operation.Targets[index].StrongVisitorCount = strong
		if strong != 0 {
			allClear = false
		}
	}
	if !allClear {
		s.touch(record)
		return false
	}
	s.setPhase(record, DrainReadyToStop)
	s.context.Metrics().Inc("drain_operations_ready_total")
	return false
}

func (s *drainCoordinatorService) directoryStillPublished(record *drainOperationRecord) bool {
	set, err := s.getDirectory(record.request.Group)
	if err != nil {
		return false
	}
	if sameDrainServiceSet(set, record.operation.Published) {
		return true
	}
	s.setPhase(record, DrainSuperseded)
	return false
}

func (s *drainCoordinatorService) getDirectory(group servicegroup.GroupName) (servicegroup.ServiceSet, error) {
	callContext, cancel := context.WithTimeout(context.Background(), s.config.CallTimeout)
	set, err := s.directory.Get(callContext, group)
	cancel()
	return set, err
}

func (s *drainCoordinatorService) publishDirectory(request StartDrainRequest) (servicegroup.ServiceSet, error) {
	callContext, cancel := context.WithTimeout(context.Background(), s.config.CallTimeout)
	set, err := s.directory.Publish(callContext, request.Group, request.Expected, request.NextRefs, request.NextTags)
	cancel()
	return set, err
}

func (s *drainCoordinatorService) beginGuard(client *drain.GuardClient) (drain.DrainStatus, error) {
	callContext, cancel := context.WithTimeout(context.Background(), s.config.CallTimeout)
	status, err := client.Begin(callContext)
	cancel()
	return status, err
}

func (s *drainCoordinatorService) guardStatus(client *drain.GuardClient) (drain.DrainStatus, error) {
	callContext, cancel := context.WithTimeout(context.Background(), s.config.CallTimeout)
	status, err := client.Status(callContext)
	cancel()
	return status, err
}

func (s *drainCoordinatorService) listVisitors(target gsr.ServiceRef) ([]drain.VisitorRef, error) {
	callContext, cancel := context.WithTimeout(context.Background(), s.config.CallTimeout)
	visitors, err := s.visitors.List(callContext, target)
	cancel()
	return visitors, err
}

func (s *drainCoordinatorService) gatewaySource(source gsr.ServiceRef) bool {
	return source == s.config.Gateway
}

func (s *drainCoordinatorService) allowed(principal Principal) bool {
	for _, allowed := range s.config.AllowedPrincipals {
		if allowed == principal {
			return true
		}
	}
	return false
}

func (s *drainCoordinatorService) setPhase(record *drainOperationRecord, phase DrainPhase) {
	record.operation.Phase = phase
	s.touch(record)
}

func (s *drainCoordinatorService) touch(record *drainOperationRecord) {
	now := s.context.Now()
	if now.Before(record.operation.UpdatedAt) {
		return
	}
	record.operation.UpdatedAt = now
}

func (s *drainCoordinatorService) appendAudit(requestID RequestID, principal Principal, action, outcome string) {
	s.sequence++
	if s.sequence == 0 {
		s.sequence++
	}
	s.audits = append(s.audits, DrainAudit{
		Sequence:   s.sequence,
		RequestID:  requestID,
		Principal:  principal,
		Action:     action,
		Outcome:    outcome,
		OccurredAt: s.context.Now(),
	})
	if len(s.audits) <= s.config.AuditLimit {
		return
	}
	copy(s.audits, s.audits[len(s.audits)-s.config.AuditLimit:])
	s.audits = s.audits[:s.config.AuditLimit]
	s.context.Metrics().Inc("drain_audit_evicted_total")
}

func (s *drainCoordinatorService) replyOperation(commandContext gsr.CommandContext, operation DrainOperation, err error) error {
	if err != nil {
		return commandContext.Reply(drainOperationResponse{Error: codeFromError(err)})
	}
	return commandContext.Reply(drainOperationResponse{Operation: cloneDrainOperation(operation)})
}

func (s *drainCoordinatorService) replyStopOperation(commandContext gsr.CommandContext, operation StopOperation, err error) error {
	if err != nil {
		return commandContext.Reply(stopOperationResponse{Error: codeFromError(err)})
	}
	return commandContext.Reply(stopOperationResponse{Operation: cloneStopOperation(operation)})
}

func (s *drainCoordinatorService) replyRecoveryOperation(commandContext gsr.CommandContext, operation RecoveryOperation, err error) error {
	if err != nil {
		return commandContext.Reply(recoveryOperationResponse{Error: codeFromError(err)})
	}
	return commandContext.Reply(recoveryOperationResponse{Operation: cloneRecoveryOperation(operation)})
}

func (s *drainCoordinatorService) replyAudits(commandContext gsr.CommandContext, audits []DrainAudit, err error) error {
	if err != nil {
		return commandContext.Reply(drainAuditsResponse{Error: codeFromError(err)})
	}
	return commandContext.Reply(drainAuditsResponse{Audits: cloneDrainAudits(audits)})
}

func validDrainCoordinatorConfig(config DrainCoordinatorConfig) bool {
	if !validServiceRef(config.Gateway) || !validServiceRef(config.Directory) || !validServiceRef(config.VisitorRegistry) || len(config.AllowedPrincipals) == 0 || config.CallTimeout < 0 || config.AuditLimit < 0 {
		return false
	}
	seen := make(map[Principal]struct{}, len(config.AllowedPrincipals))
	for _, principal := range config.AllowedPrincipals {
		if !validPrincipal(principal) {
			return false
		}
		if _, exists := seen[principal]; exists {
			return false
		}
		seen[principal] = struct{}{}
	}
	return true
}

func sameDrainServiceSet(left, right servicegroup.ServiceSet) bool {
	if left.Name != right.Name || left.Version != right.Version || len(left.Refs) != len(right.Refs) || len(left.Tags) != len(right.Tags) {
		return false
	}
	for index := range left.Refs {
		if left.Refs[index] != right.Refs[index] {
			return false
		}
	}
	for key, value := range left.Tags {
		if rightValue, exists := right.Tags[key]; !exists || rightValue != value {
			return false
		}
	}
	return true
}

func expectedPublishedSet(request StartDrainRequest, version servicegroup.ServiceSetVersion) servicegroup.ServiceSet {
	return servicegroup.ServiceSet{Name: request.Group, Version: version, Refs: append([]gsr.ServiceRef(nil), request.NextRefs...), Tags: cloneDrainTags(request.NextTags)}
}

func cloneDrainTags(tags map[string]string) map[string]string {
	copy := make(map[string]string, len(tags))
	for key, value := range tags {
		copy[key] = value
	}
	return copy
}

func isExpectedNextVersion(expected, actual servicegroup.ServiceSetVersion) bool {
	return actual.AuthorityEpoch == expected.AuthorityEpoch && actual.Revision == expected.Revision+1
}

func isVersionAfter(expected, actual servicegroup.ServiceSetVersion) bool {
	if actual.AuthorityEpoch != expected.AuthorityEpoch {
		return actual.AuthorityEpoch != 0
	}
	return actual.Revision > expected.Revision
}

var _ gsr.Service = (*drainCoordinatorService)(nil)
