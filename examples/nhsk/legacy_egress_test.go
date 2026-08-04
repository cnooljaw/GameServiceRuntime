package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestEncodeLegacyGameStartBatchExpandsTargetsInOrder(t *testing.T) {
	batch := GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs: []GameOutput{ClientGameOutput{
			Targets: []game.PlayerID{"42", "84"},
			Kind:    OutputGameStart,
			Payload: GameStartPayload{},
		}},
	}
	got, err := encodeLegacyGameOutputBatch(batch)
	if err != nil {
		t.Fatalf("encode Legacy batch: %v", err)
	}
	want := [][]byte{
		decodeEgressGolden(t, "0000000000000000000000004486000000000000720000002200d20400002a0000000000000000000000000000000074000000000000500000002a00000000000000000000005800000052000000000000003800000018000000000000000000000000000000057200000000000018000000"),
		decodeEgressGolden(t, "0000000000000000000000004486000000000000720000002200d2040000540000000000000000000000000000000074000000000000500000005400000000000000000000005800000052000000000000003800000018000000000000000000000000000000057200000000000018000000"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Legacy frames = %x, want %x", got, want)
	}
}

func TestEncodeLegacyGameOutputBatchPreservesClientThenGMControlOrder(t *testing.T) {
	batch := GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs: []GameOutput{
			ClientGameOutput{Targets: []game.PlayerID{"42"}, Kind: OutputGameStart, Payload: GameStartPayload{}},
			GameStartedOutput{ReplayName: "NHSK.xml"},
		},
	}
	got, err := encodeLegacyGameOutputBatch(batch)
	if err != nil {
		t.Fatalf("encode Legacy batch: %v", err)
	}
	want := [][]byte{
		decodeEgressGolden(t, "0000000000000000000000004486000000000000720000002200d20400002a0000000000000000000000000000000074000000000000500000002a00000000000000000000005800000052000000000000003800000018000000000000000000000000000000057200000000000018000000"),
		decodeEgressGolden(t, "0000000000000000000000005486000000000000730000002200d204000000000000014e48534b2e786d6c000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Legacy frames = %x, want %x", got, want)
	}
}

func TestEncodeLegacyGameInfoBatchMatchesReferenceRelayGolden(t *testing.T) {
	batch := GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs: []GameOutput{ClientGameOutput{
			Targets: []game.PlayerID{"42"},
			Kind:    OutputGameInfo,
			Payload: GameInfoPayload{
				OutCardSeconds: 10,
				ServiceFee:     2,
				Scores:         [4]int32{-10, 20, -30, 40},
				GameNum:        7,
			},
		}},
	}
	got, err := encodeLegacyGameOutputBatch(batch)
	if err != nil {
		t.Fatalf("encode Legacy batch: %v", err)
	}
	want := [][]byte{decodeEgressGolden(t, "00000000000000000000000044860000000000008c0000002200d20400002a00000000000000000000000000000000740000000000006a0000002a000000000000000000000058000000520000000000000038000000320000000000000000000000000000000176000000000000320000000a00000002000000f6ffffff14000000e2ffffff280000000700")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Legacy GAME_INFO frames = %x, want %x", got, want)
	}
}

func TestEncodeLegacyDealBatchMatchesReferenceRelayGolden(t *testing.T) {
	batch := GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs: []GameOutput{ClientGameOutput{
			Targets: []game.PlayerID{"1002"},
			Kind:    OutputDeal,
			Payload: DealPayload{
				Players: [4]game.PlayerID{"1001", "1002", "1003", "1004"},
				SeatID:  1,
				Cards:   testDealCards(),
			},
		}},
	}
	got, err := encodeLegacyGameOutputBatch(batch)
	if err != nil {
		t.Fatalf("encode Legacy batch: %v", err)
	}
	want := [][]byte{decodeEgressGolden(t, "0000000000000000000000004486000000000000ea0000002200d2040000ea0300000000000000000000000000000074000000000000c8000000ea03000000000000000000005800000052000000000000003800000090000000000000000000000000000000027600000000000090000000e9030000ea030000eb030000ec03000000000000000000000000000000000000000000000000000000000102030405060708090a0b0c0d0e0f101112131415161718191a00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Legacy DEAL frames = %x, want %x", got, want)
	}
}

