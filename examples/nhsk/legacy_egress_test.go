package main

import (
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
