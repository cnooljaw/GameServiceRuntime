package record

import (
	"context"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type client struct{ caller CommandCaller }

// NewClient creates typed RecorderService access through a narrow Runtime caller.
func NewClient(caller CommandCaller, _ gsr.ServiceRef) (Client, error) {
	if isNil(caller) {
		return nil, ErrInvalidConfig
	}
	return &client{caller: caller}, nil
}

func (c *client) Append(ctx context.Context, target gsr.ServiceRef, entry RecordEntry) error {
	if err := usableContext(ctx); err != nil {
		return err
	}
	if err := validateTarget(target); err != nil {
		return err
	}
	if err := validateEntry(entry); err != nil {
		return err
	}
	value, err := c.caller.Call(ctx, target, AppendRecordCommand, appendRecordRequest{Entry: cloneEntry(entry)})
	if err != nil {
		return err
	}
	response, ok := value.(emptyResponse)
	if !ok {
		return ErrInvalidResponse
	}
	return errorFromResponseCode(response.Error)
}

func (c *client) List(ctx context.Context, target gsr.ServiceRef, key StableKey, after Sequence, limit int) ([]RecordEntry, error) {
	if err := usableContext(ctx); err != nil {
		return nil, err
	}
	if err := validateTarget(target); err != nil {
		return nil, err
	}
	if err := validateKey(key); err != nil || limit <= 0 {
		return nil, ErrInvalidEntry
	}
	value, err := c.caller.Call(ctx, target, ListRecordsCommand, listRecordsRequest{Key: key, After: after, Limit: limit})
	if err != nil {
		return nil, err
	}
	response, ok := value.(listRecordsResponse)
	if !ok {
		return nil, ErrInvalidResponse
	}
	if err := errorFromResponseCode(response.Error); err != nil {
		return nil, err
	}
	for _, entry := range response.Entries {
		if err := validateEntry(entry); err != nil {
			return nil, ErrInvalidResponse
		}
	}
	return cloneEntries(response.Entries), nil
}

func (c *client) Clear(ctx context.Context, target gsr.ServiceRef, key StableKey) error {
	if err := usableContext(ctx); err != nil {
		return err
	}
	if err := validateTarget(target); err != nil {
		return err
	}
	if err := validateKey(key); err != nil {
		return err
	}
	value, err := c.caller.Call(ctx, target, ClearRecordsCommand, clearRecordsRequest{Key: key})
	if err != nil {
		return err
	}
	response, ok := value.(emptyResponse)
	if !ok {
		return ErrInvalidResponse
	}
	return errorFromResponseCode(response.Error)
}

func usableContext(ctx context.Context) error {
	if isNil(ctx) {
		return ErrInvalidContext
	}
	return context.Cause(ctx)
}
