package servicegroup

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestDirectoryPublishesCanonicalVersionedServiceSets(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "publisher-node"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service, err := NewDirectoryService(DirectoryConfig{PublisherNode: "publisher-node"})
	if err != nil {
		t.Fatalf("NewDirectoryService() error = %v", err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Name: DefaultDirectoryName, Service: service})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	client, err := NewClient(runtime, ref)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	refs := []gsr.ServiceRef{
		{Node: "node-b", ID: 3},
		{Node: "node-a", ID: 2},
		{Node: "node-a", ID: 1},
		{Node: "node-a", ID: 2},
	}
	tags := map[string]string{"version": "blue"}
	first, err := client.Publish(context.Background(), "match-worker", ServiceSetVersion{}, refs, tags)
	if err != nil {
		t.Fatalf("Publish(first) error = %v", err)
	}
	wantRefs := []gsr.ServiceRef{
		{Node: "node-a", ID: 1},
		{Node: "node-a", ID: 2},
		{Node: "node-b", ID: 3},
	}
	if first.Name != "match-worker" || first.Version.AuthorityEpoch == 0 || first.Version.Revision != 1 {
		t.Fatalf("first ServiceSet = %#v", first)
	}
	if !reflect.DeepEqual(first.Refs, wantRefs) || !reflect.DeepEqual(first.Tags, tags) {
		t.Fatalf("first ServiceSet = %#v, want refs %#v tags %#v", first, wantRefs, tags)
	}

	refs[0] = gsr.ServiceRef{Node: "mutated", ID: 99}
	tags["version"] = "mutated"
	first.Refs[0] = gsr.ServiceRef{Node: "mutated-result", ID: 100}
	first.Tags["version"] = "mutated-result"
	stored, err := client.Get(context.Background(), "match-worker")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !reflect.DeepEqual(stored.Refs, wantRefs) || stored.Tags["version"] != "blue" {
		t.Fatalf("stored ServiceSet mutated through input/result: %#v", stored)
	}

	second, err := client.Publish(
		context.Background(),
		"match-worker",
		stored.Version,
		[]gsr.ServiceRef{{Node: "node-c", ID: 4}},
		map[string]string{"version": "green"},
	)
	if err != nil {
		t.Fatalf("Publish(second) error = %v", err)
	}
	if second.Version.AuthorityEpoch != stored.Version.AuthorityEpoch || second.Version.Revision != 2 {
		t.Fatalf("second version = %#v, first = %#v", second.Version, stored.Version)
	}
	if _, err := client.Publish(context.Background(), "match-worker", stored.Version, wantRefs, nil); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("Publish(stale) error = %v, want ErrVersionConflict", err)
	}
	current, err := client.Get(context.Background(), "match-worker")
	if err != nil {
		t.Fatalf("Get(current) error = %v", err)
	}
	if current.Version != second.Version || !reflect.DeepEqual(current.Refs, second.Refs) {
		t.Fatalf("current = %#v, want second %#v", current, second)
	}
}

func TestDirectoryDistinguishesMissingAndPublishedEmptyGroup(t *testing.T) {
	client := newDirectoryClient(t, "publisher-node", DirectoryConfig{PublisherNode: "publisher-node"})

	if _, err := client.Get(context.Background(), "empty"); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("Get(missing) error = %v, want ErrGroupNotFound", err)
	}
	published, err := client.Publish(context.Background(), "empty", ServiceSetVersion{}, nil, nil)
	if err != nil {
		t.Fatalf("Publish(empty) error = %v", err)
	}
	if published.Version.Revision != 1 || published.Refs == nil || len(published.Refs) != 0 || published.Tags == nil {
		t.Fatalf("Publish(empty) = %#v, want non-nil empty collections", published)
	}
	current, err := client.Get(context.Background(), "empty")
	if err != nil {
		t.Fatalf("Get(empty) error = %v", err)
	}
	if current.Version != published.Version || len(current.Refs) != 0 {
		t.Fatalf("Get(empty) = %#v, want %#v", current, published)
	}
}

