package nhsk

import (
	"strconv"
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
	BattleID         game.BattleID
	Identity         BattleIdentity
	GameNum          uint16
	SubgameNum       uint16
	StartedAt        time.Time
	ReplayName       string
	ReplayUID        string
	RelativePath     string
	MaxGameNum       uint16
	MaxSubgameNum    uint16
	Fee              int32
	ScoreBase        int32
	ScoreDenominator int32
	ReplayMetadata   ReplayMetadata
	ReplayRules      ReplayRuleSnapshot
	RoundContext     UpdateRoundContextRequest
	Players          [4]ReplayPlayerSnapshot
	Hands            [4][]byte
	BankerSeat       uint8
}

// ReplayMoveKind identifies one in-memory NHSK replay event. Deal is one event
// containing all four final hands; per-seat child nodes are a later serializer
// concern.
type ReplayMoveKind string

const (
	// ReplayMoveDeal is the single initial-deal event for one subgame.
	ReplayMoveDeal ReplayMoveKind = "Deal"
	// ReplayMoveCurrentPoint records the scoring cards currently on the table.
	ReplayMoveCurrentPoint ReplayMoveKind = "CurrentPoint"
	// ReplayMoveOutCard records one accepted play or pass.
	ReplayMoveOutCard ReplayMoveKind = "OutCard"
	// ReplayMoveCatchPoint records the trick's scoring-card owner.
	ReplayMoveCatchPoint ReplayMoveKind = "CatchPoint"
	// ReplayMoveTurnEnd records the cumulative seat points after a trick.
	ReplayMoveTurnEnd ReplayMoveKind = "TurnEnd"
)

// ReplayMoveSource identifies the source attributed to one replay event. AI and
// timeout values are reserved for the later AI/timeout slice; the current
// Battle records player and ordinary auto-play actions.
type ReplayMoveSource string

const (
	// ReplayMoveSourceUnknown is an event with no known source.
	ReplayMoveSourceUnknown ReplayMoveSource = "unknown"
	// ReplayMoveSourceSystem is a server-generated replay event.
	ReplayMoveSourceSystem ReplayMoveSource = "system"
	// ReplayMoveSourcePlayer is a human player action.
	ReplayMoveSourcePlayer ReplayMoveSource = "player"
	// ReplayMoveSourceAI is an external AI action reserved for a later slice.
	ReplayMoveSourceAI ReplayMoveSource = "ai"
	// ReplayMoveSourceTimeout is a hard-timeout action reserved for a later slice.
	ReplayMoveSourceTimeout ReplayMoveSource = "timeout"
	// ReplayMoveSourceAuto is a Battle-owned托管 action.
	ReplayMoveSourceAuto ReplayMoveSource = "auto"
)

// ReplayMove is a deep-copyable in-memory event used to build the NHSK replay.
// Only fields relevant to the event kind are populated.
type ReplayMove struct {
	Kind             ReplayMoveKind
	SeatID           uint8
	UserID           uint32
	Hands            [4][]byte
	Cards            []byte
	Point            uint32
	Scores           [4]uint16
	CardType         string
	Source           ReplayMoveSource
	MoveMilliseconds uint32
}

// Valid reports whether the start snapshot contains a complete four-seat
// subgame input and the immutable replay identifiers derived at Start.
func (snapshot ReplayStartSnapshot) Valid() bool {
	if snapshot.BattleID == 0 || snapshot.Identity.BattleID != snapshot.BattleID || snapshot.Identity.ProductID == 0 || snapshot.Identity.MatchID == 0 || snapshot.GameNum == 0 || snapshot.SubgameNum == 0 || snapshot.StartedAt.IsZero() || snapshot.ReplayName == "" || snapshot.ReplayUID == "" || snapshot.RelativePath == "" || snapshot.BankerSeat >= 4 {
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
// subgame. It currently contains the Start snapshot and basic gameplay moves;
// settlement, summary and serialization are added by later replay slices.
type ReplayDocument struct {
	start ReplayStartSnapshot
	moves []ReplayMove
}

// NewReplayDocument creates a replay document that owns a deep copy of its
// start snapshot. The caller may safely reuse or mutate the input afterward.
func NewReplayDocument(snapshot ReplayStartSnapshot) ReplayDocument {
	document := ReplayDocument{start: cloneReplayStartSnapshot(snapshot)}
	document.moves = []ReplayMove{{Kind: ReplayMoveDeal, Hands: cloneReplayHands(document.start.Hands), Source: ReplayMoveSourceUnknown}}
	return document
}

// StartSnapshot returns a deep copy of the frozen subgame-start input.
func (document ReplayDocument) StartSnapshot() ReplayStartSnapshot {
	return cloneReplayStartSnapshot(document.start)
}

// Moves returns a deep copy of the replay events recorded so far.
func (document ReplayDocument) Moves() []ReplayMove {
	return cloneReplayMoves(document.moves)
}

// Clone returns an independent copy of the in-memory replay document.
func (document ReplayDocument) Clone() ReplayDocument {
	return ReplayDocument{start: cloneReplayStartSnapshot(document.start), moves: cloneReplayMoves(document.moves)}
}

func (document *ReplayDocument) appendMove(move ReplayMove) {
	document.moves = append(document.moves, cloneReplayMove(move))
}

func cloneReplayStartSnapshot(snapshot ReplayStartSnapshot) ReplayStartSnapshot {
	snapshot.Hands = cloneReplayHands(snapshot.Hands)
	return snapshot
}

func cloneReplayHands(hands [4][]byte) [4][]byte {
	for seat := range hands {
		hands[seat] = append([]byte(nil), hands[seat]...)
	}
	return hands
}

func cloneReplayMove(move ReplayMove) ReplayMove {
	move.Hands = cloneReplayHands(move.Hands)
	move.Cards = append([]byte(nil), move.Cards...)
	return move
}

func cloneReplayMoves(moves []ReplayMove) []ReplayMove {
	if len(moves) == 0 {
		return nil
	}
	cloned := make([]ReplayMove, len(moves))
	for index, move := range moves {
		cloned[index] = cloneReplayMove(move)
	}
	return cloned
}

func replayCardTypeName(pattern cardPattern, cardCount int) string {
	if cardCount == 0 {
		return "不出"
	}
	switch pattern.kind {
	case cardPatternSingle:
		return "单张"
	case cardPatternPair:
		return "对子"
	case cardPatternTriple:
		return "三张"
	case cardPatternThreeTwo:
		return "俘虏"
	case cardPatternBomb:
		if pattern.count > 0 {
			return "炸弹" + strconv.Itoa(pattern.count)
		}
		return "炸弹4"
	default:
		return "不出"
	}
}
