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

type drainCoordinatorService struct {
	config     DrainCoordinatorConfig
	context    gsr.ServiceContext
	directory  *servicegroup.Client
	visitors   *drain.Client
	operations map[RequestID]drainOperationRecord
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
	return &drainCoordinatorService{config: config, operations: make(map[RequestID]drainOperationRecord)}, nil
}

func (*drainCoordinatorService) Commands() []gsr.CommandID {
	return []gsr.CommandID{
		commandStartDrainOperation,
		commandResolveDrainOperation,
		commandGetDrainOperation,
		commandListDrainAudit,
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
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (s *drainCoordinatorService) Stop(context.Context) error {
	s.operations = make(map[RequestID]drainOperationRecord)
	s.audits = nil
	s.sequence = 0
	return nil
}

func (s *drainCoordinatorService) Close() error {
	s.context = nil
	s.directory = nil
	s.visitors = nil
	s.operations = nil
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
