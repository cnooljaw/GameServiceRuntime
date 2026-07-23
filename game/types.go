package game

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	// StartBattleCommand starts one created Battle.
	StartBattleCommand gsr.CommandID = 0x03000101
	// GetBattleSnapshotCommand returns an independent BattleSnapshot.
	GetBattleSnapshotCommand gsr.CommandID = 0x03000102
	// SetParticipantConnectedCommand changes one participant reachability.
	SetParticipantConnectedCommand gsr.CommandID = 0x03000103
	// FinishBattleCommand freezes Battle settlement input.
	FinishBattleCommand gsr.CommandID = 0x03000104
	// ApplySettlementResultCommand applies a Wallet result to its requesting Battle.
	ApplySettlementResultCommand gsr.CommandID = 0x03000105
	// TimelineFireCommand is private to BattleService timer delivery.
	TimelineFireCommand gsr.CommandID = 0x03000201
	// JoinRoomCommand joins one Player to a Room.
	JoinRoomCommand gsr.CommandID = 0x03000301
	// LeaveRoomCommand removes one Player from a Room.
	LeaveRoomCommand gsr.CommandID = 0x03000302
	// StartRoomBattleCommand requests a Battle from the Room factory.
	StartRoomBattleCommand gsr.CommandID = 0x03000303
	// ApplyBattleCreatedCommand is a trusted Room factory result.
	ApplyBattleCreatedCommand gsr.CommandID = 0x03000304
	// ApplyBattleFinishedCommand is a trusted indexed Battle result.
	ApplyBattleFinishedCommand gsr.CommandID = 0x03000305
	// GetRoomSnapshotCommand returns an independent RoomSnapshot.
	GetRoomSnapshotCommand gsr.CommandID = 0x03000306
	// SetPlayerOnlineCommand records a newest Player connection generation.
	SetPlayerOnlineCommand gsr.CommandID = 0x03000401
	// SetPlayerOfflineCommand fences a Player offline transition by generation.
	SetPlayerOfflineCommand gsr.CommandID = 0x03000402
	// SetPlayerRoomCommand binds a Player to a Room reference.
	SetPlayerRoomCommand gsr.CommandID = 0x03000403
	// SetPlayerBattleCommand binds a Player to a Battle reference.
	SetPlayerBattleCommand gsr.CommandID = 0x03000404
	// GetPlayerSnapshotCommand returns an independent PlayerSnapshot.
	GetPlayerSnapshotCommand gsr.CommandID = 0x03000405
	// ApplyPlayerReconnectSnapshotCommand stores a delayed Battle projection.
	ApplyPlayerReconnectSnapshotCommand gsr.CommandID = 0x03000406
	// BackupPlayerCommand requests module snapshot generation without external I/O.
	BackupPlayerCommand gsr.CommandID = 0x03000407
	// CommitSettlementCommand accepts one idempotent Wallet settlement request.
	CommitSettlementCommand gsr.CommandID = 0x03000501
	// GetSettlementCommand returns the current Wallet settlement result.
	GetSettlementCommand gsr.CommandID = 0x03000502
	// GetBalanceCommand returns one known Wallet balance.
	GetBalanceCommand        gsr.CommandID = 0x03000503
	commandApplyLedgerResult gsr.CommandID = 0x030005fe
	commandRecoverSettlement gsr.CommandID = 0x030005ff
)

// PlayerID identifies one player-owned business state.
type PlayerID string

// AccountID identifies the authenticated account associated with a PlayerID.
type AccountID string

// BattleID identifies one short-lived game activity.
type BattleID string

// RoomID identifies one room-owned member and Battle index.
type RoomID string

// RequestID identifies one idempotent cross-Service business request.
type RequestID string

// BattleEpoch fences delayed Battle and Timeline input after a logical replacement.
type BattleEpoch uint64

// TimelineID identifies one timer intention within a Battle.
type TimelineID uint64

// TimelineRevision fences replaced Timeline timer deliveries.
type TimelineRevision uint64

// TimelineState is the lifecycle state of one Battle-local timer intention.
type TimelineState string

const (
	// TimelineScheduled is a timer intention awaiting a matching fire Command.
	TimelineScheduled TimelineState = "scheduled"
	// TimelineCancelled is a logically cancelled timer intention.
	TimelineCancelled TimelineState = "cancelled"
	// TimelineFired is a timer intention already delivered to BattleLogic.
	TimelineFired TimelineState = "fired"
)

// TimelineItem is an independent read-only projection of one Battle-local timer intention.
type TimelineItem struct {
	ID       TimelineID
	Revision TimelineRevision
	DueAt    time.Time
	Command  gsr.CommandID
	State    TimelineState
}

// TimelineSnapshot is a sorted projection of one Battle-local timeline.
type TimelineSnapshot struct {
	NextID TimelineID
	Items  []TimelineItem
}

// Timeline is available only from a BattleContext while its Handler is active.
type Timeline interface {
	After(time.Duration, gsr.CommandID, any) (TimelineID, error)
	At(time.Time, gsr.CommandID, any) (TimelineID, error)
	Replace(TimelineID, time.Duration, gsr.CommandID, any) (TimelineRevision, error)
	Cancel(TimelineID) bool
	Snapshot() TimelineSnapshot
}

// ServiceCreator creates a Service only at a composition-root or explicit factory boundary.
type ServiceCreator interface {
	CreateService(gsr.ServiceSpec) (gsr.ServiceRef, error)
}

