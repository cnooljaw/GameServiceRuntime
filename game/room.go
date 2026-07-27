package game

import (
	"context"
	"reflect"
	"sort"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// RoomPhase is the lifecycle state of a RoomService.
type RoomPhase string

const (
	// RoomOpen accepts member and Battle-index mutations.
	RoomOpen RoomPhase = "open"
	// RoomClosed rejects mutable Room input after lifecycle closure.
	RoomClosed RoomPhase = "closed"
)

// RoomSnapshot is an independent projection of one Room's members and Battle index.
type RoomSnapshot struct {
	ID      RoomID
	Phase   RoomPhase
	Members []PlayerID
	Battles map[BattleID]gsr.ServiceRef
}

// BattleCreateRequest is the idempotent Room-to-factory Battle creation request.
type BattleCreateRequest struct {
	RequestID RequestID
	Room      RoomID
	Players   []PlayerID
}

// BattleCreatedResult is a trusted factory result that binds one Battle reference into Room.
type BattleCreatedResult struct {
	RequestID RequestID
	Battle    BattleID
	Ref       gsr.ServiceRef
}

// BattleFinishedNotice removes one matching Battle reference from Room.
type BattleFinishedNotice struct {
	Battle BattleID
	Ref    gsr.ServiceRef
}

// RoomFactory accepts a non-blocking Battle creation request at an application boundary.
type RoomFactory interface {
	RequestBattle(BattleCreateRequest) error
}

// RoomConfig configures a Room's member capacity and optional trusted Battle factory.
type RoomConfig struct {
	ID         RoomID
	Capacity   int
	Factory    RoomFactory
	FactoryRef gsr.ServiceRef
}

// RoomService owns Room members, pending requests and Battle reference index.
type RoomService struct {
	id         RoomID
	capacity   int
	factory    RoomFactory
	factoryRef gsr.ServiceRef
	phase      RoomPhase
	members    map[PlayerID]struct{}
	pending    map[RequestID]BattleCreateRequest
	battles    map[BattleID]gsr.ServiceRef
}

// NewRoomService creates an open RoomService with no members or Battle references.
func NewRoomService(config RoomConfig) (*RoomService, error) {
	if validateID(config.ID) != nil || config.Capacity <= 0 {
		return nil, ErrInvalidConfig
	}
	if isNil(config.Factory) {
		if config.Factory != nil || config.FactoryRef != (gsr.ServiceRef{}) {
			return nil, ErrInvalidConfig
		}
	} else if !validServiceRef(config.FactoryRef) {
		return nil, ErrInvalidConfig
	}
	return &RoomService{id: config.ID, capacity: config.Capacity, factory: config.Factory, factoryRef: config.FactoryRef, phase: RoomOpen, members: make(map[PlayerID]struct{}), pending: make(map[RequestID]BattleCreateRequest), battles: make(map[BattleID]gsr.ServiceRef)}, nil
}

// Commands declares all RoomService protocol Commands.

// Init performs no I/O and keeps Room state owned by its Mailbox.
func (*RoomService) Init(gsr.ServiceContext) error { return nil }

// Handle serializes Room membership, factory results and Battle index updates.
func (s *RoomService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	if s.phase != RoomOpen && command.ID != GetRoomSnapshotCommand {
		return ErrClosed
	}
	switch command.ID {
	case JoinRoomCommand:
		player, ok := command.Payload.(PlayerID)
		if !ok || validateID(player) != nil {
			return ErrInvalidID
		}
		if _, exists := s.members[player]; !exists {
			if len(s.members) == s.capacity {
				return ErrStateConflict
			}
			s.members[player] = struct{}{}
		}
		return reply(commandContext, s.snapshot())
	case LeaveRoomCommand:
		player, ok := command.Payload.(PlayerID)
		if !ok || validateID(player) != nil {
			return ErrInvalidID
		}
		if _, exists := s.members[player]; !exists {
			return ErrNotFound
		}
		delete(s.members, player)
		return reply(commandContext, s.snapshot())
	case StartRoomBattleCommand:
		request, ok := command.Payload.(BattleCreateRequest)
		if !ok {
			return ErrInvalidCommand
		}
		return s.start(commandContext, request)
	case ApplyBattleCreatedCommand:
		created, ok := command.Payload.(BattleCreatedResult)
		if !ok {
			return ErrInvalidCommand
		}
		return s.applyCreated(commandContext, created)
	case ApplyBattleFinishedCommand:
		notice, ok := command.Payload.(BattleFinishedNotice)
		if !ok {
			return ErrInvalidCommand
		}
		return s.applyFinished(commandContext, notice)
	case GetRoomSnapshotCommand:
		return reply(commandContext, s.snapshot())
	default:
		return gsr.ErrUnknownCommand
	}
}

// Stop does not stop indexed Battles or change Room business ownership.
func (*RoomService) Stop(context.Context) error { return nil }

// Close makes the Room closed for any direct in-process observation.
func (s *RoomService) Close() error { s.phase = RoomClosed; return nil }

func (s *RoomService) start(commandContext gsr.CommandContext, request BattleCreateRequest) error {
	if err := s.validateCreateRequest(request); err != nil {
		return err
	}
	if s.factory == nil {
		return ErrUnavailable
	}
	if current, exists := s.pending[request.RequestID]; exists {
		if !reflect.DeepEqual(current, request) {
			return ErrRequestConflict
		}
		return reply(commandContext, s.snapshot())
	}
	s.pending[request.RequestID] = cloneBattleCreateRequest(request)
	if err := s.factory.RequestBattle(cloneBattleCreateRequest(request)); err != nil {
		delete(s.pending, request.RequestID)
		return err
	}
	return reply(commandContext, s.snapshot())
}

func (s *RoomService) applyCreated(commandContext gsr.CommandContext, created BattleCreatedResult) error {
	if commandContext.Source() != s.factoryRef {
		return ErrUnauthorized
	}
	request, exists := s.pending[created.RequestID]
	if !exists {
		return ErrNotFound
	}
	if validateID(created.Battle) != nil || !validServiceRef(created.Ref) {
		return ErrInvalidCommand
	}
	if _, exists := s.battles[created.Battle]; exists {
		return ErrStateConflict
	}
	delete(s.pending, request.RequestID)
	s.battles[created.Battle] = created.Ref
	return reply(commandContext, s.snapshot())
}

func (s *RoomService) applyFinished(commandContext gsr.CommandContext, notice BattleFinishedNotice) error {
	if validateID(notice.Battle) != nil || !validServiceRef(notice.Ref) {
		return ErrInvalidCommand
	}
	ref, exists := s.battles[notice.Battle]
	if !exists || ref != notice.Ref {
		return ErrNotFound
	}
	if commandContext.Source() != ref {
		return ErrUnauthorized
	}
	delete(s.battles, notice.Battle)
	return reply(commandContext, s.snapshot())
}

func (s *RoomService) validateCreateRequest(request BattleCreateRequest) error {
	if validateRequestID(request.RequestID) != nil || request.Room != s.id || len(request.Players) == 0 {
		return ErrInvalidCommand
	}
	previous := PlayerID("")
	for index, player := range request.Players {
		if validateID(player) != nil || (index > 0 && player <= previous) {
			return ErrInvalidCommand
		}
		if _, exists := s.members[player]; !exists {
			return ErrStateConflict
		}
		previous = player
	}
	return nil
}

func (s *RoomService) snapshot() RoomSnapshot {
	members := sortedPlayerIDs(s.members)
	battles := make(map[BattleID]gsr.ServiceRef, len(s.battles))
	for battle, ref := range s.battles {
		battles[battle] = ref
	}
	return RoomSnapshot{ID: s.id, Phase: s.phase, Members: members, Battles: battles}
}

func cloneBattleCreateRequest(request BattleCreateRequest) BattleCreateRequest {
	request.Players = append([]PlayerID(nil), request.Players...)
	return request
}

var _ gsr.Service = (*RoomService)(nil)

func sortBattleIDs(values map[BattleID]gsr.ServiceRef) []BattleID {
	result := make([]BattleID, 0, len(values))
	for battle := range values {
		result = append(result, battle)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}
