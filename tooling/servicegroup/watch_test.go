package servicegroup

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const commandWatchAction gsr.CommandID = 0x7f260001

func TestWatchCanPrecedePublishAndReceivesFullSnapshot(t *testing.T) {
	fixture := newWatchFixture(t, DirectoryConfig{PublisherNode: "publisher-node"})
	result := fixture.watch(t, "match")
	if result.Found || result.Current.Name != "" {
		t.Fatalf("Watch(missing) = %#v, want Found=false", result)
	}

	published, err := fixture.directory.Publish(
		context.Background(),
		"match",
		ServiceSetVersion{},
		[]gsr.ServiceRef{{Node: "node-b", ID: 2}, {Node: "node-a", ID: 1}},
		map[string]string{"version": "blue"},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	change := fixture.nextChange(t)
	if !reflect.DeepEqual(change.Set, published) {
		t.Fatalf("ServiceSetChanged = %#v, want %#v", change.Set, published)
	}
	change.Set.Refs[0] = gsr.ServiceRef{Node: "mutated", ID: 99}
	change.Set.Tags["version"] = "mutated"
	current, err := fixture.directory.Get(context.Background(), "match")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !reflect.DeepEqual(current, published) {
		t.Fatalf("Directory state mutated through notification: %#v", current)
	}
}

func TestWatchReturnsCurrentSnapshotWithoutSyntheticNotification(t *testing.T) {
	fixture := newWatchFixture(t, DirectoryConfig{PublisherNode: "publisher-node"})
	published, err := fixture.directory.Publish(
		context.Background(),
		"match",
		ServiceSetVersion{},
		[]gsr.ServiceRef{{Node: "node-a", ID: 1}},
		map[string]string{"version": "blue"},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	result := fixture.watch(t, "match")
	if !result.Found || !reflect.DeepEqual(result.Current, published) {
		t.Fatalf("Watch(existing) = %#v, want current %#v", result, published)
	}
	result.Current.Refs[0] = gsr.ServiceRef{Node: "mutated", ID: 99}
	result.Current.Tags["version"] = "mutated"
	current, err := fixture.directory.Get(context.Background(), "match")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !reflect.DeepEqual(current, published) {
		t.Fatalf("Directory state mutated through Watch result: %#v", current)
	}
	fixture.expectNoChange(t)
}

func TestWatchGenerationFencesLateRenewAndUnwatch(t *testing.T) {
	fixture := newWatchFixture(t, DirectoryConfig{PublisherNode: "publisher-node"})
	first := fixture.watch(t, "match").Lease
	second := fixture.watch(t, "match").Lease
	if second.AuthorityEpoch != first.AuthorityEpoch || second.Generation == first.Generation {
		t.Fatalf("watch generations first=%#v second=%#v", first, second)
	}
	if err := fixture.unwatch(t, first); !errors.Is(err, ErrWatchExpired) {
		t.Fatalf("Unwatch(old generation) error = %v, want ErrWatchExpired", err)
	}
	if _, err := fixture.renew(t, first); !errors.Is(err, ErrWatchExpired) {
		t.Fatalf("RenewWatch(old generation) error = %v, want ErrWatchExpired", err)
	}
	renewed, err := fixture.renew(t, second)
	if err != nil {
		t.Fatalf("RenewWatch(current) error = %v", err)
	}
	if renewed.AuthorityEpoch != second.AuthorityEpoch || renewed.Generation != second.Generation || !renewed.ExpiresAt.After(second.ExpiresAt) {
		t.Fatalf("RenewWatch(current) = %#v, want identity %#v with later expiry", renewed, second)
	}
	if err := fixture.unwatch(t, renewed); err != nil {
		t.Fatalf("Unwatch(current) error = %v", err)
	}
	if _, err := fixture.directory.Publish(context.Background(), "match", ServiceSetVersion{}, nil, nil); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	fixture.expectNoChange(t)
}

func TestWatchAuthorityFencesLeaseAfterDirectoryRestart(t *testing.T) {
	fixture := newWatchFixture(t, DirectoryConfig{PublisherNode: "publisher-node"})
	old := fixture.watch(t, "match").Lease
	created, err := NewDirectoryService(DirectoryConfig{PublisherNode: "publisher-node"})
	if err != nil {
		t.Fatalf("NewDirectoryService() error = %v", err)
	}
	restarted := created.(*directoryService)
	if _, err := restarted.renewWatch(time.Now(), old.Subscriber, old); !errors.Is(err, ErrWatchExpired) {
		t.Fatalf("RenewWatch(old authority) error = %v, want ErrWatchExpired", err)
	}
	if err := restarted.unwatch(old.Subscriber, old); !errors.Is(err, ErrWatchExpired) {
		t.Fatalf("Unwatch(old authority) error = %v, want ErrWatchExpired", err)
	}
}

func TestWatchTimerExpiresLeaseAndUpdatesGauge(t *testing.T) {
	fixture := newWatchFixture(t, DirectoryConfig{
		PublisherNode: "publisher-node",
		WatchTTL:      15 * time.Millisecond,
		SweepInterval: 2 * time.Millisecond,
	})
	lease := fixture.watch(t, "match").Lease
	eventuallyServiceGroup(t, func() bool {
		return fixture.runtime.Inspect().Metrics.Gauge("servicegroup_watchers") == 1
	})
	eventuallyServiceGroup(t, func() bool {
		return fixture.runtime.Inspect().Metrics.Gauge("servicegroup_watchers") == 0
	})
	if _, err := fixture.renew(t, lease); !errors.Is(err, ErrWatchExpired) {
		t.Fatalf("RenewWatch(expired) error = %v, want ErrWatchExpired", err)
	}
}

func TestWatchNotificationFailureDoesNotRollbackPublish(t *testing.T) {
	fixture := newWatchFixture(t, DirectoryConfig{
		PublisherNode: "publisher-node",
		WatchTTL:      time.Minute,
		SweepInterval: time.Hour,
	})
	fixture.watch(t, "match")
	if err := fixture.runtime.Stop(context.Background(), fixture.subscriberRef); err != nil {
		t.Fatalf("Stop(subscriber) error = %v", err)
	}

	published, err := fixture.directory.Publish(context.Background(), "match", ServiceSetVersion{}, nil, map[string]string{"state": "ready"})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	current, err := fixture.directory.Get(context.Background(), "match")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !reflect.DeepEqual(current, published) {
		t.Fatalf("current = %#v, want published %#v", current, published)
	}
	if got := fixture.runtime.Inspect().Metrics.Counter("servicegroup_notify_failed_total"); got != 1 {
		t.Fatalf("notify failed metric = %d, want 1", got)
	}
	if got := fixture.runtime.Inspect().Metrics.Gauge("servicegroup_watchers"); got != 1 {
		t.Fatalf("watcher gauge after transient failure = %d, want 1", got)
	}
}

func TestWatchRejectsSpoofedSubscriberAndInvalidLease(t *testing.T) {
	fixture := newWatchFixture(t, DirectoryConfig{PublisherNode: "publisher-node"})
	if _, err := fixture.directory.Watch(context.Background(), "match", fixture.subscriberRef); !errors.Is(err, ErrWatchOwnerMismatch) {
		t.Fatalf("Watch(spoofed source) error = %v, want ErrWatchOwnerMismatch", err)
	}
	if _, err := fixture.directory.Watch(context.Background(), "match", gsr.ServiceRef{}); !errors.Is(err, ErrInvalidWatch) {
		t.Fatalf("Watch(invalid subscriber) error = %v, want ErrInvalidWatch", err)
	}
	if got := fixture.runtime.Inspect().Metrics.Gauge("servicegroup_watchers"); got != 0 {
		t.Fatalf("watcher gauge = %d, want 0", got)
	}
}

type watchFixture struct {
	runtime       *gsr.Runtime
	directory     *Client
	subscriberRef gsr.ServiceRef
	changes       chan ServiceSetChanged
}

func newWatchFixture(t *testing.T, config DirectoryConfig) watchFixture {
	t.Helper()
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "publisher-node", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service, err := NewDirectoryService(config)
	if err != nil {
		t.Fatalf("NewDirectoryService() error = %v", err)
	}
	directoryRef, err := runtime.CreateService(gsr.ServiceSpec{Name: DefaultDirectoryName, Service: service})
	if err != nil {
		t.Fatalf("CreateService(directory) error = %v", err)
	}
	client, err := NewClient(runtime, directoryRef)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	changes := make(chan ServiceSetChanged, 8)
	subscriberRef, err := runtime.CreateService(gsr.ServiceSpec{Service: &watchSubscriber{
		directory: directoryRef,
		changes:   changes,
	}})
	if err != nil {
		t.Fatalf("CreateService(subscriber) error = %v", err)
	}
	return watchFixture{
		runtime:       runtime,
		directory:     client,
		subscriberRef: subscriberRef,
		changes:       changes,
	}
}

func (f watchFixture) watch(t *testing.T, group GroupName) WatchResult {
	t.Helper()
	value, err := f.runtime.Call(context.Background(), f.subscriberRef, commandWatchAction, watchActionRequest{
		Kind:  watchActionStart,
		Group: group,
	})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	result, ok := value.(WatchResult)
	if !ok {
		t.Fatalf("Watch() response = %#v", value)
	}
	return result
}

func (f watchFixture) renew(t *testing.T, lease WatchLease) (WatchLease, error) {
	t.Helper()
	value, err := f.runtime.Call(context.Background(), f.subscriberRef, commandWatchAction, watchActionRequest{
		Kind:  watchActionRenew,
		Lease: lease,
	})
	if err != nil {
		return WatchLease{}, err
	}
	result, ok := value.(WatchLease)
	if !ok {
		t.Fatalf("RenewWatch() response = %#v", value)
	}
	return result, nil
}

func (f watchFixture) unwatch(t *testing.T, lease WatchLease) error {
	t.Helper()
	_, err := f.runtime.Call(context.Background(), f.subscriberRef, commandWatchAction, watchActionRequest{
		Kind:  watchActionStop,
		Lease: lease,
	})
	return err
}

func (f watchFixture) nextChange(t *testing.T) ServiceSetChanged {
	t.Helper()
	select {
	case change := <-f.changes:
		return change
	case <-time.After(time.Second):
		t.Fatal("ServiceSetChanged was not delivered")
		return ServiceSetChanged{}
	}
}

func (f watchFixture) expectNoChange(t *testing.T) {
	t.Helper()
	select {
	case change := <-f.changes:
		t.Fatalf("unexpected ServiceSetChanged: %#v", change)
	default:
	}
}

type watchActionKind uint8

const (
	watchActionStart watchActionKind = iota + 1
	watchActionRenew
	watchActionStop
)

type watchActionRequest struct {
	Kind  watchActionKind
	Group GroupName
	Lease WatchLease
}

type watchSubscriber struct {
	context   gsr.ServiceContext
	directory gsr.ServiceRef
	changes   chan<- ServiceSetChanged
}

func (*watchSubscriber) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandWatchAction, ServiceSetChangedCommand}
}

