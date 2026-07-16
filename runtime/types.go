// Package gsr provides the core types for the Game Service Runtime.
package gsr

// NodeID identifies a Runtime node.
type NodeID string

// ServiceID identifies a Service instance within a node.
type ServiceID uint64

// ServiceName identifies a long-lived logical Service.
type ServiceName string

// CommandID identifies a Service command.
type CommandID uint32

// SessionID correlates a Call with its Reply.
type SessionID uint64

// TimerID identifies a scheduled timer.
type TimerID uint64

// ServiceRef is a Runtime address, not an object pointer.
type ServiceRef struct {
	Node NodeID
	ID   ServiceID
}

// Command is a Service capability invocation.
type Command struct {
	ID      CommandID
	Payload any
}

// Envelope is the Runtime's internal message representation.
type Envelope struct {
	Source  ServiceRef
	Target  ServiceRef
	Session SessionID
	Command CommandID
	Payload any
}
