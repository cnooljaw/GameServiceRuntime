package record

import "errors"

var (
	// ErrInvalidConfig indicates an invalid dependency or configuration value.
	ErrInvalidConfig = errors.New("record: invalid config")
	// ErrInvalidContext indicates an operation has no usable context.
	ErrInvalidContext = errors.New("record: invalid context")
	// ErrInvalidKey indicates a stable target key is malformed.
	ErrInvalidKey = errors.New("record: invalid key")
	// ErrInvalidTarget indicates a target is not a concrete ServiceRef.
	ErrInvalidTarget = errors.New("record: invalid target")
	// ErrInvalidEntry indicates RecordEntry metadata or payload is malformed.
	ErrInvalidEntry = errors.New("record: invalid entry")
	// ErrInvalidBundle indicates RecordBundle metadata or sequence continuity is malformed.
	ErrInvalidBundle = errors.New("record: invalid bundle")
	// ErrInvalidResponse indicates a Recorder reply has an unexpected shape.
	ErrInvalidResponse = errors.New("record: invalid response")
	// ErrRecordNotFound indicates no retained record exists for a stable target key.
	ErrRecordNotFound = errors.New("record: not found")
	// ErrSequenceConflict indicates an appended entry did not continue the target sequence.
	ErrSequenceConflict = errors.New("record: sequence conflict")
	// ErrSequenceExhausted indicates no next sequence value can be allocated.
	ErrSequenceExhausted = errors.New("record: sequence exhausted")
	// ErrCodecEncode classifies a business CommandCodec encoding failure.
	ErrCodecEncode = errors.New("record: codec encode")
	// ErrCodecDecode classifies a business CommandCodec decoding failure.
	ErrCodecDecode = errors.New("record: codec decode")
	// ErrRedaction classifies a Redactor failure.
	ErrRedaction = errors.New("record: redaction")
	// ErrReplayTarget indicates a TargetFactory returned an unusable isolated target.
	ErrReplayTarget = errors.New("record: invalid replay target")
	// ErrUnsupportedCommand indicates a Cluster Codec cannot encode a Command.
	ErrUnsupportedCommand = errors.New("record: unsupported command")
)
