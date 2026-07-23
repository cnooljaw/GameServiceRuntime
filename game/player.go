package game

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// SessionIdentity is authenticated entry identity without any ticket or secret material.
type SessionIdentity struct {
	Player  PlayerID
	Account AccountID
}

// PlayerState is PlayerService-owned visible player state.
type PlayerState struct {
	Player  PlayerID
	Account AccountID
	Online  bool
	Room    gsr.ServiceRef
	Battle  gsr.ServiceRef
}

// PlayerSnapshot is an independent Player state and per-module byte projection.
type PlayerSnapshot struct {
	State   PlayerState
	Modules map[string][]byte
}

// PlayerConfig configures one PlayerService identity and co-located modules.
type PlayerConfig struct {
	Identity SessionIdentity
	Modules  []PlayerModule
}

// PlayerPresence fences a Player online or offline transition by application-defined generation.
type PlayerPresence struct {
	Identity   SessionIdentity
	Generation string
}

// PlayerBinding binds a Room or Battle reference under one idempotency key.
type PlayerBinding struct {
	RequestID RequestID
	Ref       gsr.ServiceRef
}

// PlayerReconnectSnapshot carries a delayed Battle projection back to a Player.
type PlayerReconnectSnapshot struct {
	Player    PlayerID
	RequestID RequestID
	State     []byte
}

// PlayerService owns one Player's identity, reachability, bindings and modules.
type PlayerService struct {
	identity     SessionIdentity
	state        PlayerState
	generation   string
	modules      map[string]PlayerModule
	moduleNames  []string
	commands     []gsr.CommandID
	context      gsr.ServiceContext
	roomBindings map[RequestID]PlayerBinding
	battleBinds  map[RequestID]PlayerBinding
	reconnect    []byte
}

// NewPlayerService validates module ownership and creates an uninitialized PlayerService.
func NewPlayerService(config PlayerConfig) (*PlayerService, error) {
	if validateID(config.Identity.Player) != nil || validateID(config.Identity.Account) != nil {
		return nil, ErrInvalidConfig
	}
	modules := make(map[string]PlayerModule, len(config.Modules))
	commands := []gsr.CommandID{SetPlayerOnlineCommand, SetPlayerOfflineCommand, SetPlayerRoomCommand, SetPlayerBattleCommand, GetPlayerSnapshotCommand, ApplyPlayerReconnectSnapshotCommand, BackupPlayerCommand}
	seenCommands := make(map[gsr.CommandID]struct{}, len(commands))
	for _, command := range commands {
		seenCommands[command] = struct{}{}
	}
	for _, module := range config.Modules {
		if isNil(module) || !validModuleName(module.Name()) {
			return nil, ErrInvalidConfig
		}
		moduleCommands := module.Commands()
		if _, exists := modules[module.Name()]; exists || (len(moduleCommands) > 0 && !strictCommandIDs(moduleCommands)) {
			return nil, ErrInvalidConfig
		}
		for _, command := range moduleCommands {
			if _, exists := seenCommands[command]; exists {
				return nil, ErrInvalidConfig
			}
			seenCommands[command] = struct{}{}
			commands = append(commands, command)
		}
		modules[module.Name()] = module
	}
	moduleNames := make([]string, 0, len(modules))
	for name := range modules {
		moduleNames = append(moduleNames, name)
	}
	sort.Strings(moduleNames)
	return &PlayerService{identity: config.Identity, state: PlayerState{Player: config.Identity.Player, Account: config.Identity.Account}, modules: modules, moduleNames: moduleNames, commands: commands, roomBindings: make(map[RequestID]PlayerBinding), battleBinds: make(map[RequestID]PlayerBinding)}, nil
}

// Commands declares Player reserved and module Commands.
func (s *PlayerService) Commands() []gsr.CommandID {
	return append([]gsr.CommandID(nil), s.commands...)
}

// Init dispatches the explicit PlayerActivated event in PlayerService's initialization boundary.
func (s *PlayerService) Init(serviceContext gsr.ServiceContext) error {
	if isNil(serviceContext) {
		return ErrInvalidConfig
	}
	s.context = serviceContext
	return s.withContext(initPlayerCommandContext{self: serviceContext.Self()}, func(ctx *playerCommandContext) error {
		return s.dispatchEvent(ctx, PlayerEvent{Kind: PlayerActivated, At: serviceContext.Now()})
	})
}