func TestEncodeLegacyAskOutCardBatchMatchesReferenceRelayGolden(t *testing.T) {
	batch := GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs: []GameOutput{ClientGameOutput{
			Targets: []game.PlayerID{"1001"},
			Kind:    OutputAskOutCard,
			Payload: AskOutCardPayload{
				ActivePlayer:       "1001",
				VerifyCode:         3,
				ActionMilliseconds: 9000,
			},
		}},
	}
	got, err := encodeLegacyGameOutputBatch(batch)
	if err != nil {
		t.Fatalf("encode Legacy batch: %v", err)
	}
	want := [][]byte{decodeEgressGolden(t, "00000000000000000000000044860000000000007e0000002200d2040000e903000000000000000000000000000000740000000000005c000000e903000000000000000000005800000052000000000000003800000024000000000000000000000000000000037600000000000024000000e90300000300000028230000")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Legacy ASK_OUT_CARD frames = %x, want %x", got, want)
	}
}

func TestEncodeLegacyAskOutCardAllowsObserverTargetsWhenActiveExcluded(t *testing.T) {
	batch := testAskOutCardBatch([]game.PlayerID{"1002", "1003"}, AskOutCardPayload{
		ActivePlayer:       "1001",
		VerifyCode:         3,
		ActionMilliseconds: 9000,
	})
	got, err := encodeLegacyGameOutputBatch(batch)
	if err != nil {
		t.Fatalf("encode broadcast ASK_OUT_CARD: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ASK_OUT_CARD frame count = %d, want 2", len(got))
	}
	if binary.LittleEndian.Uint32(got[1][30:34]) != 1003 || binary.LittleEndian.Uint32(got[1][58:62]) != 1003 {
		t.Fatalf("second ASK_OUT_CARD route users = %d/%d, want 1003/1003", binary.LittleEndian.Uint32(got[1][30:34]), binary.LittleEndian.Uint32(got[1][58:62]))
	}
	if !reflect.DeepEqual(got[0][90:], got[1][90:]) {
		t.Fatal("broadcast ASK_OUT_CARD payload changed between targets")
	}
}

func TestEncodeLegacyOutCardInfoBatchMatchesReferenceRelayGolden(t *testing.T) {
	batch := GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs: []GameOutput{ClientGameOutput{
			Targets: []game.PlayerID{"1002"},
			Kind:    OutputOutCardInfo,
			Payload: OutCardInfoPayload{
				Player:    "1001",
				Cards:     [8]byte{0x03, 0x13},
				CardCount: 2,
			},
		}},
	}
	got, err := encodeLegacyGameOutputBatch(batch)
	if err != nil {
		t.Fatalf("encode Legacy batch: %v", err)
	}
	want := [][]byte{decodeEgressGolden(t, "0000000000000000000000004486000000000000910000002200d2040000ea03000000000000000000000000000000740000000000006f000000ea03000000000000000000005800000052000000000000003800000037000000000000000000000000000000047600000000000037000000e9030000031300000000000000000000000000000000000000000000000002")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Legacy OUT_CARD_INFO frames = %x, want %x", got, want)
	}
}

