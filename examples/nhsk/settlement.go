package nhsk

const (
	settlementFlagWin    int32 = 0x00000000
	settlementFlagLose   int32 = 0x00000001
	settlementFlagDraw   int32 = 0x00000002
	settlementFlagAsAuto int32 = 0x00000010
)

type subgameResultSnapshot struct {
	reason      GameOverReason
	result      SubgameResult
	winningTeam uint8
	multiples   [4]int32
	outcomes    [4]PlayerOutcome
	points      [4]uint16
	ranks       [4]uint8
	automated   [4]bool
}

func calculateSubgameResult(reason GameOverReason, ranks [4]uint8, points [4]uint16, automated [4]bool) subgameResultSnapshot {
	result := subgameResultSnapshot{reason: reason, result: SubgameResultPeace, points: points, ranks: ranks, automated: automated}
	for seat := range result.ranks {
		if result.ranks[seat] == 0 {
			result.ranks[seat] = 4
		}
		result.outcomes[seat] = PlayerOutcomePeace
	}
	if reason != GameOverReasonSuccess {
		return result
	}
	rankAt := func(seat int) uint8 {
		if seat < 0 || seat >= len(result.ranks) {
			return 4
		}
		return result.ranks[seat]
	}
	pointAt := func(seat int) uint16 {
		if seat < 0 || seat >= len(points) {
			return 0
		}
		return points[seat]
	}
	first := 0
	if rankAt(1) == 1 || rankAt(3) == 1 {
		first = 1
	}
	winner := first % 2
	switch {
	case rankAt(0)+rankAt(2) == 3 || rankAt(1)+rankAt(3) == 3:
		result.result = SubgameResultDouble
	case rankAt((first+2)%4) == 3 || rankAt(first) == 3:
		second := (first + 3) % 4
		if rankAt(first+1) == 2 {
			second = first + 1
		}
		switch {
		case pointAt(second) == 200:
			result.result, winner = SubgameResultDouble, (first+1)%2
		case pointAt(first)+pointAt((first+2)%4) == 200:
			result.result = SubgameResultDouble
		case pointAt(second) < 105:
			result.result = SubgameResultSingle
		default:
			result.result, winner = SubgameResultSingle, (first+1)%2
		}
	default:
		second := (first + 2) % 4
		if rankAt(first) == 1 {
			second = first
		}
		switch {
		case pointAt(second) == 200:
			result.result = SubgameResultDouble
		case pointAt(first+1)+pointAt((first+3)%4) == 200:
			result.result, winner = SubgameResultDouble, (first+1)%2
		case pointAt((first+2)%4)+pointAt(first) < 100:
			result.result, winner = SubgameResultSingle, (first+1)%2
		default:
			result.result = SubgameResultSingle
		}
	}
	result.winningTeam = uint8(winner)
	unit := int32(1)
	if result.result == SubgameResultDouble {
		unit = 2
	}
	result.multiples[winner], result.multiples[(winner+2)%4] = unit, unit
	result.multiples[(winner+1)%4], result.multiples[(winner+3)%4] = -unit, -unit
	for seat := range result.multiples {
		partner := (seat + 2) % 4
		switch {
		case automated[seat] && automated[partner]:
		case automated[seat] && result.multiples[seat] < 0:
			result.multiples[seat] -= unit
		case automated[partner] && result.multiples[seat] < 0:
			result.multiples[seat] = 0
		}
	}
	for seat, score := range result.multiples {
		switch {
		case score > 0:
			result.outcomes[seat] = PlayerOutcomeWin
		case score < 0:
			result.outcomes[seat] = PlayerOutcomeLoss
		default:
			result.outcomes[seat] = PlayerOutcomePeace
		}
	}
	return result
}

func buildSettlementRequest(result subgameResultSnapshot, players [4]BattlePlayer) SettlementRequestOutput {
	matrix := resultScoreMatrix(result)
	request := SettlementRequestOutput{ResultType: 1, TeamCount: 4, LevelScoreType: 1, Players: make([]SettlementPlayerResult, 4)}
	for payTeam, row := range matrix {
		for gainTeam, score := range row {
			if score > 0 {
				request.Gains = append(request.Gains, SettlementGain{PayTeamID: uint32(payTeam), GainTeamID: uint32(gainTeam), Score: score})
			}
		}
	}
	for seat, player := range players {
		flag := settlementFlagDraw
		switch result.outcomes[seat] {
		case PlayerOutcomeWin:
			flag = settlementFlagWin
		case PlayerOutcomeLoss:
			flag = settlementFlagLose
		}
		if result.automated[seat] {
			flag |= settlementFlagAsAuto
		}
		request.Players[seat] = SettlementPlayerResult{PlayerID: player.UserID, Flag: flag, Exp: player.Exp, TeamID: uint32(seat)}
	}
	return request
}

func resultScoreMatrix(result subgameResultSnapshot) [4][4]int32 {
	var matrix [4][4]int32
	if result.result == SubgameResultPeace || result.winningTeam > 1 {
		return matrix
	}
	winner := int(result.winningTeam)
	loser := 1 - winner
	unit := result.multiples[winner]
	if result.multiples[loser] != 0 {
		matrix[loser][winner] = unit
	}
	matrix[loser][winner+2] = -result.multiples[loser] - matrix[loser][winner]
	if result.multiples[loser+2] != 0 {
		matrix[loser+2][winner+2] = unit
	}
	matrix[loser+2][winner] = -result.multiples[loser+2] - matrix[loser+2][winner+2]
	return matrix
}
