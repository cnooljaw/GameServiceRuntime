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

	want := "{\"captured_at\":\"2026-07-22T10:30:00Z\",\"node\":\"node-a\",\"status\":\"running\",\"service_count\":0,\"services\":[],\"runner_count\":0,\"runners\":[],\"task_count\":0,\"tasks\":[],\"pending_calls\":0,\"timers\":0,\"metrics\":{\"counters\":{},\"gauges\":{},\"durations_ns\":{}}}\n"
	if got := output.String(); got != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
	if inspector.calls != 1 {
		t.Fatalf("Inspect calls = %d, want 1", inspector.calls)
	}
}

func TestWriteJSONUsesStableNestedFields(t *testing.T) {
	capturedAt := time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC)
	inspector := &stubInspector{inspection: gsr.RuntimeInspection{
		CapturedAt: capturedAt,
		Node:       "node-a",
		Status:     gsr.RuntimeClosing,
		Services: []gsr.ServiceInspection{{
			Ref:          gsr.ServiceRef{Node: "node-a", ID: 7},
			Name:         "lobby",
			Status:       gsr.ServiceStopping,
			MailboxDepth: 3,
		}},
		Runners: []gsr.RunnerInspection{{Name: "external", Status: gsr.RunnerClosing, Workers: 2, QueueDepth: 3, Active: 1, Submitted: 10, Completed: 6, Failed: 2, Rejected: 4, DeliveryFailed: 1}},
		Tasks: []gsr.RuntimeTaskInspection{{
			ID:        11,
			Owner:     gsr.ServiceRef{Node: "node-a", ID: 7},
			Kind:      gsr.RuntimeTaskStop,
			StartedAt: capturedAt.Add(-time.Minute),
			TimedOut:  true,
		}},
		PendingCalls: 2,
		Timers:       5,
	}}
	monitor, err := New(inspector)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer

	if err := monitor.WriteJSON(&output); err != nil {
		t.Fatal(err)
	}

	want := "{\"captured_at\":\"2026-07-22T10:30:00Z\",\"node\":\"node-a\",\"status\":\"closing\",\"service_count\":1,\"services\":[{\"ref\":{\"node\":\"node-a\",\"id\":7},\"name\":\"lobby\",\"status\":\"stopping\",\"mailbox_depth\":3}],\"runner_count\":1,\"runners\":[{\"name\":\"external\",\"status\":\"closing\",\"workers\":2,\"queue_depth\":3,\"active\":1,\"submitted\":10,\"completed\":6,\"failed\":2,\"rejected\":4,\"delivery_failed\":1}],\"task_count\":1,\"tasks\":[{\"id\":11,\"owner\":{\"node\":\"node-a\",\"id\":7},\"kind\":\"stop\",\"started_at\":\"2026-07-22T10:29:00Z\",\"timed_out\":true}],\"pending_calls\":2,\"timers\":5,\"metrics\":{\"counters\":{},\"gauges\":{},\"durations_ns\":{}}}\n"
	if got := output.String(); got != want {
		t.Fatalf("JSON = %s, want %s", got, want)
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

func TestWriteJSONRejectsTypedNilWriter(t *testing.T) {
	inspector := &stubInspector{}
	monitor, err := New(inspector)
	if err != nil {
		t.Fatal(err)
	}
	var writer *bytes.Buffer

	if err := monitor.WriteJSON(writer); !errors.Is(err, ErrInvalidWriter) {
		t.Fatalf("WriteJSON error = %v, want ErrInvalidWriter", err)
	}
	if inspector.calls != 0 {
		t.Fatalf("Inspect calls = %d, want 0", inspector.calls)
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
