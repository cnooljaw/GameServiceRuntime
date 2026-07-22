package supervisor

import (
	"context"
	"errors"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type service struct {
	executor RecoveryExecutor
	context  gsr.ServiceContext
	entries  map[ServiceKey]*serviceEntry
}

type serviceEntry struct {
	registration    Registration
	status          ServiceStatus
	nextAttempt     uint64
	activeTask      RecoveryTask
	attemptsInFault int
	restarts        []time.Time
	lastFailure     RecoveryFailure
	decided         uint64
	preparedRef     gsr.ServiceRef
}

// NewService creates a Supervisor Service backed by a non-blocking RecoveryExecutor.
func NewService(executor RecoveryExecutor) (gsr.Service, error) {
	if isNil(executor) {
		return nil, ErrInvalidConfig
	}
	return &service{executor: executor, entries: make(map[ServiceKey]*serviceEntry)}, nil
}

func (*service) Commands() []gsr.CommandID {
	return []gsr.CommandID{
		registerCommand, getCommand, failureCommand, recoveryStartedCommand,
		recoveryPreparedCommand, recoveryCommittedCommand, recoveryFailedCommand,
	}
}

func (s *service) Init(ctx gsr.ServiceContext) error {
	if isNil(ctx) {
		return ErrInvalidConfig
	}
	s.context = ctx
	return nil
}

func (s *service) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case registerCommand:
		request, ok := command.Payload.(registerRequest)
		if !ok {
			return ctx.Reply(operationResponse{Error: responseInvalidRegistration})
		}
		return ctx.Reply(operationResponse{Error: responseFromError(s.register(request.Registration))})
	case getCommand:
		request, ok := command.Payload.(getRequest)
		if !ok {
			return ctx.Reply(recordResponse{Error: responseInvalidKey})
		}
		record, err := s.get(request.Key)
		return ctx.Reply(recordResponse{Record: record, Error: responseFromError(err)})
	case failureCommand:
		notice, ok := command.Payload.(FailureNotice)
		if !ok {
			return ErrInvalidNotice
		}
		return s.handleFailure(ctx.Source(), notice)
	case recoveryStartedCommand:
		request, ok := command.Payload.(recoveryStartedRequest)
		if !ok {
			return ctx.Reply(operationResponse{Error: responseStaleRecovery})
		}
		return ctx.Reply(operationResponse{Error: responseFromError(s.recoveryStarted(ctx.Source(), request.Task))})
	case recoveryPreparedCommand:
		request, ok := command.Payload.(recoveryPreparedRequest)
		if !ok {
			return ctx.Reply(operationResponse{Error: responseStaleRecovery})
		}
		return ctx.Reply(operationResponse{Error: responseFromError(s.recoveryPrepared(ctx.Source(), request))})
	case recoveryCommittedCommand:
		request, ok := command.Payload.(recoveryCommittedRequest)
		if !ok {
			return ctx.Reply(operationResponse{Error: responseStaleRecovery})
		}
		return ctx.Reply(operationResponse{Error: responseFromError(s.recoveryCommitted(ctx.Source(), request))})
	case recoveryFailedCommand:
		request, ok := command.Payload.(recoveryFailedRequest)
		if !ok {
			return ctx.Reply(operationResponse{Error: responseStaleRecovery})
		}
		return ctx.Reply(operationResponse{Error: responseFromError(s.recoveryFailed(ctx.Source(), request))})
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (*service) Stop(context.Context) error { return nil }
func (*service) Close() error               { return nil }

func (s *service) register(registration Registration) error {
	if err := validateRegistration(registration); err != nil {
		return err
	}
	self := s.context.Self()
	if registration.Ref.Node != self.Node || registration.Ref == self {
		return ErrInvalidRegistration
	}
	if _, exists := s.entries[registration.Key]; exists {
		return ErrAlreadyRegistered
	}
	s.entries[registration.Key] = &serviceEntry{registration: registration, status: ServiceRunning}
	return nil
}

func (s *service) get(key ServiceKey) (Record, error) {
	if err := validateServiceKey(key); err != nil {
		return Record{}, err
	}
	entry := s.entries[key]
	if entry == nil {
		return Record{}, ErrServiceNotRegistered
	}
	s.pruneRestarts(entry, s.context.Now())
	return Record{
		Registration:     entry.registration,
		Status:           entry.status,
		Attempt:          entry.activeTask.Attempt,
		AttemptsInFault:  entry.attemptsInFault,
		RestartsInWindow: len(entry.restarts),
		LastFailure:      entry.lastFailure,
	}, nil
}

func (s *service) handleFailure(source gsr.ServiceRef, notice FailureNotice) error {
	if err := validateFailureNotice(notice); err != nil || source != notice.FailedRef {
		return ErrInvalidNotice
	}
	entry := s.entries[notice.Key]
	if entry == nil {
		return ErrServiceNotRegistered
	}
	if notice.FailedRef != entry.registration.Ref || notice.Generation != entry.registration.Generation {
		s.context.Metrics().Inc(metricFailureNoticesStale)
		return ErrStaleNotice
	}
	if entry.status != ServiceRunning || entry.decided == notice.Generation {
		s.context.Metrics().Inc(metricFailureNoticesDuplicate)
		return ErrDuplicateNotice
	}
	entry.decided = notice.Generation
	s.context.Metrics().Inc(metricFailureNotices)
	switch entry.registration.Policy.Strategy {
	case RestartNever:
		entry.status = ServiceRestartStopped
		return nil
	case DestroyOnFailure:
		entry.status = ServiceDestroyed
		return nil
	case RestartOnFailure:
		now := s.context.Now()
		s.pruneRestarts(entry, now)
		if len(entry.restarts) >= entry.registration.Policy.MaxRestarts {
			entry.status = ServiceRestartSuppressed
			entry.lastFailure = RecoveryFailureSuppressed
			s.context.Metrics().Inc(metricRestartsSuppressed)
			return ErrRestartSuppressed
		}
		entry.attemptsInFault = 0
		entry.lastFailure = RecoveryFailureNone
		return s.schedule(entry)
	default:
		return ErrInvalidPolicy
	}
}

func (s *service) recoveryStarted(source gsr.ServiceRef, task RecoveryTask) error {
	if source == runtimeRoot(s.context.Self().Node) {
		if entry := s.entries[task.Key]; entry != nil && entry.activeTask == task && entry.status == ServicePreparing {
			return nil
		}
	}
	entry, err := s.activeEntry(source, task, ServiceBackoff)
	if err != nil {
		return err
	}
	entry.status = ServicePreparing
	return nil
}

func (s *service) recoveryPrepared(source gsr.ServiceRef, request recoveryPreparedRequest) error {
	if source == runtimeRoot(s.context.Self().Node) {
		if entry := s.entries[request.Task.Key]; entry != nil && entry.activeTask == request.Task && entry.status == ServicePublishing && entry.preparedRef == request.Ref {
			return nil
		}
	}
	entry, err := s.activeEntry(source, request.Task, ServicePreparing)
	if err != nil {
		return err
	}
	if validateConcreteRef(request.Ref) != nil || request.Ref.Node != s.context.Self().Node || request.Ref == s.context.Self() {
		return ErrStaleRecovery
	}
	entry.preparedRef = request.Ref
	entry.status = ServicePublishing
	return nil
}

func (s *service) recoveryCommitted(source gsr.ServiceRef, request recoveryCommittedRequest) error {
	if source == runtimeRoot(s.context.Self().Node) {
		if entry := s.entries[request.Task.Key]; entry != nil && entry.activeTask == request.Task && entry.status == ServiceRunning && entry.registration.Generation == request.Task.Generation && entry.registration.Ref == request.Ref {
			return nil
		}
	}
	entry, err := s.activeEntry(source, request.Task, ServicePublishing)
	if err != nil || entry.preparedRef != request.Ref {
		return ErrStaleRecovery
	}
	entry.registration.Ref = request.Ref
	entry.registration.Generation = request.Task.Generation
	entry.status = ServiceRunning
	entry.attemptsInFault = 0
	entry.lastFailure = RecoveryFailureNone
	entry.preparedRef = gsr.ServiceRef{}
	entry.restarts = append(entry.restarts, s.context.Now())
	s.pruneRestarts(entry, s.context.Now())
	s.context.Metrics().Inc(metricRestartsSucceeded)
	return nil
}

func (s *service) recoveryFailed(source gsr.ServiceRef, request recoveryFailedRequest) error {
	if request.Failure <= RecoveryFailureNone || request.Failure > RecoveryFailureSuppressed {
		return ErrStaleRecovery
	}
	entry, err := s.activeEntry(source, request.Task, ServiceBackoff, ServicePreparing, ServicePublishing)
	if err != nil {
		return err
	}
	entry.preparedRef = gsr.ServiceRef{}
	entry.lastFailure = request.Failure
	s.context.Metrics().Inc(metricRestartsFailed)
	if request.Failure == RecoveryFailureAbort || entry.attemptsInFault >= entry.registration.Policy.MaxAttempts {
		entry.status = ServiceRecoveryFailed
		s.context.Metrics().Inc(metricRestartsSuppressed)
		return nil
	}
	return s.schedule(entry)
}

func (s *service) activeEntry(source gsr.ServiceRef, task RecoveryTask, statuses ...ServiceStatus) (*serviceEntry, error) {
	if source != runtimeRoot(s.context.Self().Node) || task.Supervisor != s.context.Self() {
		return nil, ErrStaleRecovery
	}
	entry := s.entries[task.Key]
	if entry == nil || entry.activeTask != task || task.Generation != entry.registration.Generation+1 {
		return nil, ErrStaleRecovery
	}
	for _, status := range statuses {
		if entry.status == status {
			return entry, nil
		}
	}
	return nil, ErrStaleRecovery
}

func (s *service) schedule(entry *serviceEntry) error {
	if entry.attemptsInFault >= entry.registration.Policy.MaxAttempts {
		entry.status = ServiceRecoveryFailed
		s.context.Metrics().Inc(metricRestartsSuppressed)
		return ErrRestartSuppressed
	}
	entry.nextAttempt++
	entry.attemptsInFault++
	task := RecoveryTask{
		Supervisor: s.context.Self(),
		Key:        entry.registration.Key,
		FailedRef:  entry.registration.Ref,
		Generation: entry.registration.Generation + 1,
		Attempt:    entry.nextAttempt,
		Delay:      restartBackoff(entry.registration.Policy, entry.attemptsInFault),
	}
	if err := s.executor.Submit(task); err != nil {
		entry.status = ServiceRecoveryFailed
		entry.activeTask = task
		if errors.Is(err, ErrRecoveryQueueFull) {
			entry.lastFailure = RecoveryFailureQueueFull
		} else {
			entry.lastFailure = RecoveryFailurePrepare
		}
		s.context.Metrics().Inc(metricRestartsFailed)
		return err
	}
	entry.activeTask = task
	entry.status = ServiceBackoff
	s.context.Metrics().Inc(metricRestartsScheduled)
	return nil
}

func (s *service) pruneRestarts(entry *serviceEntry, now time.Time) {
	cutoff := now.Add(-entry.registration.Policy.Window)
	first := 0
	for first < len(entry.restarts) && !entry.restarts[first].After(cutoff) {
		first++
	}
	if first > 0 {
		entry.restarts = append(entry.restarts[:0], entry.restarts[first:]...)
	}
}

func restartBackoff(policy RestartPolicy, attempt int) time.Duration {
	delay := policy.MinBackoff
	for index := 1; index < attempt && delay < policy.MaxBackoff; index++ {
		if delay > policy.MaxBackoff/2 {
			return policy.MaxBackoff
		}
		delay *= 2
	}
	if delay > policy.MaxBackoff {
		return policy.MaxBackoff
	}
	return delay
}

func runtimeRoot(node gsr.NodeID) gsr.ServiceRef { return gsr.ServiceRef{Node: node} }
