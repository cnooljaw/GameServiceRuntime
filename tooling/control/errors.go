package control

import "errors"

var (
	// ErrInvalidConfig reports an invalid Control Plane configuration or target.
	ErrInvalidConfig = errors.New("control: invalid config")
	// ErrInvalidCaller reports a nil CommandCaller dependency.
	ErrInvalidCaller = errors.New("control: invalid caller")
	// ErrInvalidNode reports a malformed node identifier or request.
	ErrInvalidNode = errors.New("control: invalid node")
	// ErrNodeNotFound reports a node absent from static NodeConfig.
	ErrNodeNotFound = errors.New("control: node not found")
	// ErrNodeDisabled reports a refresh requested for a disabled node.
	ErrNodeDisabled = errors.New("control: node disabled")
	// ErrUnauthorized reports a Control Plane request from an untrusted source or principal.
	ErrUnauthorized = errors.New("control: unauthorized")
	// ErrInvalidPrincipal reports a malformed authenticated control-plane principal.
	ErrInvalidPrincipal = errors.New("control: invalid principal")
	// ErrInvalidRequestID reports a malformed idempotency key.
	ErrInvalidRequestID = errors.New("control: invalid request id")
	// ErrInvalidDrainRequest reports an invalid Drain operation input.
	ErrInvalidDrainRequest = errors.New("control: invalid drain request")
	// ErrRequestConflict reports a reused RequestID with different immutable inputs.
	ErrRequestConflict = errors.New("control: request conflict")
	// ErrDrainOperationNotFound reports an unknown Drain operation RequestID.
	ErrDrainOperationNotFound = errors.New("control: drain operation not found")
	// ErrOperationOwnerMismatch reports an operation read or resolve by another principal.
	ErrOperationOwnerMismatch = errors.New("control: drain operation owner mismatch")
	// ErrInvalidResponse reports malformed Control Plane domain responses.
	ErrInvalidResponse = errors.New("control: invalid response")
	// ErrUnsupportedCommand reports a payload not owned by the Control Plane codec.
	ErrUnsupportedCommand = errors.New("control: unsupported command")
	// ErrNodeStopQueueFull reports that the bounded Node Stop runner queue has no capacity.
	ErrNodeStopQueueFull = errors.New("control: node stop queue full")
	// ErrNodeStopRunnerClosed reports a submission after its composition-root-owned runner was closed.
	ErrNodeStopRunnerClosed = errors.New("control: node stop runner closed")
	// ErrInvalidStopRequest reports malformed Node Stop input or result facts.
	ErrInvalidStopRequest = errors.New("control: invalid stop request")
	// ErrStopOperationNotFound reports an unknown Stop operation or local Node Stop receipt.
	ErrStopOperationNotFound = errors.New("control: stop operation not found")
	// ErrStopDisabled reports a NodeAgent without its paired Stop configuration.
	ErrStopDisabled = errors.New("control: node stop disabled")
	// ErrStopRequestConflict reports a reused Stop RequestID with different immutable inputs.
	ErrStopRequestConflict = errors.New("control: stop request conflict")
	// ErrStopNotReady reports a Drain operation that is not owned and ReadyToStop for a Stop request.
	ErrStopNotReady = errors.New("control: stop not ready")
	// ErrStopTargetMismatch reports a Stop target set that does not exactly match the drained targets.
	ErrStopTargetMismatch = errors.New("control: stop target mismatch")
	// ErrInvalidRecoveryRequest reports malformed manual recovery input or result facts.
	ErrInvalidRecoveryRequest = errors.New("control: invalid recovery request")
	// ErrRecoveryOperationNotFound reports an unknown manual recovery operation or local receipt.
	ErrRecoveryOperationNotFound = errors.New("control: recovery operation not found")
	// ErrRecoveryRequestConflict reports a reused recovery RequestID with different immutable inputs.
	ErrRecoveryRequestConflict = errors.New("control: recovery request conflict")
	// ErrRecoveryNotReady reports an operation that is not ready for the requested recovery transition.
	ErrRecoveryNotReady = errors.New("control: recovery not ready")
	// ErrRecoveryQueueFull reports that the bounded Recovery runner queue has no capacity.
	ErrRecoveryQueueFull = errors.New("control: recovery queue full")
	// ErrRecoveryRunnerClosed reports a submission after its composition-root-owned runner was closed.
	ErrRecoveryRunnerClosed = errors.New("control: recovery runner closed")
)
