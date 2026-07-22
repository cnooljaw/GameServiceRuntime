package snapshot

import (
	"context"
	"sync"
)

// MemoryStore keeps the newest Snapshot revision for each Key in memory.
type MemoryStore struct {
	mu        sync.RWMutex
	snapshots map[Key]Snapshot
}

// NewMemoryStore creates an empty, concurrent-safe in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{snapshots: make(map[Key]Snapshot)}
}

// Save atomically stores a newer Snapshot revision and returns the retained value.
func (s *MemoryStore) Save(ctx context.Context, candidate Snapshot) (Snapshot, error) {
	if s == nil {
		return Snapshot{}, ErrInvalidConfig
	}
	if isNil(ctx) {
		return Snapshot{}, ErrInvalidContext
	}
	if err := context.Cause(ctx); err != nil {
		return Snapshot{}, err
	}
	if err := validateSnapshot(candidate, 0); err != nil {
		return Snapshot{}, err
	}
	candidate = cloneSnapshot(candidate)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshots == nil {
		s.snapshots = make(map[Key]Snapshot)
	}
	current, exists := s.snapshots[candidate.Key]
	switch {
	case !exists || candidate.State.Revision > current.State.Revision:
		s.snapshots[candidate.Key] = candidate
		return cloneSnapshot(candidate), nil
	case candidate.State.Revision < current.State.Revision:
		return Snapshot{}, ErrStaleSnapshot
	case equalState(candidate.State, current.State):
		return cloneSnapshot(current), nil
	default:
		return Snapshot{}, ErrSnapshotConflict
	}
}

// Load returns an independent copy of the Snapshot stored for key.
func (s *MemoryStore) Load(ctx context.Context, key Key) (Snapshot, error) {
	if s == nil {
		return Snapshot{}, ErrInvalidConfig
	}
	if isNil(ctx) {
		return Snapshot{}, ErrInvalidContext
	}
	if err := context.Cause(ctx); err != nil {
		return Snapshot{}, err
	}
	if err := validateKey(key); err != nil {
		return Snapshot{}, err
	}

	s.mu.RLock()
	snapshot, exists := s.snapshots[key]
	s.mu.RUnlock()
	if !exists {
		return Snapshot{}, ErrSnapshotNotFound
	}
	return cloneSnapshot(snapshot), nil
}
