package control

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
	"github.com/lijiawang/GameServiceRuntime/tooling/monitor"
)

func TestNodeAgentRegistersThenHeartbeatsWithRuntimeCommands(t *testing.T) {
	renewed := discovery.NodeLease{Node: "node-b", AuthorityEpoch: 1, Generation: 1, ExpiresAt: time.Now().Add(2 * time.Minute)}
	client := &fakeNodeLeaseClient{
		registrations: []discovery.NodeLease{{Node: "node-b", AuthorityEpoch: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Minute)}},
		heartbeats:    []discovery.NodeLease{renewed},
	}
	agent, serviceContext := newLeaseNodeAgent(t, client)
	startup, ok := agent.StartupCommand()
	if !ok || startup.ID != commandRegisterNodeLease {
		t.Fatalf("StartupCommand() = %#v, %t", startup, ok)
	}
	if err := agent.Handle(&recordingContext{source: serviceContext.self}, startup); err != nil {
		t.Fatalf("Handle(register) error = %v", err)
	}
	if client.registerCalls != 1 || client.registeredNode != "node-b" || client.registeredAddress != "node-b:9000" {
		t.Fatalf("Register calls = %d, node = %q, address = %q", client.registerCalls, client.registeredNode, client.registeredAddress)
	}
	if agent.lease.Generation != 1 || !agent.hasLease {
		t.Fatalf("agent lease = %#v, hasLease = %t", agent.lease, agent.hasLease)
	}
	assertScheduledCommand(t, serviceContext, commandHeartbeatNodeLease)

	serviceContext.resetTimers()
	if err := agent.Handle(&recordingContext{source: gsr.ServiceRef{Node: "node-b"}}, gsr.Command{ID: commandHeartbeatNodeLease}); err != nil {
		t.Fatalf("Handle(heartbeat) error = %v", err)
	}
	if client.heartbeatCalls != 1 || agent.lease.ExpiresAt != renewed.ExpiresAt {
		t.Fatalf("Heartbeat calls = %d, lease = %#v", client.heartbeatCalls, agent.lease)
	}
	assertScheduledCommand(t, serviceContext, commandHeartbeatNodeLease)
}

func TestNodeAgentReregistersExpiredLeaseAndRetriesTransientFailure(t *testing.T) {
	client := &fakeNodeLeaseClient{
		registrations: []discovery.NodeLease{
			{Node: "node-b", AuthorityEpoch: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Minute)},
			{Node: "node-b", AuthorityEpoch: 2, Generation: 1, ExpiresAt: time.Now().Add(time.Minute)},
		},
		heartbeatErr: discovery.ErrLeaseExpired,
	}
	agent, serviceContext := newLeaseNodeAgent(t, client)
	if err := agent.Handle(&recordingContext{source: serviceContext.self}, gsr.Command{ID: commandRegisterNodeLease}); err != nil {
		t.Fatalf("Handle(register) error = %v", err)
	}
	client.heartbeatErr = errors.New("temporary")
	serviceContext.resetTimers()
	if err := agent.Handle(&recordingContext{source: gsr.ServiceRef{Node: "node-b"}}, gsr.Command{ID: commandHeartbeatNodeLease}); err != nil {
		t.Fatalf("Handle(transient heartbeat) error = %v", err)
	}
	if client.registerCalls != 1 {
		t.Fatalf("Register calls after transient heartbeat = %d, want 1", client.registerCalls)
	}
	assertScheduledCommand(t, serviceContext, commandHeartbeatNodeLease)

	client.heartbeatErr = discovery.ErrLeaseExpired
	serviceContext.resetTimers()
	if err := agent.Handle(&recordingContext{source: gsr.ServiceRef{Node: "node-b"}}, gsr.Command{ID: commandHeartbeatNodeLease}); err != nil {
		t.Fatalf("Handle(expired heartbeat) error = %v", err)
	}
	if client.registerCalls != 2 || agent.lease.AuthorityEpoch != 2 {
		t.Fatalf("Register calls = %d, lease = %#v", client.registerCalls, agent.lease)
	}
	assertScheduledCommand(t, serviceContext, commandHeartbeatNodeLease)

	client.registerErr = errors.New("temporary")
	agent.hasLease = false
	serviceContext.resetTimers()
	if err := agent.Handle(&recordingContext{source: serviceContext.self}, gsr.Command{ID: commandRegisterNodeLease}); err != nil {
		t.Fatalf("Handle(retry register) error = %v", err)
	}
	assertScheduledCommand(t, serviceContext, commandRegisterNodeLease)

	client.registerErr = nil
	client.registrations = append(client.registrations, discovery.NodeLease{Node: "node-b", AuthorityEpoch: 3, Generation: 1, ExpiresAt: time.Now().Add(time.Minute)})
	serviceContext.resetTimers()
	if err := agent.Handle(&recordingContext{source: gsr.ServiceRef{Node: "node-b"}}, gsr.Command{ID: commandRegisterNodeLease}); err != nil {
		t.Fatalf("Handle(timer retry register) error = %v", err)
	}
	if client.registerCalls != 4 || agent.lease.AuthorityEpoch != 3 {
		t.Fatalf("Register calls = %d, lease = %#v", client.registerCalls, agent.lease)
	}
	assertScheduledCommand(t, serviceContext, commandHeartbeatNodeLease)
}

