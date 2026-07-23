package record

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestJSONArchiveRoundTripsIndependentBundle(t *testing.T) {
	archive, err := NewJSONArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bundle := testRecordBundle()
	if err := archive.Save(context.Background(), bundle); err != nil {
		t.Fatal(err)
	}
	bundle.InitialState[0] = '!'
	bundle.Entries[0].Payload[0] = '!'
	loaded, err := archive.Load(context.Background(), "battle:42")
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded.InitialState) != "initial" || string(loaded.Entries[0].Payload) != "3" {
		t.Fatalf("Load() = %#v", loaded)
	}
	loaded.Entries[0].Payload[0] = '!'
	again, err := archive.Load(context.Background(), "battle:42")
	if err != nil || string(again.Entries[0].Payload) != "3" {
		t.Fatalf("second Load() = %#v, %v", again, err)
	}
	if _, err := archive.Load(context.Background(), "battle:missing"); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Load(missing) error = %v, want ErrRecordNotFound", err)
	}
}

func TestJSONArchiveRejectsInvalidBundleAndCorruptPayload(t *testing.T) {
	archive, err := NewJSONArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invalid := testRecordBundle()
	invalid.FormatVersion = FormatVersion + 1
	if err := archive.Save(context.Background(), invalid); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("Save(invalid) error = %v, want ErrInvalidBundle", err)
	}
	if err := os.WriteFile(archive.pathFor("battle:42"), []byte(`{"FormatVersion":1,"TargetKey":"battle:42","Entries":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Load(context.Background(), "battle:42"); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("Load(corrupt) error = %v, want ErrInvalidBundle", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := archive.Save(cancelled, testRecordBundle()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save(cancelled) error = %v, want context.Canceled", err)
	}
}

func testRecordBundle() RecordBundle {
	return RecordBundle{FormatVersion: FormatVersion, TargetKey: "battle:42", InitialState: []byte("initial"), Entries: []RecordEntry{
		{FormatVersion: FormatVersion, TargetKey: "battle:42", Target: gsr.ServiceRef{Node: "old", ID: 10}, Sequence: 1, RecordedAt: time.Unix(1, 0), Command: commandRecordTestIncrement, Payload: []byte("3")},
		{FormatVersion: FormatVersion, TargetKey: "battle:42", Target: gsr.ServiceRef{Node: "old", ID: 10}, Sequence: 2, RecordedAt: time.Unix(2, 0), Command: commandRecordTestIncrement, Payload: []byte("5")},
	}}
}
