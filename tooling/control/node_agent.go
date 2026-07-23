package control

import (
	"context"
	"errors"
	"strings"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
)

const defaultHeartbeatInterval = 10 * time.Second

type nodeLeaseClient interface {
	RegisterNode(context.Context, gsr.NodeID, string) (discovery.NodeLease, error)
	Heartbeat(context.Context, discovery.NodeLease) (discovery.NodeLease, error)
	UnregisterNode(context.Context, discovery.NodeLease) error
}

type nodeAgent struct {
	config         NodeAgentConfig
	context        gsr.ServiceContext
	leaseClient    nodeLeaseClient
	newLeaseClient func(gsr.ServiceContext, gsr.ServiceRef) (nodeLeaseClient, error)
	lease          discovery.NodeLease
	hasLease       bool
}

// NewNodeAgentService creates a NodeAgentService that maintains its local Discovery lease.
func NewNodeAgentService(config NodeAgentConfig) (gsr.Service, error) {
	if !validNodeAgentConfig(config) {
		return nil, ErrInvalidConfig
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = defaultHeartbeatInterval
	}
	if config.CallTimeout == 0 {
		config.CallTimeout = defaultCallTimeout
	}
	return &nodeAgent{config: config, newLeaseClient: newNodeLeaseClient}, nil
}

func (*nodeAgent) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandGetNodeReport, commandRegisterNodeLease, commandHeartbeatNodeLease}
}

// StartupCommand starts registration only after Runtime has entered Running.
func (*nodeAgent) StartupCommand() (gsr.Command, bool) {
	return gsr.Command{ID: commandRegisterNodeLease}, true
}

func (a *nodeAgent) Init(serviceContext gsr.ServiceContext) error {
	client, err := a.newLeaseClient(serviceContext, a.config.Discovery)
	if err != nil {
		return err
	}
	a.context = serviceContext
	a.leaseClient = client
	return nil
}

func (a *nodeAgent) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case commandGetNodeReport:
		return a.handleGetNodeReport(commandContext, command)
	case commandRegisterNodeLease:
		if command.Payload != nil || !a.allowedLeaseCommandSource(commandContext.Source()) {
			return ErrUnauthorized
		}
		return a.registerLease()
	case commandHeartbeatNodeLease:
		if command.Payload != nil || !a.runtimeNodeSource(commandContext.Source()) {
			return ErrUnauthorized
		}
		return a.heartbeatLease()
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (a *nodeAgent) allowedLeaseCommandSource(source gsr.ServiceRef) bool {
	return source == a.context.Self() || a.runtimeNodeSource(source)
}

func (a *nodeAgent) runtimeNodeSource(source gsr.ServiceRef) bool {
	return source.Node == a.context.Self().Node && source.ID == 0
}

func (a *nodeAgent) handleGetNodeReport(commandContext gsr.CommandContext, command gsr.Command) error {
	if _, ok := command.Payload.(getNodeReportRequest); !ok {
		return commandContext.Reply(nodeReportResponse{Error: responseInvalidRequest})
	}
	source := commandContext.Source()
	if source.Node != a.config.ObserverNode || source.ID == 0 {
		return commandContext.Reply(nodeReportResponse{Error: responseUnauthorized})
	}
	return commandContext.Reply(nodeReportResponse{Report: cloneReport(a.config.Reporter.Capture())})
}

func (a *nodeAgent) registerLease() error {
	callContext, cancel := context.WithTimeout(context.Background(), a.config.CallTimeout)
	lease, err := a.leaseClient.RegisterNode(callContext, a.context.Self().Node, a.config.Address)
	cancel()
	if err == nil {
		a.lease = lease
		a.hasLease = true
		return a.schedule(commandHeartbeatNodeLease)
	}
	return a.schedule(commandRegisterNodeLease)
}

func (a *nodeAgent) heartbeatLease() error {
	if !a.hasLease {
		return a.registerLease()
	}
	callContext, cancel := context.WithTimeout(context.Background(), a.config.CallTimeout)
	lease, err := a.leaseClient.Heartbeat(callContext, a.lease)
	cancel()
	if err == nil {
		a.lease = lease
		return a.schedule(commandHeartbeatNodeLease)
	}
	if errors.Is(err, discovery.ErrLeaseExpired) {
		return a.registerLease()
	}
	return a.schedule(commandHeartbeatNodeLease)
}

func (a *nodeAgent) schedule(command gsr.CommandID) error {
	_, err := a.context.After(a.config.HeartbeatInterval, command, nil)
	return err
}

func (a *nodeAgent) Stop(stopContext context.Context) error {
	if !a.hasLease || a.leaseClient == nil {
		return nil
	}
	lease := a.lease
	a.hasLease = false
	callContext, cancel := context.WithTimeout(stopContext, a.config.CallTimeout)
	err := a.leaseClient.UnregisterNode(callContext, lease)
	cancel()
	if errors.Is(err, discovery.ErrLeaseExpired) {
		return nil
	}
	return nil
}

func (a *nodeAgent) Close() error {
	a.context = nil
	a.leaseClient = nil
	a.lease = discovery.NodeLease{}
	a.hasLease = false
	return nil
}

func newNodeLeaseClient(caller gsr.ServiceContext, target gsr.ServiceRef) (nodeLeaseClient, error) {
	return discovery.NewClient(caller, target)
}

func validNodeAgentConfig(config NodeAgentConfig) bool {
	return !isNil(config.Reporter) && validNode(config.ObserverNode) && validServiceRef(config.Discovery) && strings.TrimSpace(config.Address) != "" && config.HeartbeatInterval >= 0 && config.CallTimeout >= 0
}

var _ gsr.Service = (*nodeAgent)(nil)
var _ gsr.StartupCommandDeclarer = (*nodeAgent)(nil)