func TestEncodeLegacyOutCardInfoAllowsPass(t *testing.T) {
	batch := testOutCardInfoBatch(OutCardInfoPayload{Player: "1001"})
	got, err := encodeLegacyGameOutputBatch(batch)
	if err != nil {
		t.Fatalf("encode pass OUT_CARD_INFO: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("pass OUT_CARD_INFO frame count = %d, want 1", len(got))
	}
	if len(got[0]) != 145 {
		t.Fatalf("pass OUT_CARD_INFO frame length = %d, want 145", len(got[0]))
	}
	if got[0][144] != 0 {
		t.Fatalf("pass OUT_CARD_INFO card count = %d, want 0", got[0][144])
	}
}

func TestEncodeLegacyTurnEndBatchMatchesReferenceRelayGolden(t *testing.T) {
	batch := testTurnEndBatch(TurnEndPayload{Winner: "1001", CapturedPoints: 10})
	got, err := encodeLegacyGameOutputBatch(batch)
	if err != nil {
		t.Fatalf("encode Legacy batch: %v", err)
	}
	want := [][]byte{decodeEgressGolden(t, "00000000000000000000000044860000000000007a0000002200d2040000ea030000000000000000000000000000007400000000000058000000ea03000000000000000000005800000052000000000000003800000020000000000000000000000000000000057600000000000020000000e90300000a000000")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Legacy TURN_END frames = %x, want %x", got, want)
	}
}

func TestEncodeLegacyTurnEndAllowsZeroCapturedPoints(t *testing.T) {
	got, err := encodeLegacyGameOutputBatch(testTurnEndBatch(TurnEndPayload{Winner: "1001"}))
	if err != nil {
		t.Fatalf("encode zero-point TURN_END: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("zero-point TURN_END frame count = %d, want 1", len(got))
	}
	if len(got[0]) != 122 {
		t.Fatalf("zero-point TURN_END frame length = %d, want 122", len(got[0]))
	}
}

func TestEncodeLegacyShowCardsBatchMatchesReferenceRelayGolden(t *testing.T) {
	got, err := encodeLegacyGameOutputBatch(testShowCardsBatch(testShowCardsPayload()))
	if err != nil {
		t.Fatalf("encode Legacy batch: %v", err)
	}
	want := [][]byte{decodeEgressGolden(t, "0000000000000000000000004486000000000000ee0000002200d2040000e90300000000000000000000000000000074000000000000cc000000e903000000000000000000005800000052000000000000003800000094000000000000000000000000000000067600000000000094000000e9030000ea030000eb030000ec030000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000515000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000010201")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Legacy SHOW_CARDS frames = %x, want %x", got, want)
	}
}

func TestEncodeLegacyGameResultBatchMatchesReferenceRelayGolden(t *testing.T) {
	got, err := encodeLegacyGameOutputBatch(testGameResultBatch(testGameResultPayload()))
	if err != nil {
		t.Fatalf("encode Legacy batch: %v", err)
	}
	want := [][]byte{decodeEgressGolden(t, "0000000000000000000000004486000000000000f40000002200d2040000e90300000000000000000000000000000074000000000000d2000000e90300000000000000000000580000005200000000000000380000009a00000000000000000000000000000007760000000000009a000000200000007a00000000000000e9030000ea030000eb030000ec03000000010001c800000038ffffffc800000038ffffff000100017800500050007800010302040100726f756e642d667570616e2d31323300000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Legacy GAME_RESULT frames = %x, want %x", got, want)
	}
}

func TestEncodeLegacyGameSceneBatchMatchesReferenceRelayGolden(t *testing.T) {
	got, err := encodeLegacyGameOutputBatch(testGameSceneBatch(testGameScenePayload()))
	if err != nil {
		t.Fatalf("encode Legacy batch: %v", err)
	}
	want := [][]byte{decodeEgressGolden(t, "0000000000000000000000004486000000000000740100002200d2040000e9030000000000000000000000000000007400000000000052010000e90300000000000000000000580000005200000000000000380000001a01000000000000000000000000000008760000000000001a0100002c0000002a0000000400000056000000c4000000030000000100000003000000070000000515000000000000000000000000000000000000000000000201e903000000000313000000000000000000000000000000000000000000000000022000000000000000010000000a000000ea030000010000000000000000000000000000000000000000000000000000000200000000000000000000000014000100eb03000002000000000000000000000000000000000000000000000000000000020000000000000000ffffffff1e000000ec030000030000000000000000000000000000000000000000000000000000000223330000000000000200000028000200")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Legacy GAME_SCENE frames = %x, want %x", got, want)
	}
}

func TestEncodeLegacyGameOutputBatchRejectsInvalidOutput(t *testing.T) {
	valid := GameOutputBatch{
		BattleID:             1,
		MatchID:              2,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 3},
		ConnectionGeneration: 4,
		Outputs: []GameOutput{ClientGameOutput{
			Targets: []game.PlayerID{"42"},
			Kind:    OutputGameStart,
			Payload: GameStartPayload{},
		}},
	}
	tests := []struct {
		name   string
		mutate func(*GameOutputBatch)
	}{
		{name: "empty targets", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{ClientGameOutput{Kind: OutputGameStart, Payload: GameStartPayload{}}}
		}},
		{name: "zero target", mutate: setGameStartTargets("0")},
		{name: "non-numeric target", mutate: setGameStartTargets("player-42")},
		{name: "overflow target", mutate: setGameStartTargets("4294967296")},
		{name: "duplicate target", mutate: setGameStartTargets("42", "42")},
		{name: "unknown kind", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"42"}, Kind: "unknown", Payload: GameStartPayload{}}}
		}},
		{name: "wrong payload", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"42"}, Kind: OutputGameStart, Payload: testOutputPayload{}}}
		}},
		{name: "wrong game info payload", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"42"}, Kind: OutputGameInfo, Payload: GameStartPayload{}}}
		}},
		{name: "deal has multiple targets", mutate: setDealOutput([]game.PlayerID{"1001", "1002"}, testDealPayload())},
		{name: "deal target mismatches seat", mutate: setDealOutput([]game.PlayerID{"1001"}, testDealPayload())},
		{name: "deal seat out of range", mutate: func(batch *GameOutputBatch) {
			payload := testDealPayload()
			payload.SeatID = 4
			setDealOutput([]game.PlayerID{"1002"}, payload)(batch)
		}},
		{name: "deal has zero player", mutate: func(batch *GameOutputBatch) {
			payload := testDealPayload()
			payload.Players[2] = "0"
			setDealOutput([]game.PlayerID{"1002"}, payload)(batch)
		}},
		{name: "deal has duplicate player", mutate: func(batch *GameOutputBatch) {
			payload := testDealPayload()
			payload.Players[2] = payload.Players[1]
			setDealOutput([]game.PlayerID{"1002"}, payload)(batch)
		}},
		{name: "wrong deal payload", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1002"}, Kind: OutputDeal, Payload: GameStartPayload{}}}
		}},
		{name: "ask has zero active player", mutate: setAskOutput([]game.PlayerID{"1001"}, AskOutCardPayload{VerifyCode: 3, ActionMilliseconds: 9000})},
		{name: "ask has non-numeric active player", mutate: setAskOutput([]game.PlayerID{"1001"}, AskOutCardPayload{ActivePlayer: "active", VerifyCode: 3, ActionMilliseconds: 9000})},
		{name: "ask has zero verify code", mutate: setAskOutput([]game.PlayerID{"1001"}, AskOutCardPayload{ActivePlayer: "1001", ActionMilliseconds: 9000})},
		{name: "wrong ask payload", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1001"}, Kind: OutputAskOutCard, Payload: GameStartPayload{}}}
		}},
		{name: "out card info has zero player", mutate: setOutCardInfoOutput(OutCardInfoPayload{CardCount: 1})},
		{name: "out card info has non-numeric player", mutate: setOutCardInfoOutput(OutCardInfoPayload{Player: "player", CardCount: 1})},
		{name: "out card info exceeds rule card count", mutate: setOutCardInfoOutput(OutCardInfoPayload{Player: "1001", CardCount: 9})},
		{name: "wrong out card info payload", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1002"}, Kind: OutputOutCardInfo, Payload: GameStartPayload{}}}
		}},
		{name: "turn end has zero winner", mutate: setTurnEndOutput(TurnEndPayload{CapturedPoints: 10})},
		{name: "turn end has non-numeric winner", mutate: setTurnEndOutput(TurnEndPayload{Winner: "winner", CapturedPoints: 10})},
		{name: "wrong turn end payload", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1002"}, Kind: OutputTurnEnd, Payload: GameStartPayload{}}}
		}},
		{name: "show cards has zero player", mutate: func(batch *GameOutputBatch) {
			payload := testShowCardsPayload()
			payload.Players[2] = "0"
			setShowCardsOutput(payload)(batch)
		}},
		{name: "show cards has duplicate player", mutate: func(batch *GameOutputBatch) {
			payload := testShowCardsPayload()
			payload.Players[2] = payload.Players[1]
			setShowCardsOutput(payload)(batch)
		}},
		{name: "show cards count exceeds hand capacity", mutate: func(batch *GameOutputBatch) {
			payload := testShowCardsPayload()
			payload.HandCounts[2] = 27
			setShowCardsOutput(payload)(batch)
		}},
		{name: "show cards leaks card after count", mutate: func(batch *GameOutputBatch) {
			payload := testShowCardsPayload()
			payload.Cards[2][2] = 0x25
			setShowCardsOutput(payload)(batch)
		}},
		{name: "wrong show cards payload", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1001"}, Kind: OutputShowCards, Payload: GameStartPayload{}}}
		}},
		{name: "game result has zero player", mutate: func(batch *GameOutputBatch) {
			payload := testGameResultPayload()
			payload.Players[2] = "0"
			setGameResultOutput(payload)(batch)
		}},
		{name: "game result has duplicate player", mutate: func(batch *GameOutputBatch) {
			payload := testGameResultPayload()
			payload.Players[2] = payload.Players[1]
			setGameResultOutput(payload)(batch)
		}},
		{name: "game result has invalid reason", mutate: func(batch *GameOutputBatch) {
			payload := testGameResultPayload()
			payload.Reason = GameOverReasonDissolve + 1
			setGameResultOutput(payload)(batch)
		}},
		{name: "game result has invalid outcome", mutate: func(batch *GameOutputBatch) {
			payload := testGameResultPayload()
			payload.Outcomes[2] = PlayerOutcomePeace + 1
			setGameResultOutput(payload)(batch)
		}},
		{name: "game result has invalid rank", mutate: func(batch *GameOutputBatch) {
			payload := testGameResultPayload()
			payload.Ranks[2] = 0
			setGameResultOutput(payload)(batch)
		}},
		{name: "game result has invalid result", mutate: func(batch *GameOutputBatch) {
			payload := testGameResultPayload()
			payload.Result = SubgameResultPeace + 1
			setGameResultOutput(payload)(batch)
		}},
		{name: "peace game result has winning team", mutate: func(batch *GameOutputBatch) {
			payload := testGameResultPayload()
			payload.Result = SubgameResultPeace
			payload.WinningTeam = 1
			setGameResultOutput(payload)(batch)
		}},
		{name: "wrong game result payload", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1001"}, Kind: OutputGameResult, Payload: GameStartPayload{}}}
		}},
		{name: "game scene has multiple targets", mutate: setGameSceneOutput([]game.PlayerID{"1001", "1002"}, testGameScenePayload())},
		{name: "game scene target is not a player", mutate: setGameSceneOutput([]game.PlayerID{"1005"}, testGameScenePayload())},
		{name: "game scene has invalid state", mutate: func(batch *GameOutputBatch) {
			payload := testGameScenePayload()
			payload.State = 2
			setGameSceneOutput([]game.PlayerID{"1001"}, payload)(batch)
		}},
		{name: "game scene has invalid active seat", mutate: func(batch *GameOutputBatch) {
			payload := testGameScenePayload()
			payload.ActiveSeat = 4
			setGameSceneOutput([]game.PlayerID{"1001"}, payload)(batch)
		}},
		{name: "game scene has too many trick cards", mutate: func(batch *GameOutputBatch) {
			payload := testGameScenePayload()
			payload.TrickScoreCardCount = 25
			setGameSceneOutput([]game.PlayerID{"1001"}, payload)(batch)
		}},
		{name: "game scene has zero player", mutate: func(batch *GameOutputBatch) {
			payload := testGameScenePayload()
			payload.Players[2].Player = "0"
			setGameSceneOutput([]game.PlayerID{"1001"}, payload)(batch)
		}},
		{name: "game scene has invalid hand count", mutate: func(batch *GameOutputBatch) {
			payload := testGameScenePayload()
			payload.Players[2].HandCount = 27
			setGameSceneOutput([]game.PlayerID{"1001"}, payload)(batch)
		}},
		{name: "game scene has invalid last play count", mutate: func(batch *GameOutputBatch) {
			payload := testGameScenePayload()
			payload.Players[2].LastPlayCount = 9
			setGameSceneOutput([]game.PlayerID{"1001"}, payload)(batch)
		}},
		{name: "game scene leaks cards after hand count", mutate: func(batch *GameOutputBatch) {
			payload := testGameScenePayload()
			payload.Players[0].HandCards[2] = 0x23
			setGameSceneOutput([]game.PlayerID{"1001"}, payload)(batch)
		}},
		{name: "game scene has cards for pass", mutate: func(batch *GameOutputBatch) {
			payload := testGameScenePayload()
			payload.Players[1].LastPlayedCards[0] = 0x04
			setGameSceneOutput([]game.PlayerID{"1001"}, payload)(batch)
		}},
		{name: "game scene has invalid rank", mutate: func(batch *GameOutputBatch) {
			payload := testGameScenePayload()
			payload.Players[2].Rank = 5
			setGameSceneOutput([]game.PlayerID{"1001"}, payload)(batch)
		}},
		{name: "wrong game scene payload", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1001"}, Kind: OutputGameScene, Payload: GameStartPayload{}}}
		}},
		{name: "unsupported output", mutate: func(batch *GameOutputBatch) { batch.Outputs = []GameOutput{unsupportedGameOutput{}} }},
		{name: "empty replay name", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = []GameOutput{GameStartedOutput{}}
		}},
		{name: "invalid output after valid output", mutate: func(batch *GameOutputBatch) {
			batch.Outputs = append(batch.Outputs, unsupportedGameOutput{})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			batch := valid
			test.mutate(&batch)
			frames, err := encodeLegacyGameOutputBatch(batch)
			if !errors.Is(err, errInvalidLegacyGameOutput) {
				t.Fatalf("encode error = %v, want errInvalidLegacyGameOutput", err)
			}
			if len(frames) != 0 {
				t.Fatalf("invalid batch returned %d partial frames", len(frames))
			}
		})
	}
}

