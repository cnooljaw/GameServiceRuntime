package nhsk

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestGameplayCommandIDsUseDedicatedNHSKNamespace(t *testing.T) {
	if PlayCardsCommand != gsr.CommandID(0x04100301) {
		t.Fatalf("PlayCardsCommand = %#x, want 0x04100301", PlayCardsCommand)
	}
	if PreviewCardSelectionCommand != gsr.CommandID(0x04100302) {
		t.Fatalf("PreviewCardSelectionCommand = %#x, want 0x04100302", PreviewCardSelectionCommand)
	}
	if SetPlayerAutoStateCommand != gsr.CommandID(0x04100303) {
		t.Fatalf("SetPlayerAutoStateCommand = %#x, want 0x04100303", SetPlayerAutoStateCommand)
	}
	if PlayCardsCommand == 0x7701 || PreviewCardSelectionCommand == 0x7702 {
		t.Fatal("gameplay CommandID reused a Legacy MessageID")
	}
}

func TestMapLegacyGameplayCommandMapsUserStateChangeAutoBit(t *testing.T) {
	frame := decodeLegacyMapperGolden(t, "0000000000000000000000000a7200000000000020000000e903000005000000")

	got, err := mapLegacyGameplayCommand(1001, frame)
	if err != nil {
		t.Fatalf("map USER_STATE_CHANGE: %v", err)
	}
	want := gsr.Command{
		ID: SetPlayerAutoStateCommand,
		Payload: SetPlayerAutoStateRequest{
			Player:  game.PlayerID("1001"),
			Enabled: true,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped USER_STATE_CHANGE = %#v, want %#v", got, want)
	}

	binary.LittleEndian.PutUint32(frame[28:32], 4)
	got, err = mapLegacyGameplayCommand(1001, frame)
	if err != nil {
		t.Fatalf("map cleared USER_STATE_CHANGE: %v", err)
	}
	if got.Payload.(SetPlayerAutoStateRequest).Enabled {
		t.Fatalf("state without auto bit mapped enabled: %#v", got.Payload)
	}
}

func TestMapLegacyGameplayCommandMapsOutCardAndOwnsCards(t *testing.T) {
	frame := decodeLegacyMapperGolden(t, "00000000000000000000000001770000000000003700000003130000000000000000000000000000000000000000000000000205000000")

	got, err := mapLegacyGameplayCommand(1001, frame)
	if err != nil {
		t.Fatalf("map OUT_CARD: %v", err)
	}
	want := gsr.Command{
		ID: PlayCardsCommand,
		Payload: PlayCardsRequest{
			Player:     game.PlayerID("1001"),
			Cards:      []byte{0x03, 0x13},
			VerifyCode: 5,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped OUT_CARD = %#v, want %#v", got, want)
	}

	frame[24] = 0xff
	if got.Payload.(PlayCardsRequest).Cards[0] == 0xff {
		t.Fatal("mapped OUT_CARD retained Legacy frame storage")
	}
}

func TestMapLegacyGameplayCommandMapsCardActionAndOwnsCards(t *testing.T) {
	frame := decodeLegacyMapperGolden(t, "000000000000000000000000027700000000000033000000030325000000000000000000000000000000000000000000000003")

	got, err := mapLegacyGameplayCommand(1001, frame)
	if err != nil {
		t.Fatalf("map CARD_ACTION: %v", err)
	}
	want := gsr.Command{
		ID: PreviewCardSelectionCommand,
		Payload: PreviewCardSelectionRequest{
			Player: game.PlayerID("1001"),
			Cards:  []byte{0x03, 0x03, 0x25},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped CARD_ACTION = %#v, want %#v", got, want)
	}

	frame[24] = 0xff
	if got.Payload.(PreviewCardSelectionRequest).Cards[0] == 0xff {
		t.Fatal("mapped CARD_ACTION retained Legacy frame storage")
	}
}

func TestMapLegacyGameplayCommandMapsReconnectAndScene(t *testing.T) {
	for _, test := range []struct {
		name   string
		typeID uint32
		want   gsr.CommandID
	}{
		{name: "reconnect", typeID: 0x7208, want: ReconnectPlayerCommand},
		{name: "scene", typeID: 0x720d, want: RequestGameSceneCommand},
	} {
		t.Run(test.name, func(t *testing.T) {
			frame := legacyPlayerViewRequest(t, test.typeID, 1001)
			got, err := mapLegacyGameplayCommand(1001, frame)
			if err != nil {
				t.Fatalf("map %s: %v", test.name, err)
			}
			if got.ID != test.want {
				t.Fatalf("map %s command = %#x, want %#x", test.name, got.ID, test.want)
			}
			if request, ok := got.Payload.(ReconnectPlayerRequest); !ok || request.Player != "1001" {
				t.Fatalf("map %s payload = %#v", test.name, got.Payload)
			}
		})
	}
}

func TestMapLegacyGameplayCommandRejectsReconnectUserMismatch(t *testing.T) {
	frame := legacyPlayerViewRequest(t, 0x7208, 1002)
	if _, err := mapLegacyGameplayCommand(1001, frame); !errors.Is(err, errInvalidLegacyGameplayCommand) {
		t.Fatalf("mismatched reconnect error = %v, want errInvalidLegacyGameplayCommand", err)
	}
}

func TestMapLegacyGameplayCommandLeavesGameplayValidationToBattle(t *testing.T) {
	nineCards := decodeLegacyMapperGolden(t, "00000000000000000000000001770000000000003700000001020304050607080900000000000000000000000000000000000907000000")
	emptyPreview := decodeLegacyMapperGolden(t, "000000000000000000000000027700000000000033000000000000000000000000000000000000000000000000000000000000")

	outCard, err := mapLegacyGameplayCommand(1001, nineCards)
	if err != nil {
		t.Fatalf("map nine-card OUT_CARD: %v", err)
	}
	if got := outCard.Payload.(PlayCardsRequest); len(got.Cards) != 9 || got.VerifyCode != 7 {
		t.Fatalf("mapped nine-card OUT_CARD = %#v, want 9 cards and verify 7", got)
	}

	preview, err := mapLegacyGameplayCommand(1001, emptyPreview)
	if err != nil {
		t.Fatalf("map empty CARD_ACTION: %v", err)
	}
	if got := preview.Payload.(PreviewCardSelectionRequest); len(got.Cards) != 0 {
		t.Fatalf("mapped empty CARD_ACTION = %#v, want empty cards", got)
	}
}

func TestMapLegacyGameplayCommandRejectsInvalidIdentityOrPayload(t *testing.T) {
	outCard := decodeLegacyMapperGolden(t, "00000000000000000000000001770000000000003700000003130000000000000000000000000000000000000000000000000205000000")
	malformedOutCard := append([]byte(nil), outCard[:54]...)
	binary.LittleEndian.PutUint32(malformedOutCard[20:24], uint32(len(malformedOutCard)))
	unknown := make([]byte, 24)
	binary.LittleEndian.PutUint32(unknown[12:16], 0x7777)
	binary.LittleEndian.PutUint32(unknown[20:24], uint32(len(unknown)))

	tests := []struct {
		name    string
		userID  uint32
		payload []byte
	}{
		{name: "zero user", payload: outCard},
		{name: "short payload", userID: 1001, payload: outCard[:23]},
		{name: "known message with bad body", userID: 1001, payload: malformedOutCard},
		{name: "state change payload user mismatch", userID: 1001, payload: decodeLegacyMapperGolden(t, "0000000000000000000000000a7200000000000020000000ea03000001000000")},
		{name: "unsupported message", userID: 1001, payload: unknown},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := mapLegacyGameplayCommand(test.userID, test.payload); !errors.Is(err, errInvalidLegacyGameplayCommand) {
				t.Fatalf("map invalid gameplay command error = %v, want errInvalidLegacyGameplayCommand", err)
			}
		})
	}
}

func decodeLegacyMapperGolden(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode Legacy mapper golden: %v", err)
	}
	return data
}

func legacyPlayerViewRequest(t *testing.T, typeID, userID uint32) []byte {
	t.Helper()
	data := make([]byte, 24+24)
	binary.LittleEndian.PutUint32(data[12:16], typeID)
	binary.LittleEndian.PutUint32(data[20:24], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[24:28], userID)
	binary.LittleEndian.PutUint32(data[36:40], 88)
	binary.LittleEndian.PutUint32(data[40:44], 82)
	return data
}
