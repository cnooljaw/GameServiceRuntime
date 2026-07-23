package game

import (
	"context"
	"sync"
)

// MemoryLedgerStore is a process-local LedgerStore for tests and examples only.
type MemoryLedgerStore struct {
	mu       sync.RWMutex
	records  map[RequestID]LedgerRecord
	balances map[Currency]map[PlayerID]Amount
}

// NewMemoryLedgerStore creates an empty in-memory idempotent ledger.
func NewMemoryLedgerStore() *MemoryLedgerStore {
	return &MemoryLedgerStore{records: make(map[RequestID]LedgerRecord), balances: make(map[Currency]map[PlayerID]Amount)}
}

// Commit atomically stores one canonical request and its committed balance projection.
func (s *MemoryLedgerStore) Commit(ctx context.Context, record LedgerRecord) (SettlementResult, error) {
	if s == nil {
		return SettlementResult{}, ErrInvalidConfig
	}
	if err := usableContext(ctx); err != nil {
		return SettlementResult{}, err
	}
	if err := validateSettlementRequest(record.Request); err != nil {
		return SettlementResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, exists := s.records[record.Request.RequestID]; exists {
		if !sameSettlementRequest(current.Request, record.Request) {
			return SettlementResult{}, ErrRequestConflict
		}
		return cloneSettlementResult(current.Result), nil
	}
	balances := s.balances[record.Request.Currency]
	if balances == nil {
		balances = make(map[PlayerID]Amount)
		s.balances[record.Request.Currency] = balances
	}
	result := SettlementResult{RequestID: record.Request.RequestID, State: SettlementCommitted, Currency: record.Request.Currency, Balances: make([]Balance, len(record.Request.Entries))}
	for index, entry := range record.Request.Entries {
		balances[entry.Player] += entry.Delta
		result.Balances[index] = Balance{Player: entry.Player, Currency: record.Request.Currency, Amount: balances[entry.Player]}
	}
	s.records[record.Request.RequestID] = LedgerRecord{Request: cloneSettlementRequest(record.Request), Result: cloneSettlementResult(result)}
	return cloneSettlementResult(result), nil
}

// Lookup returns an independent terminal result for request ID when it exists.
func (s *MemoryLedgerStore) Lookup(ctx context.Context, requestID RequestID) (SettlementResult, bool, error) {
	if s == nil {
		return SettlementResult{}, false, ErrInvalidConfig
	}
	if err := usableContext(ctx); err != nil {
		return SettlementResult{}, false, err
	}
	if err := validateRequestID(requestID); err != nil {
		return SettlementResult{}, false, err
	}
	s.mu.RLock()
	record, exists := s.records[requestID]
	s.mu.RUnlock()
	if !exists {
		return SettlementResult{}, false, nil
	}
	return cloneSettlementResult(record.Result), true, nil
}

func sameSettlementRequest(left, right SettlementRequest) bool {
	if left.RequestID != right.RequestID || left.Source != right.Source || left.Currency != right.Currency || len(left.Entries) != len(right.Entries) {
		return false
	}
	for index := range left.Entries {
		if left.Entries[index] != right.Entries[index] {
			return false
		}
	}
	return true
}
