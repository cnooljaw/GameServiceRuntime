package snapshot

import (
	"errors"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestCodecUsesStableWireFormat(t *testing.T) {
	codec := NewCodec(nil)
	request, err := codec.Encode(CaptureCommand, false, CaptureRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if string(request) != `{}` {
		t.Fatalf("request = %s, want {}", request)
	}

	response, err := codec.Encode(CaptureCommand, true, CaptureResponse{State: State{
		Schema: "player", Version: 1, Revision: 2, Payload: []byte("ok"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"state":{"schema":"player","version":1,"revision":2,"payload":"b2s="}}`
	if string(response) != want {
		t.Fatalf("response = %s, want %s", response, want)
	}
}

func TestCodecRoundTripsCapturePayloads(t *testing.T) {
	codec := NewCodec(nil)
	requestPayload, err := codec.Decode(CaptureCommand, false, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := requestPayload.(CaptureRequest); !ok {
		t.Fatalf("request payload = %T, want CaptureRequest", requestPayload)
	}

	want := CaptureResponse{State: State{Schema: "player", Version: 2, Revision: 7, Payload: []byte("state")}}
	wire, err := codec.Encode(CaptureCommand, true, want)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(CaptureCommand, true, wire)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := decoded.(CaptureResponse)
	if !ok {
		t.Fatalf("response payload = %T, want CaptureResponse", decoded)
	}
	if got.State.Schema != want.State.Schema || got.State.Version != want.State.Version ||
		got.State.Revision != want.State.Revision || string(got.State.Payload) != string(want.State.Payload) {
		t.Fatalf("response = %#v, want %#v", got, want)
	}
}

func TestCodecAllowsUnknownFieldsAndRejectsTrailingJSON(t *testing.T) {
	codec := NewCodec(nil)
	if _, err := codec.Decode(CaptureCommand, false, []byte(`{"future":true}`)); err != nil {
		t.Fatalf("Decode request with unknown field error = %v", err)
	}
	decoded, err := codec.Decode(CaptureCommand, true, []byte(`{"state":{"schema":"player","version":1,"revision":2,"payload":"b2s=","future":true},"future":true}`))
	if err != nil {
		t.Fatalf("Decode response with unknown fields error = %v", err)
	}
	if _, ok := decoded.(CaptureResponse); !ok {
		t.Fatalf("decoded = %T, want CaptureResponse", decoded)
	}
	if _, err := codec.Decode(CaptureCommand, false, []byte(`{} {}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode trailing JSON error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(CaptureCommand, true, []byte(`{"state":`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode malformed JSON error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(CaptureCommand, false, []byte(`null`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode null error = %v, want ErrInvalidResponse", err)
	}
}

func TestCodecRejectsWrongGoTypes(t *testing.T) {
	codec := NewCodec(nil)
	tests := []struct {
		response bool
		value    any
	}{
		{response: false, value: struct{}{}},
		{response: true, value: State{}},
	}
	for _, test := range tests {
		if _, err := codec.Encode(CaptureCommand, test.response, test.value); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("Encode response=%t value=%T error = %v, want ErrInvalidResponse", test.response, test.value, err)
		}
	}
}

func TestCodecDelegatesUnknownCommands(t *testing.T) {
	fallback := &recordingCodec{}
	codec := NewCodec(fallback)
	const other gsr.CommandID = 99
	encoded, err := codec.Encode(other, false, "request")
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "fallback" || !fallback.encodeCalled {
		t.Fatalf("encoded = %q, fallback called=%t", encoded, fallback.encodeCalled)
	}
	decoded, err := codec.Decode(other, true, []byte("reply"))
	if err != nil {
		t.Fatal(err)
	}
	if decoded != "decoded" || !fallback.decodeCalled {
		t.Fatalf("decoded = %#v, fallback called=%t", decoded, fallback.decodeCalled)
	}
}

func TestCodecRejectsUnknownCommandWithoutFallback(t *testing.T) {
	const other gsr.CommandID = 99
	codec := NewCodec(nil)
	if _, err := codec.Encode(other, false, nil); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode error = %v, want ErrUnsupportedCommand", err)
	}
	if _, err := codec.Decode(other, false, nil); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedCommand", err)
	}
	var typedNil *recordingCodec
	codec = NewCodec(typedNil)
	if _, err := codec.Encode(other, false, nil); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("typed nil fallback error = %v, want ErrUnsupportedCommand", err)
	}
}

type recordingCodec struct {
	encodeCalled bool
	decodeCalled bool
}

func (c *recordingCodec) Encode(gsr.CommandID, bool, any) ([]byte, error) {
	c.encodeCalled = true
	return []byte("fallback"), nil
}

func (c *recordingCodec) Decode(gsr.CommandID, bool, []byte) (any, error) {
	c.decodeCalled = true
	return "decoded", nil
}