// Handle serializes Player state and module command handling.
func (s *PlayerService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case SetPlayerOnlineCommand:
		presence, ok := command.Payload.(PlayerPresence)
		if !ok {
			return ErrInvalidCommand
		}
		return s.online(commandContext, presence)
	case SetPlayerOfflineCommand:
		presence, ok := command.Payload.(PlayerPresence)
		if !ok {
			return ErrInvalidCommand
		}
		return s.offline(commandContext, presence)
	case SetPlayerRoomCommand, SetPlayerBattleCommand:
		binding, ok := command.Payload.(PlayerBinding)
		if !ok {
			return ErrInvalidCommand
		}
		return s.bind(commandContext, command.ID, binding)
	case GetPlayerSnapshotCommand:
		return s.replySnapshot(commandContext)
	case ApplyPlayerReconnectSnapshotCommand:
		projection, ok := command.Payload.(PlayerReconnectSnapshot)
		if !ok || projection.Player != s.identity.Player || validateRequestID(projection.RequestID) != nil {
			return ErrInvalidCommand
		}
		s.reconnect = append([]byte(nil), projection.State...)
		return s.replySnapshot(commandContext)
	case BackupPlayerCommand:
		return s.backup(commandContext)
	default:
		module, exists := s.moduleFor(command.ID)
		if !exists {
			return gsr.ErrCommandNotRegistered
		}
		return s.withContext(commandContext, func(ctx *playerCommandContext) error { return module.Handle(ctx, command) })
	}
}

// Stop does not create Player business transitions.
func (*PlayerService) Stop(context.Context) error { return nil }

// Close releases PlayerService's Runtime context.
func (s *PlayerService) Close() error { s.context = nil; return nil }

func (s *PlayerService) online(commandContext gsr.CommandContext, presence PlayerPresence) error {
	if !sameIdentity(presence.Identity, s.identity) || !validText(presence.Generation, maxBusinessIDBytes) {
		return ErrUnauthorized
	}
	if s.state.Online && s.generation == presence.Generation {
		return s.replySnapshot(commandContext)
	}
	s.generation = presence.Generation
	s.state.Online = true
	if err := s.withContext(commandContext, func(ctx *playerCommandContext) error {
		return s.dispatchEvent(ctx, PlayerEvent{Kind: PlayerOnline, Generation: presence.Generation, At: s.context.Now()})
	}); err != nil {
		return err
	}
	return s.replySnapshot(commandContext)
}

func (s *PlayerService) offline(commandContext gsr.CommandContext, presence PlayerPresence) error {
	if !sameIdentity(presence.Identity, s.identity) || !validText(presence.Generation, maxBusinessIDBytes) {
		return ErrUnauthorized
	}
	if !s.state.Online || s.generation != presence.Generation {
		return s.replySnapshot(commandContext)
	}
	s.state.Online = false
	if err := s.withContext(commandContext, func(ctx *playerCommandContext) error {
		return s.dispatchEvent(ctx, PlayerEvent{Kind: PlayerOffline, Generation: presence.Generation, At: s.context.Now()})
	}); err != nil {
		return err
	}
	return s.replySnapshot(commandContext)
}

func (s *PlayerService) bind(commandContext gsr.CommandContext, command gsr.CommandID, binding PlayerBinding) error {
	if validateRequestID(binding.RequestID) != nil || (binding.Ref != (gsr.ServiceRef{}) && !validServiceRef(binding.Ref)) {
		return ErrInvalidCommand
	}
	bindings := s.roomBindings
	if command == SetPlayerBattleCommand {
		bindings = s.battleBinds
	}
	if current, exists := bindings[binding.RequestID]; exists {
		if !reflect.DeepEqual(current, binding) {
			return ErrRequestConflict
		}
		return s.replySnapshot(commandContext)
	}
	bindings[binding.RequestID] = binding
	if command == SetPlayerRoomCommand {
		s.state.Room = binding.Ref
	} else {
		s.state.Battle = binding.Ref
	}
	return s.replySnapshot(commandContext)
}

