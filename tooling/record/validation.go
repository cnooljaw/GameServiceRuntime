package record

import (
	"math"
	"reflect"
	"strings"
	"unicode/utf8"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const maxStableKeyBytes = 384

func validateKey(key StableKey) error {
	if key == "" || !utf8.ValidString(string(key)) || strings.TrimSpace(string(key)) != string(key) || len(key) > maxStableKeyBytes {
		return ErrInvalidKey
	}
	return nil
}

func validateTarget(target gsr.ServiceRef) error {
	if target.Node == "" || target.ID == 0 {
		return ErrInvalidTarget
	}
	return nil
}

func validateEntry(entry RecordEntry) error {
	if entry.FormatVersion != FormatVersion {
		return ErrInvalidEntry
	}
	if err := validateKey(entry.TargetKey); err != nil {
		return ErrInvalidEntry
	}
	if err := validateTarget(entry.Target); err != nil || entry.Sequence == 0 || entry.RecordedAt.IsZero() || entry.Command == 0 || entry.Payload == nil {
		return ErrInvalidEntry
	}
	return nil
}

func validateBundle(bundle RecordBundle) error {
	if bundle.FormatVersion != FormatVersion {
		return ErrInvalidBundle
	}
	if err := validateKey(bundle.TargetKey); err != nil {
		return ErrInvalidBundle
	}
	for index, entry := range bundle.Entries {
		if err := validateEntry(entry); err != nil || entry.TargetKey != bundle.TargetKey || entry.Sequence != Sequence(index+1) {
			return ErrInvalidBundle
		}
	}
	return nil
}

func nextSequence(sequence Sequence) (Sequence, error) {
	if sequence == Sequence(math.MaxUint64) {
		return 0, ErrSequenceExhausted
	}
	return sequence + 1, nil
}

func cloneEntry(entry RecordEntry) RecordEntry {
	entry.Payload = append([]byte(nil), entry.Payload...)
	return entry
}

func cloneEntries(entries []RecordEntry) []RecordEntry {
	result := make([]RecordEntry, len(entries))
	for index, entry := range entries {
		result[index] = cloneEntry(entry)
	}
	return result
}

func cloneBundle(bundle RecordBundle) RecordBundle {
	bundle.InitialState = append([]byte(nil), bundle.InitialState...)
	bundle.Entries = cloneEntries(bundle.Entries)
	return bundle
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
