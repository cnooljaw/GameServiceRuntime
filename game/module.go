package game

import (
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// PlayerEventKind identifies a lifecycle event dispatched inside PlayerService's Mailbox.
type PlayerEventKind string

const (
	// PlayerActivated is sent during PlayerService initialization.
	PlayerActivated PlayerEventKind = "activated"
	// PlayerOnline is sent after a newer connection generation becomes reachable.
	PlayerOnline PlayerEventKind = "online"
	// PlayerOffline is sent after the current connection generation becomes unreachable.
	PlayerOffline PlayerEventKind = "offline"
	// PlayerBackup is sent before PlayerService collects Module snapshots.
	PlayerBackup PlayerEventKind = "backup"
)

// PlayerEvent is one explicit PlayerService-to-module lifecycle notification.
type PlayerEvent struct {
	Kind       PlayerEventKind
	Generation string
	At         time.Time
}

// PlayerContext exposes the current Command and Player effects while PlayerService dispatches one Command or Event.
type PlayerContext interface {
	gsr.CommandContext
	PlayerID() PlayerID
	AccountID() AccountID
	Now() time.Time
	Send(gsr.ServiceRef, gsr.CommandID, any) error
}

// PlayerModule is a PlayerService-local state extension with declared Commands and snapshots.
type PlayerModule interface {
	Name() string
	Commands() []gsr.CommandID
	Handle(PlayerContext, gsr.Command) error
	HandleEvent(PlayerContext, PlayerEvent) error
	Snapshot(PlayerContext) ([]byte, error)
}
