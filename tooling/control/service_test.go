package control

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
)

func TestClusterObserverListsGetsAndRefreshesIndependentNodeDetails(t *testing.T) {
	fixture := newObserverFixture(t)

	nodes, err := fixture.client.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].Config.ID != "node-a" || nodes[0].Observed.Status != NodeUnknown || nodes[0].HasReport {
		t.Fatalf("initial nodes = %#v", nodes)
	}
	if fixture.reporter.calls != 0 {
		t.Fatalf("Capture calls after ListNodes = %d, want 0", fixture.reporter.calls)
	}

	refreshed, err := fixture.client.RefreshNode(context.Background(), "node-a")
	if err != nil {
		t.Fatalf("RefreshNode() error = %v", err)
	}
	if refreshed.Observed.Status != NodeHealthy || !refreshed.HasReport || fixture.reporter.calls != 1 {
		t.Fatalf("refreshed = %#v, Capture calls = %d", refreshed, fixture.reporter.calls)
	}
	refreshed.Report.Metrics.Counters["requests"] = 99
	refreshed.Report.Services[0].Name = "mutated"

	detail, err := fixture.client.GetNodeDetail(context.Background(), "node-a")
	if err != nil {
		t.Fatalf("GetNodeDetail() error = %v", err)
	}
	if detail.Report.Metrics.Counters["requests"] != 1 || detail.Report.Services[0].Name != "service" {
		t.Fatalf("cached detail mutated through Refresh result: %#v", detail)
	}
	if fixture.reporter.calls != 1 {
		t.Fatalf("Capture calls after GetNodeDetail = %d, want 1", fixture.reporter.calls)
	}
}

