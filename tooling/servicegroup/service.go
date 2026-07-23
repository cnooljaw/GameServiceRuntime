package servicegroup

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
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
	}, nil
}

func (*directoryService) Commands() []gsr.CommandID {
	return []gsr.CommandID{
		commandPublishServiceSet,
		commandGetServiceSet,
		commandSweepExpiredWatches,
	}
}

func (s *directoryService) Init(serviceContext gsr.ServiceContext) error {
	s.context = serviceContext
	return nil
}

func (s *directoryService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
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
	case commandSweepExpiredWatches:
		return nil
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (s *directoryService) Stop(context.Context) error {
	s.groups = make(map[GroupName]ServiceSet)
	return nil
}

func (s *directoryService) Close() error {
	s.groups = nil
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
	case responseInvalidRequest:
		return ErrInvalidResponse
	default:
		return ErrInvalidResponse
	}
}