type testOutputPayload struct{}

func (testOutputPayload) isNHSKOutputPayload() {}

type unsupportedGameOutput struct{}

func (unsupportedGameOutput) isNHSKGameOutput() {}

func setGameStartTargets(targets ...game.PlayerID) func(*GameOutputBatch) {
	return func(batch *GameOutputBatch) {
		batch.Outputs = []GameOutput{ClientGameOutput{Targets: targets, Kind: OutputGameStart, Payload: GameStartPayload{}}}
	}
}

func decodeEgressGolden(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	return data
}

func testDealCards() [26]byte {
	return [26]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26}
}

func testDealPayload() DealPayload {
	return DealPayload{
		Players: [4]game.PlayerID{"1001", "1002", "1003", "1004"},
		SeatID:  1,
		Cards:   testDealCards(),
	}
}

func setDealOutput(targets []game.PlayerID, payload DealPayload) func(*GameOutputBatch) {
	return func(batch *GameOutputBatch) {
		batch.Outputs = []GameOutput{ClientGameOutput{Targets: targets, Kind: OutputDeal, Payload: payload}}
	}
}

func testAskOutCardBatch(targets []game.PlayerID, payload AskOutCardPayload) GameOutputBatch {
	return GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs:              []GameOutput{ClientGameOutput{Targets: targets, Kind: OutputAskOutCard, Payload: payload}},
	}
}

