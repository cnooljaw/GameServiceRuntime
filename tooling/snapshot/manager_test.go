package snapshot

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestManagerCaptureCallsServiceBeforeStore(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	target := gsr.ServiceRef{Node: "node-a", ID: 7}
	key := Key{Namespace: "player", ID: "42"}
	caller := &fakeCaller{value: CaptureResponse{Key: key, State: State{
		Schema: "player", Version: 1, Revision: 3, Payload: []byte("ok"),
	}}}
	store := &recordingStore{caller: caller}
	manager, err := NewManager(caller, store, Config{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	got, err := manager.Capture(context.Background(), target, key)
	if err != nil {
		t.Fatal(err)
	}
	if caller.command != CaptureCommand || caller.target != target {
		t.Fatalf("call target=%v command=%v, want %v %v", caller.target, caller.command, target, CaptureCommand)
	}
	request, ok := caller.payload.(CaptureRequest)
	if !ok {
		t.Fatalf("payload = %T, want CaptureRequest", caller.payload)
	}
	if request.Key != key {
		t.Fatalf("request Key = %#v, want %#v", request.Key, key)
	}
	if got.Key != key || got.Source != target || !got.CapturedAt.Equal(now) {
		t.Fatalf("snapshot = %#v", got)
	}
	if store.saved.State.Revision != 3 || string(store.saved.State.Payload) != "ok" {
		t.Fatalf("saved = %#v", store.saved)
	}
}

func TestManagerCaptureCopiesCallerAndStoreValues(t *testing.T) {
	responsePayload := []byte("state")
	caller := &fakeCaller{value: CaptureResponse{Key: testKey(), State: State{
		Schema: "player", Version: 1, Revision: 3, Payload: responsePayload,
	}}}
	store := &recordingStore{caller: caller, mutateSavedPayload: true}
	manager := newTestManager(t, caller, store, Config{})

	got, err := manager.Capture(context.Background(), testTarget(), testKey())
	if err != nil {
		t.Fatal(err)
	}
	responsePayload[0] = 'X'
	if string(got.State.Payload) != "state" {
		t.Fatalf("Capture payload = %q, want independent state", got.State.Payload)
	}
}

func TestManagerRejectsNilPayloadBeforeCopy(t *testing.T) {
	caller := &fakeCaller{value: CaptureResponse{Key: testKey(), State: State{
		Schema: "player", Version: 1, Revision: 1, Payload: nil,
	}}}
	store := &recordingStore{}
	manager := newTestManager(t, caller, store, Config{})

	if _, err := manager.Capture(context.Background(), testTarget(), testKey()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("Capture error = %v, want ErrInvalidState", err)
	}
	if store.saveCalled {
		t.Fatal("Store.Save called for nil payload")
	}
}

func TestManagerRejectsInvalidOrMismatchedResponseKey(t *testing.T) {
	tests := []struct {
		name string
		key  Key
	}{
		{name: "empty", key: Key{}},
		{name: "mismatched", key: Key{Namespace: "player", ID: "other"}},
		{name: "invalid UTF-8", key: Key{Namespace: string([]byte{0xff}), ID: "42"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caller := validFakeCaller()
			response := caller.value.(CaptureResponse)
			response.Key = test.key
			caller.value = response
			store := &recordingStore{}
			manager := newTestManager(t, caller, store, Config{})

			if _, err := manager.Capture(context.Background(), testTarget(), testKey()); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("Capture error = %v, want ErrInvalidResponse", err)
			}
			if store.saveCalled {
				t.Fatal("Store.Save called for invalid response Key")
			}
		})
	}
}

func TestManagerRejectsInvalidConfiguration(t *testing.T) {
	validCaller := &fakeCaller{}
	validStore := &recordingStore{}
	var nilCaller *fakeCaller
	var nilStore *recordingStore
	tests := []struct {
		name   string
		caller CommandCaller
		store  Store
		config Config
	}{
		{name: "nil caller", store: validStore},
		{name: "typed nil caller", caller: nilCaller, store: validStore},
		{name: "nil store", caller: validCaller},
		{name: "typed nil store", caller: validCaller, store: nilStore},
		{name: "negative maximum", caller: validCaller, store: validStore, config: Config{MaxPayloadBytes: -1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewManager(test.caller, test.store, test.config); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("NewManager error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestManagerCaptureValidatesInputs(t *testing.T) {
	caller := validFakeCaller()
	store := &recordingStore{}
	manager := newTestManager(t, caller, store, Config{})

	if _, err := manager.Capture(nil, testTarget(), testKey()); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil context error = %v, want ErrInvalidContext", err)
	}
	var typedNil *testContext
	if _, err := manager.Capture(typedNil, testTarget(), testKey()); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("typed nil context error = %v, want ErrInvalidContext", err)
	}
	if _, err := manager.Capture(context.Background(), gsr.ServiceRef{}, testKey()); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("invalid target error = %v, want ErrInvalidTarget", err)
	}
	if _, err := manager.Capture(context.Background(), testTarget(), Key{}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("invalid key error = %v, want ErrInvalidKey", err)
	}
	if _, err := manager.Capture(context.Background(), testTarget(), Key{Namespace: string([]byte{0xff}), ID: "42"}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("invalid UTF-8 key error = %v, want ErrInvalidKey", err)
	}
	canceled, cancel := context.WithCancelCause(context.Background())
	want := errors.New("capture canceled")
	cancel(want)
	if _, err := manager.Capture(canceled, testTarget(), testKey()); !errors.Is(err, want) {
		t.Fatalf("canceled context error = %v, want %v", err, want)
	}
	if caller.called {
		t.Fatal("caller was invoked for invalid inputs")
	}
}

func TestManagerCapturePreservesCallAndStoreErrors(t *testing.T) {
	callErr := errors.New("call failed")
	caller := &fakeCaller{err: callErr}
	store := &recordingStore{}
	manager := newTestManager(t, caller, store, Config{})
	if got, err := manager.Capture(context.Background(), testTarget(), testKey()); !errors.Is(err, callErr) || !zeroSnapshot(got) {
		t.Fatalf("Capture = %#v, %v, want zero and call error", got, err)
	}
	if store.saveCalled {
		t.Fatal("Store.Save called after Call failure")
	}

	storeErr := errors.New("store failed")
	caller = validFakeCaller()
	store = &recordingStore{saveErr: storeErr}
	manager = newTestManager(t, caller, store, Config{})
	if got, err := manager.Capture(context.Background(), testTarget(), testKey()); !errors.Is(err, storeErr) || !zeroSnapshot(got) {
		t.Fatalf("Capture = %#v, %v, want zero and store error", got, err)
	}
}

func TestManagerCaptureValidatesResponseAndPayloadLimit(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		config Config
		want   error
	}{
		{name: "wrong response", value: "state", want: ErrInvalidResponse},
		{name: "invalid state", value: CaptureResponse{Key: testKey(), State: State{}}, want: ErrInvalidState},
		{name: "invalid UTF-8 schema", value: CaptureResponse{Key: testKey(), State: State{Schema: string([]byte{0xff}), Version: 1, Revision: 1, Payload: []byte("state")}}, want: ErrInvalidState},
		{name: "payload too large", value: CaptureResponse{Key: testKey(), State: State{Schema: "player", Version: 1, Revision: 1, Payload: []byte("large")}}, config: Config{MaxPayloadBytes: 4}, want: ErrPayloadTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingStore{}
			manager := newTestManager(t, &fakeCaller{value: test.value}, store, test.config)
			if _, err := manager.Capture(context.Background(), testTarget(), testKey()); !errors.Is(err, test.want) {
				t.Fatalf("Capture error = %v, want %v", err, test.want)
			}
			if store.saveCalled {
				t.Fatal("Store.Save called with invalid response")
			}
		})
	}
}