// CommandRuntime is the narrow Runtime capability used by external composition code.
type CommandRuntime interface {
	Send(gsr.ServiceRef, gsr.CommandID, any) error
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

// Participant binds a stable PlayerID to an optional PlayerService target.
type Participant struct {
	Player PlayerID
	Ref    gsr.ServiceRef
}

// BattlePhase is the externally observable Battle state.
type BattlePhase string

const (
	// BattleCreated is the state before StartBattleCommand.
	BattleCreated BattlePhase = "created"
	// BattleRunning accepts game Logic Commands.
	BattleRunning BattlePhase = "running"
	// BattleSettling waits for terminal Wallet results.
	BattleSettling BattlePhase = "settling"
	// BattleFinished is the successful terminal state.
	BattleFinished BattlePhase = "finished"
	// BattleFailed is the failed terminal state.
	BattleFailed BattlePhase = "failed"
)

// ParticipantStatus is a Battle participant's connection reachability.
type ParticipantStatus string

const (
	// ParticipantConnected indicates the Player target is currently reachable.
	ParticipantConnected ParticipantStatus = "connected"
	// ParticipantOffline indicates the Player target is currently unavailable.
	ParticipantOffline ParticipantStatus = "offline"
)

// ParticipantConnection changes one Battle participant's current reachability.
type ParticipantConnection struct {
	Player    PlayerID
	Connected bool
}

// BattleSnapshot is an independent read-only projection of one Battle.
type BattleSnapshot struct {
	ID           BattleID
	Epoch        BattleEpoch
	Phase        BattlePhase
	Participants map[PlayerID]ParticipantStatus
	Timeline     TimelineSnapshot
	State        []byte
}

// BattleLogic contains game-specific state co-located with one BattleService.
type BattleLogic interface {
	Commands() []gsr.CommandID
	HandleBattle(BattleContext, gsr.Command) error
	Snapshot(BattleContext) ([]byte, error)
}

// BattleConfig configures a BattleService before composition-root creation.
type BattleConfig struct {
	ID           BattleID
	Epoch        BattleEpoch
	Participants []Participant
	Wallet       gsr.ServiceRef
	Logic        BattleLogic
	RandomSeed   uint64
}

// BattleContext is valid only during one BattleService Handle invocation.
type BattleContext interface {
	gsr.CommandContext
	BattleID() BattleID
	Epoch() BattleEpoch
	Now() time.Time
	Timeline() Timeline
	Finish(FinishBattle) error
	Broadcast(gsr.CommandID, any) BroadcastResult
	Send(gsr.ServiceRef, gsr.CommandID, any) error
}

// BroadcastResult summarizes a best-effort Battle broadcast.
type BroadcastResult struct {
	Delivered int
	Rejected  int
}

// Currency identifies a Wallet denomination.
type Currency string

// Amount is one signed Wallet balance delta.
type Amount int64

// SettlementState is the lifecycle state of one Wallet request.
type SettlementState string

const (
	// SettlementPending means the LedgerRunner has not supplied a terminal result.
	SettlementPending SettlementState = "pending"
	// SettlementCommitted means the ledger atomically committed the request.
	SettlementCommitted SettlementState = "committed"
	// SettlementRejected means the ledger made a terminal business rejection.
	SettlementRejected SettlementState = "rejected"
)

// Balance is one Wallet-owned player balance projection.
type Balance struct {
	Player   PlayerID
	Currency Currency
	Amount   Amount
}

// SettlementEntry is one signed balance delta inside a SettlementRequest.
type SettlementEntry struct {
	Player PlayerID
	Delta  Amount
}

// SettlementIntent is Battle-local settlement input before Battle assigns its own source reference.
type SettlementIntent struct {
	RequestID RequestID
	Currency  Currency
	Entries   []SettlementEntry
}

// FinishBattle freezes one Battle's settlement intents under an idempotency key.
type FinishBattle struct {
	RequestID   RequestID
	Settlements []SettlementIntent
}

// SettlementRequest is one canonical idempotent Wallet mutation request.
type SettlementRequest struct {
	RequestID RequestID
	Source    gsr.ServiceRef
	Currency  Currency
	Entries   []SettlementEntry
}

// SettlementResult is the terminal or pending projection of one Wallet request.
type SettlementResult struct {
	RequestID RequestID
	State     SettlementState
	Currency  Currency
	Balances  []Balance
	Reason    string
}

// LedgerRecord groups a request with its stored terminal result.
type LedgerRecord struct {
	Request SettlementRequest
	Result  SettlementResult
}

// LedgerStore persists idempotent Wallet ledger records outside Service Handlers.
type LedgerStore interface {
	Commit(context.Context, LedgerRecord) (SettlementResult, error)
	Lookup(context.Context, RequestID) (SettlementResult, bool, error)
}

// LedgerTask carries one Wallet request from an external Runner to a LedgerStore.
type LedgerTask struct {
	Wallet  gsr.ServiceRef
	Source  gsr.ServiceRef
	Request SettlementRequest
}

// LedgerExecutor accepts bounded external ledger work without exposing its worker implementation.
type LedgerExecutor interface {
	Submit(LedgerTask) error
}

// LedgerRuntime is the narrow Runtime capability held by LedgerRunner.
type LedgerRuntime interface {
	Send(gsr.ServiceRef, gsr.CommandID, any) error
}

// LedgerRunnerConfig configures the composition-root-owned LedgerRunner.
type LedgerRunnerConfig struct {
	Store     LedgerStore
	Workers   int
	QueueSize int
	Timeout   time.Duration
}

// WalletConfig configures a WalletService's task submission capability.
type WalletConfig struct {
	Executor   LedgerExecutor
	MaxPending int
	RunnerNode gsr.NodeID
}
