package game

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// LedgerRunner adapts external LedgerStore work to the Runtime-owned Core Runner.
type LedgerRunner struct {
	core *gsr.Runner[LedgerTask, ledgerResult]
}

// NewLedgerRunner creates the Wallet adapter over a Runtime-owned Core Runner.
func NewLedgerRunner(runtime *gsr.Runtime, config LedgerRunnerConfig) (*LedgerRunner, error) {
	if isNil(runtime) || isNil(config.Store) || config.Workers <= 0 || config.QueueSize <= 0 || config.Timeout <= 0 {
		return nil, ErrInvalidConfig
	}
	core, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "game-ledger", Workers: config.Workers, QueueSize: config.QueueSize}, func(root context.Context, task LedgerTask) (ledgerResult, error) {
		result := processLedgerTask(root, config.Store, config.Timeout, task.Request)
		return ledgerResult{Request: cloneSettlementRequest(task.Request), Result: cloneSettlementResult(result.result), Terminal: result.terminal}, nil
	})
	if err != nil {
		return nil, err
	}
	return &LedgerRunner{core: core}, nil
}

// Submit queues one validated task without blocking a Service Handler.
func (r *LedgerRunner) Submit(task LedgerTask) error {
	if r == nil || !validServiceRef(task.Wallet) || !validServiceRef(task.Source) || validateSettlementRequest(task.Request) != nil || task.Request.Source != task.Source {
		return ErrInvalidSettlement
	}
	task.Request = cloneSettlementRequest(task.Request)
	err := r.core.Submit(context.Background(), task.Wallet, commandApplyLedgerResult, task)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

// Close stops accepting work, cancels not-yet-completed I/O contexts and waits for workers.
func (r *LedgerRunner) Close(ctx context.Context) error {
	if r == nil {
		return ErrInvalidConfig
	}
	if err := usableContext(ctx); err != nil {
		return err
	}
	return r.core.Close(ctx)
}

type runnerResult struct {
	result   SettlementResult
	terminal bool
}

func processLedgerTask(root context.Context, store LedgerStore, timeout time.Duration, request SettlementRequest) runnerResult {
	ctx, cancel := context.WithTimeout(root, timeout)
	defer cancel()
	result, exists, err := store.Lookup(ctx, request.RequestID)
	if err != nil {
		return runnerResult{}
	}
	if exists {
		return runnerResult{result: result, terminal: result.State == SettlementCommitted || result.State == SettlementRejected}
	}
	result, err = store.Commit(ctx, LedgerRecord{Request: cloneSettlementRequest(request)})
	if err != nil {
		return runnerResult{}
	}
	return runnerResult{result: result, terminal: result.State == SettlementCommitted || result.State == SettlementRejected}
}
