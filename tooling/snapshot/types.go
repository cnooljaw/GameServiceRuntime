package snapshot

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	// CaptureCommand asks a Service to return one in-memory State through its Mailbox.
	CaptureCommand         gsr.CommandID = 0x02000201
	defaultMaxPayloadBytes               = 1 << 20
)

// Key identifies business state owned across Service instance changes.
type Key struct {
	Namespace string
	ID        string
}

// State contains one versioned business-state revision.
type State struct {
	Schema   string
	Version  uint32
	Revision uint64
	Payload  []byte
}

// Snapshot is one persisted State associated with its capture source and time.
type Snapshot struct {
	Key        Key
	Source     gsr.ServiceRef
	CapturedAt time.Time
	State      State
}

// CaptureRequest asks a Service to capture the state owned by Key.
type CaptureRequest struct {
	Key Key
}

// CaptureResponse returns the owner Key and its captured State.
type CaptureResponse struct {
	Key   Key
	State State
}

// Store persists Snapshots by stable business Key and returns the retained canonical value.
type Store interface {
	Save(context.Context, Snapshot) (Snapshot, error)
	Load(context.Context, Key) (Snapshot, error)
}

// CommandCaller is the narrow Runtime capability required by Manager.
type CommandCaller interface {
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

// Config configures Snapshot validation and capture time.
type Config struct {
	MaxPayloadBytes int
	Now             func() time.Time
}
