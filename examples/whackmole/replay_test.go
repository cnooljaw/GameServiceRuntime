package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/record"
)

func TestWhackMoleRecordReplayReproducesScoreInIsolatedRuntime(t *testing.T) {
	originalRuntime := gsr.NewRuntime(gsr.Config{NodeID: "original", Workers: 1})
	t.Cleanup(func() { _ = originalRuntime.Close(context.Background()) })
	recorder, err := record.NewRecorderService(record.RecorderConfig{MaxEntries: 16})
	if err != nil {
		t.Fatal(err)
	}
	recorderRef, err := originalRuntime.CreateService(gsr.ServiceSpec{Service: recorder})
	if err != nil {
		t.Fatal(err)
	}
	originalLogic := newWhackMoleLogic(7)
	originalBattle, err := game.NewBattleService(game.BattleConfig{ID: "battle-replay", Participants: []game.Participant{{Player: "alice"}}, Logic: originalLogic})
	if err != nil {
		t.Fatal(err)
	}
	decorated, err := record.NewDecorator(originalBattle, recorderRef, "battle-replay", whackRecordCodec{}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	originalRef, err := originalRuntime.CreateService(gsr.ServiceSpec{Service: decorated})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []gsr.Command{
		{ID: game.StartBattleCommand, Payload: struct{}{}},
		{ID: StartCommand, Payload: struct{}{}},
		{ID: KickCommand, Payload: KickRequest{Player: "alice", Shrew: 1}},
	} {
		if _, err := originalRuntime.Call(context.Background(), originalRef, command.ID, command.Payload); err != nil {
			t.Fatalf("original Call(%d) error = %v", command.ID, err)
		}
	}
	client, err := record.NewClient(originalRuntime)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := client.List(context.Background(), recorderRef, "battle-replay", 0, 16)
	if err != nil || len(entries) != 3 {
		t.Fatalf("records = %#v, %v", entries, err)
	}
	bundle := record.RecordBundle{FormatVersion: record.FormatVersion, TargetKey: "battle-replay", Entries: entries}

	var replayRuntime *gsr.Runtime
	var replayLogic *whackMoleLogic
	err = record.Replay(context.Background(), bundle, whackRecordCodec{}, func(context.Context, record.RecordBundle) (record.ReplayTarget, error) {
		replayRuntime = gsr.NewRuntime(gsr.Config{NodeID: "replay", Workers: 1})
		replayLogic = newWhackMoleLogic(7)
		battle, createErr := game.NewBattleService(game.BattleConfig{ID: "battle-replay", Participants: []game.Participant{{Player: "alice"}}, Logic: replayLogic})
		if createErr != nil {
			return record.ReplayTarget{}, createErr
		}
		ref, createErr := replayRuntime.CreateService(gsr.ServiceSpec{Service: battle})
		if createErr != nil {
			return record.ReplayTarget{}, createErr
		}
		return record.ReplayTarget{Runtime: replayRuntime, Ref: ref}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replayRuntime.Close(context.Background()) })
	if _, err := replayRuntime.Call(context.Background(), gsr.ServiceRef{Node: "replay", ID: 1}, game.GetBattleSnapshotCommand, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if originalLogic.scores["alice"] != replayLogic.scores["alice"] || replayLogic.scores["alice"] != 1 {
		t.Fatalf("scores original=%#v replay=%#v", originalLogic.scores, replayLogic.scores)
	}
}

type whackRecordCodec struct{}

func (whackRecordCodec) Encode(command gsr.CommandID, payload any) ([]byte, error) {
	return json.Marshal(payload)
}
func (whackRecordCodec) Decode(command gsr.CommandID, payload []byte) (any, error) {
	switch command {
	case game.StartBattleCommand, StartCommand:
		return struct{}{}, nil
	case KickCommand:
		var request KickRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, err
		}
		return request, nil
	default:
		return nil, record.ErrCodecDecode
	}
}
