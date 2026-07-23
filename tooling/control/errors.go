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
	// ErrUnauthorized reports an Agent request from a non-ControlService source.
	ErrUnauthorized = errors.New("control: unauthorized")
	// ErrInvalidResponse reports malformed Control Plane domain responses.
	ErrInvalidResponse = errors.New("control: invalid response")
	// ErrUnsupportedCommand reports a payload not owned by the Control Plane codec.
	ErrUnsupportedCommand = errors.New("control: unsupported command")
)
