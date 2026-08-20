package nhsk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	legacyAIEnvelopeHeaderSize = 78
	legacyAISceneHeaderSize    = 88
	legacyAIResponseHeaderSize = 67
	legacyAIOutCardSize        = 55
	legacyAIMaxResponseBytes   = 1 << 20

	legacyAskMoveWithSceneID = uint32(0x8581)
	legacyAISceneID          = uint32(0x7612)
	legacyAskOutCardID       = uint32(0x7603)
	legacyOutCardID          = uint32(0x7701)
)

var errInvalidLegacyAI = errors.New("nhsk: invalid Legacy AI exchange")

// LegacyHTTPAIProvider adapts the old RobotTran JSON/base64 HTTP contract at
// the process boundary. Battle Services only see the typed AIProvider API.
type LegacyHTTPAIProvider struct {
	URL    string
	Client *http.Client
	GameID uint32
}

// Move posts one exact old-format scene and returns the candidate card bytes.
func (provider LegacyHTTPAIProvider) Move(ctx context.Context, request AIRequest) ([]byte, error) {
	if strings.TrimSpace(provider.URL) == "" || provider.Client == nil || provider.GameID == 0 || !validAIRequest(request) {
		return nil, errInvalidLegacyAI
	}
	packet, err := encodeLegacyAIRequest(provider.GameID, request)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		GameID uint32 `json:"game_id"`
		Data   string `json:"data"`
	}{GameID: provider.GameID, Data: base64.StdEncoding.EncodeToString(packet)})
	if err != nil {
		return nil, fmt.Errorf("%w: encode request", errInvalidLegacyAI)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: create request", errInvalidLegacyAI)
	}
	httpRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := provider.Client.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("%w: request failed", errInvalidLegacyAI)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("%w: HTTP status %d", errInvalidLegacyAI, response.StatusCode)
	}
	limited := io.LimitReader(response.Body, legacyAIMaxResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil || len(responseBody) > legacyAIMaxResponseBytes {
		return nil, fmt.Errorf("%w: response body", errInvalidLegacyAI)
	}
	return decodeLegacyAIResponse(responseBody, request.SeatID)
}

func encodeLegacyAIRequest(gameID uint32, request AIRequest) ([]byte, error) {
	if gameID == 0 || !validAIRequest(request) || len(request.Scene.Hand) > 26 {
		return nil, errInvalidLegacyAI
	}
	scene := encodeLegacyAIScene(request)
	move := make([]byte, 36)
	putLegacyHeader(move, legacyAskOutCardID)
	binary.LittleEndian.PutUint32(move[24:28], request.UserID)
	binary.LittleEndian.PutUint32(move[28:32], request.VerifyCode)
	binary.LittleEndian.PutUint32(move[32:36], request.ActionMS)

	packet := make([]byte, legacyAIEnvelopeHeaderSize+len(scene)+len(move))
	putLegacyHeader(packet, legacyAskMoveWithSceneID)
	binary.LittleEndian.PutUint32(packet[24:28], request.ProductID)
	binary.LittleEndian.PutUint32(packet[36:40], request.MatchID)
	binary.LittleEndian.PutUint32(packet[46:50], request.RoundID)
	binary.LittleEndian.PutUint32(packet[54:58], gameID)
	binary.LittleEndian.PutUint32(packet[58:62], legacyAIEnvelopeHeaderSize)
	binary.LittleEndian.PutUint32(packet[62:66], uint32(len(scene)))
	moveOffset := legacyAIEnvelopeHeaderSize + len(scene)
	binary.LittleEndian.PutUint32(packet[66:70], uint32(moveOffset))
	binary.LittleEndian.PutUint32(packet[70:74], uint32(len(move)))
	binary.LittleEndian.PutUint32(packet[74:78], request.MoveMS)
	copy(packet[legacyAIEnvelopeHeaderSize:], scene)
	copy(packet[moveOffset:], move)
	return packet, nil
}

func encodeLegacyAIScene(request AIRequest) []byte {
	suffixSize := 0
	for _, cards := range request.Scene.OutedCards {
		if len(cards) > 26 {
			suffixSize += 27
		} else {
			suffixSize += 1 + len(cards)
		}
	}
	data := make([]byte, legacyAISceneHeaderSize+suffixSize)
	putLegacyHeader(data, legacyAISceneID)
	data[24], data[25] = request.Scene.ActiveSeat, request.Scene.FirstOutSeat
	copy(data[26:30], request.Scene.HandCounts[:])
	copy(data[30:56], request.Scene.Hand)
	for index, points := range request.Scene.CapturedPoints {
		binary.LittleEndian.PutUint32(data[56+index*4:60+index*4], uint32(points))
	}
	copy(data[72:76], request.Scene.Ranks[:])
	binary.LittleEndian.PutUint32(data[76:80], request.Scene.TrickPoint)
	binary.LittleEndian.PutUint32(data[80:84], uint32(len(request.Scene.OutedCards)))
	binary.LittleEndian.PutUint32(data[84:88], request.VerifyCode)
	offset := legacyAISceneHeaderSize
	for _, cards := range request.Scene.OutedCards {
		count := len(cards)
		if count > 26 {
			count = 26
		}
		data[offset] = byte(count)
		offset++
		copy(data[offset:offset+count], cards[:count])
		offset += count
	}
	return data
}

func decodeLegacyAIResponse(body []byte, wantSeat uint8) ([]byte, error) {
	var response struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Code != http.StatusOK || response.Data == "" {
		return nil, fmt.Errorf("%w: response JSON", errInvalidLegacyAI)
	}
	packet, err := base64.StdEncoding.DecodeString(response.Data)
	if err != nil || len(packet) < legacyAIResponseHeaderSize || binary.LittleEndian.Uint32(packet[20:24]) != uint32(len(packet)) || packet[58] != wantSeat {
		return nil, fmt.Errorf("%w: response envelope", errInvalidLegacyAI)
	}
	offset := binary.LittleEndian.Uint32(packet[59:63])
	size := binary.LittleEndian.Uint32(packet[63:67])
	end := uint64(offset) + uint64(size)
	if size != legacyAIOutCardSize || offset < legacyAIResponseHeaderSize || end > uint64(len(packet)) {
		return nil, fmt.Errorf("%w: response suffix", errInvalidLegacyAI)
	}
	move := packet[offset:uint32(end)]
	count := int(move[50])
	if binary.LittleEndian.Uint32(move[12:16]) != legacyOutCardID || binary.LittleEndian.Uint32(move[20:24]) != uint32(len(move)) || count > 26 {
		return nil, fmt.Errorf("%w: response move", errInvalidLegacyAI)
	}
	return append([]byte(nil), move[24:24+count]...), nil
}

func putLegacyHeader(data []byte, messageID uint32) {
	binary.LittleEndian.PutUint32(data[12:16], messageID)
	binary.LittleEndian.PutUint32(data[20:24], uint32(len(data)))
}

var _ AIProvider = LegacyHTTPAIProvider{}
