package gsr_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestRuntimeInspectReportsIdentityAndStatus(t *testing.T) {
	now := time.Date(2026, 7, 17, 18, 0, 0, 0, time.UTC)
	runtime := gsr.NewRuntime(gsr.Config{
		NodeID: "node-a",
		Now:    func() time.Time { return now },
	})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	inspection := runtime.Inspect()
	if inspection.Node != "node-a" {
		t.Fatalf("Node = %q, want node-a", inspection.Node)
	}
	if inspection.Status != gsr.RuntimeRunning {
		t.Fatalf("Status = %v, want RuntimeRunning", inspection.Status)
	}
	if !inspection.CapturedAt.Equal(now) {
		t.Fatalf("CapturedAt = %v, want %v", inspection.CapturedAt, now)
	}
}

func TestRuntimeInspectReturnsServicesInStableOrder(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	refs := make([]gsr.ServiceRef, 0, 3)
	for _, name := range []gsr.ServiceName{"third", "first", "second"} {
		ref, err := runtime.CreateService(gsr.ServiceSpec{Name: name, Service: inspectionService{}})
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
	}

	inspection := runtime.Inspect()
	if len(inspection.Services) != len(refs) {
		t.Fatalf("Services length = %d, want %d", len(inspection.Services), len(refs))
	}
	for index, service := range inspection.Services {
		if service.Ref != refs[index] {
			t.Fatalf("Services[%d].Ref = %v, want %v", index, service.Ref, refs[index])
		}
		if service.Status != gsr.ServiceRunning {
			t.Fatalf("Services[%d].Status = %v, want ServiceRunning", index, service.Status)
		}
		if service.MailboxDepth != 0 {
			t.Fatalf("Services[%d].MailboxDepth = %d, want 0", index, service.MailboxDepth)
		}
	}
	if inspection.Services[0].Name != "third" || inspection.Services[1].Name != "first" || inspection.Services[2].Name != "second" {
		t.Fatalf("service names are not paired with stable refs: %#v", inspection.Services)
	}
}

func TestRuntimeInspectReturnsIndependentCopies(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	ref, err := runtime.CreateService(gsr.ServiceSpec{Name: "original", Service: inspectionService{}})
	if err != nil {
		t.Fatal(err)
	}

	first := runtime.Inspect()
	first.Services[0].Ref = gsr.ServiceRef{Node: "changed", ID: 99}
	first.Services[0].Name = "changed"
	first.Services = append(first.Services, gsr.ServiceInspection{})

	second := runtime.Inspect()
	if len(second.Services) != 1 {
		t.Fatalf("Services length = %d, want 1", len(second.Services))
	}
	if second.Services[0].Ref != ref || second.Services[0].Name != "original" {
		t.Fatalf("second inspection was changed through first copy: %#v", second.Services[0])
	}
}

func TestRuntimeInspectReturnsIndependentMetricsSnapshot(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	first := runtime.Inspect()
	if got := first.Metrics.Counter("service_created_total"); got != 0 {
		t.Fatalf("first service_created_total = %d, want 0", got)
	}

	if _, err := runtime.CreateService(gsr.ServiceSpec{Service: inspectionService{}}); err != nil {
		t.Fatal(err)
	}
	second := runtime.Inspect()
	if got := first.Metrics.Counter("service_created_total"); got != 0 {
		t.Fatalf("first service_created_total changed to %d, want 0", got)
	}
	if got := second.Metrics.Counter("service_created_total"); got != 1 {
		t.Fatalf("second service_created_total = %d, want 1", got)
	}
}

func TestMetricsSnapshotEnumerationsReturnIndependentCopies(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.CreateService(gsr.ServiceSpec{Service: inspectionMetricsService{}}); err != nil {
		t.Fatal(err)
	}

	snapshot := runtime.Inspect().Metrics
	counters := snapshot.Counters()
	gauges := snapshot.Gauges()
	durations := snapshot.Durations()
	if counters["inspection_counter"] != 7 {
		t.Fatalf("Counters inspection_counter = %d, want 7", counters["inspection_counter"])
	}
	if gauges["inspection_gauge"] != -3 {
		t.Fatalf("Gauges inspection_gauge = %d, want -3", gauges["inspection_gauge"])
	}
	if durations["inspection_duration"] != 5*time.Second {
		t.Fatalf("Durations inspection_duration = %v, want 5s", durations["inspection_duration"])
	}

	counters["inspection_counter"] = 99
	gauges["inspection_gauge"] = 99
	durations["inspection_duration"] = 99
	if snapshot.Counter("inspection_counter") != 7 || snapshot.Gauge("inspection_gauge") != -3 || snapshot.Duration("inspection_duration") != 5*time.Second {
		t.Fatal("enumeration map mutation changed MetricsSnapshot")
	}
	second := runtime.Inspect().Metrics
	if second.Counters()["inspection_counter"] != 7 || second.Gauges()["inspection_gauge"] != -3 || second.Durations()["inspection_duration"] != 5*time.Second {
		t.Fatal("enumeration map mutation changed Runtime metrics")
	}
}

