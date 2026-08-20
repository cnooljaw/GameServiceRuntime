package nhsk

import (
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	// BeginCreateBattleCommand asks the NHSK Host to create a Battle asynchronously.
	BeginCreateBattleCommand gsr.CommandID = 0x04100101
	// GetCreateBattleOperationCommand reads one Host create operation.
	GetCreateBattleOperationCommand gsr.CommandID = 0x04100102
	// ResolveBattleCommand resolves an active BattleID to its current ServiceRef.
	ResolveBattleCommand gsr.CommandID = 0x04100103
	// RequestDeleteBattleCommand asks the NHSK Host to stop one Battle.
	RequestDeleteBattleCommand gsr.CommandID = 0x04100104
	// GetHostSnapshotCommand reads the Host's active and quarantined index.
	GetHostSnapshotCommand gsr.CommandID = 0x04100105
	// InitializeBattleCommand freezes one Battle's identity and game configuration.
	InitializeBattleCommand gsr.CommandID = 0x04100201
	// UpdatePlayersCommand atomically upserts Battle participants.
	UpdatePlayersCommand gsr.CommandID = 0x04100202
	// PrepareSubgameCommand moves an initialized Battle into a prepared subgame.
	PrepareSubgameCommand gsr.CommandID = 0x04100203
	// StartSubgameCommand starts the prepared NHSK subgame.
	StartSubgameCommand gsr.CommandID = 0x04100204
	// UpdateRoundContextCommand updates replay context for the next subgame.
	UpdateRoundContextCommand gsr.CommandID = 0x04100205
	// ExitPlayerCommand marks one player exited without deleting identity.
	ExitPlayerCommand gsr.CommandID = 0x04100206
	// UpdatePlayerDressCommand updates a player's next-subgame dress metadata.
	UpdatePlayerDressCommand gsr.CommandID = 0x04100207
	// ForceFinishSubgameCommand requests an exceptional local subgame finish.
	ForceFinishSubgameCommand gsr.CommandID = 0x04100208
	// ProvideCustomDeckCommand supplies an already-compatible custom deck for a prepared subgame.
	ProvideCustomDeckCommand gsr.CommandID = 0x04100209
	// PlayCardsCommand submits one player play or pass to an NHSK Battle.
	PlayCardsCommand gsr.CommandID = 0x04100301
	// PreviewCardSelectionCommand publishes one non-authoritative player selection.
	PreviewCardSelectionCommand gsr.CommandID = 0x04100302
	// SetPlayerAutoStateCommand changes one NHSK player's auto-play state.
	SetPlayerAutoStateCommand gsr.CommandID = 0x04100303
	// SetPlayerOfflineCommand records a player transport disconnect.
	SetPlayerOfflineCommand gsr.CommandID = 0x04100304
	// ReconnectPlayerCommand marks a player reachable again.
	ReconnectPlayerCommand gsr.CommandID = 0x04100305
	// RequestGameSceneCommand requests a receiver-specific scene restore.
	RequestGameSceneCommand gsr.CommandID = 0x04100306
	// RecordPropUseCommand records a supported prop use without a reply.
	RecordPropUseCommand gsr.CommandID = 0x04100307
	// CompleteSettlementCommand applies the terminal settlement response.
	CompleteSettlementCommand gsr.CommandID = 0x04100401
	// GetNHSKBattleSnapshotCommand returns a read-only Battle projection.
	GetNHSKBattleSnapshotCommand gsr.CommandID = 0x04100402
)

// GameDescriptor identifies the concrete game selected by this composition root.
type GameDescriptor struct {
	GameID   uint32
	GameName string
}

// NHSKDescriptor is the fixed descriptor for Ninghai Double Buckle.
var NHSKDescriptor = GameDescriptor{GameID: 82, GameName: "宁海双扣"}

// CommandResult is the stable reply for lifecycle and player state Commands.
type CommandResult struct {
	Accepted  bool
	Rejection string
}

// ActionResult is the stable reply for a card action or preview.
type ActionResult struct {
	Accepted  bool
	Rejection string
}

