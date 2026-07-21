package discovery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	defaultLeaseTTL      = 30 * time.Second
	defaultSweepInterval = 5 * time.Second
)

type service struct {
	config         Config
	context        gsr.ServiceContext
	nodes          map[gsr.NodeID]NodeRecord
	names          map[gsr.ServiceName]nameBinding
	namesByLease   map[leaseKey]map[gsr.ServiceName]struct{}
	nextGeneration uint64
	sweepScheduled bool
}

type leaseKey struct {
	node       gsr.NodeID
	generation uint64
}

type nameBinding struct {
	ref   gsr.ServiceRef
	owner leaseKey
}

// NewService creates a DiscoveryService with private node and name registries.
func NewService(config Config) (gsr.Service, error) {
	if config.LeaseTTL < 0 || config.SweepInterval < 0 {
		return nil, ErrInvalidConfig
	}
	if config.LeaseTTL == 0 {
		config.LeaseTTL = defaultLeaseTTL
	}
	if config.SweepInterval == 0 {
		config.SweepInterval = defaultSweepInterval
	}
	return &service{
		config:       config,
		nodes:        make(map[gsr.NodeID]NodeRecord),
		names:        make(map[gsr.ServiceName]nameBinding),
		namesByLease: make(map[leaseKey]map[gsr.ServiceName]struct{}),
	}, nil
}

func (*service) Commands() []gsr.CommandID {
	return []gsr.CommandID{
		commandRegisterNode,
		commandHeartbeat,
		commandUnregisterNode,
		commandGetNode,
		commandListNodes,
		commandRegisterName,
		commandUnregisterName,
		commandResolveName,
		commandSweepExpired,
	}
}

func (s *service) Init(context gsr.ServiceContext) error {
	s.context = context
	return nil
}

func (s *service) Handle(context gsr.CommandContext, command gsr.Command) error {
	now := s.context.Now()
	s.pruneExpired(now)
	switch command.ID {
	case commandRegisterNode:
		request, ok := command.Payload.(registerNodeRequest)
		if !ok {
			return invalidPayload(command.ID)
		}
		lease, err := s.registerNode(now, request)
		return context.Reply(leaseResponse{Lease: lease, Error: codeFromError(err)})
	case commandHeartbeat:
		request, ok := command.Payload.(heartbeatRequest)
		if !ok {
			return invalidPayload(command.ID)
		}
		lease, err := s.heartbeat(now, request.Lease)
		return context.Reply(leaseResponse{Lease: lease, Error: codeFromError(err)})
	case commandUnregisterNode:
		request, ok := command.Payload.(unregisterNodeRequest)
		if !ok {
			return invalidPayload(command.ID)
		}
		err := s.unregisterNode(request.Lease)
		return context.Reply(emptyResponse{Error: codeFromError(err)})
	case commandGetNode:
		request, ok := command.Payload.(getNodeRequest)
		if !ok {
			return invalidPayload(command.ID)
		}
		node, err := s.getNode(request.Node)
		return context.Reply(nodeResponse{Node: node, Error: codeFromError(err)})
	case commandListNodes:
		if _, ok := command.Payload.(listNodesRequest); !ok {
			return invalidPayload(command.ID)
		}
		return context.Reply(nodesResponse{Nodes: s.listNodes()})
	case commandRegisterName:
		request, ok := command.Payload.(registerNameRequest)
		if !ok {
			return invalidPayload(command.ID)
		}
		err := s.registerName(request)
		return context.Reply(emptyResponse{Error: codeFromError(err)})
	case commandUnregisterName:
		request, ok := command.Payload.(unregisterNameRequest)
		if !ok {
			return invalidPayload(command.ID)
		}
		err := s.unregisterName(request)
		return context.Reply(emptyResponse{Error: codeFromError(err)})
	case commandResolveName:
		request, ok := command.Payload.(resolveNameRequest)
		if !ok {
			return invalidPayload(command.ID)
		}
		ref, err := s.resolveName(request.Name)
		return context.Reply(refResponse{Ref: ref, Error: codeFromError(err)})
	case commandSweepExpired:
		s.sweepScheduled = false
		if len(s.nodes) > 0 {
			return s.scheduleSweep()
		}
		return nil
	default:
		return fmt.Errorf("discovery: unsupported service command %d", command.ID)
	}
}

func (s *service) Stop(context.Context) error {
	s.nodes = make(map[gsr.NodeID]NodeRecord)
	s.names = make(map[gsr.ServiceName]nameBinding)
	s.namesByLease = make(map[leaseKey]map[gsr.ServiceName]struct{})
	s.sweepScheduled = false
	return nil
}

func (s *service) Close() error {
	s.nodes = nil
	s.names = nil
	s.namesByLease = nil
	s.context = nil
	return nil
}

