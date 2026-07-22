package supervisor

import (
	"context"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// Client provides typed registration and status access to one Supervisor Service.
type Client struct {
	caller CommandCaller
	target gsr.ServiceRef
}

// NewClient binds a typed Client to a local Supervisor ServiceRef.
func NewClient(caller CommandCaller, target gsr.ServiceRef) (*Client, error) {
	if isNil(caller) || validateConcreteRef(target) != nil {
		return nil, ErrInvalidConfig
	}
	return &Client{caller: caller, target: target}, nil
}

// Register adds one initial committed Service generation before business traffic is published.
func (c *Client) Register(ctx context.Context, registration Registration) error {
	if isNil(ctx) {
		return ErrInvalidContext
	}
	if err := validateRegistration(registration); err != nil {
		return err
	}
	value, err := c.caller.Call(ctx, c.target, registerCommand, registerRequest{Registration: registration})
	if err != nil {
		return err
	}
	response, ok := value.(operationResponse)
	if !ok {
		return ErrInvalidResponse
	}
	return errorFromResponse(response.Error)
}

// Get returns an independent status record for key.
func (c *Client) Get(ctx context.Context, key ServiceKey) (Record, error) {
	if isNil(ctx) {
		return Record{}, ErrInvalidContext
	}
	if err := validateServiceKey(key); err != nil {
		return Record{}, err
	}
	value, err := c.caller.Call(ctx, c.target, getCommand, getRequest{Key: key})
	if err != nil {
		return Record{}, err
	}
	response, ok := value.(recordResponse)
	if !ok {
		return Record{}, ErrInvalidResponse
	}
	if err := errorFromResponse(response.Error); err != nil {
		return Record{}, err
	}
	if err := validateRecord(response.Record, key); err != nil {
		return Record{}, ErrInvalidResponse
	}
	return response.Record, nil
}
