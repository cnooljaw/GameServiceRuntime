package nhsk

import (
	"testing"
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

	got, err := marshalReplayMovesXML(document)
	if err != nil {
		t.Fatal(err)
	}
	want := `<?xml version="1.0" encoding="UTF-8"?>
<Moves Count="5">
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
</Moves>`
	if string(got) != want {
		t.Fatalf("moves XML mismatch\ngot:\n%s\nwant:\n%s", got, want)
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
		got, err := replaySourceActor(test.source)
		if err != nil || got != test.want {
			t.Fatalf("source %q actor = %q, %v; want %q", test.source, got, err, test.want)
		}
	}
	if _, err := replaySourceActor("invented"); err == nil {
		t.Fatal("unknown replay source accepted")
	}
}
