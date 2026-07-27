package drain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/drain"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

const (
	commandRemoteVisitorAcquire gsr.CommandID = 0x7f270101
	commandRemoteVisitorRenew   gsr.CommandID = 0x7f270102
	commandRemoteVisitorRelease gsr.CommandID = 0x7f270103
)

func TestRemoteVisitorRegistryUsesComposableCodec(t *testing.T) {
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 2}, transportB, drain.NewCodec(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeB.Close(context.Background()) })
	registryService, err := drain.NewVisitorRegistryService(drain.VisitorRegistryConfig{
		LeaseTTL:      time.Minute,
		SweepInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	registryRef, err := nodeB.CreateService(gsr.ServiceSpec{
		Name:    drain.DefaultVisitorRegistryName,
		Service: registryService,
	})
	if err != nil {
		t.Fatal(err)
	}

	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 2}, transportA, drain.NewCodec(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeA.Close(context.Background()) })
	remoteRegistry, err := nodeA.ResolveRemote(context.Background(), "node-b", drain.DefaultVisitorRegistryName)
	if err != nil {
		t.Fatal(err)
	}
	if remoteRegistry != registryRef {
		t.Fatalf("ResolveRemote() = %#v, want %#v", remoteRegistry, registryRef)
	}
	target := gsr.ServiceRef{Node: "node-b", ID: 99}
	visitorRef, err := nodeA.CreateService(gsr.ServiceSpec{Service: &remoteVisitorService{
		registry: remoteRegistry,
		target:   target,
	}})
	if err != nil {
		t.Fatal(err)
	}

	value, err := nodeA.Call(context.Background(), visitorRef, commandRemoteVisitorAcquire, false)
	if err != nil {
		t.Fatalf("Acquire through Service caller error = %v", err)
	}
	lease, ok := value.(drain.VisitorLease)
	if !ok {
		t.Fatalf("Acquire result = %#v", value)
	}
	client, err := drain.NewClient(nodeA, remoteRegistry)
	if err != nil {
		t.Fatal(err)
	}
	visitors, err := client.List(context.Background(), target)
	if err != nil {
		t.Fatalf("remote List() error = %v", err)
	}
	if len(visitors) != 1 || visitors[0].Visitor != visitorRef || visitors[0].Weak {
		t.Fatalf("remote List() = %#v", visitors)
	}
	if _, err := client.Acquire(context.Background(), target, visitorRef, false); !errors.Is(err, drain.ErrLeaseOwnerMismatch) {
		t.Fatalf("Acquire(node source) error = %v, want ErrLeaseOwnerMismatch", err)
	}

	value, err = nodeA.Call(context.Background(), visitorRef, commandRemoteVisitorRenew, struct{}{})
	if err != nil {
		t.Fatalf("remote Renew() error = %v", err)
	}
	renewed, ok := value.(drain.VisitorLease)
	if !ok || renewed.Generation != lease.Generation || !renewed.ExpiresAt.After(lease.ExpiresAt) {
		t.Fatalf("remote Renew() = %#v, want extended %#v", value, lease)
	}
	if _, err := nodeA.Call(context.Background(), visitorRef, commandRemoteVisitorRelease, struct{}{}); err != nil {
		t.Fatalf("remote Release() error = %v", err)
	}
	visitors, err = client.List(context.Background(), target)
	if err != nil {
		t.Fatalf("remote List() after release error = %v", err)
	}
	if len(visitors) != 0 {
		t.Fatalf("remote List() after release = %#v, want empty", visitors)
	}
}

type remoteVisitorService struct {
	context  gsr.ServiceContext
	registry gsr.ServiceRef
	target   gsr.ServiceRef
	lease    drain.VisitorLease
}

func (s *remoteVisitorService) Init(serviceContext gsr.ServiceContext) error {
	s.context = serviceContext
	return nil
}

func (s *remoteVisitorService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	client, err := drain.NewClient(s.context, s.registry)
	if err != nil {
		return err
	}
	switch command.ID {
	case commandRemoteVisitorAcquire:
		weak, ok := command.Payload.(bool)
		if !ok {
			return drain.ErrInvalidVisitor
		}
		lease, err := client.Acquire(context.Background(), s.target, commandContext.Self(), weak)
		if err != nil {
			return err
		}
		s.lease = lease
		return commandContext.Reply(lease)
	case commandRemoteVisitorRenew:
		if _, ok := command.Payload.(struct{}); !ok {
			return drain.ErrInvalidLease
		}
		lease, err := client.Renew(context.Background(), s.lease)
		if err != nil {
			return err
		}
		s.lease = lease
		return commandContext.Reply(lease)
	case commandRemoteVisitorRelease:
		if _, ok := command.Payload.(struct{}); !ok {
			return drain.ErrInvalidLease
		}
		if err := client.Release(context.Background(), s.lease); err != nil {
			return err
		}
		s.lease = drain.VisitorLease{}
		return commandContext.Reply(struct{}{})
	default:
		return gsr.ErrUnknownCommand
	}
}

func (*remoteVisitorService) Stop(context.Context) error { return nil }
func (*remoteVisitorService) Close() error               { return nil }
