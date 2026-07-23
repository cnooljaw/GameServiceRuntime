package game

import (
	"context"
	"sync"
	"time"
)

// LedgerRunner executes bounded external LedgerStore work and returns facts by private Command.
type LedgerRunner struct {
	runtime LedgerRuntime
	store   LedgerStore
	timeout time.Duration
	tasks   chan LedgerTask
	cancel  context.CancelFunc
	workers sync.WaitGroup
	mu      sync.Mutex
	closed  bool
}

// NewLedgerRunner creates and starts a fixed worker pool owned by the composition root.
func NewLedgerRunner(runtime LedgerRuntime, config LedgerRunnerConfig) (*LedgerRunner, error) {
	if isNil(runtime) || isNil(config.Store) || config.Workers <= 0 || config.QueueSize <= 0 || config.Timeout <= 0 {
		return nil, ErrInvalidConfig
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := &LedgerRunner{runtime: runtime, store: config.Store, timeout: config.Timeout, tasks: make(chan LedgerTask, config.QueueSize), cancel: cancel}
	for index := 0; index < config.Workers; index++ {
		runner.workers.Add(1)
		go runner.run(ctx)
	}
	return runner, nil
}

// Submit queues one validated task without blocking a Service Handler.
func (r *LedgerRunner) Submit(task LedgerTask) error {
	if r == nil || !validServiceRef(task.Wallet) || !validServiceRef(task.Source) || validateSettlementRequest(task.Request) != nil || task.Request.Source != task.Source {
		return ErrInvalidSettlement
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrUnavailable
	}
	select {
	case r.tasks <- LedgerTask{Wallet: task.Wallet, Source: task.Source, Request: cloneSettlementRequest(task.Request)}:
		return nil
	default:
		return ErrUnavailable
	}
}

// Close stops accepting work, cancels not-yet-completed I/O contexts and waits for workers.
func (r *LedgerRunner) Close(ctx context.Context) error {
	if r == nil {
		return ErrInvalidConfig
	}
	if err := usableContext(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.tasks)
		r.cancel()
	}
	r.mu.Unlock()
	done := make(chan struct{})
	go func() { r.workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (r *LedgerRunner) run(root context.Context) {
	defer r.workers.Done()
	for task := range r.tasks {
		result := r.process(root, task.Request)
		_ = r.runtime.Send(task.Wallet, commandApplyLedgerResult, ledgerResult{Request: cloneSettlementRequest(task.Request), Result: cloneSettlementResult(result.result), Terminal: result.terminal})
	}
}

type runnerResult struct {
	result   SettlementResult
	terminal bool
}

func (r *LedgerRunner) process(root context.Context, request SettlementRequest) runnerResult {
	ctx, cancel := context.WithTimeout(root, r.timeout)
	defer cancel()
	result, exists, err := r.store.Lookup(ctx, request.RequestID)
	if err != nil {
		return runnerResult{}
	}
	if exists {
		return runnerResult{result: result, terminal: result.State == SettlementCommitted || result.State == SettlementRejected}
	}
	result, err = r.store.Commit(ctx, LedgerRecord{Request: cloneSettlementRequest(request)})
	if err != nil {
		return runnerResult{}
	}
	return runnerResult{result: result, terminal: result.State == SettlementCommitted || result.State == SettlementRejected}
}
