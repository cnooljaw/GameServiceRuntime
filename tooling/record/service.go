package record

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type responseCode uint8

const (
	responseOK responseCode = iota
	responseInvalid
	responseNotFound
	responseSequenceConflict
)

type appendRecordRequest struct{ Entry RecordEntry }
type listRecordsRequest struct {
	Key   StableKey
	After Sequence
	Limit int
}
type clearRecordsRequest struct{ Key StableKey }
type emptyResponse struct{ Error responseCode }
type listRecordsResponse struct {
	Entries []RecordEntry
	Error   responseCode
}

// RecorderService owns bounded RecordEntry windows by StableKey.
type RecorderService struct {
	maxEntries int
	context    gsr.ServiceContext
	records    map[StableKey][]RecordEntry
	last       map[StableKey]Sequence
}

// NewRecorderService creates a RecorderService with a positive per-key entry limit.
func NewRecorderService(config RecorderConfig) (*RecorderService, error) {
	if config.MaxEntries <= 0 {
		return nil, ErrInvalidConfig
	}
	return &RecorderService{maxEntries: config.MaxEntries, records: make(map[StableKey][]RecordEntry), last: make(map[StableKey]Sequence)}, nil
}

// Commands declares RecorderService's typed Command protocol.
func (*RecorderService) Commands() []gsr.CommandID {
	return []gsr.CommandID{AppendRecordCommand, ListRecordsCommand, ClearRecordsCommand}
}

// Init stores the Runtime capability used only for metrics and clock alignment.
func (s *RecorderService) Init(serviceContext gsr.ServiceContext) error {
	if isNil(serviceContext) {
		return ErrInvalidConfig
	}
	s.context = serviceContext
	return nil
}

// Handle owns all RecordEntry mutations and reads through one Mailbox.
func (s *RecorderService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case AppendRecordCommand:
		request, ok := command.Payload.(appendRecordRequest)
		if !ok {
			return commandContext.Reply(emptyResponse{Error: responseInvalid})
		}
		return commandContext.Reply(emptyResponse{Error: responseCodeFromError(s.append(request.Entry))})
	case ListRecordsCommand:
		request, ok := command.Payload.(listRecordsRequest)
		if !ok {
			return commandContext.Reply(listRecordsResponse{Error: responseInvalid})
		}
		entries, err := s.list(request.Key, request.After, request.Limit)
		return commandContext.Reply(listRecordsResponse{Entries: entries, Error: responseCodeFromError(err)})
	case ClearRecordsCommand:
		request, ok := command.Payload.(clearRecordsRequest)
		if !ok {
			return commandContext.Reply(emptyResponse{Error: responseInvalid})
		}
		return commandContext.Reply(emptyResponse{Error: responseCodeFromError(s.clear(request.Key))})
	default:
		return gsr.ErrCommandNotRegistered
	}
}

// Stop releases retained records after Runtime serializes shutdown.
func (s *RecorderService) Stop(context.Context) error {
	s.records = make(map[StableKey][]RecordEntry)
	s.last = make(map[StableKey]Sequence)
	return nil
}

// Close discards the Runtime capability after RecorderService has stopped.
func (s *RecorderService) Close() error {
	s.records = nil
	s.last = nil
	s.context = nil
	return nil
}

func (s *RecorderService) append(entry RecordEntry) error {
	if err := validateEntry(entry); err != nil {
		return err
	}
	previous := s.last[entry.TargetKey]
	expected, err := nextSequence(previous)
	if err != nil || entry.Sequence != expected {
		return ErrSequenceConflict
	}
	entry = cloneEntry(entry)
	entries := append(s.records[entry.TargetKey], entry)
	if len(entries) > s.maxEntries {
		entries = append([]RecordEntry(nil), entries[len(entries)-s.maxEntries:]...)
		s.context.Metrics().Inc("record_evicted_total")
	}
	s.records[entry.TargetKey] = entries
	s.last[entry.TargetKey] = entry.Sequence
	s.context.Metrics().SetGauge(retainedMetric(entry.TargetKey), int64(len(entries)))
	return nil
}

func (s *RecorderService) list(key StableKey, after Sequence, limit int) ([]RecordEntry, error) {
	if err := validateKey(key); err != nil || limit <= 0 || limit > s.maxEntries {
		return nil, ErrInvalidEntry
	}
	entries := s.records[key]
	result := make([]RecordEntry, 0, limit)
	for _, entry := range entries {
		if entry.Sequence > after {
			result = append(result, cloneEntry(entry))
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (s *RecorderService) clear(key StableKey) error {
	if err := validateKey(key); err != nil {
		return err
	}
	delete(s.records, key)
	s.context.Metrics().SetGauge(retainedMetric(key), 0)
	return nil
}

func responseCodeFromError(err error) responseCode {
	switch err {
	case nil:
		return responseOK
	case ErrRecordNotFound:
		return responseNotFound
	case ErrSequenceConflict:
		return responseSequenceConflict
	default:
		return responseInvalid
	}
}

func errorFromResponseCode(code responseCode) error {
	switch code {
	case responseOK:
		return nil
	case responseNotFound:
		return ErrRecordNotFound
	case responseSequenceConflict:
		return ErrSequenceConflict
	default:
		return ErrInvalidResponse
	}
}

func retainedMetric(key StableKey) string {
	digest := sha256.Sum256([]byte(key))
	return "record_retained." + hex.EncodeToString(digest[:8])
}

func (s *RecorderService) String() string {
	return fmt.Sprintf("RecorderService(maxEntries=%d)", s.maxEntries)
}
