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
)
