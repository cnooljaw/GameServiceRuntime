package discovery

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestServiceReturnsInfrastructureErrorWithoutDiscoveryReply(t *testing.T) {
	want := gsr.ErrRuntimeClosed
	created, err := NewService(Config{LeaseTTL: time.Minute, SweepInterval: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	service := created.(*service)
	serviceContext := infrastructureServiceContext{afterErr: want}
	if err := service.Init(serviceContext); err != nil {
		t.Fatal(err)
	}
	commandContext := &recordingCommandContext{source: gsr.ServiceRef{Node: "node-a"}}

	err = service.Handle(commandContext, gsr.Command{
		ID:      commandRegisterNode,
		Payload: registerNodeRequest{Node: "node-a", Address: "node-a:9000"},
	})
	if !errors.Is(err, want) {
		t.Fatalf("Handle error = %v, want %v", err, want)
	}
	if commandContext.replied {
		t.Fatal("infrastructure error was converted into a Discovery Reply")
	}
	if len(service.nodes) != 0 {
		t.Fatalf("node registry changed after infrastructure failure: %#v", service.nodes)
	}
}

type infrastructureServiceContext struct {
	afterErr error
}

func (infrastructureServiceContext) Self() gsr.ServiceRef {
	return gsr.ServiceRef{Node: "node-a", ID: 1}
}
func (infrastructureServiceContext) Send(gsr.ServiceRef, gsr.CommandID, any) error {
	return nil
}
func (infrastructureServiceContext) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}
func (c infrastructureServiceContext) After(time.Duration, gsr.CommandID, any) (gsr.TimerID, error) {
	return 0, c.afterErr
}
func (infrastructureServiceContext) Now() time.Time {
	return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
}
func (infrastructureServiceContext) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
func (infrastructureServiceContext) Metrics() gsr.Metrics { return noopMetrics{} }

type recordingCommandContext struct {
	source  gsr.ServiceRef
	replied bool
}

func (c *recordingCommandContext) Self() gsr.ServiceRef   { return gsr.ServiceRef{Node: "node-a", ID: 1} }
func (c *recordingCommandContext) Source() gsr.ServiceRef { return c.source }
func (c *recordingCommandContext) Reply(any) error {
	c.replied = true
	return nil
}

type noopMetrics struct{}

func (noopMetrics) Inc(string)                    {}
func (noopMetrics) Add(string, uint64)            {}
func (noopMetrics) SetGauge(string, int64)        {}
func (noopMetrics) Observe(string, time.Duration) {}
