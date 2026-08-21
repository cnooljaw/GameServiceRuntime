package game

import (
	"context"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestLedgerRunnerReturnsCommittedResultAndCloses(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "wallet"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	receiver := &ledgerRunnerResultService{sent: make(chan ledgerResult, 1)}
	wallet, err := runtime.CreateService(gsr.ServiceSpec{Service: receiver})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewLedgerRunner(runtime, LedgerRunnerConfig{Store: NewMemoryLedgerStore(), Workers: 1, QueueSize: 1, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	request := SettlementRequest{RequestID: "settle-runner", Source: gsr.ServiceRef{Node: "battle", ID: 1}, Currency: "coin", Entries: []SettlementEntry{{Player: "alice", Delta: 1}}}
	if err := runner.Submit(LedgerTask{Wallet: wallet, Source: request.Source, Request: request}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-receiver.sent:
		if !result.Terminal || result.Result.State != SettlementCommitted {
			t.Fatalf("runner result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("LedgerRunner did not return a result")
	}
	if err := runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(LedgerTask{Wallet: wallet, Source: request.Source, Request: request}); err != ErrUnavailable {
		t.Fatalf("Submit(after Close) error = %v, want ErrUnavailable", err)
	}
}

type ledgerRunnerResultService struct{ sent chan ledgerResult }

func (*ledgerRunnerResultService) Init(gsr.ServiceContext) error { return nil }
func (service *ledgerRunnerResultService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandApplyLedgerResult {
		return gsr.ErrUnknownCommand
	}
	result, ok := command.Payload.(gsr.RunnerResult[ledgerResult])
	if !ok || result.Err != nil {
		return ErrInvalidSettlement
	}
	service.sent <- result.Value
	return nil
}
func (*ledgerRunnerResultService) Stop(context.Context) error { return nil }
func (*ledgerRunnerResultService) Close() error               { return nil }
