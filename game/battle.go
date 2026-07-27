package game

import (
	"context"
	"errors"
	"reflect"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// BattleService owns one Battle's phase, participant reachability, Timeline and game Logic.
type BattleService struct {
	id           BattleID
	epoch        BattleEpoch
	wallet       gsr.ServiceRef
	logic        BattleLogic
	participants map[PlayerID]Participant
	statuses     map[PlayerID]ParticipantStatus
	service      gsr.ServiceContext
	timeline     *battleTimeline
	phase        BattlePhase
	finish       FinishBattle
	settlements  map[RequestID]SettlementResult
}

// NewBattleService creates a created Battle with immutable identity and participant membership.
func NewBattleService(config BattleConfig) (*BattleService, error) {
	if err := validateBattleConfig(config); err != nil {
		return nil, err
	}
	if config.Epoch == 0 {
		config.Epoch = 1
	}
	participants := make(map[PlayerID]Participant, len(config.Participants))
	statuses := make(map[PlayerID]ParticipantStatus, len(config.Participants))
	for _, participant := range config.Participants {
		participants[participant.Player] = participant
		statuses[participant.Player] = ParticipantOffline
	}
	return &BattleService{id: config.ID, epoch: config.Epoch, wallet: config.Wallet, logic: config.Logic, participants: participants, statuses: statuses, phase: BattleCreated, settlements: make(map[RequestID]SettlementResult)}, nil
}

// CreateBattle creates a BattleService through a composition-root ServiceCreator.
func CreateBattle(creator ServiceCreator, name gsr.ServiceName, config BattleConfig) (gsr.ServiceRef, error) {
	if isNil(creator) {
		return gsr.ServiceRef{}, ErrInvalidConfig
	}
	battle, err := NewBattleService(config)
	if err != nil {
		return gsr.ServiceRef{}, err
	}
	return creator.CreateService(gsr.ServiceSpec{Name: name, Service: battle})
}

// Init captures the current Service capability and creates Battle-local Timeline state.
func (s *BattleService) Init(serviceContext gsr.ServiceContext) error {
	if isNil(serviceContext) {
		return ErrInvalidConfig
	}
	s.service = serviceContext
	s.timeline = &battleTimeline{battle: s, items: make(map[TimelineID]timelineRecord)}
	return nil
}

// Handle serializes all Battle state transitions and game Logic input.
func (s *BattleService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case StartBattleCommand:
		return s.start(commandContext, command.Payload)
	case GetBattleSnapshotCommand:
		if command.Payload != nil {
			if _, ok := command.Payload.(struct{}); !ok {
				return ErrInvalidCommand
			}
		}
		snapshot, err := s.snapshot(commandContext)
		if err != nil {
			return err
		}
		return reply(commandContext, snapshot)
	case SetParticipantConnectedCommand:
		request, ok := command.Payload.(ParticipantConnection)
		if !ok {
			return ErrInvalidCommand
		}
		return s.setParticipantConnected(commandContext, request)
	case FinishBattleCommand:
		request, ok := command.Payload.(FinishBattle)
		if !ok {
			return ErrInvalidCommand
		}
		return s.finishBattle(commandContext, request)
	case ApplySettlementResultCommand:
		result, ok := command.Payload.(SettlementResult)
		if !ok {
			return ErrInvalidSettlement
		}
		return s.applySettlement(commandContext, result)
	case TimelineFireCommand:
		fire, ok := command.Payload.(timelineFire)
		if !ok {
			return ErrInvalidCommand
		}
		return s.fire(commandContext, fire)
	default:
		if s.phase != BattleRunning {
			return ErrStateConflict
		}
		return s.withContext(commandContext, func(ctx *battleContext) error { return s.logic.HandleBattle(ctx, command) })
	}
}

// Stop only releases lifecycle-local state and never advances Battle business phase.
func (s *BattleService) Stop(context.Context) error {
	if s.timeline != nil {
		s.timeline.cancelAll()
	}
	return nil
}