func TestZeroMetricsSnapshotEnumerationsAreNonNil(t *testing.T) {
	var snapshot gsr.MetricsSnapshot
	if snapshot.Counters() == nil || snapshot.Gauges() == nil || snapshot.Durations() == nil {
		t.Fatal("zero MetricsSnapshot returned a nil enumeration map")
	}
}

func TestRuntimeInspectWorksAfterClose(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	inspection := runtime.Inspect()
	if inspection.Status != gsr.RuntimeClosed {
		t.Fatalf("Status = %v, want RuntimeClosed", inspection.Status)
	}
}

func TestRuntimeInspectReportsClosingStatus(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", ShutdownTimeout: time.Second})
	service := &inspectionBlockingHandleService{started: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-service.release:
		default:
			close(service.release)
		}
		_ = runtime.Close(context.Background())
	})
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-service.started

	closeResult := make(chan error, 1)
	go func() { closeResult <- runtime.Close(context.Background()) }()
	eventually(t, func() bool { return runtime.Inspect().Status == gsr.RuntimeClosing })

	close(service.release)
	if err := <-closeResult; err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeInspectReportsPendingCallsAndTimers(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service := &inspectionBlockingReplyService{started: make(chan struct{}), release: make(chan struct{})}
	released := false
	defer func() {
		if !released {
			close(service.release)
		}
	}()
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}

	callResult := make(chan error, 1)
	go func() {
		_, callErr := runtime.Call(context.Background(), ref, 1, nil)
		callResult <- callErr
	}()
	<-service.started
	timer, err := runtime.After(ref, time.Hour, 1, nil)
	if err != nil {
		t.Fatal(err)
	}

	inspection := runtime.Inspect()
	if inspection.PendingCalls != 1 {
		t.Fatalf("PendingCalls = %d, want 1", inspection.PendingCalls)
	}
	if inspection.Timers != 1 {
		t.Fatalf("Timers = %d, want 1", inspection.Timers)
	}
	if len(inspection.Tasks) != 1 || inspection.Tasks[0].Kind != gsr.RuntimeTaskDispatch {
		t.Fatalf("Tasks = %#v, want one dispatch task", inspection.Tasks)
	}

	if err := runtime.Cancel(timer); err != nil {
		t.Fatal(err)
	}
	close(service.release)
	released = true
	if err := <-callResult; err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		inspection := runtime.Inspect()
		return inspection.PendingCalls == 0 && inspection.Timers == 0
	})
}

func TestRuntimeInspectReportsActiveTask(t *testing.T) {
	now := time.Date(2026, 7, 17, 18, 30, 0, 0, time.UTC)
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service := &inspectionBlockingInitService{started: make(chan struct{}), release: make(chan struct{})}
	released := false
	defer func() {
		if !released {
			close(service.release)
		}
	}()
	created := make(chan inspectionCreateResult, 1)
	go func() {
		ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
		created <- inspectionCreateResult{ref: ref, err: err}
	}()
	<-service.started

	inspection := runtime.Inspect()
	if len(inspection.Services) != 1 {
		t.Fatalf("Services length = %d, want 1", len(inspection.Services))
	}
	if len(inspection.Tasks) != 1 {
		t.Fatalf("Tasks length = %d, want 1", len(inspection.Tasks))
	}
	task := inspection.Tasks[0]
	if task.ID == 0 {
		t.Fatal("task ID is zero")
	}
	if task.Owner != inspection.Services[0].Ref {
		t.Fatalf("task Owner = %v, want %v", task.Owner, inspection.Services[0].Ref)
	}
	if task.Kind != gsr.RuntimeTaskInit {
		t.Fatalf("task Kind = %q, want %q", task.Kind, gsr.RuntimeTaskInit)
	}
	if !task.StartedAt.Equal(now) {
		t.Fatalf("task StartedAt = %v, want %v", task.StartedAt, now)
	}
	if task.TimedOut {
		t.Fatal("active task is marked timed out")
	}
	inspection.Tasks[0].Owner = gsr.ServiceRef{Node: "changed", ID: 99}
	inspection.Tasks = append(inspection.Tasks, gsr.RuntimeTaskInspection{})
	second := runtime.Inspect()
	if len(second.Tasks) != 1 || second.Tasks[0].Owner != task.Owner {
		t.Fatalf("second task inspection was changed through first copy: %#v", second.Tasks)
	}

	close(service.release)
	released = true
	result := <-created
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.ref != task.Owner {
		t.Fatalf("created ref = %v, want task owner %v", result.ref, task.Owner)
	}
	eventually(t, func() bool { return len(runtime.Inspect().Tasks) == 0 })
}

