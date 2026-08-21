package game

import (
	"context"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type ledgerResult struct {
	Request  SettlementRequest
	Result   SettlementResult
	Terminal bool
}

// BalanceQuery asks WalletService for one known balance projection.
type BalanceQuery struct {
	Player   PlayerID
	Currency Currency
}

// WalletService owns settlement request state and applies external LedgerRunner results.
type WalletService struct {
	executor   LedgerExecutor
	maxPending int
	runnerNode gsr.NodeID
	service    gsr.ServiceContext
	requests   map[RequestID]SettlementRequest
	results    map[RequestID]SettlementResult
	balances   map[Currency]map[PlayerID]Amount
}

// NewWalletService creates a WalletService with a bounded pending request set.
func NewWalletService(config WalletConfig) (*WalletService, error) {
	if isNil(config.Executor) || config.MaxPending <= 0 || config.RunnerNode == "" {
		return nil, ErrInvalidConfig
	}
	return &WalletService{executor: config.Executor, maxPending: config.MaxPending, runnerNode: config.RunnerNode, requests: make(map[RequestID]SettlementRequest), results: make(map[RequestID]SettlementResult), balances: make(map[Currency]map[PlayerID]Amount)}, nil
}

// Commands declares Wallet's public request/query Commands and private Runner result Commands.

// Init stores only the Service capability required to submit results back to Battle.
func (s *WalletService) Init(serviceContext gsr.ServiceContext) error {
	if isNil(serviceContext) {
		return ErrInvalidConfig
	}
	s.service = serviceContext
	return nil
}

// Handle serializes Wallet request facts and never calls LedgerStore directly.
func (s *WalletService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case CommitSettlementCommand:
		request, ok := command.Payload.(SettlementRequest)
		if !ok {
			return ErrInvalidSettlement
		}
		return s.commit(commandContext, request)
	case GetSettlementCommand:
		requestID, ok := command.Payload.(RequestID)
		if !ok || validateRequestID(requestID) != nil {
			return ErrInvalidRequestID
		}
		result, exists := s.results[requestID]
		if !exists {
			return ErrNotFound
		}
		return reply(commandContext, cloneSettlementResult(result))
	case GetBalanceCommand:
		query, ok := command.Payload.(BalanceQuery)
		if !ok || validateID(query.Player) != nil || !validText(string(query.Currency), maxBusinessIDBytes) {
			return ErrInvalidCommand
		}
		return reply(commandContext, Balance{Player: query.Player, Currency: query.Currency, Amount: s.balances[query.Currency][query.Player]})
	case commandApplyLedgerResult:
		runnerResult, ok := command.Payload.(gsr.RunnerResult[ledgerResult])
		if !ok {
			return ErrInvalidSettlement
		}
		if commandContext.Source() != (gsr.ServiceRef{Node: s.runnerNode}) {
			return ErrUnauthorized
		}
		if runnerResult.Err != nil {
			return nil
		}
		return s.apply(commandContext, runnerResult.Value)
	case commandRecoverSettlement:
		requestID, ok := command.Payload.(RequestID)
		if !ok || validateRequestID(requestID) != nil {
			return ErrInvalidRequestID
		}
		return s.recover(commandContext, requestID)
	default:
		return gsr.ErrUnknownCommand
	}
}

// Stop only releases lifecycle-local work references; it does not manufacture terminal results.
func (*WalletService) Stop(context.Context) error { return nil }

// Close releases WalletService's Service capability.
func (s *WalletService) Close() error { s.service = nil; return nil }

func (s *WalletService) commit(commandContext gsr.CommandContext, request SettlementRequest) error {
	if validateSettlementRequest(request) != nil || request.Source != commandContext.Source() {
		return ErrInvalidSettlement
	}
	if current, exists := s.requests[request.RequestID]; exists {
		if !sameSettlementRequest(current, request) {
			return ErrRequestConflict
		}
		return reply(commandContext, cloneSettlementResult(s.results[request.RequestID]))
	}
	if s.pendingCount() >= s.maxPending {
		return ErrUnavailable
	}
	pending := SettlementResult{RequestID: request.RequestID, State: SettlementPending, Currency: request.Currency}
	s.requests[request.RequestID] = cloneSettlementRequest(request)
	s.results[request.RequestID] = pending
	if err := s.executor.Submit(LedgerTask{Wallet: s.service.Self(), Source: request.Source, Request: cloneSettlementRequest(request)}); err != nil {
		rejected := SettlementResult{RequestID: request.RequestID, State: SettlementRejected, Currency: request.Currency, Reason: "unavailable"}
		s.results[request.RequestID] = rejected
		return reply(commandContext, cloneSettlementResult(rejected))
	}
	return reply(commandContext, pending)
}

func (s *WalletService) apply(commandContext gsr.CommandContext, applied ledgerResult) error {
	if commandContext.Source() != (gsr.ServiceRef{Node: s.runnerNode}) {
		return ErrUnauthorized
	}
	request, exists := s.requests[applied.Request.RequestID]
	if !exists || !sameSettlementRequest(request, applied.Request) {
		return ErrNotFound
	}
	if !applied.Terminal {
		return reply(commandContext, cloneSettlementResult(s.results[request.RequestID]))
	}
	if applied.Result.State != SettlementCommitted && applied.Result.State != SettlementRejected || applied.Result.RequestID != request.RequestID || applied.Result.Currency != request.Currency {
		return ErrInvalidSettlement
	}
	current := s.results[request.RequestID]
	if current.State != SettlementPending {
		return reply(commandContext, cloneSettlementResult(current))
	}
	result := cloneSettlementResult(applied.Result)
	s.results[request.RequestID] = result
	if result.State == SettlementCommitted {
		for _, balance := range result.Balances {
			currency := s.balances[balance.Currency]
			if currency == nil {
				currency = make(map[PlayerID]Amount)
				s.balances[balance.Currency] = currency
			}
			currency[balance.Player] = balance.Amount
		}
	}
	if err := s.service.Send(request.Source, ApplySettlementResultCommand, cloneSettlementResult(result)); err != nil {
		s.service.Metrics().Inc("wallet_result_notify_failed_total")
	}
	return reply(commandContext, cloneSettlementResult(result))
}

func (s *WalletService) recover(commandContext gsr.CommandContext, requestID RequestID) error {
	request, exists := s.requests[requestID]
	if !exists {
		return ErrNotFound
	}
	if s.results[requestID].State != SettlementPending {
		return reply(commandContext, cloneSettlementResult(s.results[requestID]))
	}
	if err := s.executor.Submit(LedgerTask{Wallet: s.service.Self(), Source: request.Source, Request: cloneSettlementRequest(request)}); err != nil {
		return ErrUnavailable
	}
	return reply(commandContext, cloneSettlementResult(s.results[requestID]))
}

func (s *WalletService) pendingCount() int {
	count := 0
	for _, result := range s.results {
		if result.State == SettlementPending {
			count++
		}
	}
	return count
}
