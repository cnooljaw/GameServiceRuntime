package main

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestGameOutputServiceSerializesBatchesForOneGeneration(t *testing.T) {
	sink := &recordingGameOutputSink{received: make(chan struct{}, 3)}
	reporter := &recordingConnectionFailureReporter{}
	spec, err := newGameOutputServiceSpec(7, sink, reporter)
	if err != nil {
		t.Fatalf("new output service: %v", err)
	}
	if spec.Policy.Mailbox != gsr.DiscardMailbox {
		t.Fatalf("mailbox policy = %v, want DiscardMailbox", spec.Policy.Mailbox)
	}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk", Workers: 4})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	ref, err := runtime.CreateService(spec)
	if err != nil {
		t.Fatalf("create output service: %v", err)
	}

	for battleID := game.BattleID(1); battleID <= 3; battleID++ {
		if err := runtime.Send(ref, deliverGameOutputBatchCommand, testOutputBatch(battleID, 7)); err != nil {
			t.Fatalf("send batch %d: %v", battleID, err)
		}
	}
	for range 3 {
		select {
		case <-sink.received:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for output batch")
		}
	}

	got := sink.snapshot()
	want := []game.BattleID{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sink batch order = %v, want %v", got, want)
	}
	if len(reporter.snapshot()) != 0 {
		t.Fatalf("unexpected failure reports = %v", reporter.snapshot())
	}
}

func TestGameOutputServiceReportsSinkRejection(t *testing.T) {
	sinkError := errors.New("sink queue full")
	sink := &recordingGameOutputSink{err: sinkError}
	reporter := &recordingConnectionFailureReporter{}
	spec, err := newGameOutputServiceSpec(9, sink, reporter)
	if err != nil {
		t.Fatalf("new output service: %v", err)
	}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	ref, err := runtime.CreateService(spec)
	if err != nil {
		t.Fatalf("create output service: %v", err)
	}

	_, err = runtime.Call(context.Background(), ref, deliverGameOutputBatchCommand, testOutputBatch(1, 9))
	if !errors.Is(err, sinkError) {
		t.Fatalf("deliver error = %v, want sink error", err)
	}
	want := []failureReport{{generation: 9, kind: ConnectionFailureOutputSinkRejected}}
	if got := reporter.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("failure reports = %v, want %v", got, want)
	}
}

func TestGameOutputServiceRejectsStaleOrMalformedBatchWithoutFailingConnection(t *testing.T) {
	tests := []struct {
		name  string
		batch GameOutputBatch
		err   error
	}{
		{name: "old generation", batch: testOutputBatch(1, 6), err: errOutputGenerationMismatch},
		{name: "zero battle", batch: testOutputBatch(0, 7), err: errInvalidGameOutputBatch},
		{name: "zero ref", batch: GameOutputBatch{BattleID: 1, ConnectionGeneration: 7, Outputs: []GameOutput{testGameOutput{value: 1}}}, err: errInvalidGameOutputBatch},
		{name: "empty outputs", batch: GameOutputBatch{BattleID: 1, Ref: gsr.ServiceRef{Node: "nhsk", ID: 10}, ConnectionGeneration: 7}, err: errInvalidGameOutputBatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sink := &recordingGameOutputSink{}
			reporter := &recordingConnectionFailureReporter{}
			spec, err := newGameOutputServiceSpec(7, sink, reporter)
			if err != nil {
				t.Fatalf("new output service: %v", err)
			}
			runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk"})
			t.Cleanup(func() { _ = runtime.Close(context.Background()) })
			ref, err := runtime.CreateService(spec)
			if err != nil {
				t.Fatalf("create output service: %v", err)
			}
			if _, err := runtime.Call(context.Background(), ref, deliverGameOutputBatchCommand, test.batch); !errors.Is(err, test.err) {
				t.Fatalf("deliver error = %v, want %v", err, test.err)
			}
			if len(sink.snapshot()) != 0 || len(reporter.snapshot()) != 0 {
				t.Fatalf("invalid batch reached sink or reporter: sink=%v reporter=%v", sink.snapshot(), reporter.snapshot())
			}
		})
	}
}

func TestGameOutputServiceRejectsInvalidConstructionAndUnknownCommand(t *testing.T) {
	sink := &recordingGameOutputSink{}
	reporter := &recordingConnectionFailureReporter{}
	if _, err := newGameOutputServiceSpec(0, sink, reporter); err == nil {
		t.Fatal("zero generation output service succeeded")
	}
	if _, err := newGameOutputServiceSpec(1, nil, reporter); err == nil {
		t.Fatal("nil sink output service succeeded")
	}
	if _, err := newGameOutputServiceSpec(1, sink, nil); err == nil {
		t.Fatal("nil reporter output service succeeded")
	}

	spec, err := newGameOutputServiceSpec(1, sink, reporter)
	if err != nil {
		t.Fatalf("new output service: %v", err)
	}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	ref, err := runtime.CreateService(spec)
	if err != nil {
		t.Fatalf("create output service: %v", err)
	}
	if _, err := runtime.Call(context.Background(), ref, 0x0410ffff, struct{}{}); !errors.Is(err, gsr.ErrUnknownCommand) {
		t.Fatalf("unknown command error = %v, want ErrUnknownCommand", err)
	}
}

type testGameOutput struct{ value int }

func (testGameOutput) isNHSKGameOutput() {}

func testOutputBatch(battleID game.BattleID, generation ConnectionGeneration) GameOutputBatch {
	return GameOutputBatch{
		BattleID:             battleID,
		Ref:                  gsr.ServiceRef{Node: "nhsk", ID: gsr.ServiceID(battleID + 10)},
		ConnectionGeneration: generation,
		Outputs:              []GameOutput{testGameOutput{value: int(battleID)}},
	}
}

type recordingGameOutputSink struct {
	mu       sync.Mutex
	battles  []game.BattleID
	received chan struct{}
	err      error
}

func (sink *recordingGameOutputSink) Submit(batch GameOutputBatch) error {
	if sink.err != nil {
		return sink.err
	}
	sink.mu.Lock()
	sink.battles = append(sink.battles, batch.BattleID)
	sink.mu.Unlock()
	if sink.received != nil {
		sink.received <- struct{}{}
	}
	return nil
}

func (sink *recordingGameOutputSink) snapshot() []game.BattleID {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]game.BattleID(nil), sink.battles...)
}

type failureReport struct {
	generation ConnectionGeneration
	kind       ConnectionFailureKind
}

type recordingConnectionFailureReporter struct {
	mu      sync.Mutex
	reports []failureReport
}

func (reporter *recordingConnectionFailureReporter) FailConnection(generation ConnectionGeneration, kind ConnectionFailureKind) {
	reporter.mu.Lock()
	reporter.reports = append(reporter.reports, failureReport{generation: generation, kind: kind})
	reporter.mu.Unlock()
}

func (reporter *recordingConnectionFailureReporter) snapshot() []failureReport {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	return append([]failureReport(nil), reporter.reports...)
}
