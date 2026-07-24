package main

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// ShrewID identifies one WhackMole target within a Battle.
type ShrewID uint64

// ShrewState is the visible lifecycle state of one target.
type ShrewState string

const (
	// ShrewVisible can be hit once.
	ShrewVisible ShrewState = "visible"
	// ShrewHit is a target already scored by one player.
	ShrewHit ShrewState = "hit"
	// ShrewExpired is a target that timed out without a hit.
	ShrewExpired ShrewState = "expired"
)

const (
	// StartCommand starts WhackMole-specific state after the generic Battle starts.
	StartCommand gsr.CommandID = 0x04000101
	// SpawnCommand creates one target.
	SpawnCommand gsr.CommandID = 0x04000102
	// KickCommand attempts one target hit.
	KickCommand gsr.CommandID = 0x04000103
	// ExpireCommand marks a visible target expired when Timeline fires.
	ExpireCommand gsr.CommandID = 0x04000104
	// FinishCommand freezes scores into Wallet settlement intents.
	FinishCommand gsr.CommandID = 0x04000105
	// GetSnapshotCommand returns a WhackMole projection.
	GetSnapshotCommand gsr.CommandID = 0x04000106
)

// KickRequest is one player input to hit a visible target.
type KickRequest struct {
	Player game.PlayerID
	Shrew  ShrewID
	Epoch  game.BattleEpoch
}

// KickResult is the idempotent visible result of one target hit attempt.
type KickResult struct {
	Hit    bool
	Score  int64
	Reason string
}

// Snapshot is an independent WhackMole projection for a caller or Record/Replay assertion.
type Snapshot struct {
	Battle game.BattleSnapshot
	Shrews map[ShrewID]ShrewState
	Scores map[game.PlayerID]int64
}

type whackMoleLogic struct {
	seed    uint64
	next    ShrewID
	shrews  map[ShrewID]ShrewState
	scores  map[game.PlayerID]int64
	started bool
}

func newWhackMoleLogic(seed uint64) *whackMoleLogic {
	return &whackMoleLogic{seed: seed, shrews: make(map[ShrewID]ShrewState), scores: make(map[game.PlayerID]int64)}
}

func (*whackMoleLogic) Commands() []gsr.CommandID {
	return []gsr.CommandID{StartCommand, SpawnCommand, KickCommand, ExpireCommand, FinishCommand, GetSnapshotCommand}
}

func (l *whackMoleLogic) HandleBattle(ctx game.BattleContext, command gsr.Command) error {
	switch command.ID {
	case StartCommand:
		if l.started {
			return game.ErrStateConflict
		}
		l.started = true
		if err := l.spawn(ctx); err != nil {
			return err
		}
		return ctx.Reply(struct{}{})
	case SpawnCommand:
		return l.spawn(ctx)
	case KickCommand:
		request, ok := command.Payload.(KickRequest)
		if !ok || request.Epoch != ctx.Epoch() {
			return game.ErrInvalidCommand
		}
		state, exists := l.shrews[request.Shrew]
		if !exists || state != ShrewVisible {
			return ctx.Reply(KickResult{Score: l.scores[request.Player], Reason: "not-visible"})
		}
		l.shrews[request.Shrew] = ShrewHit
		l.scores[request.Player]++
		return ctx.Reply(KickResult{Hit: true, Score: l.scores[request.Player]})
	case ExpireCommand:
		id, ok := command.Payload.(ShrewID)
		if !ok {
			return game.ErrInvalidCommand
		}
		if l.shrews[id] == ShrewVisible {
			l.shrews[id] = ShrewExpired
		}
		return nil
	case FinishCommand:
		requestID, ok := command.Payload.(game.RequestID)
		if !ok {
			return game.ErrInvalidCommand
		}
		entries := make([]game.SettlementEntry, 0, len(l.scores))
		players := make([]game.PlayerID, 0, len(l.scores))
		for player := range l.scores {
			players = append(players, player)
		}
		sort.Slice(players, func(left, right int) bool { return players[left] < players[right] })
		for _, player := range players {
			if score := l.scores[player]; score != 0 {
				entries = append(entries, game.SettlementEntry{Player: player, Delta: game.Amount(score)})
			}
		}
		if len(entries) == 0 {
			return game.ErrStateConflict
		}
		return ctx.Finish(game.FinishBattle{RequestID: requestID, Settlements: []game.SettlementIntent{{RequestID: game.RequestID(string(requestID) + ".score"), Currency: "score", Entries: entries}}})
	case GetSnapshotCommand:
		return ctx.Reply(l.snapshot(ctx))
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (l *whackMoleLogic) Snapshot(game.BattleContext) ([]byte, error) {
	state := struct {
		Seed   uint64                  `json:"seed"`
		Next   ShrewID                 `json:"next"`
		Shrews map[ShrewID]ShrewState  `json:"shrews"`
		Scores map[game.PlayerID]int64 `json:"scores"`
	}{Seed: l.seed, Next: l.next, Shrews: cloneShrews(l.shrews), Scores: cloneScores(l.scores)}
	return json.Marshal(state)
}

func (l *whackMoleLogic) spawn(ctx game.BattleContext) error {
	l.next++
	id := l.next
	l.shrews[id] = ShrewVisible
	_, err := ctx.Timeline().After(time.Hour, ExpireCommand, id)
	return err
}

func (l *whackMoleLogic) snapshot(ctx game.BattleContext) Snapshot {
	return Snapshot{Battle: game.BattleSnapshot{ID: ctx.BattleID(), Epoch: ctx.Epoch()}, Shrews: cloneShrews(l.shrews), Scores: cloneScores(l.scores)}
}

func cloneShrews(values map[ShrewID]ShrewState) map[ShrewID]ShrewState {
	result := make(map[ShrewID]ShrewState, len(values))
	for id, state := range values {
		result[id] = state
	}
	return result
}
func cloneScores(values map[game.PlayerID]int64) map[game.PlayerID]int64 {
	result := make(map[game.PlayerID]int64, len(values))
	for player, score := range values {
		result[player] = score
	}
	return result
}