func (s *service) registerNode(now time.Time, request registerNodeRequest) (NodeLease, error) {
	if request.Node == "" || strings.TrimSpace(request.Address) == "" {
		return NodeLease{}, ErrInvalidNode
	}
	if !s.sweepScheduled {
		if err := s.scheduleSweep(); err != nil {
			return NodeLease{}, err
		}
	}
	s.nextGeneration++
	if s.nextGeneration == 0 {
		s.nextGeneration++
	}
	expires := now.Add(s.config.LeaseTTL)
	record := NodeRecord{ID: request.Node, Address: request.Address, Generation: s.nextGeneration, LastSeen: now, ExpiresAt: expires}
	s.removeNode(request.Node)
	s.nodes[request.Node] = record
	return NodeLease{Node: request.Node, Generation: record.Generation, ExpiresAt: expires}, nil
}

func (s *service) heartbeat(now time.Time, lease NodeLease) (NodeLease, error) {
	if !validLease(lease) {
		return NodeLease{}, ErrInvalidNode
	}
	record, exists := s.nodes[lease.Node]
	if !exists || record.Generation != lease.Generation {
		return NodeLease{}, ErrLeaseExpired
	}
	record.LastSeen = now
	record.ExpiresAt = now.Add(s.config.LeaseTTL)
	s.nodes[lease.Node] = record
	return NodeLease{Node: record.ID, Generation: record.Generation, ExpiresAt: record.ExpiresAt}, nil
}

func (s *service) unregisterNode(lease NodeLease) error {
	if !validLease(lease) {
		return ErrInvalidNode
	}
	record, exists := s.nodes[lease.Node]
	if !exists || record.Generation != lease.Generation {
		return ErrLeaseExpired
	}
	s.removeNode(lease.Node)
	return nil
}

func (s *service) getNode(node gsr.NodeID) (NodeRecord, error) {
	if node == "" {
		return NodeRecord{}, ErrInvalidNode
	}
	record, exists := s.nodes[node]
	if !exists {
		return NodeRecord{}, ErrNodeNotFound
	}
	return record, nil
}

func (s *service) listNodes() []NodeRecord {
	nodes := make([]NodeRecord, 0, len(s.nodes))
	for _, record := range s.nodes {
		nodes = append(nodes, record)
	}
	sort.Slice(nodes, func(left, right int) bool { return nodes[left].ID < nodes[right].ID })
	return nodes
}

func (s *service) registerName(request registerNameRequest) error {
	if !validLease(request.Lease) || !validNameBinding(request.Lease, request.Name, request.Ref) {
		return ErrInvalidName
	}
	owner := leaseKey{node: request.Lease.Node, generation: request.Lease.Generation}
	if !s.leaseActive(owner) {
		return ErrLeaseExpired
	}
	if binding, exists := s.names[request.Name]; exists && binding.owner != owner {
		return ErrNameConflict
	}
	s.names[request.Name] = nameBinding{ref: request.Ref, owner: owner}
	owned := s.namesByLease[owner]
	if owned == nil {
		owned = make(map[gsr.ServiceName]struct{})
		s.namesByLease[owner] = owned
	}
	owned[request.Name] = struct{}{}
	return nil
}

func (s *service) unregisterName(request unregisterNameRequest) error {
	if !validLease(request.Lease) || !validNameBinding(request.Lease, request.Name, request.Ref) {
		return ErrInvalidName
	}
	owner := leaseKey{node: request.Lease.Node, generation: request.Lease.Generation}
	if !s.leaseActive(owner) {
		return ErrLeaseExpired
	}
	binding, exists := s.names[request.Name]
	if !exists || binding.owner != owner || binding.ref != request.Ref {
		return ErrNameNotFound
	}
	delete(s.names, request.Name)
	owned := s.namesByLease[owner]
	delete(owned, request.Name)
	if len(owned) == 0 {
		delete(s.namesByLease, owner)
	}
	return nil
}

func (s *service) resolveName(name gsr.ServiceName) (gsr.ServiceRef, error) {
	if strings.TrimSpace(string(name)) == "" {
		return gsr.ServiceRef{}, ErrInvalidName
	}
	binding, exists := s.names[name]
	if !exists || !s.leaseActive(binding.owner) {
		return gsr.ServiceRef{}, ErrNameNotFound
	}
	return binding.ref, nil
}

func (s *service) scheduleSweep() error {
	if _, err := s.context.After(s.config.SweepInterval, commandSweepExpired, nil); err != nil {
		return err
	}
	s.sweepScheduled = true
	return nil
}

func (s *service) pruneExpired(now time.Time) {
	for node, record := range s.nodes {
		if !record.ExpiresAt.After(now) {
			s.removeNode(node)
		}
	}
}

func (s *service) removeNode(node gsr.NodeID) {
	if record, exists := s.nodes[node]; exists {
		owner := leaseKey{node: node, generation: record.Generation}
		for name := range s.namesByLease[owner] {
			delete(s.names, name)
		}
		delete(s.namesByLease, owner)
	}
	delete(s.nodes, node)
}

func (s *service) leaseActive(owner leaseKey) bool {
	record, exists := s.nodes[owner.node]
	return exists && record.Generation == owner.generation
}

func invalidPayload(command gsr.CommandID) error {
	return fmt.Errorf("discovery: invalid payload for command %d", command)
}
