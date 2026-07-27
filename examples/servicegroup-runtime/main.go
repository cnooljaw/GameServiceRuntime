package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

const (
	commandStartWatch gsr.CommandID = 0x7f260201
	commandCurrent    gsr.CommandID = 0x7f260202
	commandRoute      gsr.CommandID = 0x7f260203
	commandBroadcast  gsr.CommandID = 0x7f260204
	commandWork       gsr.CommandID = 0x7f260205
	commandNotice     gsr.CommandID = 0x7f260206
)

func main() {
	codecB := servicegroup.NewCodec(workCodec{})
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 2}, transportB, codecB)
	if err != nil {
		log.Fatal(err)
	}
	defer nodeB.Close(context.Background())
	directoryService, err := servicegroup.NewDirectoryService(servicegroup.DirectoryConfig{
		PublisherNode: "node-a",
		WatchTTL:      time.Minute,
		SweepInterval: 10 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := nodeB.CreateService(gsr.ServiceSpec{Name: servicegroup.DefaultDirectoryName, Service: directoryService}); err != nil {
		log.Fatal(err)
	}
	notices := make(chan string, 2)
	workerOne, err := nodeB.CreateService(gsr.ServiceSpec{Service: workerService{name: "worker-1", notices: notices}})
	if err != nil {
		log.Fatal(err)
	}
	workerTwo, err := nodeB.CreateService(gsr.ServiceSpec{Service: workerService{name: "worker-2", notices: notices}})
	if err != nil {
		log.Fatal(err)
	}

	codecA := servicegroup.NewCodec(workCodec{})
	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 2}, transportA, codecA)
	if err != nil {
		log.Fatal(err)
	}
	defer nodeA.Close(context.Background())
	directoryRef, err := nodeA.ResolveRemote(context.Background(), "node-b", servicegroup.DefaultDirectoryName)
	if err != nil {
		log.Fatal(err)
	}
	groupClientRef, err := nodeA.CreateService(gsr.ServiceSpec{Service: &groupRouteService{
		group:     "match-worker",
		directory: directoryRef,
	}})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := nodeA.Call(context.Background(), groupClientRef, commandStartWatch, struct{}{}); err != nil {
		log.Fatal(err)
	}

	directory, err := servicegroup.NewClient(nodeA, directoryRef)
	if err != nil {
		log.Fatal(err)
	}
	published, err := directory.Publish(
		context.Background(),
		"match-worker",
		servicegroup.ServiceSetVersion{},
		[]gsr.ServiceRef{workerTwo, workerOne},
		map[string]string{"version": "blue"},
	)
	if err != nil {
		log.Fatal(err)
	}
	current, err := directory.Get(context.Background(), "match-worker")
	if err != nil || current.Version != published.Version {
		log.Fatalf("servicegroup example: Get returned %#v, error=%v", current.Version, err)
	}
	if err := waitForVersion(nodeA, groupClientRef, published.Version); err != nil {
		log.Fatal(err)
	}
	if _, err := nodeA.Call(context.Background(), groupClientRef, commandBroadcast, "reload-config"); err != nil {
		log.Fatal(err)
	}
	if err := waitForNotices(notices, 2); err != nil {
		log.Fatal(err)
	}
	value, err := nodeA.Call(context.Background(), groupClientRef, commandRoute, routeRequest{
		Key:     "player-42",
		Payload: "ping",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("group=%s revision=%d reply=%s\n", published.Name, published.Version.Revision, value)
}

func waitForNotices(notices <-chan string, count int) error {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for index := 0; index < count; index++ {
		select {
		case <-notices:
		case <-timer.C:
			return errors.New("servicegroup example: Broadcast was not delivered to every worker")
		}
	}
	return nil
}

func waitForVersion(runtime *gsr.Runtime, client gsr.ServiceRef, version servicegroup.ServiceSetVersion) error {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		value, err := runtime.Call(context.Background(), client, commandCurrent, struct{}{})
		if err != nil {
			return err
		}
		set, ok := value.(servicegroup.ServiceSet)
		if !ok {
			return errors.New("servicegroup example: invalid current response")
		}
		if set.Version == version {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("servicegroup example: watch did not observe published version")
}

type routeRequest struct {
	Key     servicegroup.RoutingKey
	Payload string
}

type groupRouteService struct {
	client    *servicegroup.Client
	router    *servicegroup.Router
	group     servicegroup.GroupName
	directory gsr.ServiceRef
	current   servicegroup.ServiceSet
}

func (s *groupRouteService) Init(serviceContext gsr.ServiceContext) error {
	client, err := servicegroup.NewClient(serviceContext, s.directory)
	if err != nil {
		return err
	}
	router, err := servicegroup.NewRouter(serviceContext)
	if err != nil {
		return err
	}
	s.client = client
	s.router = router
	return nil
}

func (s *groupRouteService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case commandStartWatch:
		if _, ok := command.Payload.(struct{}); !ok {
			return servicegroup.ErrInvalidWatch
		}
		callContext, cancel := context.WithTimeout(context.Background(), time.Second)
		result, err := s.client.Watch(callContext, s.group, commandContext.Self())
		cancel()
		if err != nil {
			return err
		}
		if result.Found {
			s.current = cloneServiceSet(result.Current)
		}
		return commandContext.Reply(struct{}{})
	case servicegroup.ServiceSetChangedCommand:
		change, ok := command.Payload.(servicegroup.ServiceSetChanged)
		if !ok || change.Set.Name != s.group {
			return servicegroup.ErrInvalidServiceSet
		}
		if shouldReplace(s.current.Version, change.Set.Version) {
			s.current = cloneServiceSet(change.Set)
		}
		return nil
	case commandCurrent:
		if _, ok := command.Payload.(struct{}); !ok {
			return servicegroup.ErrInvalidServiceSet
		}
		return commandContext.Reply(cloneServiceSet(s.current))
	case commandRoute:
		request, ok := command.Payload.(routeRequest)
		if !ok {
			return servicegroup.ErrInvalidRoutingKey
		}
		callContext, cancel := context.WithTimeout(context.Background(), time.Second)
		result, err := s.router.Call(callContext, s.current, servicegroup.Hash{}, request.Key, commandWork, request.Payload)
		cancel()
		if err != nil {
			return err
		}
		return commandContext.Reply(result)
	case commandBroadcast:
		payload, ok := command.Payload.(string)
		if !ok {
			return servicegroup.ErrInvalidServiceSet
		}
		if err := s.router.Send(s.current, servicegroup.Broadcast{}, "", commandNotice, payload); err != nil {
			return err
		}
		return commandContext.Reply(struct{}{})
	default:
		return gsr.ErrUnknownCommand
	}
}

func (s *groupRouteService) Stop(context.Context) error {
	s.current = servicegroup.ServiceSet{}
	return nil
}

func (s *groupRouteService) Close() error {
	s.client = nil
	s.router = nil
	return nil
}

func shouldReplace(current, next servicegroup.ServiceSetVersion) bool {
	return current == (servicegroup.ServiceSetVersion{}) ||
		current.AuthorityEpoch != next.AuthorityEpoch ||
		next.Revision > current.Revision
}

func cloneServiceSet(set servicegroup.ServiceSet) servicegroup.ServiceSet {
	refs := make([]gsr.ServiceRef, len(set.Refs))
	copy(refs, set.Refs)
	tags := make(map[string]string, len(set.Tags))
	for key, value := range set.Tags {
		tags[key] = value
	}
	return servicegroup.ServiceSet{Name: set.Name, Version: set.Version, Refs: refs, Tags: tags}
}

type workerService struct {
	name    string
	notices chan<- string
}

func (workerService) Init(gsr.ServiceContext) error { return nil }
func (s workerService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	payload, ok := command.Payload.(string)
	if !ok {
		return errors.New("servicegroup example: invalid work payload")
	}
	switch command.ID {
	case commandWork:
		return commandContext.Reply(s.name + ":" + payload)
	case commandNotice:
		s.notices <- s.name + ":" + payload
		return nil
	default:
		return gsr.ErrUnknownCommand
	}
}
func (workerService) Stop(context.Context) error { return nil }
func (workerService) Close() error               { return nil }

type workCodec struct{}

func (workCodec) Encode(command gsr.CommandID, response bool, value any) ([]byte, error) {
	if command != commandWork && command != commandNotice {
		return nil, fmt.Errorf("servicegroup example: unsupported command %d", command)
	}
	if command == commandNotice && response {
		return nil, errors.New("servicegroup example: notice has no response")
	}
	if _, ok := value.(string); !ok {
		return nil, fmt.Errorf("servicegroup example: invalid work payload %T", value)
	}
	return json.Marshal(value)
}

func (workCodec) Decode(command gsr.CommandID, response bool, payload []byte) (any, error) {
	if command != commandWork && command != commandNotice {
		return nil, fmt.Errorf("servicegroup example: unsupported command %d", command)
	}
	if command == commandNotice && response {
		return nil, errors.New("servicegroup example: notice has no response")
	}
	var value string
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, err
	}
	return value, nil
}