func TestNodeAgentStopUnregistersCurrentLeaseOnce(t *testing.T) {
	client := &fakeNodeLeaseClient{registrations: []discovery.NodeLease{{Node: "node-b", AuthorityEpoch: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Minute)}}}
	agent, serviceContext := newLeaseNodeAgent(t, client)
	if err := agent.Handle(&recordingContext{source: serviceContext.self}, gsr.Command{ID: commandRegisterNodeLease}); err != nil {
		t.Fatalf("Handle(register) error = %v", err)
	}
	if err := agent.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := agent.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
	if client.unregisterCalls != 1 || agent.hasLease {
		t.Fatalf("Unregister calls = %d, hasLease = %t", client.unregisterCalls, agent.hasLease)
	}
}

func TestNodeAgentAutomaticallyRegistersAndRenewsDiscoveryLease(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-b"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	discoveryService, err := discovery.NewService(discovery.Config{LeaseTTL: time.Second, SweepInterval: time.Second})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	discoveryRef, err := runtime.CreateService(gsr.ServiceSpec{Service: discoveryService})
	if err != nil {
		t.Fatalf("CreateService(discovery) error = %v", err)
	}
	config := testNodeAgentConfig(&countingReporter{report: testReport("node-b")})
	config.ObserverNode = "node-b"
	config.Discovery = discoveryRef
	config.HeartbeatInterval = 10 * time.Millisecond
	agentService, err := NewNodeAgentService(config)
	if err != nil {
		t.Fatalf("NewNodeAgentService() error = %v", err)
	}
	agentRef, err := runtime.CreateService(gsr.ServiceSpec{Service: agentService})
	if err != nil {
		t.Fatalf("CreateService(agent) error = %v", err)
	}
	client, err := discovery.NewClient(runtime, discoveryRef)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	first := waitForDiscoveryNode(t, client, "node-b")
	time.Sleep(30 * time.Millisecond)
	second := waitForDiscoveryNode(t, client, "node-b")
	if !second.LastSeen.After(first.LastSeen) {
		t.Fatalf("second LastSeen = %v, want after %v", second.LastSeen, first.LastSeen)
	}
	if err := runtime.Stop(context.Background(), agentRef); err != nil {
		t.Fatalf("Stop(agent) error = %v", err)
	}
	if _, err := client.GetNode(context.Background(), "node-b"); !errors.Is(err, discovery.ErrNodeNotFound) {
		t.Fatalf("GetNode() after Stop error = %v, want ErrNodeNotFound", err)
	}
}

