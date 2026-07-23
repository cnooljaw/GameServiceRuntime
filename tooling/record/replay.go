package record

import (
	"context"
	"fmt"
)

// Replay decodes a validated bundle and sends its inputs to a newly created isolated target.
func Replay(ctx context.Context, bundle RecordBundle, codec CommandCodec, factory TargetFactory) error {
	if err := usableContext(ctx); err != nil {
		return err
	}
	if isNil(codec) || isNil(factory) {
		return ErrInvalidConfig
	}
	if err := validateBundle(bundle); err != nil {
		return err
	}
	target, err := factory(ctx, cloneBundle(bundle))
	if err != nil {
		return err
	}
	if isNil(target.Runtime) {
		return ErrReplayTarget
	}
	if err := validateTarget(target.Ref); err != nil {
		return ErrReplayTarget
	}
	for _, entry := range bundle.Entries {
		payload, err := codec.Decode(entry.Command, append([]byte(nil), entry.Payload...))
		if err != nil {
			return fmt.Errorf("%w: %w", ErrCodecDecode, err)
		}
		if err := target.Runtime.Send(target.Ref, entry.Command, payload); err != nil {
			return err
		}
	}
	return nil
}
