package nhsk

const (
	cardValueA  = 1
	cardValueK  = 13
	cardMinBomb = 4
)

type cardPatternKind uint8

const (
	cardPatternInvalid cardPatternKind = iota
	cardPatternSingle
	cardPatternPair
	cardPatternTriple
	cardPatternThreeTwo
	cardPatternBomb
)

type cardPattern struct {
	kind  cardPatternKind
	rank  int
	count int
}

func classifyCards(cards []byte) (cardPattern, bool) {
	if len(cards) == 0 {
		return cardPattern{}, false
	}
	counts := [14]int{}
	for _, card := range cards {
		value := int(card & 0x0f)
		if value < cardValueA || value > cardValueK {
			return cardPattern{}, false
		}
		counts[value]++
	}
	unique, tripleValue, pairValue := 0, 0, 0
	for value := cardValueA; value <= cardValueK; value++ {
		if counts[value] == 0 {
			continue
		}
		unique++
		if counts[value] == 3 {
			tripleValue = value
		}
		if counts[value] == 2 {
			pairValue = value
		}
	}
	switch {
	case len(cards) >= cardMinBomb && unique == 1:
		return cardPattern{kind: cardPatternBomb, rank: firstCardValue(counts), count: len(cards)}, true
	case len(cards) == 2 && unique == 1:
		return cardPattern{kind: cardPatternPair, rank: firstCardValue(counts), count: len(cards)}, true
	case len(cards) == 3 && unique == 1:
		return cardPattern{kind: cardPatternTriple, rank: firstCardValue(counts), count: len(cards)}, true
	case len(cards) == 5 && tripleValue != 0 && pairValue != 0:
		return cardPattern{kind: cardPatternThreeTwo, rank: tripleValue, count: len(cards)}, true
	case len(cards) == 1:
		return cardPattern{kind: cardPatternSingle, rank: firstCardValue(counts), count: len(cards)}, true
	default:
		return cardPattern{}, false
	}
}

func firstCardValue(counts [14]int) int {
	for value := cardValueA; value <= cardValueK; value++ {
		if counts[value] != 0 {
			return value
		}
	}
	return 0
}

func compareCardSets(higher, lower []byte) int {
	higherPattern, higherOK := classifyCards(higher)
	lowerPattern, lowerOK := classifyCards(lower)
	if !higherOK || !lowerOK {
		return 0
	}
	if higherPattern.kind != lowerPattern.kind {
		if higherPattern.kind == cardPatternBomb {
			return 1
		}
		if lowerPattern.kind == cardPatternBomb {
			return -1
		}
		return 0
	}
	if higherPattern.kind == cardPatternBomb && higherPattern.count != lowerPattern.count {
		if higherPattern.count > lowerPattern.count {
			return 1
		}
		return -1
	}
	higherRank := cardLogicValue(higherPattern.rank)
	lowerRank := cardLogicValue(lowerPattern.rank)
	if higherRank > lowerRank {
		return 1
	}
	if higherRank < lowerRank {
		return -1
	}
	return 0
}

func cardLogicValue(value int) int {
	if value < 3 {
		return value + 13
	}
	return value
}

func scoreCardsIn(cards []byte) (uint32, []byte) {
	var score uint32
	scoreCards := make([]byte, 0, len(cards))
	for _, card := range cards {
		if card>>4 > 4 {
			continue
		}
		switch card & 0x0f {
		case 5:
			score += 5
			scoreCards = append(scoreCards, card)
		case 10, 13:
			score += 10
			scoreCards = append(scoreCards, card)
		}
	}
	return score, scoreCards
}
