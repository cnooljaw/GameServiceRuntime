package drain

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	defaultLeaseTTL      = 30 * time.Second
	defaultSweepInterval = 5 * time.Second
)

type visitorRegistryService struct {
	config         VisitorRegistryConfig
	context        gsr.ServiceContext
	authorityEpoch uint64
	leases         map[gsr.ServiceRef]map[gsr.ServiceRef]VisitorLease
	nextGeneration uint64
	sweepScheduled bool
}

// NewVisitorRegistryService creates the single owner of Visitor lease facts.
func NewVisitorRegistryService(config VisitorRegistryConfig) (gsr.Service, error) {
	if config.LeaseTTL < 0 || config.SweepInterval < 0 {
		return nil, ErrInvalidConfig
	}
	if config.LeaseTTL == 0 {
		config.LeaseTTL = defaultLeaseTTL
	}
	if config.SweepInterval == 0 {
		config.SweepInterval = defaultSweepInterval
	}
	authorityEpoch, err := newAuthorityEpoch()
	if err != nil {
		return nil, fmt.Errorf("drain: create authority epoch: %w", err)
	}
	return &visitorRegistryService{
		config:         config,
		authorityEpoch: authorityEpoch,
		leases:         make(map[gsr.ServiceRef]map[gsr.ServiceRef]VisitorLease),
	}, nil
}

func (*visitorRegistryService) Commands() []gsr.CommandID {
	return []gsr.CommandID{
		commandAcquireVisitorLease,
		commandRenewVisitorLease,
		commandReleaseVisitorLease,
		commandListVisitors,
		commandSweepVisitors,
	}
}

func (s *visitorRegistryService) Init(serviceContext gsr.ServiceContext) error {
	s.context = serviceContext
	return nil
}

