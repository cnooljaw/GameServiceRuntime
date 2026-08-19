package nhsk

import (
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
)

// ReplayPlayerSnapshot is the four-seat player metadata captured when an NHSK
// subgame starts. Platform is the old CltID projection; CntID is deliberately
// not part of the replay model.
type ReplayPlayerSnapshot struct {
	SeatID    uint8
	Player    game.PlayerID
	UserID    uint32
	Nickname  string
	InitScore int32
	Platform  uint32
	Dress     string
	Automated bool
}

// ReplayStartSnapshot is the immutable input captured before the first
// client-facing output of one NHSK subgame. Hands are the final dealt hands,
// including any custom-deck or ordinary-deal adjustment.
type ReplayStartSnapshot struct {
	BattleID     game.BattleID
	Identity     BattleIdentity
	GameNum      uint16
	SubgameNum   uint16
	StartedAt    time.Time
	ReplayName   string
	RoundContext UpdateRoundContextRequest
	Players      [4]ReplayPlayerSnapshot
	Hands        [4][]byte
	BankerSeat   uint8
}

// Valid reports whether the start snapshot contains a complete four-seat
// subgame input. It intentionally validates only data available at Start;
// replay naming, UID and XML-specific rules remain later builder concerns.
func (snapshot ReplayStartSnapshot) Valid() bool {
	if snapshot.BattleID == 0 || snapshot.Identity.BattleID != snapshot.BattleID || snapshot.Identity.ProductID == 0 || snapshot.Identity.MatchID == 0 || snapshot.GameNum == 0 || snapshot.SubgameNum == 0 || snapshot.StartedAt.IsZero() || snapshot.ReplayName == "" || snapshot.BankerSeat >= 4 {
		return false
	}
	seenPlayers := make(map[game.PlayerID]struct{}, len(snapshot.Players))
	seenUsers := make(map[uint32]struct{}, len(snapshot.Players))
	for seat, player := range snapshot.Players {
		if player.SeatID != uint8(seat) || player.Player == "" || player.UserID == 0 || len(snapshot.Hands[seat]) != 26 {
			return false
		}
		if _, exists := seenPlayers[player.Player]; exists {
			return false
		}
		if _, exists := seenUsers[player.UserID]; exists {
			return false
		}
		seenPlayers[player.Player] = struct{}{}
		seenUsers[player.UserID] = struct{}{}
	}
	return true
}

// ReplayDocument is the in-memory replay document under construction for one
// subgame. The first slice freezes only the Start snapshot; moves, settlement,
// summary and serialization are added by later replay slices.
type ReplayDocument struct {
	start ReplayStartSnapshot
}

// NewReplayDocument creates a replay document that owns a deep copy of its
// start snapshot. The caller may safely reuse or mutate the input afterward.
func NewReplayDocument(snapshot ReplayStartSnapshot) ReplayDocument {
	return ReplayDocument{start: cloneReplayStartSnapshot(snapshot)}
}

// StartSnapshot returns a deep copy of the frozen subgame-start input.
func (document ReplayDocument) StartSnapshot() ReplayStartSnapshot {
	return cloneReplayStartSnapshot(document.start)
}

// Clone returns an independent copy of the in-memory replay document.
func (document ReplayDocument) Clone() ReplayDocument {
	return NewReplayDocument(document.start)
}

func cloneReplayStartSnapshot(snapshot ReplayStartSnapshot) ReplayStartSnapshot {
	for seat := range snapshot.Hands {
		snapshot.Hands[seat] = append([]byte(nil), snapshot.Hands[seat]...)
	}
	return snapshot
}
