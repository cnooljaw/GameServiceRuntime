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