func (s *watchSubscriber) Init(serviceContext gsr.ServiceContext) error {
	s.context = serviceContext
	return nil
}

func (s *watchSubscriber) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case commandWatchAction:
		request, ok := command.Payload.(watchActionRequest)
		if !ok {
			return ErrInvalidWatch
		}
		client, err := NewClient(s.context, s.directory)
		if err != nil {
			return err
		}
		switch request.Kind {
		case watchActionStart:
			result, err := client.Watch(context.Background(), request.Group, commandContext.Self())
			if err != nil {
				return err
			}
			return commandContext.Reply(result)
		case watchActionRenew:
			lease, err := client.RenewWatch(context.Background(), request.Lease)
			if err != nil {
				return err
			}
			return commandContext.Reply(lease)
		case watchActionStop:
			if err := client.Unwatch(context.Background(), request.Lease); err != nil {
				return err
			}
			return commandContext.Reply(struct{}{})
		default:
			return ErrInvalidWatch
		}
	case ServiceSetChangedCommand:
		change, ok := command.Payload.(ServiceSetChanged)
		if !ok {
			return ErrInvalidServiceSet
		}
		s.changes <- change
		return nil
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (*watchSubscriber) Stop(context.Context) error { return nil }
func (*watchSubscriber) Close() error               { return nil }

func eventuallyServiceGroup(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