func TestDirectoryAuthorityEpochFencesOldVersions(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "publisher-node"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	first := createDirectoryClient(t, runtime, DirectoryConfig{PublisherNode: "publisher-node"})
	firstSet, err := first.Publish(context.Background(), "battle", ServiceSetVersion{}, nil, nil)
	if err != nil {
		t.Fatalf("first Publish() error = %v", err)
	}
	second := createDirectoryClient(t, runtime, DirectoryConfig{PublisherNode: "publisher-node"})
	if _, err := second.Publish(context.Background(), "battle", firstSet.Version, nil, nil); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("Publish(old authority) error = %v, want ErrVersionConflict", err)
	}
	secondSet, err := second.Publish(context.Background(), "battle", ServiceSetVersion{}, nil, nil)
	if err != nil {
		t.Fatalf("second Publish() error = %v", err)
	}
	if secondSet.Version.AuthorityEpoch == firstSet.Version.AuthorityEpoch {
		t.Fatalf("authority epochs are equal: %#v", secondSet.Version)
	}
}

func TestDirectoryRejectsRevisionExhaustionWithoutChangingCurrentSet(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "publisher-node"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	created, err := NewDirectoryService(DirectoryConfig{PublisherNode: "publisher-node"})
	if err != nil {
		t.Fatalf("NewDirectoryService() error = %v", err)
	}
	directory := created.(*directoryService)
	exhausted := ServiceSet{
		Name:    "match",
		Version: ServiceSetVersion{AuthorityEpoch: directory.authorityEpoch, Revision: math.MaxUint64},
		Refs:    make([]gsr.ServiceRef, 0),
		Tags:    make(map[string]string),
	}
	directory.groups[exhausted.Name] = cloneServiceSet(exhausted)
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: directory})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	client, err := NewClient(runtime, ref)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.Publish(context.Background(), exhausted.Name, exhausted.Version, nil, nil); !errors.Is(err, ErrVersionExhausted) {
		t.Fatalf("Publish(exhausted) error = %v, want ErrVersionExhausted", err)
	}
	current, err := client.Get(context.Background(), exhausted.Name)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if current.Version != exhausted.Version {
		t.Fatalf("current version = %#v, want %#v", current.Version, exhausted.Version)
	}
}

func TestDirectoryRejectsUnauthorizedAndInvalidRequests(t *testing.T) {
	client := newDirectoryClient(t, "directory-node", DirectoryConfig{PublisherNode: "publisher-node"})
	if _, err := client.Publish(context.Background(), "match", ServiceSetVersion{}, nil, nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Publish(unauthorized) error = %v, want ErrUnauthorized", err)
	}
	if _, err := client.Get(context.Background(), "match"); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("Get(after unauthorized) error = %v, want ErrGroupNotFound", err)
	}

	authorized := newDirectoryClient(t, "publisher-node", DirectoryConfig{PublisherNode: "publisher-node"})
	if _, err := authorized.Publish(context.Background(), " ", ServiceSetVersion{}, nil, nil); !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("Publish(invalid group) error = %v, want ErrInvalidGroup", err)
	}
	if _, err := authorized.Publish(context.Background(), "match", ServiceSetVersion{}, []gsr.ServiceRef{{Node: "node-a"}}, nil); !errors.Is(err, ErrInvalidServiceSet) {
		t.Fatalf("Publish(invalid ref) error = %v, want ErrInvalidServiceSet", err)
	}
	if _, err := authorized.Publish(context.Background(), "match", ServiceSetVersion{}, nil, map[string]string{" ": "value"}); !errors.Is(err, ErrInvalidServiceSet) {
		t.Fatalf("Publish(invalid tags) error = %v, want ErrInvalidServiceSet", err)
	}
	if _, err := authorized.Publish(context.Background(), GroupName("match\xff"), ServiceSetVersion{}, nil, nil); !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("Publish(invalid UTF-8 group) error = %v, want ErrInvalidGroup", err)
	}
	if _, err := authorized.Publish(context.Background(), "match", ServiceSetVersion{}, nil, map[string]string{"version": "\xff"}); !errors.Is(err, ErrInvalidServiceSet) {
		t.Fatalf("Publish(invalid UTF-8 tag) error = %v, want ErrInvalidServiceSet", err)
	}
	if _, err := authorized.Publish(context.Background(), "match", ServiceSetVersion{AuthorityEpoch: 1}, nil, nil); !errors.Is(err, ErrInvalidServiceSet) {
		t.Fatalf("Publish(partial version) error = %v, want ErrInvalidServiceSet", err)
	}
}

