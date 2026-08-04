package legacywire

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestRetainedNHSKMessageIDsMatchReference(t *testing.T) {
	if messageNHSKBase != 0x7600 {
		t.Fatalf("BS_MSG_GAME = %#x, want 0x7600", messageNHSKBase)
	}
	want := map[string]uint32{
		"user_state_change":  0x720a,
		"user_reconnect":     0x7208,
		"game_scene_request": 0x720d,
		"game_info":          0x7601,
		"deal":               0x7602,
		"ask_out_card":       0x7603,
		"out_card_info":      0x7604,
		"turn_end":           0x7605,
		"show_cards":         0x7606,
		"game_result":        0x7607,
		"game_scene":         0x7608,
		"out_card_result":    0x7609,
		"card_action_watch":  0x7611,
		"out_card":           0x7701,
		"card_action":        0x7702,
	}
	got := map[string]uint32{
		"user_state_change":  messageGameUserStateChange,
		"user_reconnect":     messageGameUserReconnect,
		"game_scene_request": messageGameScene,
		"game_info":          messageNHSKGameInfo,
		"deal":               messageNHSKDeal,
		"ask_out_card":       messageNHSKAskOutCard,
		"out_card_info":      messageNHSKOutCardInfo,
		"turn_end":           messageNHSKTurnEnd,
		"show_cards":         messageNHSKShowCards,
		"game_result":        messageNHSKGameResult,
		"game_scene":         messageNHSKGameScene,
		"out_card_result":    messageNHSKOutCardResult,
		"card_action_watch":  messageNHSKCardActionWatch,
		"out_card":           messageNHSKOutCard,
		"card_action":        messageNHSKCardAction,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retained NHSK message IDs = %#v, want %#v", got, want)
	}
}

func TestLegacyEnvelopeMessageIDsMatchReference(t *testing.T) {
	want := map[string]uint32{
		"origin":                0x0600,
		"agent_to_game_relay":   0x7402,
		"game_to_agent_relay":   0x7400,
		"gm_to_gl_game_message": 0x8605,
		"gl_to_gm_game_message": 0x8644,
	}
	got := map[string]uint32{
		"origin":                messageOrigin,
		"agent_to_game_relay":   messageAgentToGameRelay,
		"game_to_agent_relay":   messageGameToAgentRelay,
		"gm_to_gl_game_message": messageGMToGLGame,
		"gl_to_gm_game_message": messageGLToGMGame,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Legacy envelope message IDs = %#v, want %#v", got, want)
	}
}

func TestOriginFramesMatchReferenceGoldenBytes(t *testing.T) {
	tests := []struct {
		name   string
		origin uint16
		hex    string
	}{
		{name: "GameLogic", origin: originGameLogic, hex: "00000000000000006b000000000600000000000018000000"},
		{name: "GameMaster", origin: originGameMaster, hex: "000000000000000064000000000600000000000018000000"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want, err := hex.DecodeString(test.hex)
			if err != nil {
				t.Fatalf("decode golden: %v", err)
			}
			got := encodeOrigin(test.origin)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("origin bytes = %x, want %x", got, want)
			}
		})
	}
}

func TestHeaderUsesExactLittleEndianOffsets(t *testing.T) {
	want, err := hex.DecodeString("4433221188776655aa99ccbbeeddccbb0403020118000000")
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	header := bsHeader{
		Magic:   0x11223344,
		Serial:  0x55667788,
		Origin:  0x99aa,
		Reserve: 0xbbcc,
		Type:    0xbbccddee,
		Param:   0x01020304,
		Length:  headerSize,
	}
	got := make([]byte, headerSize)
	encodeHeader(got, header)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("header bytes = %x, want %x", got, want)
	}
	decoded, err := decodeHeader(got)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if decoded != header {
		t.Fatalf("decoded header = %#v, want %#v", decoded, header)
	}
}

func TestDecodeHeaderRejectsShortInput(t *testing.T) {
	if _, err := decodeHeader(make([]byte, headerSize-1)); err == nil {
		t.Fatal("decode short header succeeded")
	}
}