func (s *visitorRegistryService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	s.pruneExpired(s.context.Now())
	switch command.ID {
	case commandAcquireVisitorLease:
		request, ok := command.Payload.(acquireVisitorLeaseRequest)
		if !ok {
			return commandContext.Reply(leaseResponse{Error: responseInvalidRequest})
		}
		lease, err := s.acquire(commandContext.Source(), request)
		if err != nil {
			if codeFromError(err) == responseInvalidRequest {
				return err
			}
			return commandContext.Reply(leaseResponse{Error: codeFromError(err)})
		}
		return commandContext.Reply(leaseResponse{Lease: newWireVisitorLease(lease)})
	case commandRenewVisitorLease:
		request, ok := command.Payload.(renewVisitorLeaseRequest)
		if !ok {
			return commandContext.Reply(leaseResponse{Error: responseInvalidRequest})
		}
		lease, err := s.renew(commandContext.Source(), request.Lease.visitorLease())
		if err != nil {
			return commandContext.Reply(leaseResponse{Error: codeFromError(err)})
		}
		return commandContext.Reply(leaseResponse{Lease: newWireVisitorLease(lease)})
	case commandReleaseVisitorLease:
		request, ok := command.Payload.(releaseVisitorLeaseRequest)
		if !ok {
			return commandContext.Reply(emptyResponse{Error: responseInvalidRequest})
		}
		if err := s.release(commandContext.Source(), request.Lease.visitorLease()); err != nil {
			return commandContext.Reply(emptyResponse{Error: codeFromError(err)})
		}
		return commandContext.Reply(emptyResponse{})
	case commandListVisitors:
		request, ok := command.Payload.(listVisitorsRequest)
		if !ok {
			return commandContext.Reply(listVisitorsResponse{Error: responseInvalidRequest})
		}
		visitors, err := s.list(request.Target.serviceRef())
		if err != nil {
			return commandContext.Reply(listVisitorsResponse{Error: codeFromError(err)})
		}
		response := listVisitorsResponse{Visitors: make([]wireVisitorRef, len(visitors))}
		for index, visitor := range visitors {
			response.Visitors[index] = newWireVisitorRef(visitor)
		}
		return commandContext.Reply(response)
	case commandSweepVisitors:
		if _, ok := command.Payload.(sweepVisitorsRequest); !ok {
			return ErrInvalidLease
		}
		s.sweepScheduled = false
		if len(s.leases) > 0 {
			return s.scheduleSweep()
		}
		return nil
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (s *visitorRegistryService) Stop(context.Context) error {
	s.leases = make(map[gsr.ServiceRef]map[gsr.ServiceRef]VisitorLease)
	s.sweepScheduled = false
	s.updateLeaseGauges()
	return nil
}

func (s *visitorRegistryService) Close() error {
	s.leases = nil
	s.context = nil
	return nil
}

func (s *visitorRegistryService) acquire(source gsr.ServiceRef, request acquireVisitorLeaseRequest) (VisitorLease, error) {
	target := request.Target.serviceRef()
	visitor := request.Visitor.serviceRef()
	if !validServiceRef(target) {
		return VisitorLease{}, ErrInvalidTarget
	}
	if !validServiceRef(visitor) {
		return VisitorLease{}, ErrInvalidVisitor
	}
	if source != visitor {
		return VisitorLease{}, ErrLeaseOwnerMismatch
	}
	generation, err := s.nextLeaseGeneration()
	if err != nil {
		return VisitorLease{}, err
	}
	if !s.sweepScheduled {
		if err := s.scheduleSweep(); err != nil {
			return VisitorLease{}, err
		}
	}
	s.nextGeneration = generation
	lease := VisitorLease{
		Target:         target,
		Visitor:        visitor,
		AuthorityEpoch: s.authorityEpoch,
		Generation:     generation,
		Weak:           request.Weak,
		ExpiresAt:      s.context.Now().Add(s.config.LeaseTTL),
	}
	targetLeases := s.leases[target]
	if targetLeases == nil {
		targetLeases = make(map[gsr.ServiceRef]VisitorLease)
		s.leases[target] = targetLeases
	}
	targetLeases[visitor] = lease
	s.context.Metrics().Inc("visitor_acquire_total")
	s.updateLeaseGauges()
	return cloneLease(lease), nil
}

func (s *visitorRegistryService) renew(source gsr.ServiceRef, lease VisitorLease) (VisitorLease, error) {
	if !validLease(lease) {
		return VisitorLease{}, ErrInvalidLease
	}
	if source != lease.Visitor {
		return VisitorLease{}, ErrLeaseOwnerMismatch
	}
	targetLeases := s.leases[lease.Target]
	current, exists := targetLeases[lease.Visitor]
	if !exists || !sameLease(current, lease) {
		return VisitorLease{}, ErrLeaseExpired
	}
	expiresAt := s.context.Now().Add(s.config.LeaseTTL)
	if !expiresAt.After(current.ExpiresAt) {
		expiresAt = current.ExpiresAt.Add(time.Nanosecond)
	}
	current.ExpiresAt = expiresAt
	targetLeases[current.Visitor] = current
	s.context.Metrics().Inc("visitor_renew_total")
	return cloneLease(current), nil
}

func (s *visitorRegistryService) release(source gsr.ServiceRef, lease VisitorLease) error {
	if !validLease(lease) {
		return ErrInvalidLease
	}
	if source != lease.Visitor {
		return ErrLeaseOwnerMismatch
	}
	targetLeases := s.leases[lease.Target]
	current, exists := targetLeases[lease.Visitor]
	if !exists || !sameLease(current, lease) {
		return ErrLeaseExpired
	}
	delete(targetLeases, lease.Visitor)
	if len(targetLeases) == 0 {
		delete(s.leases, lease.Target)
	}
	s.context.Metrics().Inc("visitor_release_total")
	s.updateLeaseGauges()
	return nil
}

func (s *visitorRegistryService) list(target gsr.ServiceRef) ([]VisitorRef, error) {
	if !validServiceRef(target) {
		return nil, ErrInvalidTarget
	}
	targetLeases := s.leases[target]
	visitors := make([]VisitorRef, 0, len(targetLeases))
	for _, lease := range targetLeases {
		visitors = append(visitors, VisitorRef{
			Visitor:    lease.Visitor,
			Generation: lease.Generation,
			Weak:       lease.Weak,
			ExpiresAt:  lease.ExpiresAt,
		})
	}
	sortVisitorRefs(visitors)
	return cloneVisitorRefs(visitors), nil
}

func (s *visitorRegistryService) nextLeaseGeneration() (uint64, error) {
	if s.nextGeneration == ^uint64(0) {
		return 0, ErrLeaseExhausted
	}
	return s.nextGeneration + 1, nil
}

func (s *visitorRegistryService) scheduleSweep() error {
	if _, err := s.context.After(s.config.SweepInterval, commandSweepVisitors, sweepVisitorsRequest{}); err != nil {
		return err
	}
	s.sweepScheduled = true
	return nil
}

func (s *visitorRegistryService) pruneExpired(now time.Time) {
	expired := uint64(0)
	for target, targetLeases := range s.leases {
		for visitor, lease := range targetLeases {
			if now.Before(lease.ExpiresAt) {
				continue
			}
			delete(targetLeases, visitor)
			expired++
		}
		if len(targetLeases) == 0 {
			delete(s.leases, target)
		}
	}
	if expired == 0 {
		return
	}
	s.context.Metrics().Add("visitor_expired_total", expired)
	s.updateLeaseGauges()
}

func (s *visitorRegistryService) updateLeaseGauges() {
	leases := int64(0)
	strong := int64(0)
	for _, targetLeases := range s.leases {
		for _, lease := range targetLeases {
			leases++
			if !lease.Weak {
				strong++
			}
		}
	}
	s.context.Metrics().SetGauge("visitor_leases", leases)
	s.context.Metrics().SetGauge("visitor_strong_leases", strong)
}

func newAuthorityEpoch() (uint64, error) {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return 0, err
	}
	epoch := binary.BigEndian.Uint64(data[:])
	if epoch == 0 {
		return 1, nil
	}
	return epoch, nil
}
