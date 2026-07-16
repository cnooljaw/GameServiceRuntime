package tcp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestWireEnvelopeRoundTrip(t *testing.T) {
	want := gsr.WireEnvelope{
		Source:   gsr.ServiceRef{Node: "node-a", ID: 11},
		Target:   gsr.ServiceRef{Node: "node-b", ID: 22},
		Session:  33,
		Command:  44,
		Payload:  []byte("payload"),
		CallPath: []gsr.ServiceRef{{Node: "node-a", ID: 11}, {Node: "node-b", ID: 22}},
	}
	body, err := encodeWireEnvelope(want, defaultMaxFrameSize)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeWireEnvelope(body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded envelope = %#v, want %#v", got, want)
	}
}

func TestWireErrorReplyRoundTrip(t *testing.T) {
	want := gsr.WireEnvelope{
		Source:       gsr.ServiceRef{Node: "node-b", ID: 22},
		Target:       gsr.ServiceRef{Node: "node-a"},
		Session:      33,
		Command:      44,
		Response:     true,
		ErrorCode:    "service_failed",
		ErrorMessage: "failed",
	}
	body, err := encodeWireEnvelope(want, defaultMaxFrameSize)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeWireEnvelope(body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded envelope = %#v, want %#v", got, want)
	}
}

func TestWireEnvelopeRejectsOversizedFields(t *testing.T) {
	tests := []struct {
		name     string
		envelope gsr.WireEnvelope
	}{
		{
			name: "node id",
			envelope: gsr.WireEnvelope{
				Source: gsr.ServiceRef{Node: gsr.NodeID(string(bytes.Repeat([]byte{'a'}, maxNodeIDLength+1)))},
				Target: gsr.ServiceRef{Node: "node-b"},
			},
		},
		{
			name: "call path",
			envelope: gsr.WireEnvelope{
				Source:   gsr.ServiceRef{Node: "node-a"},
				Target:   gsr.ServiceRef{Node: "node-b"},
				CallPath: make([]gsr.ServiceRef, maxCallPathLength+1),
			},
		},
		{
			name: "frame",
			envelope: gsr.WireEnvelope{
				Source:  gsr.ServiceRef{Node: "node-a"},
				Target:  gsr.ServiceRef{Node: "node-b"},
				Payload: bytes.Repeat([]byte{'x'}, 128),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limit := uint32(defaultMaxFrameSize)
			if test.name == "frame" {
				limit = 32
			}
			if _, err := encodeWireEnvelope(test.envelope, limit); !errors.Is(err, ErrFrameTooLarge) {
				t.Fatalf("encode error = %v", err)
			}
		})
	}
}

func TestReadFrameRejectsLengthBeforeAllocation(t *testing.T) {
	var input bytes.Buffer
	if err := binary.Write(&input, binary.BigEndian, uint32(1024)); err != nil {
		t.Fatal(err)
	}
	if _, err := readFrame(&input, 32); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("readFrame error = %v", err)
	}
}
