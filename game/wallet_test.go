package game

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestMemoryLedgerStoreIsIdempotentAndCopiesResults(t *testing.T) {
	store := NewMemoryLedgerStore()
	request := SettlementRequest{RequestID: "settle-42", Source: gsr.ServiceRef{Node: "battle", ID: 1}, Currency: "coin", Entries: []SettlementEntry{{Player: "alice", Delta: 5}, {Player: "bob", Delta: -5}}}
	result, err := store.Commit(context.Background(), LedgerRecord{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != SettlementCommitted || len(result.Balances) != 2 {
		t.Fatalf("Commit result = %#v", result)
	}
	result.Balances[0].Amount = 999
	loaded, exists, err := store.Lookup(context.Background(), request.RequestID)
	if err != nil || !exists || loaded.Balances[0].Amount == 999 {
		t.Fatalf("Lookup = %#v, %t, %v", loaded, exists, err)
	}
	if _, err := store.Commit(context.Background(), LedgerRecord{Request: request}); err != nil {
		t.Fatalf("duplicate Commit error = %v", err)
	}
	conflict := request
	conflict.Entries = []SettlementEntry{{Player: "alice", Delta: 6}, {Player: "bob", Delta: -6}}
	if _, err := store.Commit(context.Background(), LedgerRecord{Request: conflict}); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("conflicting Commit error = %v, want ErrRequestConflict", err)
	}
}

func TestWalletAppliesOnlyTrustedLedgerResultAndNotifiesBattle(t *testing.T) {
	executor := &walletTestExecutor{}
	wallet, err := NewWalletService(WalletConfig{Executor: executor, MaxPending: 2, RunnerNode: "runner"})
	if err != nil {
		t.Fatal(err)
	}
	serviceContext := &walletTestServiceContext{self: gsr.ServiceRef{Node: "wallet", ID: 1}}
	if err := wallet.Init(serviceContext); err != nil {
		t.Fatal(err)
	}
	request := SettlementRequest{RequestID: "settle-42", Source: gsr.ServiceRef{Node: "battle", ID: 1}, Currency: "coin", Entries: []SettlementEntry{{Player: "alice", Delta: 5}}}
	if err := wallet.Handle(&walletTestCommandContext{source: request.Source}, gsr.Command{ID: CommitSettlementCommand, Payload: request}); err != nil {
		t.Fatal(err)
	}
	if len(executor.tasks) != 1 {
		t.Fatalf("executor tasks = %#v", executor.tasks)
	}
	runnerFailure := gsr.RunnerResult[ledgerResult]{Err: gsr.ErrRunnerClosed}
	if err := wallet.Handle(&walletTestCommandContext{source: gsr.ServiceRef{Node: "other"}}, gsr.Command{ID: commandApplyLedgerResult, Payload: runnerFailure}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("untrusted Runner failure error = %v, want ErrUnauthorized", err)
	}
	if err := wallet.Handle(&walletTestCommandContext{source: gsr.ServiceRef{Node: "runner"}}, gsr.Command{ID: commandApplyLedgerResult, Payload: runnerFailure}); err != nil {
		t.Fatalf("trusted Runner failure error = %v, want nil", err)
	}
	if wallet.results[request.RequestID].State != SettlementPending || len(serviceContext.sent) != 0 {
		t.Fatalf("Runner failure changed Wallet state: result=%#v sent=%#v", wallet.results[request.RequestID], serviceContext.sent)
	}
	result := SettlementResult{RequestID: "settle-42", State: SettlementCommitted, Currency: "coin", Balances: []Balance{{Player: "alice", Currency: "coin", Amount: 5}}}
	if err := wallet.Handle(&walletTestCommandContext{source: gsr.ServiceRef{Node: "other"}}, gsr.Command{ID: commandApplyLedgerResult, Payload: gsr.RunnerResult[ledgerResult]{Value: ledgerResult{Request: request, Result: result, Terminal: true}}}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("untrusted Apply error = %v, want ErrUnauthorized", err)
	}
	if err := wallet.Handle(&walletTestCommandContext{source: gsr.ServiceRef{Node: "runner"}}, gsr.Command{ID: commandApplyLedgerResult, Payload: gsr.RunnerResult[ledgerResult]{Value: ledgerResult{Request: request, Result: result, Terminal: true}}}); err != nil {
		t.Fatal(err)
	}
	if len(serviceContext.sent) != 1 || serviceContext.sent[0].target != request.Source || serviceContext.sent[0].command != ApplySettlementResultCommand {
		t.Fatalf("battle notifications = %#v", serviceContext.sent)
	}
}

type walletTestExecutor struct{ tasks []LedgerTask }

func (e *walletTestExecutor) Submit(task LedgerTask) error {
	e.tasks = append(e.tasks, task)
	return nil
}

type walletTestServiceContext struct {
	self gsr.ServiceRef
	sent []battleTestSend
}

func (c *walletTestServiceContext) Self() gsr.ServiceRef { return c.self }
func (c *walletTestServiceContext) Send(target gsr.ServiceRef, command gsr.CommandID, payload any) error {
	c.sent = append(c.sent, battleTestSend{target: target, command: command, payload: payload})
	return nil
}
func (*walletTestServiceContext) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}
func (*walletTestServiceContext) After(time.Duration, gsr.CommandID, any) (gsr.TimerID, error) {
	return 0, nil
}
func (*walletTestServiceContext) Now() time.Time { return time.Unix(1, 0) }
func (*walletTestServiceContext) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
func (*walletTestServiceContext) Metrics() gsr.Metrics { return roomPlayerMetrics{} }

type walletTestCommandContext struct {
	source gsr.ServiceRef
	reply  any
}

func (*walletTestCommandContext) Self() gsr.ServiceRef     { return gsr.ServiceRef{Node: "wallet", ID: 1} }
func (c *walletTestCommandContext) Source() gsr.ServiceRef { return c.source }
func (c *walletTestCommandContext) Reply(value any) error  { c.reply = value; return nil }
