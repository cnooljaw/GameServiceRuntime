package servicegroup

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	defaultWatchTTL      = 30 * time.Second
	defaultSweepInterval = 5 * time.Second
)

type directoryService struct {
	config         DirectoryConfig
	context        gsr.ServiceContext
	authorityEpoch uint64
	groups         map[GroupName]ServiceSet
	watchers       map[GroupName]map[gsr.ServiceRef]WatchLease
	nextGeneration uint64
	watcherCount   int
	sweepScheduled bool
}

// NewDirectoryService creates a DirectoryService with one private ServiceGroup registry.
func NewDirectoryService(config DirectoryConfig) (gsr.Service, error) {
	if !validNode(config.PublisherNode) || config.WatchTTL < 0 || config.SweepInterval < 0 {
		return nil, ErrInvalidConfig
	}
	if config.WatchTTL == 0 {
		config.WatchTTL = defaultWatchTTL
	}
	if config.SweepInterval == 0 {
		config.SweepInterval = defaultSweepInterval
	}
	epoch, err := newAuthorityEpoch()
	if err != nil {
		return nil, fmt.Errorf("servicegroup: create authority epoch: %w", err)
	}
	return &directoryService{
		config:         config,
		authorityEpoch: epoch,
		groups:         make(map[GroupName]ServiceSet),
		watchers:       make(map[GroupName]map[gsr.ServiceRef]WatchLease),
	}, nil
}

func (*directoryService) Commands() []gsr.CommandID {
	return []gsr.CommandID{
		commandPublishServiceSet,
		commandGetServiceSet,
		commandWatchServiceGroup,
		commandRenewServiceGroupWatch,
		commandUnwatchServiceGroup,
		commandSweepExpiredWatches,
	}
}

func (s *directoryService) Init(serviceContext gsr.ServiceContext) error {
	s.context = serviceContext
	return nil
}