// Close releases the Service capability after BattleService stops.
func (s *BattleService) Close() error {
	s.service = nil
	s.timeline = nil
	return nil
}

func (s *BattleService) start(commandContext gsr.CommandContext, payload any) error {
	if payload != nil {
		if _, ok := payload.(struct{}); !ok {
			return ErrInvalidCommand
		}
	}
	if s.phase != BattleCreated {
		return ErrStateConflict
	}
	s.phase = BattleRunning
	snapshot, err := s.snapshot(commandContext)
	if err != nil {
		return err
	}
	return reply(commandContext, snapshot)
}

func (s *BattleService) setParticipantConnected(commandContext gsr.CommandContext, request ParticipantConnection) error {
	if _, exists := s.participants[request.Player]; !exists {
		return ErrInvalidParticipant
	}
	if request.Connected {
		s.statuses[request.Player] = ParticipantConnected
	} else {
		s.statuses[request.Player] = ParticipantOffline
	}
	return reply(commandContext, s.statuses[request.Player])
}

func (s *BattleService) finishBattle(commandContext gsr.CommandContext, finish FinishBattle) error {
	if validateRequestID(finish.RequestID) != nil {
		return ErrInvalidRequestID
	}
	if s.phase != BattleRunning {
		if s.finish.RequestID == finish.RequestID {
			if !reflect.DeepEqual(s.finish, finish) {
				return ErrRequestConflict
			}
			return reply(commandContext, s.phase)
		}
		return ErrStateConflict
	}
	requests := make([]SettlementRequest, len(finish.Settlements))
	seen := make(map[RequestID]struct{}, len(finish.Settlements))
	for index, intent := range finish.Settlements {
		if validateSettlementIntent(intent) != nil {
			return ErrInvalidSettlement
		}
		if _, exists := seen[intent.RequestID]; exists {
			return ErrInvalidSettlement
		}
		seen[intent.RequestID] = struct{}{}
		requests[index] = SettlementRequest{RequestID: intent.RequestID, Source: s.service.Self(), Currency: intent.Currency, Entries: append([]SettlementEntry(nil), intent.Entries...)}
	}
	if len(requests) > 0 && !validServiceRef(s.wallet) {
		return ErrUnavailable
	}
	s.finish = cloneFinishBattle(finish)
	s.phase = BattleSettling
	for _, request := range requests {
		s.settlements[request.RequestID] = SettlementResult{RequestID: request.RequestID, State: SettlementPending, Currency: request.Currency}
		if err := s.service.Send(s.wallet, CommitSettlementCommand, cloneSettlementRequest(request)); err != nil {
			s.service.Metrics().Inc("battle_settlement_send_failed_total")
		}
	}
	if len(requests) == 0 {
		s.phase = BattleFinished
		s.timeline.cancelAll()
	}
	return reply(commandContext, s.phase)
}

func (s *BattleService) applySettlement(commandContext gsr.CommandContext, result SettlementResult) error {
	if commandContext.Source() != s.wallet {
		return ErrUnauthorized
	}
	current, exists := s.settlements[result.RequestID]
	if !exists {
		return ErrNotFound
	}
	if current.State != SettlementPending {
		return reply(commandContext, s.phase)
	}
	if result.State != SettlementCommitted && result.State != SettlementRejected || result.Currency != current.Currency {
		return ErrInvalidSettlement
	}
	s.settlements[result.RequestID] = cloneSettlementResult(result)
	if result.State == SettlementRejected {
		s.phase = BattleFailed
		s.timeline.cancelAll()
		return reply(commandContext, s.phase)
	}
	for _, pending := range s.settlements {
		if pending.State != SettlementCommitted {
			return reply(commandContext, s.phase)
		}
	}
	s.phase = BattleFinished
	s.timeline.cancelAll()
	return reply(commandContext, s.phase)
}

