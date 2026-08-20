package nhsk

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestLegacyHTTPAIProviderEncodesReferenceRequestAndDecodesMove(t *testing.T) {
	var packet []byte
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("content type = %q", request.Header.Get("Content-Type"))
		}
		var body struct {
			GameID uint32 `json:"game_id"`
			Data   string `json:"data"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return nil, err
		}
		if body.GameID != NHSKDescriptor.GameID {
			t.Errorf("game id = %d", body.GameID)
		}
		packet, _ = base64.StdEncoding.DecodeString(body.Data)
		response := legacyAIResponsePacket(2, []byte{0x03, 0x13})
		encoded, _ := json.Marshal(map[string]any{"code": 200, "message": "success", "data": base64.StdEncoding.EncodeToString(response)})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(encoded))), Header: make(http.Header)}, nil
	})}

	provider := LegacyHTTPAIProvider{URL: "http://robot.example/ai", Client: client, GameID: NHSKDescriptor.GameID}
	request := AIRequest{
		Target: gsr.ServiceRef{Node: "n", ID: 1}, BattleID: game.BattleID(9), ProductID: 101, MatchID: 202, RoundID: 303,
		GameNum: 1, SubgameNum: 2, UserID: 1003, SeatID: 2, TurnRevision: 7, VerifyCode: 11,
		StartedAt: time.Unix(10, 0), MoveMS: 1000, ActionMS: 9000,
		Scene: AIScene{ActiveSeat: 2, FirstOutSeat: 1, HandCounts: [4]uint8{3, 4, 5, 6}, Hand: []byte{0x03, 0x13, 0x25}, CapturedPoints: [4]uint16{10, 20, 30, 40}, Ranks: [4]uint8{1, 2, 3, 4}, TrickPoint: 15, Leading: true, OutedCards: [][]byte{{0x04}, {}}},
	}
	cards, err := provider.Move(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0] != 0x03 || cards[1] != 0x13 {
		t.Fatalf("cards = %x", cards)
	}
	if len(packet) < 78 || binary.LittleEndian.Uint32(packet[12:16]) != 0x8581 || binary.LittleEndian.Uint32(packet[20:24]) != uint32(len(packet)) {
		t.Fatalf("envelope header = %x", packet)
	}
	if got := binary.LittleEndian.Uint32(packet[24:28]); got != 101 {
		t.Fatalf("MPID = %d", got)
	}
	if got := binary.LittleEndian.Uint32(packet[36:40]); got != 202 {
		t.Fatalf("MatchID = %d", got)
	}
	if got := binary.LittleEndian.Uint32(packet[46:50]); got != 303 {
		t.Fatalf("RoundID = %d", got)
	}
	if got := binary.LittleEndian.Uint32(packet[54:58]); got != NHSKDescriptor.GameID {
		t.Fatalf("GameID = %d", got)
	}
	sceneOffset, sceneSize := binary.LittleEndian.Uint32(packet[58:62]), binary.LittleEndian.Uint32(packet[62:66])
	moveOffset, moveSize := binary.LittleEndian.Uint32(packet[66:70]), binary.LittleEndian.Uint32(packet[70:74])
	if sceneOffset != 78 || moveOffset != sceneOffset+sceneSize || moveOffset+moveSize != uint32(len(packet)) || binary.LittleEndian.Uint32(packet[74:78]) != 1000 {
		t.Fatalf("suffixes scene=%d/%d move=%d/%d", sceneOffset, sceneSize, moveOffset, moveSize)
	}
	scene := packet[sceneOffset : sceneOffset+sceneSize]
	if len(scene) != 91 || binary.LittleEndian.Uint32(scene[12:16]) != 0x7612 || scene[24] != 2 || scene[25] != 1 || scene[30] != 0x03 || binary.LittleEndian.Uint32(scene[84:88]) != 11 || scene[88] != 1 || scene[89] != 0x04 || scene[90] != 0 {
		t.Fatalf("scene = %x", scene)
	}
	move := packet[moveOffset : moveOffset+moveSize]
	if len(move) != 36 || binary.LittleEndian.Uint32(move[12:16]) != 0x7603 || binary.LittleEndian.Uint32(move[24:28]) != 1003 || binary.LittleEndian.Uint32(move[28:32]) != 11 || binary.LittleEndian.Uint32(move[32:36]) != 9000 {
		t.Fatalf("move = %x", move)
	}
}

func TestLegacyHTTPAIProviderRejectsUnsafeResponses(t *testing.T) {
	tests := []struct {
		name string
		body any
	}{
		{name: "provider code", body: map[string]any{"code": 500, "message": "secret", "data": ""}},
		{name: "bad base64", body: map[string]any{"code": 200, "data": "%%%"}},
		{name: "short envelope", body: map[string]any{"code": 200, "data": base64.StdEncoding.EncodeToString([]byte{1})}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, _ := json.Marshal(test.body)
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(encoded))), Header: make(http.Header)}, nil
			})}
			provider := LegacyHTTPAIProvider{URL: "http://robot.example/ai", Client: client, GameID: NHSKDescriptor.GameID}
			if _, err := provider.Move(context.Background(), validLegacyAIRequest()); err == nil {
				t.Fatal("Move succeeded, want error")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func legacyAIResponsePacket(seat uint8, cards []byte) []byte {
	move := make([]byte, 55)
	binary.LittleEndian.PutUint32(move[12:16], 0x7701)
	binary.LittleEndian.PutUint32(move[20:24], uint32(len(move)))
	copy(move[24:50], cards)
	move[50] = byte(len(cards))
	binary.LittleEndian.PutUint32(move[51:55], 999)
	packet := make([]byte, 67+len(move))
	binary.LittleEndian.PutUint32(packet[20:24], uint32(len(packet)))
	packet[58] = seat
	binary.LittleEndian.PutUint32(packet[59:63], 67)
	binary.LittleEndian.PutUint32(packet[63:67], uint32(len(move)))
	copy(packet[67:], move)
	return packet
}

func validLegacyAIRequest() AIRequest {
	return AIRequest{Target: gsr.ServiceRef{Node: "n", ID: 1}, BattleID: 1, ProductID: 82, MatchID: 1, GameNum: 1, SubgameNum: 1, UserID: 1, SeatID: 0, TurnRevision: 1, VerifyCode: 1, StartedAt: time.Unix(1, 0), MoveMS: 1000, ActionMS: 10000, Scene: AIScene{Hand: []byte{3}, Leading: true}}
}
