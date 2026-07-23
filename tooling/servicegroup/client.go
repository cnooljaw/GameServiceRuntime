package servicegroup

import (
	"context"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// NewClient binds a typed Directory Client to a known DirectoryService.
func NewClient(caller CommandCaller, target gsr.ServiceRef) (*Client, error) {
	if isNil(caller) {
		return nil, ErrInvalidCaller
	}
	if !validServiceRef(target) {
		return nil, ErrInvalidConfig
	}
	return &Client{caller: caller, target: target}, nil
}

// Publish compare-and-sets one complete ServiceSet and returns its Directory-assigned version.
func (c *Client) Publish(ctx context.Context, name GroupName, expected ServiceSetVersion, refs []gsr.ServiceRef, tags map[string]string) (ServiceSet, error) {
	if !validExpectedVersion(expected) {
		return ServiceSet{}, ErrInvalidServiceSet
	}
	input, err := normalizeServiceSet(name, refs, tags)
	if err != nil {
		return ServiceSet{}, err
	}
	wireRefs := make([]wireServiceRef, len(input.Refs))
	for index, ref := range input.Refs {
		wireRefs[index] = newWireServiceRef(ref)
	}
	value, err := c.caller.Call(ctx, c.target, commandPublishServiceSet, publishServiceSetRequest{
		Name:     input.Name,
		Expected: expected,
		Refs:     wireRefs,
		Tags:     cloneTags(input.Tags),
	})
	if err != nil {
		return ServiceSet{}, err
	}
	set, err := serviceSetFromResponse(value, input.Name)
	if err != nil {
		return ServiceSet{}, err
	}
	switch {
	case expected == (ServiceSetVersion{}) && set.Version.Revision != 1:
		return ServiceSet{}, ErrInvalidResponse
	case expected != (ServiceSetVersion{}) &&
		(set.Version.AuthorityEpoch != expected.AuthorityEpoch || set.Version.Revision != expected.Revision+1):
		return ServiceSet{}, ErrInvalidResponse
	}
	if !sameServiceSetContent(set, input) {
		return ServiceSet{}, ErrInvalidResponse
	}
	return cloneServiceSet(set), nil
}

// Get returns one independent complete ServiceSet.
func (c *Client) Get(ctx context.Context, name GroupName) (ServiceSet, error) {
	if !validGroup(name) {
		return ServiceSet{}, ErrInvalidGroup
	}
	value, err := c.caller.Call(ctx, c.target, commandGetServiceSet, getServiceSetRequest{Name: name})
	if err != nil {
		return ServiceSet{}, err
	}
	return serviceSetFromResponse(value, name)
}

// Watch registers subscriber for complete ServiceSetChanged snapshots.
func (c *Client) Watch(ctx context.Context, name GroupName, subscriber gsr.ServiceRef) (WatchResult, error) {
	if !validGroup(name) {
		return WatchResult{}, ErrInvalidGroup
	}
	if !validServiceRef(subscriber) {
		return WatchResult{}, ErrInvalidWatch
	}
	value, err := c.caller.Call(ctx, c.target, commandWatchServiceGroup, watchServiceGroupRequest{
		Name:       name,
		Subscriber: newWireServiceRef(subscriber),
	})
	if err != nil {
		return WatchResult{}, err
	}
	response, ok := value.(watchResultResponse)
	if !ok {
		return WatchResult{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return WatchResult{}, err
	}
	if !validWireWatchResult(response) {
		return WatchResult{}, ErrInvalidResponse
	}
	lease := response.Lease.watchLease()
	if lease.Group != name || lease.Subscriber != subscriber {
		return WatchResult{}, ErrInvalidResponse
	}
	result := WatchResult{Lease: lease, Found: response.Found}
	if response.Found {
		result.Current = response.Current.serviceSet()
	}
	return cloneWatchResult(result), nil
}

// RenewWatch extends one current Watch lease without changing its identity.
func (c *Client) RenewWatch(ctx context.Context, lease WatchLease) (WatchLease, error) {
	if !validWatchLease(lease) {
		return WatchLease{}, ErrInvalidWatch
	}
	value, err := c.caller.Call(ctx, c.target, commandRenewServiceGroupWatch, renewServiceGroupWatchRequest{
		Lease: newWireWatchLease(lease),
	})
	if err != nil {
		return WatchLease{}, err
	}
	response, ok := value.(watchLeaseResponse)
	if !ok {
		return WatchLease{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return WatchLease{}, err
	}
	if !validWireWatchLease(response.Lease) {
		return WatchLease{}, ErrInvalidResponse
	}
	renewed := response.Lease.watchLease()
	if renewed.Group != lease.Group ||
		renewed.Subscriber != lease.Subscriber ||
		renewed.AuthorityEpoch != lease.AuthorityEpoch ||
		renewed.Generation != lease.Generation ||
		renewed.ExpiresAt.Before(lease.ExpiresAt) {
		return WatchLease{}, ErrInvalidResponse
	}
	return renewed, nil
}

// Unwatch removes one exact current Watch lease.
func (c *Client) Unwatch(ctx context.Context, lease WatchLease) error {
	if !validWatchLease(lease) {
		return ErrInvalidWatch
	}
	value, err := c.caller.Call(ctx, c.target, commandUnwatchServiceGroup, unwatchServiceGroupRequest{
		Lease: newWireWatchLease(lease),
	})
	if err != nil {
		return err
	}
	response, ok := value.(emptyResponse)
	if !ok {
		return ErrInvalidResponse
	}
	return errorFromCode(response.Error)
}

func serviceSetFromResponse(value any, name GroupName) (ServiceSet, error) {
	response, ok := value.(serviceSetResponse)
	if !ok {
		return ServiceSet{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return ServiceSet{}, err
	}
	if !validWireServiceSet(response.Set) || response.Set.Name != name {
		return ServiceSet{}, ErrInvalidResponse
	}
	return cloneServiceSet(response.Set.serviceSet()), nil
}
