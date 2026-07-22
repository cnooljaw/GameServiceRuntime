package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestNewRejectsNilInspector(t *testing.T) {
	monitor, err := New(nil)
	if !errors.Is(err, ErrInvalidInspector) {
		t.Fatalf("New error = %v, want ErrInvalidInspector", err)
	}
	if monitor != nil {
		t.Fatal("New returned a Monitor for nil Inspector")
	}
}

func TestNewRejectsTypedNilInspector(t *testing.T) {
	var inspector *stubInspector

	monitor, err := New(inspector)
	if !errors.Is(err, ErrInvalidInspector) {
		t.Fatalf("New error = %v, want ErrInvalidInspector", err)
	}
	if monitor != nil {
		t.Fatal("New returned a Monitor for typed-nil Inspector")
	}
}

func TestCaptureConvertsRuntimeInspection(t *testing.T) {
	capturedAt := time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC)
	startedAt := capturedAt.Add(-time.Minute)
	inspector := &stubInspector{inspection: gsr.RuntimeInspection{
		CapturedAt: capturedAt,
		Node:       "node-a",
		Status:     gsr.RuntimeRunning,
		Services: []gsr.ServiceInspection{{
			Ref:          gsr.ServiceRef{Node: "node-a", ID: 7},
			Name:         "lobby",
			Status:       gsr.ServiceRunning,
			MailboxDepth: 3,
		}},
		Tasks: []gsr.RuntimeTaskInspection{{
			ID:        11,
			Owner:     gsr.ServiceRef{Node: "node-a", ID: 7},
			Kind:      gsr.RuntimeTaskDispatch,
			StartedAt: startedAt,
			TimedOut:  true,
		}},
		PendingCalls: 2,
		Timers:       5,
	}}
	monitor, err := New(inspector)
	if err != nil {
		t.Fatal(err)
	}

	report := monitor.Capture()

	if inspector.calls != 1 {
		t.Fatalf("Inspect calls = %d, want 1", inspector.calls)
	}
	if report.CapturedAt != capturedAt || report.Node != "node-a" || report.Status != "running" {
		t.Fatalf("runtime report = %#v", report)
	}
	if report.ServiceCount != 1 || len(report.Services) != 1 {
		t.Fatalf("service count = %d/%d, want 1/1", report.ServiceCount, len(report.Services))
	}
	service := report.Services[0]
	if service.Ref != (Ref{Node: "node-a", ID: 7}) || service.Name != "lobby" || service.Status != "running" || service.MailboxDepth != 3 {
		t.Fatalf("service report = %#v", service)
	}
	if report.TaskCount != 1 || len(report.Tasks) != 1 {
		t.Fatalf("task count = %d/%d, want 1/1", report.TaskCount, len(report.Tasks))
	}
	task := report.Tasks[0]
	if task.ID != 11 || task.Owner != (Ref{Node: "node-a", ID: 7}) || task.Kind != "dispatch" || task.StartedAt != startedAt || !task.TimedOut {
		t.Fatalf("task report = %#v", task)
	}
	if report.PendingCalls != 2 || report.Timers != 5 {
		t.Fatalf("pending calls/timers = %d/%d, want 2/5", report.PendingCalls, report.Timers)
	}
	if report.Metrics.Counters == nil || report.Metrics.Gauges == nil || report.Metrics.DurationsNanos == nil {
		t.Fatal("Capture returned nil metric maps")
	}
	report.Services[0].Name = "changed"
	report.Tasks[0].Kind = "changed"
	second := monitor.Capture()
	if second.Services[0].Name != "lobby" || second.Tasks[0].Kind != "dispatch" {
		t.Fatalf("second report reused mutable slices: %#v", second)
	}
	if inspector.calls != 2 {
		t.Fatalf("Inspect calls after second Capture = %d, want 2", inspector.calls)
	}
}

