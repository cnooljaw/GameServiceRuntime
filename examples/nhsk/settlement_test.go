package nhsk

import (
	"reflect"
	"testing"
)

func TestCalculateSubgameResultMatchesReferenceCases(t *testing.T) {
	tests := []struct {
		name      string
		ranks     [4]uint8
		points    [4]uint16
		automated [4]bool
		result    SubgameResult
		winner    uint8
		scores    [4]int32
	}{
		{name: "partners first and second", ranks: [4]uint8{1, 3, 2, 4}, result: SubgameResultDouble, winner: 0, scores: [4]int32{2, -2, 2, -2}},
		{name: "opponent second catches 150", ranks: [4]uint8{1, 2, 3, 4}, points: [4]uint16{50, 150}, result: SubgameResultSingle, winner: 1, scores: [4]int32{-1, 1, -1, 1}},
		{name: "opponent second catches 200", ranks: [4]uint8{1, 2, 3, 4}, points: [4]uint16{0, 200}, result: SubgameResultDouble, winner: 1, scores: [4]int32{-2, 2, -2, 2}},
		{name: "first team single", ranks: [4]uint8{1, 2, 3, 4}, points: [4]uint16{100, 100}, result: SubgameResultSingle, winner: 0, scores: [4]int32{1, -1, 1, -1}},
		{name: "first team double by zero opponent points", ranks: [4]uint8{1, 2, 3, 4}, points: [4]uint16{100, 0, 100, 0}, result: SubgameResultDouble, winner: 0, scores: [4]int32{2, -2, 2, -2}},
		{name: "one automated loser bears partner loss", ranks: [4]uint8{1, 3, 2, 4}, automated: [4]bool{false, true}, result: SubgameResultDouble, winner: 0, scores: [4]int32{2, -4, 2, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := calculateSubgameResult(GameOverReasonSuccess, test.ranks, test.points, test.automated)
			if got.result != test.result || got.winningTeam != test.winner || got.multiples != test.scores {
				t.Fatalf("result = %d/%d/%v, want %d/%d/%v", got.result, got.winningTeam, got.multiples, test.result, test.winner, test.scores)
			}
		})
	}
}

func TestBuildSettlementRequestUsesDirectedScoreMatrixAndPlayerFacts(t *testing.T) {
	result := calculateSubgameResult(GameOverReasonSuccess, [4]uint8{1, 3, 2, 4}, [4]uint16{}, [4]bool{false, true})
	players := [4]BattlePlayer{
		{UserID: 1001, SeatID: 0, Exp: 11}, {UserID: 1002, SeatID: 1, Exp: 12},
		{UserID: 1003, SeatID: 2, Exp: 13}, {UserID: 1004, SeatID: 3, Exp: 14},
	}
	request := buildSettlementRequest(result, players)
	wantGains := []SettlementGain{{PayTeamID: 1, GainTeamID: 0, Score: 2}, {PayTeamID: 1, GainTeamID: 2, Score: 2}}
	if request.ResultType != 1 || request.TeamCount != 4 || request.LevelScoreType != 1 || !reflect.DeepEqual(request.Gains, wantGains) {
		t.Fatalf("settlement request = %#v", request)
	}
	if request.Players[1].PlayerID != 1002 || request.Players[1].Flag != settlementFlagLose|settlementFlagAsAuto || request.Players[1].Exp != 12 || request.Players[1].TeamID != 1 {
		t.Fatalf("settlement player = %#v", request.Players[1])
	}
}

func TestBattleAutomationClassificationUsesReferenceThresholdFormula(t *testing.T) {
	rules := DefaultNHSKConfig()
	rules.AutoSettlementMinCount = 2
	rules.AutoSettlementRatioFactor = 3
	battle := &NHSKBattleService{rules: rules, moveCount: [4]uint32{5, 5, 0, 0}, autoCount: [4]uint32{2, 1, 0, 0}}
	if got := battle.settlementAutomated(); got != [4]bool{true, false, false, false} {
		t.Fatalf("automated = %v", got)
	}
	battle.rules.AutoSettlementMinCount = -1
	battle.rules.AutoSettlementRatioFactor = -1
	if got := battle.settlementAutomated(); got != [4]bool{} {
		t.Fatalf("disabled automated = %v", got)
	}
}
