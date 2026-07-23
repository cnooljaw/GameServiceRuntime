package servicegroup

import (
	"errors"
	"reflect"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestCodecEncodesDirectoryPayloadAndDelegatesFallback(t *testing.T) {
	fallback := &recordingCodec{}
	codec := NewCodec(fallback)
	request := publishServiceSetRequest{
		Name: "match",
		Refs: []wireServiceRef{{Node: "node-a", ID: 1}},
	}
	payload, err := codec.Encode(commandPublishServiceSet, false, request)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := codec.Decode(commandPublishServiceSet, false, payload)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	got, ok := decoded.(publishServiceSetRequest)
	if !ok || got.Name != request.Name || len(got.Refs) != 1 || got.Refs[0] != request.Refs[0] {
		t.Fatalf("Decode() = %#v, want %#v", decoded, request)
	}
	if _, err := codec.Encode(99, false, "fallback"); err != nil {
		t.Fatalf("Encode(fallback) error = %v", err)
	}
	if fallback.encoded != 1 {
		t.Fatalf("fallback Encode calls = %d, want 1", fallback.encoded)
	}
}

func TestCodecRejectsInvalidPayloadPrivateCommandAndMalformedResponse(t *testing.T) {
	codec := NewCodec(nil)
	if _, err := codec.Encode(commandGetServiceSet, false, publishServiceSetRequest{}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Encode(wrong type) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(commandGetServiceSet, false, []byte(`{"name":"match"} {}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(trailing JSON) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(commandGetServiceSet, true, []byte(`{"set":{"name":"match","version":{"authority_epoch":1,"revision":1},"refs":[{"node":"node-a","id":0}],"tags":{}},"error":""}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(invalid response) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(commandGetServiceSet, true, []byte(`{"set":{},"error":"unknown"}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(unknown error) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Encode(commandSweepExpiredWatches, false, nil); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode(private command) error = %v, want ErrUnsupportedCommand", err)
	}
	if _, err := codec.Encode(99, false, nil); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode(unsupported) error = %v, want ErrUnsupportedCommand", err)
	}
}

func TestCodecEncodesPublicServiceSetChangedPayload(t *testing.T) {
	codec := NewCodec(nil)
	change := ServiceSetChanged{Set: ServiceSet{
		Name:    "match",
		Version: ServiceSetVersion{AuthorityEpoch: 1, Revision: 2},
		Refs:    []gsr.ServiceRef{{Node: "node-a", ID: 1}},
		Tags:    map[string]string{"version": "blue"},
	}}
	payload, err := codec.Encode(ServiceSetChangedCommand, false, change)
	if err != nil {
		t.Fatalf("Encode(ServiceSetChanged) error = %v", err)
	}
	decoded, err := codec.Decode(ServiceSetChangedCommand, false, payload)
	if err != nil {
		t.Fatalf("Decode(ServiceSetChanged) error = %v", err)
	}
	got, ok := decoded.(ServiceSetChanged)
	if !ok || !reflect.DeepEqual(got, change) {
		t.Fatalf("Decode(ServiceSetChanged) = %#v, want %#v", decoded, change)
	}
	if _, err := codec.Encode(ServiceSetChangedCommand, true, change); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode(ServiceSetChanged response) error = %v, want ErrUnsupportedCommand", err)
	}
}

func TestCodecValidatesWatchResponses(t *testing.T) {
	codec := NewCodec(nil)
	lease := wireWatchLease{
		Group:          "match",
		Subscriber:     wireServiceRef{Node: "node-a", ID: 1},
		AuthorityEpoch: 2,
		Generation:     3,
		ExpiresAt:      time.Now().Add(time.Minute),
	}
	response := watchResultResponse{Lease: lease}
	payload, err := codec.Encode(commandWatchServiceGroup, true, response)
	if err != nil {
		t.Fatalf("Encode(Watch response) error = %v", err)
	}
	decoded, err := codec.Decode(commandWatchServiceGroup, true, payload)
	if err != nil {
		t.Fatalf("Decode(Watch response) error = %v", err)
	}
	got, ok := decoded.(watchResultResponse)
	if !ok || !got.Lease.ExpiresAt.Equal(response.Lease.ExpiresAt) {
		t.Fatalf("Decode(Watch response) = %#v, want %#v", decoded, response)
	}
	got.Lease.ExpiresAt = response.Lease.ExpiresAt
	if !reflect.DeepEqual(got, response) {
		t.Fatalf("Decode(Watch response) = %#v, want %#v", decoded, response)
	}

	response.Found = true
	response.Current = wireServiceSet{
		Name:    "match",
		Version: ServiceSetVersion{AuthorityEpoch: 99, Revision: 1},
		Refs:    make([]wireServiceRef, 0),
		Tags:    make(map[string]string),
	}
	payload, err = codec.Encode(commandWatchServiceGroup, true, response)
	if err != nil {
		t.Fatalf("Encode(invalid Watch response) error = %v", err)
	}
	if _, err := codec.Decode(commandWatchServiceGroup, true, payload); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(invalid Watch response) error = %v, want ErrInvalidResponse", err)
	}
}

type recordingCodec struct{ encoded int }

func (c *recordingCodec) Encode(gsr.CommandID, bool, any) ([]byte, error) {
	c.encoded++
	return []byte("fallback"), nil
}

func (*recordingCodec) Decode(gsr.CommandID, bool, []byte) (any, error) {
	return "fallback", nil
}
