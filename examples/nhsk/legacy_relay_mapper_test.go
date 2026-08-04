package nhsk

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	legacyOutCardRelayGolden    = "0000000000000000000000000586000000000000910000002200d2040000e903000000000000000000000000000002740000000000006f000000e90300000300000004000000580000005200000009000000380000003700000000000000000000000000000001770000000000003700000003130000000000000000000000000000000000000000000000000205000000"
	legacyCardActionRelayGolden = "00000000000000000000000005860000000000008d0000002200d2040000e903000000000000000000000000000002740000000000006b000000e903000003000000040000005800000052000000090000003800000033000000000000000000000000000000027700000000000033000000030325000000000000000000000000000000000000000000000003"
	legacyUserStateRelayGolden  = "00000000000000000000000005860000000000007a0000002200d2040000e9030000000000000000000000000000027400000000000058000000e9030000030000000400000058000000520000000900000038000000200000000000000000000000000000000a7200000000000020000000e903000001000000"
)

func TestMapLegacyInboundGameplayRelayNormalizesOutCard(t *testing.T) {
	frame := decodeLegacyMapperGolden(t, legacyOutCardRelayGolden)

	got, err := mapLegacyInboundGameplayRelay(frame)
	if err != nil {
		t.Fatalf("map inbound OUT_CARD relay: %v", err)
	}
	want := legacyInboundGameplayCommand{
		BattleID:  1234,
		MatchID:   88,
		ProductID: 82,
		Command: gsr.Command{
			ID: PlayCardsCommand,
			Payload: PlayCardsRequest{
				Player:     game.PlayerID("1001"),
				Cards:      []byte{0x03, 0x13},
				VerifyCode: 5,
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped inbound OUT_CARD relay = %#v, want %#v", got, want)
	}

	frame[90+24] = 0xff
	if got.Command.Payload.(PlayCardsRequest).Cards[0] == 0xff {
		t.Fatal("mapped inbound relay retained frame storage")
	}
}

func TestMapLegacyInboundGameplayRelayNormalizesCardAction(t *testing.T) {
	got, err := mapLegacyInboundGameplayRelay(decodeLegacyMapperGolden(t, legacyCardActionRelayGolden))
	if err != nil {
		t.Fatalf("map inbound CARD_ACTION relay: %v", err)
	}
	if got.BattleID != 1234 || got.MatchID != 88 || got.ProductID != 82 {
		t.Fatalf("mapped inbound CARD_ACTION identity = %#v", got)
	}
	want := PreviewCardSelectionRequest{Player: game.PlayerID("1001"), Cards: []byte{0x03, 0x03, 0x25}}
	if got.Command.ID != PreviewCardSelectionCommand || !reflect.DeepEqual(got.Command.Payload, want) {
		t.Fatalf("mapped inbound CARD_ACTION command = %#v, want %#v", got.Command, want)
	}
}

func TestMapLegacyInboundGameplayRelayNormalizesUserStateChange(t *testing.T) {
	got, err := mapLegacyInboundGameplayRelay(decodeLegacyMapperGolden(t, legacyUserStateRelayGolden))
	if err != nil {
		t.Fatalf("map inbound USER_STATE_CHANGE relay: %v", err)
	}
	want := SetPlayerAutoStateRequest{Player: game.PlayerID("1001"), Enabled: true}
	if got.Command.ID != SetPlayerAutoStateCommand || !reflect.DeepEqual(got.Command.Payload, want) {
		t.Fatalf("mapped inbound USER_STATE_CHANGE command = %#v, want %#v", got.Command, want)
	}
}

func TestMapLegacyInboundGameplayRelayRejectsInvalidIdentityOrPayload(t *testing.T) {
	valid := decodeLegacyMapperGolden(t, legacyOutCardRelayGolden)
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "zero battle", mutate: mutateRelayUint32(26, 0)},
		{name: "zero outer user", mutate: mutateRelayUint32(30, 0)},
		{name: "inner user mismatch", mutate: mutateRelayUint32(58, 1002)},
		{name: "malformed outer", mutate: func(data []byte) []byte { return data[:89] }},
		{name: "unsupported client message", mutate: mutateRelayUint32(102, 0x7777)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := test.mutate(append([]byte(nil), valid...))
			if _, err := mapLegacyInboundGameplayRelay(data); !errors.Is(err, errInvalidLegacyInboundGameplayRelay) {
				t.Fatalf("map invalid inbound relay error = %v, want errInvalidLegacyInboundGameplayRelay", err)
			}
		})
	}
}

func TestMapLegacyInboundGameplayRelayRejectsUserStatePayloadIdentityMismatch(t *testing.T) {
	data := decodeLegacyMapperGolden(t, legacyUserStateRelayGolden)
	binary.LittleEndian.PutUint32(data[114:118], 1002)

	if _, err := mapLegacyInboundGameplayRelay(data); !errors.Is(err, errInvalidLegacyInboundGameplayRelay) {
		t.Fatalf("map mismatched USER_STATE_CHANGE relay error = %v, want errInvalidLegacyInboundGameplayRelay", err)
	}
}

func TestRouteLegacyGameplayCallUsesResolvedBattleRefAndMappedCommand(t *testing.T) {
	runtime := &recordingCommandRuntime{resolve: ResolveBattleResult{BattleID: 1234, Ref: gsr.ServiceRef{Node: "nhsk", ID: 9}}}
	value, err := RouteLegacyGameplayCall(context.Background(), runtime, gsr.ServiceRef{Node: "nhsk", ID: 1}, decodeLegacyMapperGolden(t, legacyOutCardRelayGolden))
	if err != nil {
		t.Fatal(err)
	}
	if value != "reply" || runtime.target.ID != 9 || runtime.command != PlayCardsCommand {
		t.Fatalf("route = value=%#v target=%#v command=%#x", value, runtime.target, runtime.command)
	}
	request := runtime.payload.(PlayCardsRequest)
	if request.Player != "1001" || !reflect.DeepEqual(request.Cards, []byte{0x03, 0x13}) {
		t.Fatalf("route payload = %#v", request)
	}
}

type recordingCommandRuntime struct {
	resolve ResolveBattleResult
	target  gsr.ServiceRef
	command gsr.CommandID
	payload any
}

func (runtime *recordingCommandRuntime) Send(gsr.ServiceRef, gsr.CommandID, any) error { return nil }
func (runtime *recordingCommandRuntime) Call(_ context.Context, target gsr.ServiceRef, command gsr.CommandID, payload any) (any, error) {
	if command == ResolveBattleCommand {
		return runtime.resolve, nil
	}
	runtime.target, runtime.command, runtime.payload = target, command, payload
	return "reply", nil
}

func mutateRelayUint32(offset int, value uint32) func([]byte) []byte {
	return func(data []byte) []byte {
		binary.LittleEndian.PutUint32(data[offset:offset+4], value)
		return data
	}
}
