package gsr

import "errors"

var (
	// ErrTimeout indicates that a Call context expired before a Reply arrived.
	ErrTimeout = errors.New("gsr: call timed out")
	// ErrReplyTwice indicates that a Command handler tried to reply more than once.
	ErrReplyTwice = errors.New("gsr: reply already sent")
	// ErrServiceNotFound indicates that a ServiceRef was never registered.
	ErrServiceNotFound = errors.New("gsr: service not found")
	// ErrServiceClosed indicates that a ServiceRef was stopped and removed.
	ErrServiceClosed = errors.New("gsr: service closed")
	// ErrMailboxFull indicates that a Service mailbox cannot accept another message.
	ErrMailboxFull = errors.New("gsr: mailbox full")
	// ErrInvalidServiceSpec indicates that CreateService received an invalid specification.
	ErrInvalidServiceSpec = errors.New("gsr: invalid service spec")
	// ErrRuntimeClosed indicates that the Runtime is closing or closed.
	ErrRuntimeClosed = errors.New("gsr: runtime closed")
	// ErrReplyUnavailable indicates that Reply was used for a Send command.
	ErrReplyUnavailable = errors.New("gsr: reply unavailable")
	// ErrReplyExpired indicates that the PendingCall no longer exists.
	ErrReplyExpired = errors.New("gsr: reply expired")
	// ErrCommandNotRegistered indicates that a Service does not support the CommandID.
	ErrCommandNotRegistered = errors.New("gsr: command not registered")
	// ErrCommandAlreadyRegistered indicates a duplicate CommandID declaration.
	ErrCommandAlreadyRegistered = errors.New("gsr: command already registered")
	// ErrServiceNameConflict indicates that a ServiceName is already registered.
	ErrServiceNameConflict = errors.New("gsr: service name conflict")
	// ErrCallCycle indicates a synchronous self-call or detected Call cycle.
	ErrCallCycle = errors.New("gsr: synchronous call cycle")
	// ErrCallNotAllowed indicates ServiceContext.Call was used outside the serial handler path.
	ErrCallNotAllowed = errors.New("gsr: service call not allowed in this context")
	// ErrServiceFailed indicates that a Service handler panicked.
	ErrServiceFailed = errors.New("gsr: service failed")
	// ErrStopTimeout indicates that Service.Stop exceeded its deadline.
	ErrStopTimeout = errors.New("gsr: service stop timed out")
	// ErrCloseTimeout indicates that Service.Close or Runtime.Close exceeded its deadline.
	ErrCloseTimeout = errors.New("gsr: close timed out")
)
