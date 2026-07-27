package record

import (
	"context"
	"fmt"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// Decorator records encoded input immediately before delegating a Command to its inner Service.
type Decorator struct {
	inner     gsr.Service
	recorder  gsr.ServiceRef
	targetKey StableKey
	codec     CommandCodec
	redactor  Redactor
	strict    bool
	context   gsr.ServiceContext
	sequence  Sequence
}

// NewDecorator creates a lifecycle-preserving Command recording decorator.
func NewDecorator(service gsr.Service, recorder gsr.ServiceRef, targetKey StableKey, codec CommandCodec, redactor Redactor, strict bool) (*Decorator, error) {
	if isNil(service) || isNil(codec) {
		return nil, ErrInvalidConfig
	}
	if err := validateTarget(recorder); err != nil {
		return nil, err
	}
	if err := validateKey(targetKey); err != nil {
		return nil, err
	}
	return &Decorator{inner: service, recorder: recorder, targetKey: targetKey, codec: codec, redactor: redactor, strict: strict}, nil
}

// StartupCommand forwards an optional startup Command declaration from the wrapped Service.
func (d *Decorator) StartupCommand() (gsr.Command, bool) {
	declarer, ok := d.inner.(gsr.StartupCommandDeclarer)
	if !ok {
		return gsr.Command{}, false
	}
	return declarer.StartupCommand()
}

// Init forwards initialization after capturing the Runtime context used for record delivery.
func (d *Decorator) Init(serviceContext gsr.ServiceContext) error {
	if isNil(serviceContext) {
		return ErrInvalidConfig
	}
	d.context = serviceContext
	return d.inner.Init(serviceContext)
}

// Handle records the incoming Command first, then delegates it unless strict recording fails.
func (d *Decorator) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	if err := d.record(commandContext, command); err != nil {
		if d.strict {
			return err
		}
		d.context.Metrics().Inc("record_recording_failure_total")
		d.context.Logger().Warn("record command failed", "target_key", d.targetKey, "command", command.ID, "sequence", d.sequence+1, "error", err)
	}
	return d.inner.Handle(commandContext, command)
}

// Stop forwards Runtime-serialized shutdown to the wrapped Service.
func (d *Decorator) Stop(ctx context.Context) error { return d.inner.Stop(ctx) }

// Close forwards closure and releases the captured Runtime context.
func (d *Decorator) Close() error {
	err := d.inner.Close()
	d.context = nil
	return err
}

func (d *Decorator) record(commandContext gsr.CommandContext, command gsr.Command) error {
	sequence, err := nextSequence(d.sequence)
	if err != nil {
		return err
	}
	payload, err := d.codec.Encode(command.ID, command.Payload)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCodecEncode, err)
	}
	payload = append([]byte(nil), payload...)
	if d.redactor != nil {
		payload, err = d.redactor.Redact(command.ID, payload)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrRedaction, err)
		}
		payload = append([]byte(nil), payload...)
	}
	entry := RecordEntry{FormatVersion: FormatVersion, TargetKey: d.targetKey, Target: d.context.Self(), Source: commandContext.Source(), Sequence: sequence, RecordedAt: d.context.Now(), Command: command.ID, Payload: payload}
	if err := validateEntry(entry); err != nil {
		return err
	}
	if err := d.context.Send(d.recorder, AppendRecordCommand, appendRecordRequest{Entry: cloneEntry(entry)}); err != nil {
		return err
	}
	d.sequence = sequence
	return nil
}
