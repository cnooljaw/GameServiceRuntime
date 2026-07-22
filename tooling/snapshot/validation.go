package snapshot

import (
	"bytes"
	"reflect"
	"strings"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	maxNamespaceBytes = 128
	maxKeyIDBytes     = 256
	maxSchemaBytes    = 128
)

func validateKey(key Key) error {
	if !validTrimmedText(key.Namespace, maxNamespaceBytes) || !validTrimmedText(key.ID, maxKeyIDBytes) {
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

func validateState(state State, maxPayloadBytes int) error {
	if !validTrimmedText(state.Schema, maxSchemaBytes) || state.Version == 0 || state.Revision == 0 || state.Payload == nil {
		return ErrInvalidState
	}
	if maxPayloadBytes > 0 && len(state.Payload) > maxPayloadBytes {
		return ErrPayloadTooLarge
	}
	return nil
}

func validateSnapshot(snapshot Snapshot, maxPayloadBytes int) error {
	if err := validateKey(snapshot.Key); err != nil {
		return err
	}
	if err := validateTarget(snapshot.Source); err != nil {
		return err
	}
	if err := validateState(snapshot.State, maxPayloadBytes); err != nil {
		return err
	}
	if snapshot.CapturedAt.IsZero() {
		return ErrInvalidState
	}
	return nil
}

func validTrimmedText(value string, maxBytes int) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= maxBytes
}

func cloneState(state State) State {
	state.Payload = append([]byte(nil), state.Payload...)
	if state.Payload == nil {
		state.Payload = []byte{}
	}
	return state
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.State = cloneState(snapshot.State)
	return snapshot
}

func equalState(left, right State) bool {
	return left.Schema == right.Schema &&
		left.Version == right.Version &&
		left.Revision == right.Revision &&
		bytes.Equal(left.Payload, right.Payload)
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