// HostCommandResult is the stable reply for Host lifecycle Commands.
type HostCommandResult struct {
	Accepted    bool
	Rejection   string
	OperationID HostOperationID
}

// SettlementCommandResult is the stable reply for settlement application.
type SettlementCommandResult struct {
	Accepted  bool
	Rejection string
}

// HostOperationID identifies an asynchronous Host lifecycle operation.
type HostOperationID uint64

// ConnectionGeneration identifies the physical GM connection used by a create request.
// It is zero when the caller is a direct Cluster caller.
type CreateBattleRequest struct {
	BattleID             game.BattleID
	IsNewbie             bool
	ConnectionGeneration ConnectionGeneration
}

// HostOperationPhase describes a create or stop operation.
type HostOperationPhase string

const (
	// HostOperationCreating means the factory has not returned a BattleRef.
	HostOperationCreating HostOperationPhase = "creating"
	// HostOperationCompleted means the requested lifecycle action completed.
	HostOperationCompleted HostOperationPhase = "completed"
	// HostOperationFailed means the requested lifecycle action failed.
	HostOperationFailed HostOperationPhase = "failed"
	// HostOperationStopping keeps the binding reserved until Runtime Stop returns.
	HostOperationStopping HostOperationPhase = "stopping"
)

// CreateBattleOperation is an independent Host operation projection.
type CreateBattleOperation struct {
	OperationID HostOperationID
	BattleID    game.BattleID
	Phase       HostOperationPhase
	Ref         gsr.ServiceRef
	Rejection   string
}

// GetCreateBattleOperationRequest reads one operation by ID.
type GetCreateBattleOperationRequest struct{ OperationID HostOperationID }

// ResolveBattleRequest resolves one business BattleID.
type ResolveBattleRequest struct{ BattleID game.BattleID }

// ResolveBattleResult is the Host's current active Battle binding.
type ResolveBattleResult struct {
	BattleID game.BattleID
	Ref      gsr.ServiceRef
}

// RequestDeleteBattleRequest asks the Host to stop one Battle.
type RequestDeleteBattleRequest struct {
	BattleID game.BattleID
	Ref      gsr.ServiceRef
}

// HostSnapshot is a read-only Host index summary.
type HostSnapshot struct {
	MaxActiveBattles uint32
	ActiveBattles    map[game.BattleID]gsr.ServiceRef
	Quarantined      []game.BattleID
}

// BattleIdentity freezes the Legacy/GameMaster identity for one Battle.
type BattleIdentity struct {
	BattleID     game.BattleID
	ProductID    uint32
	MatchID      uint32
	RoundID      uint32
	RoundUniCode string
}

// ReplayMetadata is the immutable INIT projection consumed only by the NHSK
// replay builder. Identity, progress limits and score conversion remain owned
// by their existing Battle fields and are not duplicated here.
type ReplayMetadata struct {
	MatchName string
	GameType  uint32
	ScoreType int32
	ScoreMode int32
	RoomID    uint32
	CreatorID uint32
}

// ReplayRuleSnapshot preserves old BaseRule values that are emitted into the
// replay XML but deliberately do not restore abandoned gameplay behavior.
type ReplayRuleSnapshot struct {
	TimeOutOver          bool
	VoiceMode            bool
	RandomSeatRoundStart bool
	GameNumToRandomSeat  int
}

// InitializeBattleRequest is the one-time Battle initialization payload.
type InitializeBattleRequest struct {
	Identity         BattleIdentity
	MaxGameNum       uint16
	MaxSubgameNum    uint16
	Fee              int32
	ScoreBase        int32
	ScoreDenominator int32
	ReplayMetadata   ReplayMetadata
	ReplayRules      ReplayRuleSnapshot
	// Rules is normalized at the Legacy adapter boundary. A nil value uses
	// DefaultNHSKConfig, which keeps direct Cluster callers independent from
	// the old comma-separated rule strings.
	Rules *NHSKConfig
}

