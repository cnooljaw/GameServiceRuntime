package game

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const maxBusinessIDBytes = 128

func validateID[T ~string](value T) error {
	if !validText(string(value), maxBusinessIDBytes) {
		return ErrInvalidID
	}
	return nil
}

func validateRequestID(value RequestID) error {
	if !validText(string(value), maxBusinessIDBytes) {
		return ErrInvalidRequestID
	}
	return nil
}

func validText(value string, limit int) bool {
	return value != "" && utf8.ValidString(value) && strings.TrimSpace(value) == value && len(value) <= limit
}

func validServiceRef(ref gsr.ServiceRef) bool { return ref.Node != "" && ref.ID != 0 }

func validateSettlementRequest(request SettlementRequest) error {
	if validateRequestID(request.RequestID) != nil || !validServiceRef(request.Source) || !validText(string(request.Currency), maxBusinessIDBytes) || len(request.Entries) == 0 || len(request.Entries) > 256 {
		return ErrInvalidSettlement
	}
	previous := PlayerID("")
	for index, entry := range request.Entries {
		if validateID(entry.Player) != nil || entry.Delta == 0 || (index > 0 && entry.Player <= previous) {
			return ErrInvalidSettlement
		}
		previous = entry.Player
	}
	return nil
}

func validateSettlementIntent(intent SettlementIntent) error {
	return validateSettlementRequest(SettlementRequest{RequestID: intent.RequestID, Source: gsr.ServiceRef{Node: "intent", ID: 1}, Currency: intent.Currency, Entries: intent.Entries})
}

func validateBattleConfig(config BattleConfig) error {
	if validateID(config.ID) != nil || isNil(config.Logic) || len(config.Participants) == 0 {
		return ErrInvalidConfig
	}
	seenPlayers := make(map[PlayerID]struct{}, len(config.Participants))
	for _, participant := range config.Participants {
		if validateID(participant.Player) != nil {
			return ErrInvalidParticipant
		}
		if participant.Ref != (gsr.ServiceRef{}) && !validServiceRef(participant.Ref) {
			return ErrInvalidParticipant
		}
		if _, exists := seenPlayers[participant.Player]; exists {
			return ErrInvalidParticipant
		}
		seenPlayers[participant.Player] = struct{}{}
	}
	commands := config.Logic.Commands()
	if len(commands) == 0 || !strictCommandIDs(commands) {
		return ErrInvalidConfig
	}
	for _, command := range commands {
		if reservedBattleCommand(command) {
			return ErrInvalidConfig
		}
	}
	return nil
}

func strictCommandIDs(commands []gsr.CommandID) bool {
	if len(commands) == 0 {
		return false
	}
	seen := make(map[gsr.CommandID]struct{}, len(commands))
	for _, command := range commands {
		if command == 0 {
			return false
		}
		if _, exists := seen[command]; exists {
			return false
		}
		seen[command] = struct{}{}
	}
	return true
}

func reservedBattleCommand(command gsr.CommandID) bool {
	switch command {
	case StartBattleCommand, GetBattleSnapshotCommand, SetParticipantConnectedCommand, FinishBattleCommand, ApplySettlementResultCommand, TimelineFireCommand:
		return true
	default:
		return false
	}
}

func cloneSettlementRequest(request SettlementRequest) SettlementRequest {
	request.Entries = append([]SettlementEntry(nil), request.Entries...)
	return request
}

func cloneSettlementIntent(intent SettlementIntent) SettlementIntent {
	intent.Entries = append([]SettlementEntry(nil), intent.Entries...)
	return intent
}

func cloneFinishBattle(finish FinishBattle) FinishBattle {
	finish.Settlements = make([]SettlementIntent, len(finish.Settlements))
	for index, intent := range finish.Settlements {
		finish.Settlements[index] = cloneSettlementIntent(intent)
	}
	return finish
}

func cloneSettlementResult(result SettlementResult) SettlementResult {
	result.Balances = append([]Balance(nil), result.Balances...)
	return result
}

func cloneBattleSnapshot(snapshot BattleSnapshot) BattleSnapshot {
	participants := snapshot.Participants
	snapshot.Participants = make(map[PlayerID]ParticipantStatus, len(participants))
	for player, status := range participants {
		snapshot.Participants[player] = status
	}
	snapshot.Timeline = cloneTimelineSnapshot(snapshot.Timeline)
	snapshot.State = append([]byte(nil), snapshot.State...)
	return snapshot
}

func cloneTimelineSnapshot(snapshot TimelineSnapshot) TimelineSnapshot {
	snapshot.Items = append([]TimelineItem(nil), snapshot.Items...)
	return snapshot
}

func sortedPlayerIDs(values map[PlayerID]struct{}) []PlayerID {
	result := make([]PlayerID, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func usableContext(ctx context.Context) error {
	if isNil(ctx) {
		return ErrInvalidConfig
	}
	return context.Cause(ctx)
}
