// Package record records Service Command inputs and replays them into isolated targets.
package record

import (
	"context"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// FormatVersion is the only RecordBundle and RecordEntry format accepted by this release.
const FormatVersion uint16 = 1

const (
	// AppendRecordCommand appends one encoded input to RecorderService.
	AppendRecordCommand gsr.CommandID = 0x02800101
	// ListRecordsCommand lists retained RecorderService inputs after an exclusive cursor.
	ListRecordsCommand gsr.CommandID = 0x02800102
	// ClearRecordsCommand removes retained RecorderService inputs for one stable key.
	ClearRecordsCommand gsr.CommandID = 0x02800103
)

// StableKey identifies business state across Service instance replacements.
type StableKey string

// Sequence identifies one input's order within a stable target's Mailbox history.
type Sequence uint64

// RecordEntry is one immutable encoded Command that entered a decorated Service.
type RecordEntry struct {
	FormatVersion uint16
	TargetKey     StableKey
	Target        gsr.ServiceRef
	Source        gsr.ServiceRef
	Sequence      Sequence
	RecordedAt    time.Time
	Command       gsr.CommandID
	Payload       []byte
	TraceID       string
}

// RecordBundle groups a target's replayable input window and optional business initial state.
type RecordBundle struct {
	FormatVersion uint16
	TargetKey     StableKey
	InitialState  []byte
	Entries       []RecordEntry
}

// CommandCodec encodes and decodes business Command payloads without retaining Runtime state.
type CommandCodec interface {
	Encode(gsr.CommandID, any) ([]byte, error)
	Decode(gsr.CommandID, []byte) (any, error)
}

// Redactor removes or replaces sensitive fields from an already encoded Command payload.
type Redactor interface {
	Redact(gsr.CommandID, []byte) ([]byte, error)
}

// RecorderConfig configures RecorderService's bounded per-key window and recording clock.
type RecorderConfig struct {
	MaxEntries int
}

// CommandCaller is the narrow Runtime capability used by Client.
type CommandCaller interface {
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

// Client gives typed access to one RecorderService target.
type Client interface {
	Append(context.Context, gsr.ServiceRef, RecordEntry) error
	List(context.Context, gsr.ServiceRef, StableKey, Sequence, int) ([]RecordEntry, error)
	Clear(context.Context, gsr.ServiceRef, StableKey) error
}

// Archive persists complete RecordBundles outside any Service Handler.
type Archive interface {
	Save(context.Context, RecordBundle) error
	Load(context.Context, StableKey) (RecordBundle, error)
}

// ReplayRuntime is the only capability a replay runner needs from an isolated Runtime.
type ReplayRuntime interface {
	Send(gsr.ServiceRef, gsr.CommandID, any) error
}

// ReplayTarget identifies the isolated Runtime and target Service created for one replay.
type ReplayTarget struct {
	Runtime ReplayRuntime
	Ref     gsr.ServiceRef
}

// TargetFactory creates a new isolated target for one validated RecordBundle.
type TargetFactory func(context.Context, RecordBundle) (ReplayTarget, error)
