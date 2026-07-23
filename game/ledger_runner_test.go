package game

import (
	"context"
	"sync"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestLedgerRunnerReturnsCommittedResultAndCloses(t *testing.T) {
	runtime := &ledgerRunnerTestRuntime{}
	runner, err := NewLedgerRunner(runtime, LedgerRunnerConfig{Store: NewMemoryLedgerStore(), Workers: 1, QueueSize: 1, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	request := SettlementRequest{RequestID: "settle-runner", Source: gsr.ServiceRef{Node: "battle", ID: 1}, Currency: "coin", Entries: []SettlementEntry{{Player: "alice", Delta: 1}}}
	if err := runner.Submit(LedgerTask{Wallet: gsr.ServiceRef{Node: "wallet", ID: 1}, Source: request.Source, Request: request}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && runtime.count() == 0 {
		time.Sleep(time.Millisecond)
	}
	if runtime.count() != 1 {
		t.Fatal("LedgerRunner did not return a result")
	}
	message := runtime.first()
	result, ok := message.payload.(ledgerResult)
	if !ok || !result.Terminal || result.Result.State != SettlementCommitted || message.command != commandApplyLedgerResult {
		t.Fatalf("runner result = %#v", message)
	}
	if err := runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(LedgerTask{Wallet: gsr.ServiceRef{Node: "wallet", ID: 1}, Source: request.Source, Request: request}); err != ErrUnavailable {
		t.Fatalf("Submit(after Close) error = %v, want ErrUnavailable", err)
	}
}

type ledgerRunnerTestRuntime struct {
	mu   sync.Mutex
	sent []battleTestSend
}

func (r *ledgerRunnerTestRuntime) Send(target gsr.ServiceRef, command gsr.CommandID, payload any) error {
	r.mu.Lock()
	r.sent = append(r.sent, battleTestSend{target: target, command: command, payload: payload})
	r.mu.Unlock()
	return nil
}
func (r *ledgerRunnerTestRuntime) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.sent) }
func (r *ledgerRunnerTestRuntime) first() battleTestSend {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sent[0]
}
