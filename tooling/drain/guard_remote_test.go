package drain_test

import (
	"context"
	"errors"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/drain"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

const (
	commandRemoteGuardBegin    gsr.CommandID = 0x7f270201
	commandRemoteGuardExternal gsr.CommandID = 0x7f270202
)

func TestRemoteControllerBeginsDrainGuardThroughComposableCodec(t *testing.T) {
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 2}, transportB, drain.NewCodec(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeB.Close(context.Background()) })

	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 2}, transportA, drain.NewCodec(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeA.Close(context.Background()) })

	controllerService := &remoteGuardController{}
	controller, err := nodeA.CreateService(gsr.ServiceSpec{Service: controllerService})
	if err != nil {
		t.Fatal(err)
	}
	guardedInner := &remoteGuardedService{}
	guarded, err := drain.Decorate(guardedInner, drain.GuardConfig{
		Controller:       controller,
		ExternalCommands: []gsr.CommandID{commandRemoteGuardExternal},
	})
	if err != nil {
		t.Fatal(err)
	}
	target, err := nodeB.CreateService(gsr.ServiceSpec{Service: guarded})
	if err != nil {
		t.Fatal(err)
	}
	controllerService.target = target

	nodeClient, err := drain.NewGuardClient(nodeA, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nodeClient.Begin(context.Background()); !errors.Is(err, drain.ErrUnauthorized) {
		t.Fatalf("Begin(node caller) error = %v, want ErrUnauthorized", err)
	}
	value, err := nodeA.Call(context.Background(), controller, commandRemoteGuardBegin, struct{}{})
	if err != nil {
		t.Fatalf("controller Begin error = %v", err)
	}
	status, ok := value.(drain.DrainStatus)
	if !ok || !status.Draining || status.StartedAt.IsZero() {
		t.Fatalf("controller Begin result = %#v", value)
	}
	if _, err := nodeB.Call(context.Background(), target, commandRemoteGuardExternal, struct{}{}); !errors.Is(err, drain.ErrDraining) {
		t.Fatalf("target external Command after remote Begin error = %v, want ErrDraining", err)
	}
	if guardedInner.externalCalls != 0 {
		t.Fatalf("rejected external Command reached inner target: calls=%d", guardedInner.externalCalls)
	}
	current, err := nodeClient.Status(context.Background())
	if err != nil {
		t.Fatalf("remote Status error = %v", err)
	}
	if current != status {
		t.Fatalf("remote Status = %#v, want %#v", current, status)
	}
}

type remoteGuardController struct {
	context gsr.ServiceContext
	target  gsr.ServiceRef
}

func (*remoteGuardController) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandRemoteGuardBegin}
}
func (s *remoteGuardController) Init(context gsr.ServiceContext) error {
	s.context = context
	return nil
}
func (s *remoteGuardController) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandRemoteGuardBegin {
		return gsr.ErrCommandNotRegistered
	}
	if _, ok := command.Payload.(struct{}); !ok {
		return drain.ErrInvalidGuard
	}
	client, err := drain.NewGuardClient(s.context, s.target)
	if err != nil {
		return err
	}
	status, err := client.Begin(context.Background())
	if err != nil {
		return err
	}
	return commandContext.Reply(status)
}
func (*remoteGuardController) Stop(context.Context) error { return nil }
func (*remoteGuardController) Close() error               { return nil }

type remoteGuardedService struct{ externalCalls int }

func (*remoteGuardedService) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandRemoteGuardExternal}
}
func (*remoteGuardedService) Init(gsr.ServiceContext) error { return nil }
func (s *remoteGuardedService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandRemoteGuardExternal {
		return gsr.ErrCommandNotRegistered
	}
	if _, ok := command.Payload.(struct{}); !ok {
		return drain.ErrInvalidGuard
	}
	s.externalCalls++
	return commandContext.Reply("handled")
}
func (*remoteGuardedService) Stop(context.Context) error { return nil }
func (*remoteGuardedService) Close() error               { return nil }