func setAskOutput(targets []game.PlayerID, payload AskOutCardPayload) func(*GameOutputBatch) {
	return func(batch *GameOutputBatch) {
		batch.Outputs = []GameOutput{ClientGameOutput{Targets: targets, Kind: OutputAskOutCard, Payload: payload}}
	}
}

func testOutCardInfoBatch(payload OutCardInfoPayload) GameOutputBatch {
	return GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs:              []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1002"}, Kind: OutputOutCardInfo, Payload: payload}},
	}
}

func setOutCardInfoOutput(payload OutCardInfoPayload) func(*GameOutputBatch) {
	return func(batch *GameOutputBatch) {
		batch.Outputs = testOutCardInfoBatch(payload).Outputs
	}
}

func testTurnEndBatch(payload TurnEndPayload) GameOutputBatch {
	return GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs:              []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1002"}, Kind: OutputTurnEnd, Payload: payload}},
	}
}

func setTurnEndOutput(payload TurnEndPayload) func(*GameOutputBatch) {
	return func(batch *GameOutputBatch) {
		batch.Outputs = testTurnEndBatch(payload).Outputs
	}
}

func testShowCardsPayload() ShowCardsPayload {
	return ShowCardsPayload{
		Players:    [4]game.PlayerID{"1001", "1002", "1003", "1004"},
		HandCounts: [4]uint8{0, 1, 2, 1},
		Cards:      [4][26]byte{2: {0x05, 0x15}},
	}
}