func (s *BattleService) fire(commandContext gsr.CommandContext, fire timelineFire) error {
	if fire.BattleID != s.id || fire.Epoch != s.epoch {
		s.service.Metrics().Inc("battle_timeline_ignored_total")
		return nil
	}
	record, exists := s.timeline.items[fire.ID]
	if !exists || record.item.State != TimelineScheduled || record.item.Revision != fire.Revision || record.item.Command != fire.Command {
		s.service.Metrics().Inc("battle_timeline_ignored_total")
		return nil
	}
	if s.phase != BattleRunning {
		s.service.Metrics().Inc("battle_timeline_ignored_total")
		return nil
	}
	record.item.State = TimelineFired
	s.timeline.items[fire.ID] = record
	payload, err := cloneTimelinePayload(record.payload)
	if err != nil {
		return err
	}
	return s.withContext(commandContext, func(ctx *battleContext) error {
		return s.logic.HandleBattle(ctx, gsr.Command{ID: record.item.Command, Payload: payload})
	})
}

func (s *BattleService) snapshot(commandContext gsr.CommandContext) (BattleSnapshot, error) {
	var state []byte
	err := s.withContext(commandContext, func(ctx *battleContext) error {
		value, err := s.logic.Snapshot(ctx)
		if err != nil {
			return err
		}
		state = append([]byte(nil), value...)
		return nil
	})
	if err != nil {
		return BattleSnapshot{}, err
	}
	participants := make(map[PlayerID]ParticipantStatus, len(s.statuses))
	for player, status := range s.statuses {
		participants[player] = status
	}
	return cloneBattleSnapshot(BattleSnapshot{ID: s.id, Epoch: s.epoch, Phase: s.phase, Participants: participants, Timeline: s.timeline.snapshot(), State: state}), nil
}

func (s *BattleService) withContext(commandContext gsr.CommandContext, fn func(*battleContext) error) error {
	active := true
	ctx := &battleContext{battle: s, command: commandContext, active: &active}
	defer func() { active = false }()
	return fn(ctx)
}

type battleContext struct {
	battle  *BattleService
	command gsr.CommandContext
	active  *bool
}

func (c *battleContext) Self() gsr.ServiceRef   { return c.command.Self() }
func (c *battleContext) Source() gsr.ServiceRef { return c.command.Source() }
func (c *battleContext) Reply(value any) error {
	if !c.usable() {
		return ErrContextExpired
	}
	return reply(c.command, value)
}
func (c *battleContext) BattleID() BattleID { return c.battle.id }
func (c *battleContext) Epoch() BattleEpoch { return c.battle.epoch }
func (c *battleContext) Now() time.Time     { return c.battle.service.Now() }
func (c *battleContext) Timeline() Timeline {
	return timelineHandle{timeline: c.battle.timeline, active: c.active}
}
func (c *battleContext) Finish(finish FinishBattle) error {
	if !c.usable() {
		return ErrContextExpired
	}
	return c.battle.finishBattle(c.command, finish)
}
func (c *battleContext) Broadcast(command gsr.CommandID, payload any) (BroadcastResult, error) {
	if !c.usable() {
		return BroadcastResult{}, ErrContextExpired
	}
	result := BroadcastResult{}
	for _, participant := range c.battle.participants {
		if !validServiceRef(participant.Ref) {
			continue
		}
		if err := c.battle.service.Send(participant.Ref, command, payload); err != nil {
			result.Rejected++
			continue
		}
		result.Delivered++
	}
	return result, nil
}

func (c *battleContext) Send(target gsr.ServiceRef, command gsr.CommandID, payload any) error {
	if !c.usable() {
		return ErrContextExpired
	}
	if !validServiceRef(target) {
		return ErrUnavailable
	}
	return c.battle.service.Send(target, command, payload)
}

func (c *battleContext) usable() bool { return c != nil && c.active != nil && *c.active }

func reply(commandContext gsr.CommandContext, value any) error {
	err := commandContext.Reply(value)
	if errors.Is(err, gsr.ErrReplyUnavailable) {
		return nil
	}
	return err
}
