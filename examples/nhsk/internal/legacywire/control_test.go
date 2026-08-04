package legacywire

import (
	"encoding/binary"
	"testing"
)

func TestDecodeControlCoversLegacyLifecycleFrames(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want func(t *testing.T, got LegacyControl)
	}{
		{
			name: "new game",
			data: controlFrame(messageGM2GLNewGame, 44, func(data []byte) {
				put32(data, 24, 12345)
				put32(data, 28, 7)
				put32(data, 32, 1)
				put32(data, 36, 0)
				put32(data, 40, 82)
			}),
			want: func(t *testing.T, got LegacyControl) {
				if got.Kind != ControlNewGame || got.BattleID != 12345 || got.ProductID != 7 || !got.IsNewbie || got.GameID != 82 {
					t.Fatalf("new game = %#v", got)
				}
			},
		},
		{
			name: "init game",
			data: controlFrame(messageGM2GLInitGame, 151, func(data []byte) {
				putGLHeader(data, 12345, 99)
				put32(data, 34, 7)
				put32(data, 46, 88)
				put32(data, 56, 9)
				put32(data, 104, ^uint32(1))
				put32(data, 108, 10)
				put32(data, 112, 100)
				put32(data, 116, 4)
				put32(data, 120, 8)
				putSuffix(data, 136, 144, []byte("round-1"))
			}),
			want: func(t *testing.T, got LegacyControl) {
				if got.Kind != ControlInitGame || got.BattleID != 12345 || got.UserID != 99 || got.ProductID != 7 || got.MatchID != 88 || got.RoundID != 9 || got.Fee != -2 || got.MaxGameNum != 4 || got.RoundUniCode != "round-1" {
					t.Fatalf("init game = %#v", got)
				}
			},
		},
		{
			name: "players",
			data: controlFrame(messageGM2GLUpdatePlayer, 46+controlPlayerSize, func(data []byte) {
				putGLHeader(data, 12345, 0)
				put32(data, 34, 1)
				putSuffix(data, 38, 46, playerBytes(77, 2, "alice"))
			}),
			want: func(t *testing.T, got LegacyControl) {
				if got.Kind != ControlUpdatePlayers || len(got.Players) != 1 || got.Players[0].UserID != 77 || got.Players[0].SeatID != 2 || got.Players[0].Nickname != "alice" || !got.Players[0].IsAI {
					t.Fatalf("players = %#v", got)
				}
			},
		},
		{
			name: "command",
			data: controlFrame(messageGM2GLCommand, 38, func(data []byte) {
				putGLHeader(data, 12345, 0)
				put32(data, 34, 4)
			}),
			want: func(t *testing.T, got LegacyControl) {
				if got.Kind != ControlCommand || got.Command != 4 {
					t.Fatalf("command = %#v", got)
				}
			},
		},
		{
			name: "update and start",
			data: controlFrame(messageGM2GLStartNewGame, 50+4, func(data []byte) {
				putGLHeader(data, 12345, 0)
				put32(data, 34, 60)
				put32(data, 38, 3)
				putSuffix(data, 42, 50, []byte("room"))
			}),
			want: func(t *testing.T, got LegacyControl) {
				if got.Kind != ControlStartNewGame || got.SecRoundTotal != 60 || got.SecRoundUsed != 3 || got.RoomInfo != "room" {
					t.Fatalf("start = %#v", got)
				}
			},
		},
		{
			name: "delete",
			data: controlFrame(messageGM2GLDeleteGame, 28, func(data []byte) { put32(data, 24, 12345) }),
			want: func(t *testing.T, got LegacyControl) {
				if got.Kind != ControlDeleteGame || got.BattleID != 12345 {
					t.Fatalf("delete = %#v", got)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DecodeControl(test.data)
			if err != nil {
				t.Fatal(err)
			}
			test.want(t, got)
		})
	}
}

func TestDecodeControlRejectsMalformedSuffixAndHugePlayerCount(t *testing.T) {
	frame := controlFrame(messageGM2GLUpdatePlayer, 46, func(data []byte) {
		putGLHeader(data, 1, 0)
		put32(data, 34, ^uint32(0))
		putSuffix(data, 38, 46, nil)
	})
	if _, err := DecodeControl(frame); err == nil {
		t.Fatal("expected player count rejection")
	}
	init := controlFrame(messageGM2GLInitGame, 144, func(data []byte) {
		putGLHeader(data, 1, 0)
		putSuffix(data, 136, 144, []byte("wrong"))
	})
	init[20] = 0
	if _, err := DecodeControl(init); err == nil {
		t.Fatal("expected header length rejection")
	}
}

func controlFrame(message uint32, length int, fill func([]byte)) []byte {
	data := make([]byte, length)
	put32(data, 12, message)
	put32(data, 20, uint32(length))
	if fill != nil {
		fill(data)
	}
	return data
}

func putGLHeader(data []byte, battleID, userID uint32) {
	put16(data, 24, controlGLHeaderSize)
	put32(data, 26, battleID)
	put32(data, 30, userID)
}

func putSuffix(data []byte, index, fixed int, value []byte) {
	put32(data, index, uint32(fixed))
	put32(data, index+4, uint32(len(value)))
	copy(data[fixed:], value)
}

func playerBytes(userID uint32, seat uint8, nickname string) []byte {
	data := make([]byte, controlPlayerSize)
	put32(data, 0, userID)
	data[12] = seat
	data[21] = 1
	copy(data[42:], nickname)
	return data
}

func put16(data []byte, offset int, value int) {
	binary.LittleEndian.PutUint16(data[offset:offset+2], uint16(value))
}
func put32(data []byte, offset int, value uint32) {
	binary.LittleEndian.PutUint32(data[offset:offset+4], value)
}
