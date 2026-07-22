package snapshot

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// Manager captures versioned Service state and persists it through a Store.
type Manager struct {
	caller          CommandCaller
	store           Store
	maxPayloadBytes int
	now             func() time.Time
}

// NewManager creates a Snapshot Manager from narrow Runtime and Store capabilities.
func NewManager(caller CommandCaller, store Store, config Config) (*Manager, error) {
	if isNil(caller) || isNil(store) || config.MaxPayloadBytes < 0 {
		return nil, ErrInvalidConfig
	}
	if config.MaxPayloadBytes == 0 {
		config.MaxPayloadBytes = defaultMaxPayloadBytes
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Manager{
		caller:          caller,
		store:           store,
		maxPayloadBytes: config.MaxPayloadBytes,
		now:             config.Now,
	}, nil
}

// Capture calls target through its Mailbox, validates the reply, and saves it.
func (m *Manager) Capture(ctx context.Context, target gsr.ServiceRef, key Key) (Snapshot, error) {
	if isNil(ctx) {
		return Snapshot{}, ErrInvalidContext
	}
	if err := context.Cause(ctx); err != nil {
		return Snapshot{}, err
	}
	if err := validateTarget(target); err != nil {
		return Snapshot{}, err
	}
	if err := validateKey(key); err != nil {
		return Snapshot{}, err
	}

	value, err := m.caller.Call(ctx, target, CaptureCommand, CaptureRequest{Key: key})
	if err != nil {
		return Snapshot{}, err
	}
	response, ok := value.(CaptureResponse)
	if !ok {
		return Snapshot{}, ErrInvalidResponse
	}
	if err := validateKey(response.Key); err != nil || response.Key != key {
		return Snapshot{}, ErrInvalidResponse
	}
	if err := validateState(response.State, m.maxPayloadBytes); err != nil {
		return Snapshot{}, err
	}
	result := Snapshot{
		Key:        key,
		Source:     target,
		CapturedAt: m.now(),
		State:      cloneState(response.State),
	}
	if err := validateSnapshot(result, m.maxPayloadBytes); err != nil {
		return Snapshot{}, err
	}
	canonical, err := m.store.Save(ctx, cloneSnapshot(result))
	if err != nil {
		return Snapshot{}, err
	}
	if canonical.Key != key || !equalState(canonical.State, result.State) {
		return Snapshot{}, ErrInvalidResponse
	}
	if err := validateSnapshot(canonical, m.maxPayloadBytes); err != nil {
		return Snapshot{}, ErrInvalidResponse
	}
	return cloneSnapshot(canonical), nil
}

// Load validates and returns an independent Snapshot stored for key.
func (m *Manager) Load(ctx context.Context, key Key) (Snapshot, error) {
	if isNil(ctx) {
		return Snapshot{}, ErrInvalidContext
	}
	if err := context.Cause(ctx); err != nil {
		return Snapshot{}, err
	}
	if err := validateKey(key); err != nil {
		return Snapshot{}, err
	}

	result, err := m.store.Load(ctx, key)
	if err != nil {
		return Snapshot{}, err
	}
	if result.Key != key {
		return Snapshot{}, ErrInvalidResponse
	}
	if err := validateSnapshot(result, m.maxPayloadBytes); err != nil {
		return Snapshot{}, err
	}
	return cloneSnapshot(result), nil
}