func TestManagerUsesOneMiBDefaultPayloadLimit(t *testing.T) {
	const oneMiB = 1 << 20
	store := &recordingStore{}
	caller := &fakeCaller{value: CaptureResponse{Key: testKey(), State: State{
		Schema: "player", Version: 1, Revision: 1, Payload: make([]byte, oneMiB),
	}}}
	manager := newTestManager(t, caller, store, Config{})
	if _, err := manager.Capture(context.Background(), testTarget(), testKey()); err != nil {
		t.Fatalf("Capture at default limit error = %v", err)
	}
	caller.value = CaptureResponse{Key: testKey(), State: State{
		Schema: "player", Version: 1, Revision: 2, Payload: make([]byte, oneMiB+1),
	}}
	if _, err := manager.Capture(context.Background(), testTarget(), testKey()); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Capture above default limit error = %v, want ErrPayloadTooLarge", err)
	}
}

func TestManagerCaptureReturnsStoreCanonicalSnapshot(t *testing.T) {
	caller := validFakeCaller()
	canonical := validSnapshot(3, []byte("state"))
	canonical.Source = gsr.ServiceRef{Node: "node-old", ID: 2}
	canonical.CapturedAt = time.Date(2026, 7, 22, 11, 0, 0, 0, time.UTC)
	store := &recordingStore{saveResult: &canonical}
	manager := newTestManager(t, caller, store, Config{})

	got, err := manager.Capture(context.Background(), testTarget(), testKey())
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != canonical.Source || !got.CapturedAt.Equal(canonical.CapturedAt) {
		t.Fatalf("Capture = %#v, want canonical %#v", got, canonical)
	}
}

