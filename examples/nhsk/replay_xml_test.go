package nhsk

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestMarshalReplayMovesXMLMatchesLegacyFormat(t *testing.T) {
	var start ReplayStartSnapshot
	for seat := 0; seat < 4; seat++ {
		start.Players[seat] = ReplayPlayerSnapshot{SeatID: uint8(seat), UserID: uint32(1001 + seat)}
		start.Hands[seat] = []byte{byte(seat + 1), byte(0x0a + seat)}
	}
	document := NewReplayDocument(start)
	document.appendMove(ReplayMove{Kind: ReplayMoveCurrentPoint, Cards: []byte{0x05, 0x1a}, Point: 15, Source: ReplayMoveSourceSystem})
	document.appendMove(ReplayMove{Kind: ReplayMoveOutCard, SeatID: 2, Cards: nil, CardType: "不出", Source: ReplayMoveSourceAuto, MoveMilliseconds: 1234})
	document.appendMove(ReplayMove{Kind: ReplayMoveCatchPoint, SeatID: 1, Cards: []byte{0x05, 0x1a}, Point: 15, Source: ReplayMoveSourceSystem})
	document.appendMove(ReplayMove{Kind: ReplayMoveTurnEnd, Scores: [4]uint16{0, 15, 0, 0}, Source: ReplayMoveSourceSystem})
	document.appendMove(ReplayMove{Kind: ReplayMoveProp, UserID: 1001, PropID: "flower", PropCount: 2, TargetIDs: []uint32{1002, 1002, 1001}, Source: ReplayMoveSourceUnknown})

	got, err := marshalReplayMovesXML(document)
	if err != nil {
		t.Fatal(err)
	}
	want := `<?xml version="1.0" encoding="UTF-8"?>
<Moves Count="6">
	<M0 Act="Deal">
		<D0 Cards="0x01,0x0a" ChairID="0" UserID="1001"></D0>
		<D1 Cards="0x02,0x0b" ChairID="1" UserID="1002"></D1>
		<D2 Cards="0x03,0x0c" ChairID="2" UserID="1003"></D2>
		<D3 Cards="0x04,0x0d" ChairID="3" UserID="1004"></D3>
	</M0>
	<M1 Act="CurrentPoint" Actor="系统" Cards="0x05,0x1a" Point="15"></M1>
	<M2 Act="OutCard" Actor="托管" CardType="不出" Cards="" ChairID="2" MSec="1234"></M2>
	<M3 Act="CatchPoint" Actor="系统" Cards="0x05,0x1a" ChairID="1" Point="15"></M3>
	<M4 Act="TurnEnd" Actor="系统" Scores="0,15,0,0"></M4>
	<M5 Act="Prop" Count="2" PropID="flower" TargetID="1002,1002,1001" dwSenderID="1001"></M5>
</Moves>`
	if string(got) != want {
		t.Fatalf("moves XML mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarshalReplayDocumentBuildsCompleteStableLegacyTree(t *testing.T) {
	rules := DefaultNHSKConfig()
	rules.MsFirstOutCard = 20 * time.Second
	rules.MsOutCard = 15 * time.Second
	start := ReplayStartSnapshot{
		BattleID: 9, Identity: BattleIdentity{BattleID: 9, ProductID: 7, MatchID: 8, RoundID: 6, RoundUniCode: "round"},
		GameNum: 2, SubgameNum: 3, StartedAt: time.Unix(100, 0), ReplayName: "NHSK.xml", ReplayUID: "10099", RelativePath: "FuPan/19700101/08",
		MaxGameNum: 4, MaxSubgameNum: 8, Fee: 2, ScoreBase: 100, ScoreDenominator: 1,
		ReplayMetadata: ReplayMetadata{MatchName: "比赛", GameType: 3, ScoreType: 4, ScoreMode: 5, RoomID: 6, CreatorID: 99},
		ReplayRules:    ReplayRuleSnapshot{TimeOutOver: true, VoiceMode: true, RandomSeatRoundStart: true, GameNumToRandomSeat: 7}, Rules: rules,
		RoundContext: UpdateRoundContextRequest{SecRoundTotal: 60, SecRoundUsed: 5, RoomInfo: `{"x":"<&"}`}, BankerSeat: 0,
	}
	for seat := 0; seat < 4; seat++ {
		start.Players[seat] = ReplayPlayerSnapshot{SeatID: uint8(seat), Player: game.PlayerID(string(rune('1' + seat))), UserID: uint32(1001 + seat), Nickname: "玩家", InitScore: int32(10 + seat), Platform: uint32(20 + seat), Dress: "d<&"}
		start.Hands[seat] = make([]byte, 26)
		for index := range start.Hands[seat] {
			start.Hands[seat][index] = byte(index + 1)
		}
	}
	document := NewReplayDocument(start)
	document.finalize(replayEndSnapshot{
		endedAt:     time.Unix(200, 0),
		result:      subgameResultSnapshot{reason: GameOverReasonSuccess, result: SubgameResultDouble, winningTeam: 0, multiples: [4]int32{2, -2, 2, -2}, outcomes: [4]PlayerOutcome{PlayerOutcomeWin, PlayerOutcomeLoss, PlayerOutcomeWin, PlayerOutcomeLoss}, points: [4]uint16{120, 80, 80, 120}, ranks: [4]uint8{1, 3, 2, 4}, automated: [4]bool{false, true}},
		finalScores: [4]int32{200, -200, 200, -200},
		players:     [4]BattlePlayer{{UserID: 1001, IsSeal: true}, {UserID: 1002, IsBreak: true}, {UserID: 1003}, {UserID: 1004}},
		moveCount:   [4]uint32{2, 1}, autoCount: [4]uint32{1}, moveMilliseconds: [4]uint32{345},
		actions:     [4]replayActionSummary{{counts: [6]uint32{1, 1, 0, 0, 0, 1}}},
		cardDetails: [4][]replayCardDetail{{{cardType: "ZhaDan", cards: []byte{0x03, 0x13, 0x23, 0x33}}}},
	})
	first, err := marshalReplayDocumentXML(document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := marshalReplayDocumentXML(document.Clone())
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("replay serialization is not deterministic: %v", err)
	}
	xmlText := string(first)
	wants := []string{
		`<Game GameName="宁海双扣" ID="82">`,
		`<Info OwnerID="99" RoomID="6" RoundUniCode="round" UniCode="10099">`,
		`<RoomInfo Json="{&#34;x&#34;:&#34;&lt;&amp;&#34;}"></RoomInfo>`,
		`<GameRule GameNumToRandomSeat="7" GameTime="60" PlayerNum="4" RandomSeatRoundStart="1" TimeOutAutoMove="1" TimeOutOver="1" VoiceMode="1"></GameRule>`,
		`<GameSetting MSecFirstOutCard="20000" MSecOutCard="15000"></GameSetting>`,
		`<GameOver EndReason="0" EndTime="200" GameResult="1" OverCode="0" OverStatus="0" OverUserID="0" Reason="Success" RecordValid="1" ResultType="ShuangKou_2" Scale="Game">`,
		`<Chair0 CatchScore="120" IsAuto="0" IsBreak="0" IsSeal="1" IsWin="1" Multiple="2" Result="0" Score="200" TotalScore="200" UserID="1001"></Chair0>`,
		`<Summary Count="5">`, `<S4 TotalAutoOutCount="1" TotalMoveTime="345" TotalOutCount="3"></S4>`,
		`<D1 Data="d&lt;&amp;" UserID="1001"></D1>`,
		`<CD Cards="0x03,0x13,0x23,0x33" Count="4" Type="ZhaDan"></CD>`,
	}
	for _, want := range wants {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("replay XML missing %s\n%s", want, xmlText)
		}
	}
	wantOrder := []string{"<Info ", "<Moves ", "<GameOver ", "<Summary ", "<Dress>", "<Other>"}
	last := -1
	for _, marker := range wantOrder {
		index := strings.Index(xmlText, marker)
		if index <= last {
			t.Fatalf("root order marker %q at %d after %d", marker, index, last)
		}
		last = index
	}
}

func TestReplayTextKeepsUTF8AndDecodesGBKAtNamedFields(t *testing.T) {
	if got := replayUTF8OrGBK("宁海"); got != "宁海" {
		t.Fatalf("UTF-8 text = %q", got)
	}
	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("宁海"))
	if err != nil {
		t.Fatal(err)
	}
	if got := replayUTF8OrGBK(string(encoded)); got != "宁海" {
		t.Fatalf("GBK text = %q", got)
	}
}

func TestMarshalReplayMovesXMLRejectsUnknownMoveKind(t *testing.T) {
	document := ReplayDocument{moves: []ReplayMove{{Kind: "invented"}}}
	if _, err := marshalReplayMovesXML(document); err == nil {
		t.Fatal("marshal unknown replay move succeeded")
	}
}

func TestReplaySourceActorMatchesLegacyValues(t *testing.T) {
	tests := []struct {
		source ReplayMoveSource
		want   string
	}{
		{ReplayMoveSourceUnknown, ""},
		{ReplayMoveSourceSystem, "系统"},
		{ReplayMoveSourcePlayer, "玩家"},
		{ReplayMoveSourceAI, "AI"},
		{ReplayMoveSourceTimeout, "超时"},
		{ReplayMoveSourceAuto, "托管"},
	}
	for _, test := range tests {
		got, err := replaySourceAttribute(test.source)
		if err != nil || got != test.want {
			t.Fatalf("source %q actor = %q, %v; want %q", test.source, got, err, test.want)
		}
	}
	if _, err := replaySourceAttribute("invented"); err == nil {
		t.Fatal("unknown replay source accepted")
	}
}
