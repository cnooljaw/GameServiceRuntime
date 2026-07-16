package gsr

import (
	"errors"
	"testing"
)

func TestStableRuntimeErrorsRoundTripAcrossCluster(t *testing.T) {
	tests := []error{
		ErrTimeout,
		ErrReplyTwice,
		ErrServiceNotFound,
		ErrServiceClosed,
		ErrMailboxFull,
		ErrInvalidServiceSpec,
		ErrRuntimeClosed,
		ErrReplyUnavailable,
		ErrReplyExpired,
		ErrCommandNotRegistered,
		ErrCommandAlreadyRegistered,
		ErrServiceNameConflict,
		ErrCallCycle,
		ErrCallNotAllowed,
		ErrServiceFailed,
		ErrStopTimeout,
		ErrCloseTimeout,
		ErrInvalidClusterConfig,
		ErrClusterStart,
		ErrRemoteUnavailable,
		ErrInvalidClusterEnvelope,
		ErrPayloadEncode,
		ErrPayloadDecode,
	}
	for _, want := range tests {
		code, message := encodeRemoteError(want)
		if got := decodeRemoteError(code, message); !errors.Is(got, want) {
			t.Errorf("error %q round trip = %T %v", want, got, got)
		}
	}
}
