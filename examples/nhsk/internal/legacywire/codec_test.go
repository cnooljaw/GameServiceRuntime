package legacywire

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

const inboundRelayGoldenHex = "00000000000000000000000005860000000000005d0000002200d20400002a00000000000000000000000000000002740000000000003b0000002a00000003000000040000005800000052000000090000003800000003000000112233"

const outboundRelayGoldenHex = "00000000000000000000000044860000000000005d0000002200d20400002a00000000000000000000000000000000740000000000003b0000002a00000000000000000000005800000052000000000000003800000003000000112233"

func TestDecodeInboundGameRelayMatchesReferenceGolden(t *testing.T) {
	data := decodeGoldenHex(t, inboundRelayGoldenHex)
	want := InboundGameRelay{
		BattleID: 1234,
		UserID:   42,
		GameHeader: GameHeader{
			UserID:      42,
			ConnectType: 3,
			PlatformID:  4,
			MatchID:     88,
			ProductID:   82,
			Reserved:    9,
		},
		Payload: []byte{0x11, 0x22, 0x33},
	}

	got, err := DecodeInboundGameRelay(data)
	if err != nil {
		t.Fatalf("decode inbound relay: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded relay = %#v, want %#v", got, want)
	}

	data[len(data)-1] = 0xff
	if got.Payload[len(got.Payload)-1] == 0xff {
		t.Fatal("decoded relay retained caller payload storage")
	}
}

func TestEncodeOutboundGameRelayMatchesReferenceGolden(t *testing.T) {
	got, err := EncodeOutboundGameRelay(OutboundGameRelay{
		BattleID:  1234,
		UserID:    42,
		MatchID:   88,
		ProductID: 82,
		Payload:   []byte{0x11, 0x22, 0x33},
	})
	if err != nil {
		t.Fatalf("encode outbound relay: %v", err)
	}
	want := decodeGoldenHex(t, outboundRelayGoldenHex)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outbound relay bytes = %x, want %x", got, want)
	}
}

func TestDecodeInboundGameRelayRejectsMalformedNestedBoundaries(t *testing.T) {
	golden := decodeGoldenHex(t, inboundRelayGoldenHex)
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "short outer", mutate: func(data []byte) []byte { return data[:33] }},
		{name: "outer type", mutate: mutateUint32(12, messageGLToGMGame)},
		{name: "outer zero length", mutate: mutateUint32(20, 0)},
		{name: "outer length mismatch", mutate: mutateUint32(20, uint32(len(golden)-1))},
		{name: "outer header length", mutate: mutateUint16(24, 33)},
		{name: "inner type", mutate: mutateUint32(34+12, messageGameToAgentRelay)},
		{name: "inner zero length", mutate: mutateUint32(34+20, 0)},
		{name: "inner length mismatch", mutate: mutateUint32(34+20, 58)},
		{name: "suffix below fixed body", mutate: mutateUint32(34+48, 55)},
		{name: "suffix gap", mutate: mutateUint32(34+48, 57)},
		{name: "suffix above frame", mutate: mutateUint32(34+52, 4)},
		{name: "trailing byte", mutate: func(data []byte) []byte { return append(data, 0) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := test.mutate(append([]byte(nil), golden...))
			if _, err := DecodeInboundGameRelay(data); !errors.Is(err, errMalformedRelay) {
				t.Fatalf("decode malformed relay error = %v, want errMalformedRelay", err)
			}
		})
	}
}

func TestEncodeOutboundGameRelayRejectsOversizedFrame(t *testing.T) {
	_, err := EncodeOutboundGameRelay(OutboundGameRelay{Payload: make([]byte, maxFrameSize)})
	if !errors.Is(err, errMalformedRelay) {
		t.Fatalf("encode oversized relay error = %v, want errMalformedRelay", err)
	}
}

func decodeGoldenHex(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode golden hex: %v", err)
	}
	return data
}

func mutateUint16(offset int, value uint16) func([]byte) []byte {
	return func(data []byte) []byte {
		binary.LittleEndian.PutUint16(data[offset:offset+2], value)
		return data
	}
}

func mutateUint32(offset int, value uint32) func([]byte) []byte {
	return func(data []byte) []byte {
		binary.LittleEndian.PutUint32(data[offset:offset+4], value)
		return data
	}
}
