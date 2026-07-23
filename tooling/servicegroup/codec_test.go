package servicegroup

import (
	"errors"
	"testing"

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

type recordingCodec struct{ encoded int }

func (c *recordingCodec) Encode(gsr.CommandID, bool, any) ([]byte, error) {
	c.encoded++
	return []byte("fallback"), nil
}

func (*recordingCodec) Decode(gsr.CommandID, bool, []byte) (any, error) {
	return "fallback", nil
}