func testShowCardsBatch(payload ShowCardsPayload) GameOutputBatch {
	return GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs:              []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1001"}, Kind: OutputShowCards, Payload: payload}},
	}
}

func setShowCardsOutput(payload ShowCardsPayload) func(*GameOutputBatch) {
	return func(batch *GameOutputBatch) {
		batch.Outputs = testShowCardsBatch(payload).Outputs
	}
}

func testGameResultPayload() GameResultPayload {
	return GameResultPayload{
		Reason:         GameOverReasonSuccess,
		Players:        [4]game.PlayerID{"1001", "1002", "1003", "1004"},
		Automated:      [4]bool{false, true, false, true},
		Scores:         [4]int32{200, -200, 200, -200},
		Outcomes:       [4]PlayerOutcome{PlayerOutcomeWin, PlayerOutcomeLoss, PlayerOutcomeWin, PlayerOutcomeLoss},
		CapturedPoints: [4]uint16{120, 80, 80, 120},
		Ranks:          [4]uint8{1, 3, 2, 4},
		Result:         SubgameResultDouble,
		WinningTeam:    0,
		ReplayUID:      "round-fupan-123",
	}
}

func testGameResultBatch(payload GameResultPayload) GameOutputBatch {
	return GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs:              []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1001"}, Kind: OutputGameResult, Payload: payload}},
	}
}

