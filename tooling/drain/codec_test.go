package drain

import (
	"errors"
	"reflect"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestCodecEncodesVisitorPayloadAndDelegatesFallback(t *testing.T) {
	fallback := &recordingDrainCodec{}
	codec := NewCodec(fallback)
	request := acquireVisitorLeaseRequest{
		Target:  wireServiceRef{Node: "node-a", ID: 1},
		Visitor: wireServiceRef{Node: "node-b", ID: 2},
		Weak:    true,
	}
	payload, err := codec.Encode(commandAcquireVisitorLease, false, request)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := codec.Decode(commandAcquireVisitorLease, false, payload)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, request) {
		t.Fatalf("Decode() = %#v, want %#v", decoded, request)
	}
	if _, err := codec.Encode(99, false, "fallback"); err != nil {
		t.Fatalf("Encode(fallback) error = %v", err)
	}
	if fallback.encoded != 1 {
		t.Fatalf("fallback Encode calls = %d, want 1", fallback.encoded)
	}
}

func TestCodecRejectsPrivateMalformedAndInvalidSuccessfulPayloads(t *testing.T) {
	codec := NewCodec(nil)
	if _, err := codec.Encode(commandAcquireVisitorLease, false, renewVisitorLeaseRequest{}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Encode(wrong type) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(commandListVisitors, false, []byte("{\"target\":{\"node\":\"node-a\",\"id\":1}} {}")); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(trailing JSON) error = %v, want ErrInvalidResponse", err)
	}
	invalidLeaseResponse := "{\"lease\":{\"target\":{\"node\":\"node-a\",\"id\":1},\"visitor\":{\"node\":\"node-b\",\"id\":0},\"authority_epoch\":1,\"generation\":1,\"weak\":false,\"expires_at\":\"2026-07-23T12:00:00Z\"},\"error\":\"\"}"
	if _, err := codec.Decode(commandAcquireVisitorLease, true, []byte(invalidLeaseResponse)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(invalid success response) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(commandListVisitors, true, []byte("{\"visitors\":null,\"error\":\"\"}")); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(nil visitor slice) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(commandReleaseVisitorLease, true, []byte("{\"error\":\"unknown\"}")); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(unknown error code) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Encode(commandSweepVisitors, false, sweepVisitorsRequest{}); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode(private command) error = %v, want ErrUnsupportedCommand", err)
	}
	if _, err := codec.Encode(99, false, nil); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode(unsupported) error = %v, want ErrUnsupportedCommand", err)
	}
}

func TestCodecEncodesValidLeaseAndListResponses(t *testing.T) {
	codec := NewCodec(nil)
	lease := wireVisitorLease{
		Target:         wireServiceRef{Node: "node-a", ID: 1},
		Visitor:        wireServiceRef{Node: "node-b", ID: 2},
		AuthorityEpoch: 3,
		Generation:     4,
		ExpiresAt:      time.Now().Add(time.Minute),
	}
	response := leaseResponse{Lease: lease}
	payload, err := codec.Encode(commandRenewVisitorLease, true, response)
	if err != nil {
		t.Fatalf("Encode(lease response) error = %v", err)
	}
	decoded, err := codec.Decode(commandRenewVisitorLease, true, payload)
	if err != nil {
		t.Fatalf("Decode(lease response) error = %v", err)
	}
	got, ok := decoded.(leaseResponse)
	if !ok || !got.Lease.ExpiresAt.Equal(response.Lease.ExpiresAt) {
		t.Fatalf("Decode() = %#v, want %#v", decoded, response)
	}

	list := listVisitorsResponse{Visitors: []wireVisitorRef{{
		Visitor:    wireServiceRef{Node: "node-b", ID: 2},
		Generation: 4,
		ExpiresAt:  lease.ExpiresAt,
	}}}
	payload, err = codec.Encode(commandListVisitors, true, list)
	if err != nil {
		t.Fatalf("Encode(list response) error = %v", err)
	}
	if _, err := codec.Decode(commandListVisitors, true, payload); err != nil {
		t.Fatalf("Decode(list response) error = %v", err)
	}
}

func TestCodecEncodesGuardPayloadAndRejectsInvalidGuardResponse(t *testing.T) {
	fallback := &recordingDrainCodec{}
	codec := NewCodec(fallback)
	payload, err := codec.Encode(BeginDrainCommand, false, beginDrainRequest{})
	if err != nil {
		t.Fatalf("Encode(BeginDrain) error = %v", err)
	}
	decoded, err := codec.Decode(BeginDrainCommand, false, payload)
	if err != nil {
		t.Fatalf("Decode(BeginDrain) error = %v", err)
	}
	if _, ok := decoded.(beginDrainRequest); !ok {
		t.Fatalf("Decode(BeginDrain) = %T, want beginDrainRequest", decoded)
	}
	if _, err := codec.Encode(BeginDrainCommand, false, getDrainStatusRequest{}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Encode(BeginDrain wrong payload) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(GetDrainStatusCommand, false, []byte(`{} {}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(Status trailing JSON) error = %v, want ErrInvalidResponse", err)
	}

	response := drainStatusResponse{Status: newWireDrainStatus(DrainStatus{
		Draining:  true,
		StartedAt: time.Date(2026, 7, 23, 13, 0, 0, 0, time.UTC),
	})}
	payload, err = codec.Encode(GetDrainStatusCommand, true, response)
	if err != nil {
		t.Fatalf("Encode(Status response) error = %v", err)
	}
	decoded, err = codec.Decode(GetDrainStatusCommand, true, payload)
	if !reflect.DeepEqual(decoded, response) {
		t.Fatalf("Decode(Status response) = %#v, want %#v", decoded, response)
	}

	if _, err := codec.Decode(BeginDrainCommand, true, []byte(`{"status":{"draining":true,"started_at":"0001-01-01T00:00:00Z"},"error":""}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(invalid draining status) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(GetDrainStatusCommand, true, []byte(`{"status":{"draining":false,"started_at":"0001-01-01T00:00:00Z"},"error":"unknown"}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(unknown guard code) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Encode(99, false, "fallback"); err != nil || fallback.encoded != 1 {
		t.Fatalf("Encode(fallback) = %v, calls=%d", err, fallback.encoded)
	}
}

type recordingDrainCodec struct {
	encoded int
}

func (c *recordingDrainCodec) Encode(gsr.CommandID, bool, any) ([]byte, error) {
	c.encoded++
	return []byte("fallback"), nil
}

func (*recordingDrainCodec) Decode(gsr.CommandID, bool, []byte) (any, error) {
	return "fallback", nil
}
