package legacywire

import (
	"encoding/binary"
	"testing"
)

func TestDecodePlayerViewRequests(t *testing.T) {
	for _, message := range []uint32{messageGameUserReconnect, messageGameScene} {
		data := make([]byte, playerViewRequestFrameSize)
		binary.LittleEndian.PutUint32(data[12:16], message)
		binary.LittleEndian.PutUint32(data[20:24], uint32(len(data)))
		binary.LittleEndian.PutUint32(data[24:28], 1001)
		binary.LittleEndian.PutUint32(data[36:40], 88)
		binary.LittleEndian.PutUint32(data[40:44], 82)

		var got PlayerViewRequest
		var err error
		if message == messageGameUserReconnect {
			got, err = DecodeUserReconnect(data)
		} else {
			got, err = DecodeGameSceneRequest(data)
		}
		if err != nil {
			t.Fatalf("decode player view %#x: %v", message, err)
		}
		if got.UserID != 1001 || got.MatchID != 88 || got.ProductID != 82 {
			t.Fatalf("decode player view %#x = %#v", message, got)
		}
	}
}

func TestDecodeUserReconnectRejectsWrongMessageOrLength(t *testing.T) {
	data := make([]byte, playerViewRequestFrameSize)
	binary.LittleEndian.PutUint32(data[12:16], messageGameScene)
	binary.LittleEndian.PutUint32(data[20:24], uint32(len(data)))
	if _, err := DecodeUserReconnect(data); err == nil {
		t.Fatal("wrong player view message was accepted")
	}
	if _, err := DecodeUserReconnect(data[:len(data)-1]); err == nil {
		t.Fatal("short player view frame was accepted")
	}
}
