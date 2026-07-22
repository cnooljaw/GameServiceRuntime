package snapshot

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestMemoryStoreReturnsIndependentCopies(t *testing.T) {
	store := NewMemoryStore()
	original := validSnapshot(2, []byte("state"))
	if _, err := store.Save(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	original.State.Payload[0] = 'X'

	loaded, err := store.Load(context.Background(), original.Key)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded.State.Payload) != "state" {
		t.Fatalf("payload = %q, want state", loaded.State.Payload)
	}
	loaded.State.Payload[0] = 'Y'

	again, err := store.Load(context.Background(), original.Key)
	if err != nil {
		t.Fatal(err)
	}
	if string(again.State.Payload) != "state" {
		t.Fatalf("payload = %q after caller mutation, want state", again.State.Payload)
	}
}

func TestMemoryStoreOrdersRevisionsAndDetectsConflict(t *testing.T) {
	store := NewMemoryStore()
	current := validSnapshot(2, []byte("two"))
	if _, err := store.Save(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(context.Background(), validSnapshot(1, []byte("one"))); !errors.Is(err, ErrStaleSnapshot) {
		t.Fatalf("Save stale error = %v, want ErrStaleSnapshot", err)
	}
	if _, err := store.Save(context.Background(), validSnapshot(2, []byte("other"))); !errors.Is(err, ErrSnapshotConflict) {
		t.Fatalf("Save conflict error = %v, want ErrSnapshotConflict", err)
	}
	if _, err := store.Save(context.Background(), current); err != nil {
		t.Fatalf("idempotent Save error = %v", err)
	}
	newer := validSnapshot(3, []byte("three"))
	if _, err := store.Save(context.Background(), newer); err != nil {
		t.Fatalf("newer Save error = %v", err)
	}
	loaded, err := store.Load(context.Background(), newer.Key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.Revision != 3 || string(loaded.State.Payload) != "three" {
		t.Fatalf("loaded = %#v, want revision 3", loaded)
	}
}

func TestMemoryStoreIdempotentSaveReturnsExistingCanonicalSnapshot(t *testing.T) {
	store := NewMemoryStore()
	original := validSnapshot(2, []byte("state"))
	if _, err := store.Save(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	retry := cloneSnapshotForTest(original)
	retry.Source = gsr.ServiceRef{Node: "node-b", ID: 8}
	retry.CapturedAt = original.CapturedAt.Add(time.Minute)

	canonical, err := store.Save(context.Background(), retry)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Source != original.Source || !canonical.CapturedAt.Equal(original.CapturedAt) {
		t.Fatalf("canonical = %#v, want original metadata", canonical)
	}
	canonical.State.Payload[0] = 'X'
	loaded, err := store.Load(context.Background(), original.Key)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded.State.Payload) != "state" {
		t.Fatalf("stored payload = %q after canonical mutation, want state", loaded.State.Payload)
	}
}

func TestMemoryStoreValidatesSnapshot(t *testing.T) {
	valid := validSnapshot(1, []byte{})
	tests := []struct {
		name   string
		change func(*Snapshot)
		want   error
	}{
		{name: "empty namespace", change: func(snapshot *Snapshot) { snapshot.Key.Namespace = "" }, want: ErrInvalidKey},
		{name: "untrimmed namespace", change: func(snapshot *Snapshot) { snapshot.Key.Namespace = " player" }, want: ErrInvalidKey},
		{name: "invalid UTF-8 namespace", change: func(snapshot *Snapshot) { snapshot.Key.Namespace = string([]byte{0xff}) }, want: ErrInvalidKey},
		{name: "empty id", change: func(snapshot *Snapshot) { snapshot.Key.ID = "" }, want: ErrInvalidKey},
		{name: "long id", change: func(snapshot *Snapshot) { snapshot.Key.ID = string(make([]byte, 257)) }, want: ErrInvalidKey},
		{name: "empty source node", change: func(snapshot *Snapshot) { snapshot.Source.Node = "" }, want: ErrInvalidTarget},
		{name: "zero source id", change: func(snapshot *Snapshot) { snapshot.Source.ID = 0 }, want: ErrInvalidTarget},
		{name: "empty schema", change: func(snapshot *Snapshot) { snapshot.State.Schema = "" }, want: ErrInvalidState},
		{name: "untrimmed schema", change: func(snapshot *Snapshot) { snapshot.State.Schema = " player" }, want: ErrInvalidState},
		{name: "invalid UTF-8 schema", change: func(snapshot *Snapshot) { snapshot.State.Schema = string([]byte{0xff}) }, want: ErrInvalidState},
		{name: "zero version", change: func(snapshot *Snapshot) { snapshot.State.Version = 0 }, want: ErrInvalidState},
		{name: "zero revision", change: func(snapshot *Snapshot) { snapshot.State.Revision = 0 }, want: ErrInvalidState},
		{name: "nil payload", change: func(snapshot *Snapshot) { snapshot.State.Payload = nil }, want: ErrInvalidState},
		{name: "zero captured time", change: func(snapshot *Snapshot) { snapshot.CapturedAt = time.Time{} }, want: ErrInvalidState},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneSnapshotForTest(valid)
			test.change(&candidate)
			if _, err := NewMemoryStore().Save(context.Background(), candidate); !errors.Is(err, test.want) {
				t.Fatalf("Save error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestMemoryStoreRejectsInvalidContextAndMissingKey(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.Save(nil, validSnapshot(1, []byte("state"))); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("Save nil context error = %v, want ErrInvalidContext", err)
	}
	var typedNil *testContext
	if _, err := store.Load(typedNil, Key{Namespace: "player", ID: "42"}); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("Load typed nil context error = %v, want ErrInvalidContext", err)
	}
	if _, err := store.Load(context.Background(), Key{}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Load invalid key error = %v, want ErrInvalidKey", err)
	}
	if _, err := store.Load(context.Background(), Key{Namespace: "player", ID: "missing"}); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("Load missing error = %v, want ErrSnapshotNotFound", err)
	}
}

func TestMemoryStoreZeroValueIsUsableAndNilReceiverFails(t *testing.T) {
	var store MemoryStore
	want := validSnapshot(1, []byte("state"))
	if _, err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("zero-value Save error = %v", err)
	}
	got, err := store.Load(context.Background(), want.Key)
	if err != nil {
		t.Fatalf("zero-value Load error = %v", err)
	}
	if string(got.State.Payload) != "state" {
		t.Fatalf("zero-value Load payload = %q", got.State.Payload)
	}

	var nilStore *MemoryStore
	if _, err := nilStore.Save(context.Background(), want); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil receiver Save error = %v, want ErrInvalidConfig", err)
	}
	if _, err := nilStore.Load(context.Background(), want.Key); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil receiver Load error = %v, want ErrInvalidConfig", err)
	}
}

func TestMemoryStorePreservesCanceledContextCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	want := errors.New("store canceled")
	cancel(want)
	store := NewMemoryStore()
	if _, err := store.Save(ctx, validSnapshot(1, []byte("state"))); !errors.Is(err, want) {
		t.Fatalf("Save error = %v, want %v", err, want)
	}
	if _, err := store.Load(ctx, testKey()); !errors.Is(err, want) {
		t.Fatalf("Load error = %v, want %v", err, want)
	}
}

func TestMemoryStoreConcurrentSaveLoadKeepsNewestRevision(t *testing.T) {
	store := NewMemoryStore()
	const revisions = 64
	var group sync.WaitGroup
	for revision := uint64(1); revision <= revisions; revision++ {
		revision := revision
		group.Add(1)
		go func() {
			defer group.Done()
			candidate := validSnapshot(revision, []byte{byte(revision)})
			_, err := store.Save(context.Background(), candidate)
			if err != nil && !errors.Is(err, ErrStaleSnapshot) {
				t.Errorf("Save revision %d error = %v", revision, err)
			}
			_, err = store.Load(context.Background(), candidate.Key)
			if err != nil && !errors.Is(err, ErrSnapshotNotFound) {
				t.Errorf("Load revision %d error = %v", revision, err)
			}
		}()
	}
	group.Wait()

	loaded, err := store.Load(context.Background(), Key{Namespace: "player", ID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.Revision != revisions {
		t.Fatalf("revision = %d, want %d", loaded.State.Revision, revisions)
	}
}

func validSnapshot(revision uint64, payload []byte) Snapshot {
	return Snapshot{
		Key:        Key{Namespace: "player", ID: "42"},
		Source:     gsr.ServiceRef{Node: "node-a", ID: 7},
		CapturedAt: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
		State: State{
			Schema:   "player",
			Version:  1,
			Revision: revision,
			Payload:  payload,
		},
	}
}

func cloneSnapshotForTest(snapshot Snapshot) Snapshot {
	snapshot.State.Payload = append([]byte(nil), snapshot.State.Payload...)
	return snapshot
}

type testContext struct{}

func (*testContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*testContext) Done() <-chan struct{}       { return nil }
func (*testContext) Err() error                  { return nil }
func (*testContext) Value(any) any               { return nil }
