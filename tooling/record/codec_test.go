package record

import (
	"errors"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestClusterCodecRoundTripsRecorderProtocolAndComposes(t *testing.T) {
	fallback := &recordingClusterCodec{}
	codec := NewClusterCodec(fallback)
	request := appendRecordRequest{Entry: testRecordBundle().Entries[0]}
	payload, err := codec.Encode(AppendRecordCommand, false, request)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(AppendRecordCommand, false, payload)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := decoded.(appendRecordRequest); !ok || string(got.Entry.Payload) != "3" || got.Entry.TargetKey != "battle:42" {
		t.Fatalf("Decode() = %#v", decoded)
	}
	responsePayload, err := codec.Encode(ListRecordsCommand, true, listRecordsResponse{Entries: testRecordBundle().Entries})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(ListRecordsCommand, true, responsePayload); err != nil {
		t.Fatalf("Decode(response) error = %v", err)
	}
	if _, err := codec.Encode(99, false, "fallback"); err != nil || fallback.encodeCalls != 1 {
		t.Fatalf("Encode(fallback) = %v, calls=%d", err, fallback.encodeCalls)
	}
	if _, err := NewClusterCodec(nil).Encode(99, false, nil); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode(unknown) error = %v, want ErrUnsupportedCommand", err)
	}
	if _, err := codec.Decode(AppendRecordCommand, false, []byte(`{"Entry":{"FormatVersion":2}}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(invalid entry) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(ClearRecordsCommand, false, []byte(`{"Key":"battle:42"} {}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(trailing JSON) error = %v, want ErrInvalidResponse", err)
	}
}

type recordingClusterCodec struct{ encodeCalls int }

func (c *recordingClusterCodec) Encode(gsr.CommandID, bool, any) ([]byte, error) {
	c.encodeCalls++
	return []byte("fallback"), nil
}
func (*recordingClusterCodec) Decode(gsr.CommandID, bool, []byte) (any, error) {
	return "fallback", nil
}
