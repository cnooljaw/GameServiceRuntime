package drain

import (
	"context"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// NewClient binds a typed Visitor Registry Client to a known VisitorRegistryService.
func NewClient(caller CommandCaller, target gsr.ServiceRef) (*Client, error) {
	if isNil(caller) {
		return nil, ErrInvalidCaller
	}
	if !validServiceRef(target) {
		return nil, ErrInvalidConfig
	}
	return &Client{caller: caller, target: target}, nil
}

// Acquire creates a new generation for one Visitor-owned target lease.
func (c *Client) Acquire(ctx context.Context, target, visitor gsr.ServiceRef, weak bool) (VisitorLease, error) {
	if !validServiceRef(target) {
		return VisitorLease{}, ErrInvalidTarget
	}
	if !validServiceRef(visitor) {
		return VisitorLease{}, ErrInvalidVisitor
	}
	value, err := c.caller.Call(ctx, c.target, commandAcquireVisitorLease, acquireVisitorLeaseRequest{
		Target:  newWireServiceRef(target),
		Visitor: newWireServiceRef(visitor),
		Weak:    weak,
	})
	if err != nil {
		return VisitorLease{}, err
	}
	response, ok := value.(leaseResponse)
	if !ok {
		return VisitorLease{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return VisitorLease{}, err
	}
	if !validWireLease(response.Lease) {
		return VisitorLease{}, ErrInvalidResponse
	}
	lease := response.Lease.visitorLease()
	if lease.Target != target || lease.Visitor != visitor || lease.Weak != weak {
		return VisitorLease{}, ErrInvalidResponse
	}
	return cloneLease(lease), nil
}

// Renew extends one exact current Visitor lease without changing its identity.
func (c *Client) Renew(ctx context.Context, lease VisitorLease) (VisitorLease, error) {
	if !validLease(lease) {
		return VisitorLease{}, ErrInvalidLease
	}
	value, err := c.caller.Call(ctx, c.target, commandRenewVisitorLease, renewVisitorLeaseRequest{
		Lease: newWireVisitorLease(lease),
	})
	if err != nil {
		return VisitorLease{}, err
	}
	response, ok := value.(leaseResponse)
	if !ok {
		return VisitorLease{}, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return VisitorLease{}, err
	}
	if !validWireLease(response.Lease) {
		return VisitorLease{}, ErrInvalidResponse
	}
	renewed := response.Lease.visitorLease()
	if renewed.Target != lease.Target ||
		renewed.Visitor != lease.Visitor ||
		renewed.AuthorityEpoch != lease.AuthorityEpoch ||
		renewed.Generation != lease.Generation ||
		renewed.Weak != lease.Weak ||
		!renewed.ExpiresAt.After(lease.ExpiresAt) {
		return VisitorLease{}, ErrInvalidResponse
	}
	return cloneLease(renewed), nil
}

// Release removes one exact current Visitor lease.
func (c *Client) Release(ctx context.Context, lease VisitorLease) error {
	if !validLease(lease) {
		return ErrInvalidLease
	}
	value, err := c.caller.Call(ctx, c.target, commandReleaseVisitorLease, releaseVisitorLeaseRequest{
		Lease: newWireVisitorLease(lease),
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

// List returns independent active Visitor facts for target.
func (c *Client) List(ctx context.Context, target gsr.ServiceRef) ([]VisitorRef, error) {
	if !validServiceRef(target) {
		return nil, ErrInvalidTarget
	}
	value, err := c.caller.Call(ctx, c.target, commandListVisitors, listVisitorsRequest{
		Target: newWireServiceRef(target),
	})
	if err != nil {
		return nil, err
	}
	response, ok := value.(listVisitorsResponse)
	if !ok {
		return nil, ErrInvalidResponse
	}
	if err := errorFromCode(response.Error); err != nil {
		return nil, err
	}
	if !validWireVisitorRefs(response.Visitors) {
		return nil, ErrInvalidResponse
	}
	visitors := make([]VisitorRef, len(response.Visitors))
	for index, visitor := range response.Visitors {
		visitors[index] = visitor.visitorRef()
	}
	return cloneVisitorRefs(visitors), nil
}