// BattlePlayer is the normalized four-seat player record.
type BattlePlayer struct {
	Player game.PlayerID
	UserID uint32
	SeatID uint8
	Score  int32
	Exp    int32
	// PlayerState is preserved from UPDATE_PLAYER for the player's current
	// transport-facing state projection; settlement flags remain separate.
	PlayerState int32
	Nickname    string
	// ClientID is the legacy CltID projection used only as replay Platform.
	ClientID uint32
	// Dress is opaque replay metadata for the player's next subgame.
	Dress     string
	Automated bool
	Exited    bool
	IsBreak   bool
	IsSeal    bool
}

// UpdatePlayersRequest atomically upserts a batch of players.
type UpdatePlayersRequest struct{ Players []BattlePlayer }

// PrepareSubgameRequest selects the next game/subgame number.
type PrepareSubgameRequest struct{ GameNum, SubgameNum uint16 }

// ProvideCustomDeckRequest supplies an immutable custom-deck catalog for one prepared subgame.
// Data-source lookup and legacy-format conversion happen outside the Battle API.
type ProvideCustomDeckRequest struct {
	BattleID   game.BattleID
	GameNum    uint16
	SubgameNum uint16
	Catalog    CustomDeckCatalog
}

// UpdateRoundContextRequest updates replay metadata for the next subgame.
type UpdateRoundContextRequest struct {
	SecRoundTotal, SecRoundUsed uint32
	RoomInfo                    string
}

// ExitPlayerRequest marks one player exited.
type ExitPlayerRequest struct{ Player game.PlayerID }

// UpdatePlayerDressRequest updates a player's next-subgame dress identifier.
type UpdatePlayerDressRequest struct {
	Player game.PlayerID
	Dress  string
}

// SetPlayerOfflineRequest changes a player's current reachability.
type SetPlayerOfflineRequest struct {
	Player  game.PlayerID
	Offline bool
}

// RecordPropUseRequest is a successful external prop fact retained only in
// the current subgame replay.
type RecordPropUseRequest struct {
	SenderID  uint32
	PropID    string
	SendCount uint32
	TargetIDs []uint32
}

// ReconnectPlayerRequest identifies the player receiving a reconnect or scene restore.
type ReconnectPlayerRequest struct{ Player game.PlayerID }

// SettlementGain is one directed team score transfer from the old 0x8650
// comprehensive settlement message.
type SettlementGain struct {
	PayTeamID  uint32
	GainTeamID uint32
	Score      int32
}

// SettlementPlayerResult is one decoded player metadata record from the old
// 0x8650 comprehensive settlement message. Score and Exp are retained for
// protocol compatibility but are not authoritative Battle scores.
type SettlementPlayerResult struct {
	PlayerID uint32
	Flag     int32
	Score    int32
	Exp      int32
	TeamID   uint32
}

// CompleteSettlementRequest is the terminal settlement payload shared by the
// Legacy adapter and direct Cluster callers.
type CompleteSettlementRequest struct {
	Success    bool
	ResultType int32
	TeamCount  int32
	Gains      []SettlementGain
	Players    []SettlementPlayerResult
}

// NHSKBattleSnapshot is the read-only state projection exposed by a Battle.
type NHSKBattleSnapshot struct {
	BattleID     game.BattleID
	Phase        string
	Identity     BattleIdentity
	Players      []BattlePlayer
	ActivePlayer game.PlayerID
	VerifyCode   uint32
	Hands        map[game.PlayerID][]byte
	Auto         map[game.PlayerID]bool
	Revision     uint64
	TurnRevision uint64
	DeadlineUnix int64
}

// PlayCardsRequest submits one player play or pass candidate.
type PlayCardsRequest struct {
	Player     game.PlayerID
	Cards      []byte
	VerifyCode uint32
}

// PreviewCardSelectionRequest submits one non-authoritative player selection.
type PreviewCardSelectionRequest struct {
	Player game.PlayerID
	Cards  []byte
}

// SetPlayerAutoStateRequest changes one NHSK player's auto-play state.
type SetPlayerAutoStateRequest struct {
	Player  game.PlayerID
	Enabled bool
}