func TestManagerCaptureRejectsInvalidStoreCanonicalSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Snapshot)
	}{
		{name: "wrong key", change: func(snapshot *Snapshot) { snapshot.Key.ID = "other" }},
		{name: "different state", change: func(snapshot *Snapshot) { snapshot.State.Revision++ }},
		{name: "invalid source", change: func(snapshot *Snapshot) { snapshot.Source = gsr.ServiceRef{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			canonical := validSnapshot(3, []byte("state"))
			test.change(&canonical)
			manager := newTestManager(t, validFakeCaller(), &recordingStore{saveResult: &canonical}, Config{})
			if got, err := manager.Capture(context.Background(), testTarget(), testKey()); !errors.Is(err, ErrInvalidResponse) || !zeroSnapshot(got) {
				t.Fatalf("Capture = %#v, %v, want zero and ErrInvalidResponse", got, err)
			}
		})
	}
}

func TestManagerLoadValidatesAndCopiesStoreResult(t *testing.T) {
	stored := validSnapshot(4, []byte("loaded"))
	store := &recordingStore{loaded: stored}
	manager := newTestManager(t, validFakeCaller(), store, Config{})

	got, err := manager.Load(context.Background(), stored.Key)
	if err != nil {
		t.Fatal(err)
	}
	store.loaded.State.Payload[0] = 'X'
	if string(got.State.Payload) != "loaded" {
		t.Fatalf("Load payload = %q, want independent loaded", got.State.Payload)
	}

	store.loaded.Key = Key{Namespace: "player", ID: "other"}
	if _, err := manager.Load(context.Background(), stored.Key); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("mismatched key error = %v, want ErrInvalidResponse", err)
	}

	store.loaded = stored
	store.loaded.State.Payload = []byte("large")
	limited := newTestManager(t, validFakeCaller(), store, Config{MaxPayloadBytes: 4})
	if _, err := limited.Load(context.Background(), stored.Key); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversized Load error = %v, want ErrPayloadTooLarge", err)
	}
}

func TestManagerLoadPreservesStoreError(t *testing.T) {
	want := errors.New("load failed")
	manager := newTestManager(t, validFakeCaller(), &recordingStore{loadErr: want}, Config{})
	if _, err := manager.Load(context.Background(), testKey()); !errors.Is(err, want) {
		t.Fatalf("Load error = %v, want %v", err, want)
	}
}

type fakeCaller struct {
	value     any
	err       error
	called    bool
	completed bool
	target    gsr.ServiceRef
	command   gsr.CommandID
	payload   any
}

func (c *fakeCaller) Call(_ context.Context, target gsr.ServiceRef, command gsr.CommandID, payload any) (any, error) {
	c.called = true
	c.target = target
	c.command = command
	c.payload = payload
	c.completed = true
	return c.value, c.err
}

type recordingStore struct {
	caller             *fakeCaller
	saved              Snapshot
	loaded             Snapshot
	saveErr            error
	loadErr            error
	saveCalled         bool
	mutateSavedPayload bool
	saveResult         *Snapshot
}

func (s *recordingStore) Save(_ context.Context, snapshot Snapshot) (Snapshot, error) {
	s.saveCalled = true
	if s.caller != nil && !s.caller.completed {
		return Snapshot{}, errors.New("Store.Save ran before Call completed")
	}
	s.saved = cloneSnapshotForTest(snapshot)
	result := cloneSnapshotForTest(snapshot)
	if s.saveResult != nil {
		result = cloneSnapshotForTest(*s.saveResult)
	}
	if s.mutateSavedPayload && len(snapshot.State.Payload) > 0 {
		snapshot.State.Payload[0] = 'S'
	}
	if s.saveErr != nil {
		return Snapshot{}, s.saveErr
	}
	return result, nil
}

func (s *recordingStore) Load(_ context.Context, _ Key) (Snapshot, error) {
	return s.loaded, s.loadErr
}

func validFakeCaller() *fakeCaller {
	return &fakeCaller{value: CaptureResponse{Key: testKey(), State: State{
		Schema: "player", Version: 1, Revision: 3, Payload: []byte("state"),
	}}}
}

func newTestManager(t *testing.T, caller CommandCaller, store Store, config Config) *Manager {
	t.Helper()
	if config.Now == nil {
		config.Now = func() time.Time { return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC) }
	}
	manager, err := NewManager(caller, store, config)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func testTarget() gsr.ServiceRef { return gsr.ServiceRef{Node: "node-a", ID: 7} }
func testKey() Key               { return Key{Namespace: "player", ID: "42"} }

func zeroSnapshot(snapshot Snapshot) bool {
	return snapshot.Key == (Key{}) && snapshot.Source == (gsr.ServiceRef{}) && snapshot.CapturedAt.IsZero() &&
		snapshot.State.Schema == "" && snapshot.State.Version == 0 && snapshot.State.Revision == 0 && snapshot.State.Payload == nil
}