func TestRuntimeInspectReportsTimedOutTask(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", ShutdownTimeout: 20 * time.Millisecond})
	service := &inspectionBlockingInitService{started: make(chan struct{}), release: make(chan struct{})}
	created := make(chan inspectionCreateResult, 1)
	go func() {
		ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
		created <- inspectionCreateResult{ref: ref, err: err}
	}()
	<-service.started
	released := false
	defer func() {
		if !released {
			close(service.release)
		}
	}()

	if err := runtime.Close(context.Background()); !errors.Is(err, gsr.ErrCloseTimeout) {
		t.Fatalf("Close error = %v, want ErrCloseTimeout", err)
	}
	inspection := runtime.Inspect()
	if inspection.Status != gsr.RuntimeClosed {
		t.Fatalf("Status = %v, want RuntimeClosed", inspection.Status)
	}
	if len(inspection.Tasks) != 1 {
		t.Fatalf("Tasks length = %d, want 1", len(inspection.Tasks))
	}
	if !inspection.Tasks[0].TimedOut {
		t.Fatal("unfinished Init task is not marked timed out")
	}

	close(service.release)
	released = true
	if result := <-created; !errors.Is(result.err, gsr.ErrRuntimeClosed) {
		t.Fatalf("CreateService error = %v, want ErrRuntimeClosed", result.err)
	}
	eventually(t, func() bool { return len(runtime.Inspect().Tasks) == 0 })
}

func TestRuntimeInspectReportsStopAndCloseTasks(t *testing.T) {
	t.Run("stop", func(t *testing.T) {
		runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
		service := &inspectionBlockingStopService{started: make(chan struct{}), release: make(chan struct{})}
		t.Cleanup(func() {
			select {
			case <-service.release:
			default:
				close(service.release)
			}
			_ = runtime.Close(context.Background())
		})
		ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
		if err != nil {
			t.Fatal(err)
		}
		stopResult := make(chan error, 1)
		go func() { stopResult <- runtime.Stop(context.Background(), ref) }()
		<-service.started

		assertInspectionHasTaskKind(t, runtime.Inspect(), gsr.RuntimeTaskStop)
		close(service.release)
		if err := <-stopResult; err != nil {
			t.Fatal(err)
		}
	})

	t.Run("close", func(t *testing.T) {
		runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
		service := &inspectionBlockingCloseService{started: make(chan struct{}), release: make(chan struct{})}
		t.Cleanup(func() {
			select {
			case <-service.release:
			default:
				close(service.release)
			}
			_ = runtime.Close(context.Background())
		})
		ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
		if err != nil {
			t.Fatal(err)
		}
		stopResult := make(chan error, 1)
		go func() { stopResult <- runtime.Stop(context.Background(), ref) }()
		<-service.started

		assertInspectionHasTaskKind(t, runtime.Inspect(), gsr.RuntimeTaskClose)
		close(service.release)
		if err := <-stopResult; err != nil {
			t.Fatal(err)
		}
	})
}

func TestRuntimeInspectIsSafeDuringConcurrentLifecycleChanges(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 4})
	refs := make([]gsr.ServiceRef, 0, 4)
	for range 4 {
		ref, err := runtime.CreateService(gsr.ServiceSpec{Service: inspectionService{}})
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
	}

	start := make(chan struct{})
	var wait sync.WaitGroup
	for _, ref := range refs {
		ref := ref
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for range 100 {
				_ = runtime.Send(ref, 1, nil)
			}
		}()
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for range 50 {
				timer, err := runtime.After(ref, time.Hour, 1, nil)
				if err == nil {
					_ = runtime.Cancel(timer)
				}
			}
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		for range 200 {
			_ = runtime.Inspect()
		}
	}()
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		for _, ref := range refs {
			_ = runtime.Stop(context.Background(), ref)
		}
	}()
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		_ = runtime.Close(context.Background())
	}()
	close(start)
	wait.Wait()

	inspection := runtime.Inspect()
	if inspection.Status != gsr.RuntimeClosed {
		t.Fatalf("Status = %v, want RuntimeClosed", inspection.Status)
	}
}