func TestCaptureUsesStableStatusStrings(t *testing.T) {
	runtimeCases := []struct {
		status gsr.RuntimeStatus
		want   string
	}{
		{gsr.RuntimeRunning, "running"},
		{gsr.RuntimeClosing, "closing"},
		{gsr.RuntimeClosed, "closed"},
		{gsr.RuntimeStatus(99), "unknown"},
	}
	for _, test := range runtimeCases {
		monitor, err := New(&stubInspector{inspection: gsr.RuntimeInspection{Status: test.status}})
		if err != nil {
			t.Fatal(err)
		}
		if got := monitor.Capture().Status; got != test.want {
			t.Fatalf("RuntimeStatus(%d) = %q, want %q", test.status, got, test.want)
		}
	}

	serviceCases := []struct {
		status gsr.ServiceStatus
		want   string
	}{
		{gsr.ServiceCreated, "created"},
		{gsr.ServiceStarting, "starting"},
		{gsr.ServiceRunning, "running"},
		{gsr.ServiceStopping, "stopping"},
		{gsr.ServiceClosed, "closed"},
		{gsr.ServiceFailed, "failed"},
		{gsr.ServiceRestarting, "restarting"},
		{gsr.ServiceStatus(99), "unknown"},
	}
	for _, test := range serviceCases {
		monitor, err := New(&stubInspector{inspection: gsr.RuntimeInspection{Services: []gsr.ServiceInspection{{Status: test.status}}}})
		if err != nil {
			t.Fatal(err)
		}
		if got := monitor.Capture().Services[0].Status; got != test.want {
			t.Fatalf("ServiceStatus(%d) = %q, want %q", test.status, got, test.want)
		}
	}

	taskCases := []struct {
		kind gsr.RuntimeTaskKind
		want string
	}{
		{gsr.RuntimeTaskInit, "init"},
		{gsr.RuntimeTaskDispatch, "dispatch"},
		{gsr.RuntimeTaskStop, "stop"},
		{gsr.RuntimeTaskClose, "close"},
		{gsr.RuntimeTaskKind("future"), "unknown"},
	}
	for _, test := range taskCases {
		monitor, err := New(&stubInspector{inspection: gsr.RuntimeInspection{Tasks: []gsr.RuntimeTaskInspection{{Kind: test.kind}}}})
		if err != nil {
			t.Fatal(err)
		}
		if got := monitor.Capture().Tasks[0].Kind; got != test.want {
			t.Fatalf("RuntimeTaskKind(%q) = %q, want %q", test.kind, got, test.want)
		}
	}
}

func TestCaptureReturnsIndependentReports(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.CreateService(gsr.ServiceSpec{Name: "metrics", Service: monitorMetricsService{}}); err != nil {
		t.Fatal(err)
	}
	monitor, err := New(runtime)
	if err != nil {
		t.Fatal(err)
	}

	first := monitor.Capture()
	if len(first.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(first.Services))
	}
	if first.Metrics.Counters["business_counter"] != 7 || first.Metrics.Gauges["business_gauge"] != -3 || first.Metrics.DurationsNanos["business_duration"] != int64(5*time.Second) {
		t.Fatalf("metrics = %#v", first.Metrics)
	}
	first.Services[0].Name = "changed"
	first.Metrics.Counters["business_counter"] = 99
	first.Metrics.Gauges["business_gauge"] = 99
	first.Metrics.DurationsNanos["business_duration"] = 99

	second := monitor.Capture()
	if second.Services[0].Name != "metrics" {
		t.Fatalf("second service name = %q, want metrics", second.Services[0].Name)
	}
	if second.Metrics.Counters["business_counter"] != 7 || second.Metrics.Gauges["business_gauge"] != -3 || second.Metrics.DurationsNanos["business_duration"] != int64(5*time.Second) {
		t.Fatalf("second metrics changed: %#v", second.Metrics)
	}
}

type stubInspector struct {
	inspection gsr.RuntimeInspection
	calls      int
}

func (s *stubInspector) Inspect() gsr.RuntimeInspection {
	s.calls++
	return s.inspection
}

type monitorMetricsService struct{}

func (monitorMetricsService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (monitorMetricsService) Init(context gsr.ServiceContext) error {
	context.Metrics().Add("business_counter", 7)
	context.Metrics().SetGauge("business_gauge", -3)
	context.Metrics().Observe("business_duration", 5*time.Second)
	return nil
}
func (monitorMetricsService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (monitorMetricsService) Stop(context.Context) error                   { return nil }
func (monitorMetricsService) Close() error                                 { return nil }