func TestNodeAgentRejectsUnauthorizedSourceBeforeCapture(t *testing.T) {
	reporter := &countingReporter{report: testReport("node-b")}
	service, err := NewNodeAgentService(testNodeAgentConfig(reporter))
	if err != nil {
		t.Fatalf("NewNodeAgentService() error = %v", err)
	}
	agent := service.(*nodeAgent)
	commandContext := &recordingContext{source: gsr.ServiceRef{Node: "node-a"}}
	if err := agent.Handle(commandContext, gsr.Command{ID: commandGetNodeReport, Payload: getNodeReportRequest{}}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	response, ok := commandContext.reply.(nodeReportResponse)
	if !ok || response.Error != responseUnauthorized {
		t.Fatalf("reply = %#v, want unauthorized nodeReportResponse", commandContext.reply)
	}
	if reporter.calls != 0 {
		t.Fatalf("Capture calls = %d, want 0", reporter.calls)
	}
}

func TestNodeAgentReturnsIndependentReport(t *testing.T) {
	reporter := &countingReporter{report: testReport("node-b")}
	service, err := NewNodeAgentService(testNodeAgentConfig(reporter))
	if err != nil {
		t.Fatalf("NewNodeAgentService() error = %v", err)
	}
	agent := service.(*nodeAgent)
	firstContext := &recordingContext{source: gsr.ServiceRef{Node: "node-a", ID: 1}}
	if err := agent.Handle(firstContext, gsr.Command{ID: commandGetNodeReport, Payload: getNodeReportRequest{}}); err != nil {
		t.Fatalf("Handle(first) error = %v", err)
	}
	first := firstContext.reply.(nodeReportResponse)
	first.Report.Metrics.Counters["requests"] = 99
	first.Report.Services[0].Name = "mutated"
	secondContext := &recordingContext{source: gsr.ServiceRef{Node: "node-a", ID: 1}}
	if err := agent.Handle(secondContext, gsr.Command{ID: commandGetNodeReport, Payload: getNodeReportRequest{}}); err != nil {
		t.Fatalf("Handle(second) error = %v", err)
	}
	second := secondContext.reply.(nodeReportResponse)
	if second.Report.Metrics.Counters["requests"] != 1 || second.Report.Services[0].Name != "service" {
		t.Fatalf("second report = %#v, want independent copy", second.Report)
	}
	if reporter.calls != 2 {
		t.Fatalf("Capture calls = %d, want 2", reporter.calls)
	}
}

func TestNodeAgentRejectsInvalidPayload(t *testing.T) {
	reporter := &countingReporter{report: testReport("node-b")}
	service, err := NewNodeAgentService(testNodeAgentConfig(reporter))
	if err != nil {
		t.Fatalf("NewNodeAgentService() error = %v", err)
	}
	agent := service.(*nodeAgent)
	commandContext := &recordingContext{source: gsr.ServiceRef{Node: "node-a", ID: 1}}
	if err := agent.Handle(commandContext, gsr.Command{ID: commandGetNodeReport, Payload: "wrong"}); err != nil {
		t.Fatalf("Handle(invalid payload) error = %v", err)
	}
	response, ok := commandContext.reply.(nodeReportResponse)
	if !ok || response.Error != responseInvalidRequest {
		t.Fatalf("reply = %#v, want invalid nodeReportResponse", commandContext.reply)
	}
	if reporter.calls != 0 {
		t.Fatalf("Capture calls = %d, want 0", reporter.calls)
	}
}

type countingReporter struct {
	report monitor.Report
	calls  int
}

func (r *countingReporter) Capture() monitor.Report {
	r.calls++
	return r.report
}

type recordingContext struct {
	source gsr.ServiceRef
	reply  any
}

func (*recordingContext) Self() gsr.ServiceRef     { return gsr.ServiceRef{Node: "node-b", ID: 1} }
func (c *recordingContext) Source() gsr.ServiceRef { return c.source }
func (c *recordingContext) Reply(value any) error  { c.reply = value; return nil }

type nodeAgentServiceContext struct {
	self   gsr.ServiceRef
	timers []scheduledNodeAgentCommand
}

type scheduledNodeAgentCommand struct {
	delay   time.Duration
	command gsr.CommandID
	payload any
}

func (c *nodeAgentServiceContext) Self() gsr.ServiceRef { return c.self }
func (*nodeAgentServiceContext) Send(gsr.ServiceRef, gsr.CommandID, any) error {
	return nil
}
func (*nodeAgentServiceContext) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}
func (c *nodeAgentServiceContext) After(delay time.Duration, command gsr.CommandID, payload any) (gsr.TimerID, error) {
	c.timers = append(c.timers, scheduledNodeAgentCommand{delay: delay, command: command, payload: payload})
	return gsr.TimerID(len(c.timers)), nil
}
func (*nodeAgentServiceContext) Now() time.Time       { return time.Now() }
func (*nodeAgentServiceContext) Logger() *slog.Logger { return slog.Default() }
func (*nodeAgentServiceContext) Metrics() gsr.Metrics { return nodeAgentNoopMetrics{} }