func TestDirectoryAndClientRejectInvalidConfiguration(t *testing.T) {
	if _, err := NewDirectoryService(DirectoryConfig{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewDirectoryService(empty) error = %v, want ErrInvalidConfig", err)
	}
	if _, err := NewDirectoryService(DirectoryConfig{PublisherNode: "node-a", WatchTTL: -time.Second}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewDirectoryService(negative TTL) error = %v, want ErrInvalidConfig", err)
	}
	if _, err := NewDirectoryService(DirectoryConfig{PublisherNode: "node-a", SweepInterval: -time.Second}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewDirectoryService(negative sweep) error = %v, want ErrInvalidConfig", err)
	}
	if _, err := NewClient((*nilCommandCaller)(nil), gsr.ServiceRef{Node: "node-a", ID: 1}); !errors.Is(err, ErrInvalidCaller) {
		t.Fatalf("NewClient(nil caller) error = %v, want ErrInvalidCaller", err)
	}
	if _, err := NewClient(commandCallerStub{}, gsr.ServiceRef{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewClient(invalid target) error = %v, want ErrInvalidConfig", err)
	}
}

func TestClientRejectsMalformedSuccessfulResponse(t *testing.T) {
	client, err := NewClient(commandCallerStub{
		value: serviceSetResponse{
			Set: wireServiceSet{
				Name:    "match",
				Version: ServiceSetVersion{AuthorityEpoch: 1, Revision: 1},
				Refs:    []wireServiceRef{{Node: "node-a"}},
			},
		},
	}, gsr.ServiceRef{Node: "node-a", ID: 1})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.Get(context.Background(), "match"); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Get(malformed response) error = %v, want ErrInvalidResponse", err)
	}
}

func TestClientRejectsPublishResponseThatChangesEmptyValuedTagKey(t *testing.T) {
	client, err := NewClient(commandCallerStub{
		value: serviceSetResponse{
			Set: wireServiceSet{
				Name:    "match",
				Version: ServiceSetVersion{AuthorityEpoch: 1, Revision: 1},
				Refs:    make([]wireServiceRef, 0),
				Tags:    map[string]string{"other": ""},
			},
		},
	}, gsr.ServiceRef{Node: "node-a", ID: 1})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.Publish(context.Background(), "match", ServiceSetVersion{}, nil, map[string]string{"expected": ""}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Publish(changed tag key) error = %v, want ErrInvalidResponse", err)
	}
}

func newDirectoryClient(t *testing.T, node gsr.NodeID, config DirectoryConfig) *Client {
	t.Helper()
	runtime := gsr.NewRuntime(gsr.Config{NodeID: node})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	return createDirectoryClient(t, runtime, config)
}

func createDirectoryClient(t *testing.T, runtime *gsr.Runtime, config DirectoryConfig) *Client {
	t.Helper()
	service, err := NewDirectoryService(config)
	if err != nil {
		t.Fatalf("NewDirectoryService() error = %v", err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	client, err := NewClient(runtime, ref)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

type nilCommandCaller struct{}

func (*nilCommandCaller) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}

type commandCallerStub struct {
	value any
	err   error
}

func (c commandCallerStub) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return c.value, c.err
}
