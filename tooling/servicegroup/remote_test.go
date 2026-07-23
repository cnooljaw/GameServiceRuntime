package servicegroup_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

const (
	commandSubscribe         gsr.CommandID = 0x7f260101
	commandRenewSubscription gsr.CommandID = 0x7f260102
	commandUnsubscribe       gsr.CommandID = 0x7f260103
	commandRouteSend         gsr.CommandID = 0x7f260104
	commandRouteCall         gsr.CommandID = 0x7f260105
)

func TestRemoteDirectoryWatchAndRouterUseComposableCodec(t *testing.T) {
	codecB := servicegroup.NewCodec(routeCodec{})
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 2}, transportB, codecB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeB.Close(context.Background()) })
	directoryService, err := servicegroup.NewDirectoryService(servicegroup.DirectoryConfig{
		PublisherNode: "node-a",
		WatchTTL:      time.Minute,
		SweepInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	directoryRef, err := nodeB.CreateService(gsr.ServiceSpec{Name: servicegroup.DefaultDirectoryName, Service: directoryService})
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 2)
	workerOne, err := nodeB.CreateService(gsr.ServiceSpec{Service: routeService{name: "worker-1", received: received}})
	if err != nil {
		t.Fatal(err)
	}
	workerTwo, err := nodeB.CreateService(gsr.ServiceSpec{Service: routeService{name: "worker-2", received: received}})
	if err != nil {
		t.Fatal(err)
	}

	codecA := servicegroup.NewCodec(routeCodec{})
	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 2}, transportA, codecA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeA.Close(context.Background()) })
	remoteDirectory, err := nodeA.ResolveRemote(context.Background(), "node-b", servicegroup.DefaultDirectoryName)
	if err != nil {
		t.Fatal(err)
	}
	if remoteDirectory != directoryRef {
		t.Fatalf("ResolveRemote() = %#v, want %#v", remoteDirectory, directoryRef)
	}
	directory, err := servicegroup.NewClient(nodeA, remoteDirectory)
	if err != nil {
		t.Fatal(err)
	}
	changes := make(chan servicegroup.ServiceSetChanged, 2)
	subscriberRef, err := nodeA.CreateService(gsr.ServiceSpec{Service: &remoteSubscriber{
		directory: remoteDirectory,
		changes:   changes,
	}})
	if err != nil {
		t.Fatal(err)
	}
	value, err := nodeA.Call(context.Background(), subscriberRef, commandSubscribe, servicegroup.GroupName("match"))
	if err != nil {
		t.Fatal(err)
	}
	watch, ok := value.(servicegroup.WatchResult)
	if !ok || watch.Found {
		t.Fatalf("Watch() = %#v, want missing group", value)
	}
	value, err = nodeA.Call(context.Background(), subscriberRef, commandRenewSubscription, struct{}{})
	if err != nil {
		t.Fatalf("remote RenewWatch() error = %v", err)
	}
	renewed, ok := value.(servicegroup.WatchLease)
	if !ok || renewed.Generation != watch.Lease.Generation || !renewed.ExpiresAt.After(watch.Lease.ExpiresAt) {
		t.Fatalf("remote RenewWatch() = %#v, want renewed %#v", value, watch.Lease)
	}

	published, err := directory.Publish(
		context.Background(),
		"match",
		servicegroup.ServiceSetVersion{},
		[]gsr.ServiceRef{workerTwo, workerOne},
		map[string]string{"version": "blue"},
	)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case change := <-changes:
		if change.Set.Version != published.Version || len(change.Set.Refs) != 2 {
			t.Fatalf("ServiceSetChanged = %#v, want %#v", change.Set, published)
		}
	case <-time.After(time.Second):
		t.Fatal("remote ServiceSetChanged was not delivered over the inbound cluster connection")
	}
	if _, err := directory.Publish(context.Background(), "match", servicegroup.ServiceSetVersion{}, nil, nil); !errors.Is(err, servicegroup.ErrVersionConflict) {
		t.Fatalf("Publish(stale) error = %T %v, want ErrVersionConflict", err, err)
	}
	current, err := directory.Get(context.Background(), "match")
	if err != nil {
		t.Fatalf("remote Get() error = %v", err)
	}
	if current.Version != published.Version || len(current.Refs) != len(published.Refs) {
		t.Fatalf("remote Get() = %#v, want %#v", current, published)
	}

	router, err := servicegroup.NewRouter(nodeA)
	if err != nil {
		t.Fatal(err)
	}
	if err := router.Send(published, servicegroup.Hash{}, "player-42", commandRouteSend, "hello"); err != nil {
		t.Fatalf("Router.Send() error = %v", err)
	}
	select {
	case message := <-received:
		if message != "worker-1:hello" && message != "worker-2:hello" {
			t.Fatalf("remote Send message = %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("remote routed Send was not delivered")
	}
	reply, err := router.Call(context.Background(), published, &servicegroup.RoundRobin{}, "", commandRouteCall, "ping")
	if err != nil {
		t.Fatalf("Router.Call() error = %v", err)
	}
	if reply != "worker-1:ping" {
		t.Fatalf("Router.Call() = %#v, want worker-1:ping", reply)
	}
	if _, err := nodeA.Call(context.Background(), subscriberRef, commandUnsubscribe, struct{}{}); err != nil {
		t.Fatalf("remote Unwatch() error = %v", err)
	}
}

type remoteSubscriber struct {
	context   gsr.ServiceContext
	directory gsr.ServiceRef
	changes   chan<- servicegroup.ServiceSetChanged
	lease     servicegroup.WatchLease
}

func (*remoteSubscriber) Commands() []gsr.CommandID {
	return []gsr.CommandID{
		commandSubscribe,
		commandRenewSubscription,
		commandUnsubscribe,
		servicegroup.ServiceSetChangedCommand,
	}
}

func (s *remoteSubscriber) Init(serviceContext gsr.ServiceContext) error {
	s.context = serviceContext
	return nil
}

func (s *remoteSubscriber) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case commandSubscribe:
		group, ok := command.Payload.(servicegroup.GroupName)
		if !ok {
			return servicegroup.ErrInvalidGroup
		}
		client, err := servicegroup.NewClient(s.context, s.directory)
		if err != nil {
			return err
		}
		result, err := client.Watch(context.Background(), group, commandContext.Self())
		if err != nil {
			return err
		}
		s.lease = result.Lease
		return commandContext.Reply(result)
	case commandRenewSubscription:
		if _, ok := command.Payload.(struct{}); !ok {
			return servicegroup.ErrInvalidWatch
		}
		client, err := servicegroup.NewClient(s.context, s.directory)
		if err != nil {
			return err
		}
		lease, err := client.RenewWatch(context.Background(), s.lease)
		if err != nil {
			return err
		}
		s.lease = lease
		return commandContext.Reply(lease)
	case commandUnsubscribe:
		if _, ok := command.Payload.(struct{}); !ok {
			return servicegroup.ErrInvalidWatch
		}
		client, err := servicegroup.NewClient(s.context, s.directory)
		if err != nil {
			return err
		}
		if err := client.Unwatch(context.Background(), s.lease); err != nil {
			return err
		}
		s.lease = servicegroup.WatchLease{}
		return commandContext.Reply(struct{}{})
	case servicegroup.ServiceSetChangedCommand:
		change, ok := command.Payload.(servicegroup.ServiceSetChanged)
		if !ok {
			return servicegroup.ErrInvalidServiceSet
		}
		s.changes <- change
		return nil
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (*remoteSubscriber) Stop(context.Context) error { return nil }
func (*remoteSubscriber) Close() error               { return nil }

type routeService struct {
	name     string
	received chan<- string
}

func (routeService) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandRouteSend, commandRouteCall}
}
func (routeService) Init(gsr.ServiceContext) error { return nil }
func (s routeService) Handle(context gsr.CommandContext, command gsr.Command) error {
	payload, ok := command.Payload.(string)
	if !ok {
		return fmt.Errorf("route service: invalid payload")
	}
	switch command.ID {
	case commandRouteSend:
		s.received <- s.name + ":" + payload
		return nil
	case commandRouteCall:
		return context.Reply(s.name + ":" + payload)
	default:
		return gsr.ErrCommandNotRegistered
	}
}
func (routeService) Stop(context.Context) error { return nil }
func (routeService) Close() error               { return nil }

type routeCodec struct{}

func (routeCodec) Encode(command gsr.CommandID, response bool, value any) ([]byte, error) {
	if command != commandRouteSend && command != commandRouteCall {
		return nil, fmt.Errorf("route codec: unsupported command %d", command)
	}
	if command == commandRouteSend && response {
		return nil, fmt.Errorf("route codec: Send has no response")
	}
	if _, ok := value.(string); !ok {
		return nil, fmt.Errorf("route codec: invalid payload %T", value)
	}
	return json.Marshal(value)
}

func (routeCodec) Decode(command gsr.CommandID, response bool, payload []byte) (any, error) {
	if command != commandRouteSend && command != commandRouteCall {
		return nil, fmt.Errorf("route codec: unsupported command %d", command)
	}
	if command == commandRouteSend && response {
		return nil, fmt.Errorf("route codec: Send has no response")
	}
	var value string
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, err
	}
	return value, nil
}