func TestClusterObserverKeepsDisabledAndFailedRefreshAsNodeFacts(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service, err := NewClusterObserverService(ObserverConfig{Nodes: []NodeTarget{
		{Config: NodeConfig{ID: "node-disabled", Address: "node-disabled:9000", Enabled: false}},
		{Config: NodeConfig{ID: "node-missing", Address: "node-missing:9000", Enabled: true}, Agent: gsr.ServiceRef{Node: "node-missing", ID: 999}},
	}})
	if err != nil {
		t.Fatalf("NewClusterObserverService() error = %v", err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	client, err := NewClient(runtime, ref)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.RefreshNode(context.Background(), "node-disabled"); !errors.Is(err, ErrNodeDisabled) {
		t.Fatalf("RefreshNode(disabled) error = %v, want ErrNodeDisabled", err)
	}
	failed, err := client.RefreshNode(context.Background(), "node-missing")
	if err != nil {
		t.Fatalf("RefreshNode(missing) error = %v", err)
	}
	if failed.Observed.Status != NodeUnavailable || failed.HasReport || failed.Observed.LastError != "remote_unavailable" {
		t.Fatalf("failed refresh = %#v", failed)
	}
	if _, err := client.GetNodeDetail(context.Background(), "unknown"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("GetNodeDetail(unknown) error = %v, want ErrNodeNotFound", err)
	}
}

func TestClusterObserverMarksInvalidAndTimedOutAgentRepliesUnavailable(t *testing.T) {
	t.Run("invalid response", func(t *testing.T) {
		runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
		t.Cleanup(func() { _ = runtime.Close(context.Background()) })
		agentRef, err := runtime.CreateService(gsr.ServiceSpec{Service: invalidReportService{}})
		if err != nil {
			t.Fatalf("CreateService(agent) error = %v", err)
		}
		client := newObserverClientForTarget(t, runtime, agentRef, time.Second)
		detail, err := client.RefreshNode(context.Background(), "node-a")
		if err != nil {
			t.Fatalf("RefreshNode() error = %v", err)
		}
		if detail.Observed.Status != NodeUnavailable || detail.Observed.LastError != "invalid_response" {
			t.Fatalf("detail = %#v", detail)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		// The target deliberately blocks a handler. Keep a second execution permit
		// so this test isolates Call timeout behavior instead of Scheduler starvation.
		runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 2})
		t.Cleanup(func() { _ = runtime.Close(context.Background()) })
		agent := &blockingReportService{started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
		agentRef, err := runtime.CreateService(gsr.ServiceSpec{Service: agent})
		if err != nil {
			t.Fatalf("CreateService(agent) error = %v", err)
		}
		client := newObserverClientForTarget(t, runtime, agentRef, 20*time.Millisecond)
		detail, err := client.RefreshNode(context.Background(), "node-a")
		if err != nil {
			t.Fatalf("RefreshNode() error = %v", err)
		}
		if detail.Observed.Status != NodeUnavailable || detail.Observed.LastError != "timeout" {
			t.Fatalf("detail = %#v", detail)
		}
		close(agent.release)
		select {
		case <-agent.finished:
		case <-time.After(time.Second):
			t.Fatal("blocking report handler did not return")
		}
	})
}

type observerFixture struct {
	client   *Client
	reporter *countingReporter
}

func newObserverFixture(t *testing.T) observerFixture {
	t.Helper()
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	discoveryService, err := discovery.NewService(discovery.Config{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	discoveryRef, err := runtime.CreateService(gsr.ServiceSpec{Service: discoveryService})
	if err != nil {
		t.Fatalf("CreateService(discovery) error = %v", err)
	}
	reporter := &countingReporter{report: testReport("node-a")}
	agentConfig := testNodeAgentConfig(reporter)
	agentConfig.Discovery = discoveryRef
	agentService, err := NewNodeAgentService(agentConfig)
	if err != nil {
		t.Fatalf("NewNodeAgentService() error = %v", err)
	}
	agentRef, err := runtime.CreateService(gsr.ServiceSpec{Service: agentService})
	if err != nil {
		t.Fatalf("CreateService(agent) error = %v", err)
	}
	observerService, err := NewClusterObserverService(ObserverConfig{Nodes: []NodeTarget{{
		Config: NodeConfig{ID: "node-a", Address: "node-a:9000", Enabled: true},
		Agent:  agentRef,
	}}, CallTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewClusterObserverService() error = %v", err)
	}
	observerRef, err := runtime.CreateService(gsr.ServiceSpec{Service: observerService})
	if err != nil {
		t.Fatalf("CreateService(observer) error = %v", err)
	}
	client, err := NewClient(runtime, observerRef)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return observerFixture{client: client, reporter: reporter}
}

func newObserverClientForTarget(t *testing.T, runtime *gsr.Runtime, target gsr.ServiceRef, timeout time.Duration) *Client {
	t.Helper()
	service, err := NewClusterObserverService(ObserverConfig{Nodes: []NodeTarget{{
		Config: NodeConfig{ID: "node-a", Address: "node-a:9000", Enabled: true},
		Agent:  target,
	}}, CallTimeout: timeout})
	if err != nil {
		t.Fatalf("NewClusterObserverService() error = %v", err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatalf("CreateService(observer) error = %v", err)
	}
	client, err := NewClient(runtime, ref)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

type invalidReportService struct{}

func (invalidReportService) Init(gsr.ServiceContext) error {
	return nil
}
func (invalidReportService) Handle(context gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandGetNodeReport {
		return gsr.ErrUnknownCommand
	}
	return context.Reply(nodeReportResponse{Report: testReport("unexpected-node")})
}
func (invalidReportService) Stop(context.Context) error { return nil }
func (invalidReportService) Close() error               { return nil }

type blockingReportService struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (*blockingReportService) Init(gsr.ServiceContext) error {
	return nil
}
func (s *blockingReportService) Handle(context gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandGetNodeReport {
		return gsr.ErrUnknownCommand
	}
	close(s.started)
	<-s.release
	close(s.finished)
	return context.Reply(nodeReportResponse{Report: testReport("node-a")})
}
func (*blockingReportService) Stop(context.Context) error { return nil }
func (*blockingReportService) Close() error               { return nil }