func (c *nodeAgentServiceContext) resetTimers() { c.timers = nil }

type nodeAgentNoopMetrics struct{}

func (nodeAgentNoopMetrics) Inc(string)                    {}
func (nodeAgentNoopMetrics) Add(string, uint64)            {}
func (nodeAgentNoopMetrics) SetGauge(string, int64)        {}
func (nodeAgentNoopMetrics) Observe(string, time.Duration) {}

type fakeNodeLeaseClient struct {
	registrations     []discovery.NodeLease
	heartbeats        []discovery.NodeLease
	registerErr       error
	heartbeatErr      error
	unregisterErr     error
	registerCalls     int
	heartbeatCalls    int
	unregisterCalls   int
	registeredNode    gsr.NodeID
	registeredAddress string
}

func (c *fakeNodeLeaseClient) RegisterNode(_ context.Context, node gsr.NodeID, address string) (discovery.NodeLease, error) {
	c.registerCalls++
	c.registeredNode = node
	c.registeredAddress = address
	if c.registerErr != nil {
		return discovery.NodeLease{}, c.registerErr
	}
	lease := c.registrations[0]
	c.registrations = c.registrations[1:]
	return lease, nil
}
func (c *fakeNodeLeaseClient) Heartbeat(context.Context, discovery.NodeLease) (discovery.NodeLease, error) {
	c.heartbeatCalls++
	if c.heartbeatErr != nil {
		return discovery.NodeLease{}, c.heartbeatErr
	}
	lease := c.heartbeats[0]
	c.heartbeats = c.heartbeats[1:]
	return lease, nil
}
func (c *fakeNodeLeaseClient) UnregisterNode(context.Context, discovery.NodeLease) error {
	c.unregisterCalls++
	return c.unregisterErr
}

func newLeaseNodeAgent(t *testing.T, client *fakeNodeLeaseClient) (*nodeAgent, *nodeAgentServiceContext) {
	t.Helper()
	service, err := NewNodeAgentService(testNodeAgentConfig(&countingReporter{report: testReport("node-b")}))
	if err != nil {
		t.Fatalf("NewNodeAgentService() error = %v", err)
	}
	agent := service.(*nodeAgent)
	agent.newLeaseClient = func(gsr.ServiceContext, gsr.ServiceRef) (nodeLeaseClient, error) { return client, nil }
	serviceContext := &nodeAgentServiceContext{self: gsr.ServiceRef{Node: "node-b", ID: 7}}
	if err := agent.Init(serviceContext); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	return agent, serviceContext
}

func testNodeAgentConfig(reporter Reporter) NodeAgentConfig {
	return NodeAgentConfig{
		Reporter:          reporter,
		ObserverNode:      "node-a",
		Discovery:         gsr.ServiceRef{Node: "node-a", ID: 1},
		Address:           "node-b:9000",
		HeartbeatInterval: time.Second,
		CallTimeout:       time.Second,
	}
}

func assertScheduledCommand(t *testing.T, serviceContext *nodeAgentServiceContext, command gsr.CommandID) {
	t.Helper()
	if len(serviceContext.timers) != 1 || serviceContext.timers[0].command != command || serviceContext.timers[0].delay != time.Second {
		t.Fatalf("timers = %#v, want one %d after one second", serviceContext.timers, command)
	}
}

func waitForDiscoveryNode(t *testing.T, client *discovery.Client, node gsr.NodeID) discovery.NodeRecord {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		record, err := client.GetNode(context.Background(), node)
		if err == nil {
			return record
		}
		if !errors.Is(err, discovery.ErrNodeNotFound) {
			t.Fatalf("GetNode() error = %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("node %q was not registered before timeout", node)
	return discovery.NodeRecord{}
}

func testReport(node gsr.NodeID) monitor.Report {
	return monitor.Report{Node: node, Services: []monitor.ServiceReport{{Name: "service"}}, Metrics: monitor.MetricsReport{Counters: map[string]uint64{"requests": 1}, Gauges: map[string]int64{}, DurationsNanos: map[string]int64{}}}
}

var _ gsr.Service = (*nodeAgent)(nil)
