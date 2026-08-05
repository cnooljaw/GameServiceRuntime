package nhsk

import (
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestClassifyNHSKCardPatterns(t *testing.T) {
	tests := []struct {
		name  string
		cards []byte
		kind  cardPatternKind
		rank  int
	}{
		{name: "single", cards: []byte{0x03}, kind: cardPatternSingle, rank: 3},
		{name: "pair", cards: []byte{0x03, 0x13}, kind: cardPatternPair, rank: 3},
		{name: "triple", cards: []byte{0x03, 0x13, 0x23}, kind: cardPatternTriple, rank: 3},
		{name: "three two", cards: []byte{0x03, 0x13, 0x23, 0x04, 0x14}, kind: cardPatternThreeTwo, rank: 3},
		{name: "four card bomb", cards: []byte{0x03, 0x13, 0x23, 0x33}, kind: cardPatternBomb, rank: 3},
		{name: "eight card bomb", cards: []byte{0x05, 0x15, 0x25, 0x35, 0x05, 0x15, 0x25, 0x35}, kind: cardPatternBomb, rank: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := classifyCards(test.cards)
			if !ok || got.kind != test.kind || got.rank != test.rank {
				t.Fatalf("classifyCards(%x) = %#v, %v; want kind=%d rank=%d", test.cards, got, ok, test.kind, test.rank)
			}
		})
	}
	if _, ok := classifyCards([]byte{0x03, 0x14}); ok {
		t.Fatal("mismatched pair should be invalid")
	}
}

func TestCompareNHSKCardPatternsMatchesReferenceRules(t *testing.T) {
	tests := []struct {
		name   string
		higher []byte
		lower  []byte
		want   int
	}{
		{name: "ace beats five", higher: []byte{0x01}, lower: []byte{0x05}, want: 1},
		{name: "two beats ace", higher: []byte{0x02}, lower: []byte{0x01}, want: 1},
		{name: "three two by triple rank", higher: []byte{0x02, 0x12, 0x22, 0x0c, 0x1c}, lower: []byte{0x09, 0x19, 0x29, 0x0d, 0x1d}, want: 1},
		{name: "bomb beats non bomb", higher: []byte{0x02, 0x12, 0x22, 0x32}, lower: []byte{0x05}, want: 1},
		{name: "longer bomb wins", higher: []byte{0x03, 0x13, 0x23, 0x33, 0x03}, lower: []byte{0x02, 0x12, 0x22, 0x32}, want: 1},
		{name: "different non bomb types do not press", higher: []byte{0x03, 0x13}, lower: []byte{0x04, 0x14, 0x24, 0x05, 0x15}, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := compareCardSets(test.higher, test.lower)
			if (test.want > 0 && got <= 0) || (test.want < 0 && got >= 0) || (test.want == 0 && got != 0) {
				t.Fatalf("compareCardSets(%x,%x) = %d, want sign %d", test.higher, test.lower, got, test.want)
			}
		})
	}
}

func TestScoreCardsMatchesReferenceValues(t *testing.T) {
	score, cards := scoreCardsIn([]byte{0x05, 0x1a, 0x2d, 0x03, 0x3d, 0x5d})
	if score != 35 {
		t.Fatalf("scoreCardsIn score = %d, want 35", score)
	}
	want := []byte{0x05, 0x1a, 0x2d, 0x3d}
	if string(cards) != string(want) {
		t.Fatalf("scoreCardsIn cards = %#v, want %#v", cards, want)
	}
}

func TestBattleAcceptsDuplicatePhysicalCardsFromTwoDecks(t *testing.T) {
	service, _ := newPlayingBattleForRestore(t, 24)
	service.activeSeat = 0
	service.verifyCode = 11
	service.hands[service.bySeat[0]] = []byte{0x03, 0x03}
	service.lastCards = nil
	ctx := &battleTestCommandContext{}
	if err := service.Handle(ctx, gsr.Command{ID: PlayCardsCommand, Payload: PlayCardsRequest{Player: service.bySeat[0], Cards: []byte{0x03, 0x03}, VerifyCode: 11}}); err != nil {
		t.Fatal(err)
	}
	if result := ctx.reply.(ActionResult); !result.Accepted {
		t.Fatalf("duplicate physical cards = %#v", result)
	}
}
