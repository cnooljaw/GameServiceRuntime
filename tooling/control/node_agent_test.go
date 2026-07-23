package control

import (
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/monitor"
)

func TestNodeAgentRejectsUnauthorizedSourceBeforeCapture(t *testing.T) {
	reporter := &countingReporter{report: testReport("node-b")}
	service, err := NewNodeAgentService(NodeAgentConfig{Reporter: reporter, ControlNode: "node-a"})
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
	service, err := NewNodeAgentService(NodeAgentConfig{Reporter: reporter, ControlNode: "node-a"})
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
	service, err := NewNodeAgentService(NodeAgentConfig{Reporter: reporter, ControlNode: "node-a"})
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

func testReport(node gsr.NodeID) monitor.Report {
	return monitor.Report{Node: node, Services: []monitor.ServiceReport{{Name: "service"}}, Metrics: monitor.MetricsReport{Counters: map[string]uint64{"requests": 1}, Gauges: map[string]int64{}, DurationsNanos: map[string]int64{}}}
}

var _ gsr.Service = (*nodeAgent)(nil)