type inspectionService struct{}

func (inspectionService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (inspectionService) Init(gsr.ServiceContext) error { return nil }
func (inspectionService) Handle(gsr.CommandContext, gsr.Command) error {
	return nil
}
func (inspectionService) Stop(context.Context) error { return nil }
func (inspectionService) Close() error               { return nil }

type inspectionMetricsService struct{}

func (inspectionMetricsService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (inspectionMetricsService) Init(context gsr.ServiceContext) error {
	context.Metrics().Add("inspection_counter", 7)
	context.Metrics().SetGauge("inspection_gauge", -3)
	context.Metrics().Observe("inspection_duration", 5*time.Second)
	return nil
}
func (inspectionMetricsService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (inspectionMetricsService) Stop(context.Context) error                   { return nil }
func (inspectionMetricsService) Close() error                                 { return nil }

type inspectionBlockingReplyService struct {
	started chan struct{}
	release chan struct{}
}

func (*inspectionBlockingReplyService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (*inspectionBlockingReplyService) Init(gsr.ServiceContext) error {
	return nil
}
func (s *inspectionBlockingReplyService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	close(s.started)
	<-s.release
	return ctx.Reply("ok")
}
func (*inspectionBlockingReplyService) Stop(context.Context) error { return nil }
func (*inspectionBlockingReplyService) Close() error               { return nil }

type inspectionBlockingHandleService struct {
	started chan struct{}
	release chan struct{}
}

func (*inspectionBlockingHandleService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (*inspectionBlockingHandleService) Init(gsr.ServiceContext) error {
	return nil
}
func (s *inspectionBlockingHandleService) Handle(gsr.CommandContext, gsr.Command) error {
	close(s.started)
	<-s.release
	return nil
}
func (*inspectionBlockingHandleService) Stop(context.Context) error { return nil }
func (*inspectionBlockingHandleService) Close() error               { return nil }

type inspectionBlockingInitService struct {
	started chan struct{}
	release chan struct{}
}

func (*inspectionBlockingInitService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (s *inspectionBlockingInitService) Init(gsr.ServiceContext) error {
	close(s.started)
	<-s.release
	return nil
}
func (*inspectionBlockingInitService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (*inspectionBlockingInitService) Stop(context.Context) error                   { return nil }
func (*inspectionBlockingInitService) Close() error                                 { return nil }

type inspectionCreateResult struct {
	ref gsr.ServiceRef
	err error
}

type inspectionBlockingStopService struct {
	started chan struct{}
	release chan struct{}
}

func (*inspectionBlockingStopService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (*inspectionBlockingStopService) Init(gsr.ServiceContext) error { return nil }
func (*inspectionBlockingStopService) Handle(gsr.CommandContext, gsr.Command) error {
	return nil
}
func (s *inspectionBlockingStopService) Stop(context.Context) error {
	close(s.started)
	<-s.release
	return nil
}
func (*inspectionBlockingStopService) Close() error { return nil }

type inspectionBlockingCloseService struct {
	started chan struct{}
	release chan struct{}
}

func (*inspectionBlockingCloseService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (*inspectionBlockingCloseService) Init(gsr.ServiceContext) error { return nil }
func (*inspectionBlockingCloseService) Handle(gsr.CommandContext, gsr.Command) error {
	return nil
}
func (*inspectionBlockingCloseService) Stop(context.Context) error { return nil }
func (s *inspectionBlockingCloseService) Close() error {
	close(s.started)
	<-s.release
	return nil
}

func assertInspectionHasTaskKind(t *testing.T, inspection gsr.RuntimeInspection, kind gsr.RuntimeTaskKind) {
	t.Helper()
	for index, task := range inspection.Tasks {
		if index > 0 && inspection.Tasks[index-1].ID >= task.ID {
			t.Fatalf("Tasks are not sorted by ID: %#v", inspection.Tasks)
		}
		if task.Kind == kind {
			return
		}
	}
	t.Fatalf("Tasks = %#v, want kind %q", inspection.Tasks, kind)
}
