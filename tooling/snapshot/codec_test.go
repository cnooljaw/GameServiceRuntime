package snapshot

import (
	"errors"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestCodecUsesStableWireFormat(t *testing.T) {
	codec := NewCodec(nil)
	key := testKey()
	request, err := codec.Encode(CaptureCommand, false, CaptureRequest{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	const wantRequest = `{"key":{"namespace":"player","id":"42"}}`
	if string(request) != wantRequest {
		t.Fatalf("request = %s, want %s", request, wantRequest)
	}

	response, err := codec.Encode(CaptureCommand, true, CaptureResponse{Key: key, State: State{
		Schema: "player", Version: 1, Revision: 2, Payload: []byte("ok"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"key":{"namespace":"player","id":"42"},"state":{"schema":"player","version":1,"revision":2,"payload":"b2s="}}`
	if string(response) != want {
		t.Fatalf("response = %s, want %s", response, want)
	}
}

func TestCodecRoundTripsCapturePayloads(t *testing.T) {
	codec := NewCodec(nil)
	requestPayload, err := codec.Decode(CaptureCommand, false, []byte(`{"key":{"namespace":"player","id":"42"}}`))
	if err != nil {
		t.Fatal(err)
	}
	request, ok := requestPayload.(CaptureRequest)
	if !ok {
		t.Fatalf("request payload = %T, want CaptureRequest", requestPayload)
	}
	if request.Key != testKey() {
		t.Fatalf("request Key = %#v, want %#v", request.Key, testKey())
	}

	want := CaptureResponse{Key: testKey(), State: State{Schema: "player", Version: 2, Revision: 7, Payload: []byte("state")}}
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
	if got.Key != want.Key || got.State.Schema != want.State.Schema || got.State.Version != want.State.Version ||
		got.State.Revision != want.State.Revision || string(got.State.Payload) != string(want.State.Payload) {
		t.Fatalf("response = %#v, want %#v", got, want)
	}
}

func TestCodecRoundTripsNonNilEmptyPayload(t *testing.T) {
	codec := NewCodec(nil)
	wire, err := codec.Encode(CaptureCommand, true, CaptureResponse{Key: testKey(), State: State{
		Schema: "player", Version: 1, Revision: 1, Payload: []byte{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(CaptureCommand, true, wire)
	if err != nil {
		t.Fatal(err)
	}
	response := decoded.(CaptureResponse)
	if response.State.Payload == nil || len(response.State.Payload) != 0 {
		t.Fatalf("Payload = %#v, want non-nil empty slice", response.State.Payload)
	}
}

func TestCodecAllowsUnknownFieldsAndRejectsTrailingJSON(t *testing.T) {
	codec := NewCodec(nil)
	if _, err := codec.Decode(CaptureCommand, false, []byte(`{"key":{"namespace":"player","id":"42"},"future":true}`)); err != nil {
		t.Fatalf("Decode request with unknown field error = %v", err)
	}
	decoded, err := codec.Decode(CaptureCommand, true, []byte(`{"key":{"namespace":"player","id":"42","future":true},"state":{"schema":"player","version":1,"revision":2,"payload":"b2s=","future":true},"future":true}`))
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

func TestCodecRejectsInvalidSnapshotFieldsAndUTF8(t *testing.T) {
	codec := NewCodec(nil)
	invalidUTF8 := []byte{'{', '"', 0xff, '"', ':', '1', '}'}
	tests := []struct {
		name     string
		response bool
		payload  []byte
	}{
		{name: "request missing key", payload: []byte(`{}`)},
		{name: "response null payload", response: true, payload: []byte(`{"key":{"namespace":"player","id":"42"},"state":{"schema":"player","version":1,"revision":2,"payload":null}}`)},
		{name: "response missing payload", response: true, payload: []byte(`{"key":{"namespace":"player","id":"42"},"state":{"schema":"player","version":1,"revision":2}}`)},
		{name: "invalid UTF-8 JSON", payload: invalidUTF8},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := codec.Decode(CaptureCommand, test.response, test.payload); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("Decode error = %v, want ErrInvalidResponse", err)
			}
		})
	}

	if _, err := codec.Encode(CaptureCommand, false, CaptureRequest{Key: Key{Namespace: string([]byte{0xff}), ID: "42"}}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Encode invalid request error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Encode(CaptureCommand, true, CaptureResponse{Key: testKey(), State: State{Schema: "player", Version: 1, Revision: 1}}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Encode nil payload error = %v, want ErrInvalidResponse", err)
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