func (s *directoryService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	now := s.context.Now()
	s.pruneExpiredWatches(now)
	switch command.ID {
	case commandPublishServiceSet:
		request, ok := command.Payload.(publishServiceSetRequest)
		if !ok {
			return commandContext.Reply(serviceSetResponse{Error: responseInvalidRequest})
		}
		refs := make([]gsr.ServiceRef, len(request.Refs))
		for index, ref := range request.Refs {
			refs[index] = ref.serviceRef()
		}
		set, err := s.publish(commandContext.Source(), request.Name, request.Expected, refs, request.Tags)
		if err != nil {
			return commandContext.Reply(serviceSetResponse{Error: codeFromError(err)})
		}
		return commandContext.Reply(serviceSetResponse{Set: newWireServiceSet(set)})
	case commandGetServiceSet:
		request, ok := command.Payload.(getServiceSetRequest)
		if !ok {
			return commandContext.Reply(serviceSetResponse{Error: responseInvalidRequest})
		}
		set, err := s.get(request.Name)
		if err != nil {
			return commandContext.Reply(serviceSetResponse{Error: codeFromError(err)})
		}
		return commandContext.Reply(serviceSetResponse{Set: newWireServiceSet(set)})
	case commandWatchServiceGroup:
		request, ok := command.Payload.(watchServiceGroupRequest)
		if !ok {
			return commandContext.Reply(watchResultResponse{Error: responseInvalidRequest})
		}
		result, err := s.watch(now, commandContext.Source(), request.Name, request.Subscriber.serviceRef())
		if err != nil {
			return commandContext.Reply(watchResultResponse{Error: codeFromError(err)})
		}
		response := watchResultResponse{
			Lease: newWireWatchLease(result.Lease),
			Found: result.Found,
		}
		if result.Found {
			response.Current = newWireServiceSet(result.Current)
		}
		return commandContext.Reply(response)
	case commandRenewServiceGroupWatch:
		request, ok := command.Payload.(renewServiceGroupWatchRequest)
		if !ok {
			return commandContext.Reply(watchLeaseResponse{Error: responseInvalidRequest})
		}
		lease, err := s.renewWatch(now, commandContext.Source(), request.Lease.watchLease())
		if err != nil {
			return commandContext.Reply(watchLeaseResponse{Error: codeFromError(err)})
		}
		return commandContext.Reply(watchLeaseResponse{Lease: newWireWatchLease(lease)})
	case commandUnwatchServiceGroup:
		request, ok := command.Payload.(unwatchServiceGroupRequest)
		if !ok {
			return commandContext.Reply(emptyResponse{Error: responseInvalidRequest})
		}
		if err := s.unwatch(commandContext.Source(), request.Lease.watchLease()); err != nil {
			return commandContext.Reply(emptyResponse{Error: codeFromError(err)})
		}
		return commandContext.Reply(emptyResponse{})
	case commandSweepExpiredWatches:
		if _, ok := command.Payload.(sweepExpiredWatchesRequest); !ok {
			return ErrInvalidWatch
		}
		s.sweepScheduled = false
		if s.watcherCount > 0 {
			return s.scheduleWatchSweep()
		}
		return nil
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (s *directoryService) Stop(context.Context) error {
	s.groups = make(map[GroupName]ServiceSet)
	s.watchers = make(map[GroupName]map[gsr.ServiceRef]WatchLease)
	s.watcherCount = 0
	s.sweepScheduled = false
	s.context.Metrics().SetGauge("servicegroup_groups", 0)
	s.context.Metrics().SetGauge("servicegroup_watchers", 0)
	return nil
}

func (s *directoryService) Close() error {
	s.groups = nil
	s.watchers = nil
	s.context = nil
	return nil
}

func (s *directoryService) publish(source gsr.ServiceRef, name GroupName, expected ServiceSetVersion, refs []gsr.ServiceRef, tags map[string]string) (ServiceSet, error) {
	if source.Node != s.config.PublisherNode {
		return ServiceSet{}, ErrUnauthorized
	}
	if !validExpectedVersion(expected) {
		return ServiceSet{}, ErrInvalidServiceSet
	}
	set, err := normalizeServiceSet(name, refs, tags)
	if err != nil {
		return ServiceSet{}, err
	}
	current, exists := s.groups[name]
	switch {
	case exists && current.Version != expected:
		s.context.Metrics().Inc("servicegroup_publish_conflict_total")
		return ServiceSet{}, ErrVersionConflict
	case !exists && expected != (ServiceSetVersion{}):
		s.context.Metrics().Inc("servicegroup_publish_conflict_total")
		return ServiceSet{}, ErrVersionConflict
	}
	revision := uint64(1)
	if exists {
		if current.Version.Revision == math.MaxUint64 {
			return ServiceSet{}, ErrVersionExhausted
		}
		revision = current.Version.Revision + 1
	}
	set.Version = ServiceSetVersion{AuthorityEpoch: s.authorityEpoch, Revision: revision}
	s.groups[name] = cloneServiceSet(set)
	s.context.Metrics().Inc("servicegroup_publish_succeeded_total")
	s.context.Metrics().SetGauge("servicegroup_groups", int64(len(s.groups)))
	s.notifyWatchers(set)
	return cloneServiceSet(set), nil
}

func (s *directoryService) get(name GroupName) (ServiceSet, error) {
	if !validGroup(name) {
		return ServiceSet{}, ErrInvalidGroup
	}
	set, exists := s.groups[name]
	if !exists {
		return ServiceSet{}, ErrGroupNotFound
	}
	return cloneServiceSet(set), nil
}

func newAuthorityEpoch() (uint64, error) {
	var payload [8]byte
	for {
		if _, err := rand.Read(payload[:]); err != nil {
			return 0, err
		}
		epoch := binary.LittleEndian.Uint64(payload[:])
		if epoch != 0 {
			return epoch, nil
		}
	}
}

func (s *directoryService) watch(now time.Time, source gsr.ServiceRef, name GroupName, subscriber gsr.ServiceRef) (WatchResult, error) {
	if !validGroup(name) {
		return WatchResult{}, ErrInvalidGroup
	}
	if !validServiceRef(subscriber) {
		return WatchResult{}, ErrInvalidWatch
	}
	if source != subscriber {
		return WatchResult{}, ErrWatchOwnerMismatch
	}
	if !s.sweepScheduled {
		if err := s.scheduleWatchSweep(); err != nil {
			return WatchResult{}, err
		}
	}
	s.nextGeneration++
	if s.nextGeneration == 0 {
		s.nextGeneration++
	}
	lease := WatchLease{
		Group:          name,
		Subscriber:     subscriber,
		AuthorityEpoch: s.authorityEpoch,
		Generation:     s.nextGeneration,
		ExpiresAt:      now.Add(s.config.WatchTTL),
	}
	groupWatchers := s.watchers[name]
	if groupWatchers == nil {
		groupWatchers = make(map[gsr.ServiceRef]WatchLease)
		s.watchers[name] = groupWatchers
	}
	if _, exists := groupWatchers[subscriber]; !exists {
		s.watcherCount++
	}
	groupWatchers[subscriber] = lease
	s.updateWatcherGauge()
	result := WatchResult{Lease: lease}
	if current, exists := s.groups[name]; exists {
		result.Current = cloneServiceSet(current)
		result.Found = true
	}
	return cloneWatchResult(result), nil
}

func (s *directoryService) renewWatch(now time.Time, source gsr.ServiceRef, lease WatchLease) (WatchLease, error) {
	if !validWatchLease(lease) {
		return WatchLease{}, ErrInvalidWatch
	}
	if source != lease.Subscriber {
		return WatchLease{}, ErrWatchOwnerMismatch
	}
	if lease.AuthorityEpoch != s.authorityEpoch {
		return WatchLease{}, ErrWatchExpired
	}
	groupWatchers := s.watchers[lease.Group]
	current, exists := groupWatchers[lease.Subscriber]
	if !exists || !sameWatchLease(current, lease) {
		return WatchLease{}, ErrWatchExpired
	}
	current.ExpiresAt = now.Add(s.config.WatchTTL)
	groupWatchers[lease.Subscriber] = current
	return current, nil
}

func (s *directoryService) unwatch(source gsr.ServiceRef, lease WatchLease) error {
	if !validWatchLease(lease) {
		return ErrInvalidWatch
	}
	if source != lease.Subscriber {
		return ErrWatchOwnerMismatch
	}
	if lease.AuthorityEpoch != s.authorityEpoch {
		return ErrWatchExpired
	}
	groupWatchers := s.watchers[lease.Group]
	current, exists := groupWatchers[lease.Subscriber]
	if !exists || !sameWatchLease(current, lease) {
		return ErrWatchExpired
	}
	delete(groupWatchers, lease.Subscriber)
	s.watcherCount--
	if len(groupWatchers) == 0 {
		delete(s.watchers, lease.Group)
	}
	s.updateWatcherGauge()
	return nil
}

func (s *directoryService) pruneExpiredWatches(now time.Time) {
	changed := false
	for group, groupWatchers := range s.watchers {
		for subscriber, lease := range groupWatchers {
			if now.Before(lease.ExpiresAt) {
				continue
			}
			delete(groupWatchers, subscriber)
			s.watcherCount--
			changed = true
		}
		if len(groupWatchers) == 0 {
			delete(s.watchers, group)
		}
	}
	if changed {
		s.updateWatcherGauge()
	}
}

func (s *directoryService) scheduleWatchSweep() error {
	if _, err := s.context.After(s.config.SweepInterval, commandSweepExpiredWatches, sweepExpiredWatchesRequest{}); err != nil {
		return err
	}
	s.sweepScheduled = true
	return nil
}

func (s *directoryService) notifyWatchers(set ServiceSet) {
	groupWatchers := s.watchers[set.Name]
	leases := make([]WatchLease, 0, len(groupWatchers))
	for _, lease := range groupWatchers {
		leases = append(leases, lease)
	}
	sort.Slice(leases, func(left, right int) bool {
		if leases[left].Subscriber.Node != leases[right].Subscriber.Node {
			return leases[left].Subscriber.Node < leases[right].Subscriber.Node
		}
		return leases[left].Subscriber.ID < leases[right].Subscriber.ID
	})
	for _, lease := range leases {
		change := ServiceSetChanged{Set: cloneServiceSet(set)}
		if err := s.context.Send(lease.Subscriber, ServiceSetChangedCommand, change); err != nil {
			s.context.Metrics().Inc("servicegroup_notify_failed_total")
			continue
		}
		s.context.Metrics().Inc("servicegroup_notify_succeeded_total")
	}
}

func (s *directoryService) updateWatcherGauge() {
	s.context.Metrics().SetGauge("servicegroup_watchers", int64(s.watcherCount))
}

func codeFromError(err error) errorCode {
	switch {
	case err == nil:
		return responseOK
	case errors.Is(err, ErrInvalidGroup):
		return responseInvalidGroup
	case errors.Is(err, ErrInvalidServiceSet):
		return responseInvalidServiceSet
	case errors.Is(err, ErrGroupNotFound):
		return responseGroupNotFound
	case errors.Is(err, ErrVersionConflict):
		return responseVersionConflict
	case errors.Is(err, ErrVersionExhausted):
		return responseVersionExhausted
	case errors.Is(err, ErrUnauthorized):
		return responseUnauthorized
	case errors.Is(err, ErrInvalidWatch):
		return responseInvalidWatch
	case errors.Is(err, ErrWatchExpired):
		return responseWatchExpired
	case errors.Is(err, ErrWatchOwnerMismatch):
		return responseWatchOwnerMismatch
	default:
		return responseInvalidRequest
	}
}

func errorFromCode(code errorCode) error {
	switch code {
	case responseOK:
		return nil
	case responseInvalidGroup:
		return ErrInvalidGroup
	case responseInvalidServiceSet:
		return ErrInvalidServiceSet
	case responseGroupNotFound:
		return ErrGroupNotFound
	case responseVersionConflict:
		return ErrVersionConflict
	case responseVersionExhausted:
		return ErrVersionExhausted
	case responseUnauthorized:
		return ErrUnauthorized
	case responseInvalidWatch:
		return ErrInvalidWatch
	case responseWatchExpired:
		return ErrWatchExpired
	case responseWatchOwnerMismatch:
		return ErrWatchOwnerMismatch
	case responseInvalidRequest:
		return ErrInvalidResponse
	default:
		return ErrInvalidResponse
	}
}
