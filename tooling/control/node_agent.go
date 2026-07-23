package control

import (
	"context"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type nodeAgent struct {
	config NodeAgentConfig
}

// NewNodeAgentService creates a read-only NodeAgentService for one local Monitor reporter.
func NewNodeAgentService(config NodeAgentConfig) (gsr.Service, error) {
	if isNil(config.Reporter) || !validNode(config.ObserverNode) {
		return nil, ErrInvalidConfig
	}
	return &nodeAgent{config: config}, nil
}

func (*nodeAgent) Commands() []gsr.CommandID { return []gsr.CommandID{commandGetNodeReport} }

func (*nodeAgent) Init(gsr.ServiceContext) error { return nil }

func (a *nodeAgent) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandGetNodeReport {
		return gsr.ErrCommandNotRegistered
	}
	if _, ok := command.Payload.(getNodeReportRequest); !ok {
		return commandContext.Reply(nodeReportResponse{Error: responseInvalidRequest})
	}
	source := commandContext.Source()
	if source.Node != a.config.ObserverNode || source.ID == 0 {
		return commandContext.Reply(nodeReportResponse{Error: responseUnauthorized})
	}
	return commandContext.Reply(nodeReportResponse{Report: cloneReport(a.config.Reporter.Capture())})
}

func (*nodeAgent) Stop(context.Context) error { return nil }
func (*nodeAgent) Close() error               { return nil }