func setGameResultOutput(payload GameResultPayload) func(*GameOutputBatch) {
	return func(batch *GameOutputBatch) {
		batch.Outputs = testGameResultBatch(payload).Outputs
	}
}

func testGameScenePayload() GameScenePayload {
	return GameScenePayload{
		State:               GameSceneStatePlaying,
		ActiveSeat:          1,
		PreviousPlayerSeat:  3,
		RemainingSeconds:    7,
		TrickScoreCards:     [24]byte{0x05, 0x15},
		TrickScoreCardCount: 2,
		FinishedPlayerCount: 1,
		Players: [4]GameScenePlayer{
			{Player: "1001", HandCards: [26]byte{0x03, 0x13}, HandCount: 2, LastPlayedCards: [8]byte{0x20}, LastPlayCount: 1, CapturedPoints: 10},
			{Player: "1002", Automated: true, HandCount: 2, LastPlayCount: 0, CapturedPoints: 20, Rank: 1},
			{Player: "1003", Offline: true, HandCount: 2, LastPlayCount: -1, CapturedPoints: 30},
			{Player: "1004", Automated: true, Offline: true, HandCount: 2, LastPlayedCards: [8]byte{0x23, 0x33}, LastPlayCount: 2, CapturedPoints: 40, Rank: 2},
		},
	}
}

func testGameSceneBatch(payload GameScenePayload) GameOutputBatch {
	return GameOutputBatch{
		BattleID:             1234,
		MatchID:              88,
		ProductID:            82,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: 99},
		ConnectionGeneration: 7,
		Outputs:              []GameOutput{ClientGameOutput{Targets: []game.PlayerID{"1001"}, Kind: OutputGameScene, Payload: payload}},
	}
}

func setGameSceneOutput(targets []game.PlayerID, payload GameScenePayload) func(*GameOutputBatch) {
	return func(batch *GameOutputBatch) {
		batch.Outputs = testGameSceneBatch(payload).Outputs
		batch.Outputs[0] = ClientGameOutput{Targets: targets, Kind: OutputGameScene, Payload: payload}
	}
}