func (s *PlayerService) backup(commandContext gsr.CommandContext) error {
	if err := s.withContext(commandContext, func(ctx *playerCommandContext) error {
		return s.dispatchEvent(ctx, PlayerEvent{Kind: PlayerBackup, Generation: s.generation, At: s.context.Now()})
	}); err != nil {
		return err
	}
	return s.replySnapshot(commandContext)
}

func (s *PlayerService) replySnapshot(commandContext gsr.CommandContext) error {
	snapshot, err := s.snapshot(commandContext)
	if err != nil {
		return err
	}
	return reply(commandContext, snapshot)
}

func (s *PlayerService) snapshot(commandContext gsr.CommandContext) (PlayerSnapshot, error) {
	result := PlayerSnapshot{State: s.state, Modules: make(map[string][]byte, len(s.modules))}
	if err := s.withContext(commandContext, func(ctx *playerCommandContext) error {
		for _, name := range s.moduleNames {
			bytes, err := s.modules[name].Snapshot(ctx)
			if err != nil {
				return err
			}
			result.Modules[name] = append([]byte(nil), bytes...)
		}
		return nil
	}); err != nil {
		return PlayerSnapshot{}, err
	}
	return clonePlayerSnapshot(result), nil
}

func (s *PlayerService) dispatchEvent(ctx *playerCommandContext, event PlayerEvent) error {
	for _, name := range s.moduleNames {
		if err := s.modules[name].HandleEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (s *PlayerService) moduleFor(command gsr.CommandID) (PlayerModule, bool) {
	for _, name := range s.moduleNames {
		for _, declared := range s.modules[name].Commands() {
			if declared == command {
				return s.modules[name], true
			}
		}
	}
	return nil, false
}

func (s *PlayerService) withContext(command gsr.CommandContext, fn func(*playerCommandContext) error) error {
	active := true
	ctx := &playerCommandContext{service: s, command: command, active: &active}
	err := fn(ctx)
	active = false
	return err
}

type playerCommandContext struct {
	service *PlayerService
	command gsr.CommandContext
	active  *bool
}

func (c *playerCommandContext) Self() gsr.ServiceRef   { return c.command.Self() }
func (c *playerCommandContext) Source() gsr.ServiceRef { return c.command.Source() }
func (c *playerCommandContext) Reply(value any) error  { return c.command.Reply(value) }
func (c *playerCommandContext) PlayerID() PlayerID     { return c.service.identity.Player }
func (c *playerCommandContext) AccountID() AccountID   { return c.service.identity.Account }
func (c *playerCommandContext) Now() time.Time         { return c.service.context.Now() }
func (c *playerCommandContext) Send(target gsr.ServiceRef, command gsr.CommandID, payload any) error {
	if c.active == nil || !*c.active || !validServiceRef(target) {
		return ErrUnavailable
	}
	return c.service.context.Send(target, command, payload)
}

type initPlayerCommandContext struct{ self gsr.ServiceRef }

func (c initPlayerCommandContext) Self() gsr.ServiceRef   { return c.self }
func (c initPlayerCommandContext) Source() gsr.ServiceRef { return c.self }
func (initPlayerCommandContext) Reply(any) error          { return gsr.ErrReplyUnavailable }

func validModuleName(name string) bool {
	if name == "" || len(name) > 64 || strings.TrimSpace(name) != name {
		return false
	}
	for _, character := range name {
		if character > 127 || !(character == '-' || character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}

func sameIdentity(left, right SessionIdentity) bool { return left == right }

func clonePlayerSnapshot(snapshot PlayerSnapshot) PlayerSnapshot {
	modules := snapshot.Modules
	snapshot.Modules = make(map[string][]byte, len(modules))
	for name, bytes := range modules {
		snapshot.Modules[name] = append([]byte(nil), bytes...)
	}
	return snapshot
}

var _ gsr.Service = (*PlayerService)(nil)
var _ gsr.CommandDeclarer = (*PlayerService)(nil)
