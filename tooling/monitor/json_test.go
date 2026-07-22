package monitor

import (
	"bytes"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestWriteJSONUsesStableFieldsAndNonNilEmptyCollections(t *testing.T) {
	inspector := &stubInspector{inspection: gsr.RuntimeInspection{
		CapturedAt: time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC),
		Node:       "node-a",
		Status:     gsr.RuntimeRunning,
	}}
	monitor, err := New(inspector)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer

	if err := monitor.WriteJSON(&output); err != nil {
		t.Fatal(err)
	}

	want := "{\"captured_at\":\"2026-07-22T10:30:00Z\",\"node\":\"node-a\",\"status\":\"running\",\"service_count\":0,\"services\":[],\"task_count\":0,\"tasks\":[],\"pending_calls\":0,\"timers\":0,\"metrics\":{\"counters\":{},\"gauges\":{},\"durations_ns\":{}}}\n"
	if got := output.String(); got != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
	if inspector.calls != 1 {
		t.Fatalf("Inspect calls = %d, want 1", inspector.calls)
	}
}

func TestWriteJSONRejectsNilWriter(t *testing.T) {
	monitor, err := New(&stubInspector{})
	if err != nil {
		t.Fatal(err)
	}

	if err := monitor.WriteJSON(nil); !errors.Is(err, ErrInvalidWriter) {
		t.Fatalf("WriteJSON error = %v, want ErrInvalidWriter", err)
	}
}

func TestWriteJSONReturnsWriterError(t *testing.T) {
	wantErr := errors.New("write failed")
	monitor, err := New(&stubInspector{})
	if err != nil {
		t.Fatal(err)
	}

	if err := monitor.WriteJSON(errorWriter{err: wantErr}); !errors.Is(err, wantErr) {
		t.Fatalf("WriteJSON error = %v, want %v", err, wantErr)
	}
}

func TestWriteJSONDoesNotCloseWriter(t *testing.T) {
	monitor, err := New(&stubInspector{})
	if err != nil {
		t.Fatal(err)
	}
	writer := &closeTrackingWriter{}

	if err := monitor.WriteJSON(writer); err != nil {
		t.Fatal(err)
	}
	if writer.closed {
		t.Fatal("WriteJSON closed its writer")
	}
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

type closeTrackingWriter struct {
	bytes.Buffer
	closed bool
}

func (w *closeTrackingWriter) Close() error {
	w.closed = true
	return nil
}
