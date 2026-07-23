package drain

import (
	"context"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// NewGuardClient binds a typed Guard Client to one decorated Service.
func NewGuardClient(caller CommandCaller, target gsr.ServiceRef) (*GuardClient, error) {
	if isNil(caller) {
		return nil, ErrInvalidCaller
	}
	if !validServiceRef(target) {
		return nil, ErrInvalidGuard
	}
	return &GuardClient{caller: caller, target: target}, nil
}

// Begin asks the configured controller Service to begin an irreversible Drain.
func (c *GuardClient) Begin(ctx context.Context) (DrainStatus, error) {
	value, err := c.caller.Call(ctx, c.target, BeginDrainCommand, beginDrainRequest{})
	if err != nil {
		return DrainStatus{}, err
	}
	return guardStatusFromResponse(value)
}

// Status returns one independent Drain Guard state snapshot.
func (c *GuardClient) Status(ctx context.Context) (DrainStatus, error) {
	value, err := c.caller.Call(ctx, c.target, GetDrainStatusCommand, getDrainStatusRequest{})
	if err != nil {
		return DrainStatus{}, err
	}
	return guardStatusFromResponse(value)
}

func guardStatusFromResponse(value any) (DrainStatus, error) {
	response, ok := value.(drainStatusResponse)
	if !ok {
		return DrainStatus{}, ErrInvalidResponse
	}
	if err := guardErrorFromCode(response.Error); err != nil {
		return DrainStatus{}, err
	}
	if !validWireDrainStatus(response.Status) {
		return DrainStatus{}, ErrInvalidResponse
	}
	return response.Status.drainStatus(), nil
}
