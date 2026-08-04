package legacywire

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	messageGM2GLInitGame         uint32 = 0x8600
	messageGM2GLUpdatePlayer     uint32 = 0x8601
	messageGM2GLCommand          uint32 = 0x8602
	messageGM2GLChangeSeat       uint32 = 0x8603
	messageGM2GLUpdateGame       uint32 = 0x8604
	messageGM2GLGameMessage      uint32 = 0x8605
	messageGM2GLPlayerExit       uint32 = 0x8606
	messageGM2GLReplayMove       uint32 = 0x8607
	messageGM2GLPlayerMoneyLimit uint32 = 0x8608
	messageGM2GLChangeRound      uint32 = 0x8609
	messageGM2GLWithholdMoney    uint32 = 0x860a
	messageGM2GLWithholdRefund   uint32 = 0x860b
	messageGM2GLTakePoints       uint32 = 0x860c
	messageGM2GLStartNewGame     uint32 = 0x860d
	messageGM2GLDelOneGame       uint32 = 0x860e
	messageGM2GLDress            uint32 = 0x8610
	messageGM2GLNewGame          uint32 = 0x86c1
	messageGM2GLDeleteGame       uint32 = 0x86c2
	messageGM2GLSettlementAck    uint32 = 0x80008650
	controlGLHeaderSize                 = 34
	controlSuffixSize                   = 8
	controlPlayerSize                   = 90
)

// ControlKind identifies one supported old GameMaster control message.
type ControlKind uint8

const (
	// ControlNewGame creates a Battle binding.
	ControlNewGame ControlKind = iota + 1
	// ControlInitGame freezes Battle identity and configuration.
	ControlInitGame
	// ControlUpdatePlayers updates all or part of the four seat records.
	ControlUpdatePlayers
	// ControlCommand carries the old Round command integer.
	ControlCommand
	// ControlUpdateGame updates current GameNum/SubgameNum.
	ControlUpdateGame
	// ControlStartNewGame updates next-round context.
	ControlStartNewGame
	// ControlDress updates opaque player dress data.
	ControlDress
	// ControlPlayerExit marks a player exited.
	ControlPlayerExit
	// ControlDeleteGame requests exact Battle deletion.
	ControlDeleteGame
	// ControlSettlementAck carries the old comprehensive settlement metadata.
	ControlSettlementAck
	// ControlUnsupported identifies a known old control MessageID that this
	// example intentionally ignores because it has no NHSK domain equivalent.
	ControlUnsupported
)

// LegacyControl is a normalized fixed-wire control message. Variable suffixes
// remain copied bytes until the outer adapter maps them to typed domain values.
type LegacyControl struct {
	Kind     ControlKind
	Type     uint32
	BattleID uint32
	UserID   uint32

	ProductID uint32
	GameID    uint32
	IsNewbie  bool

	MatchID          uint32
	RoundID          uint32
	RoundUniCode     string
	MaxGameNum       uint32
	MaxSubgameNum    uint32
	Fee              int32
	ScoreBase        int32
	ScoreDenominator int32

	Players       []LegacyPlayer
	GameNum       uint32
	SubgameNum    uint32
	SecRoundTotal uint32
	SecRoundUsed  uint32
	RoomInfo      string
	Dress         string
	ForceExit     uint8
	Command       int32

	SettlementSuccess  bool
	ResultType         int32
	ResultCount        int32
	PlayerCount        int32
	TeamCount          int32
	ResultSuffix       []byte
	PlayerResultSuffix []byte
}

// LegacyPlayer is the fixed 90-byte old TPLAYERINFO projection.
type LegacyPlayer struct {
	UserID            uint32
	ClientID          uint32
	ConnectionID      uint32
	SeatID            uint8
	Score             int32
	Flag              uint32
	IsAI              bool
	ScoreChangeReason int32
	PlayerState       int32
	PlayerFlag        int32
	ScoreChange       int32
	Exp               int32
	Nickname          string
}

var errUnsupportedControl = errors.New("legacywire: unsupported GameMaster control message")

// DecodeControl decodes one exact old GameMaster→GameLogic control frame.
func DecodeControl(data []byte) (LegacyControl, error) {
	if len(data) < headerSize || len(data) > maxFrameSize {
		return LegacyControl{}, fmt.Errorf("legacywire: control frame length %d", len(data))
	}
	header, err := decodeHeader(data)
	if err != nil || uint64(header.Length) != uint64(len(data)) {
		return LegacyControl{}, fmt.Errorf("legacywire: control header length")
	}
	control := LegacyControl{Type: header.Type}
	switch header.Type {
	case messageGM2GLNewGame:
		if len(data) != 44 {
			return LegacyControl{}, fmt.Errorf("legacywire: NEW_GAME length %d", len(data))
		}
		control.Kind = ControlNewGame
		control.BattleID = binary.LittleEndian.Uint32(data[24:28])
		control.ProductID = binary.LittleEndian.Uint32(data[28:32])
		control.IsNewbie = binary.LittleEndian.Uint32(data[32:36]) != 0
		control.GameID = binary.LittleEndian.Uint32(data[40:44])
		return control, nil
	case messageGM2GLDeleteGame:
		if len(data) != 28 {
			return LegacyControl{}, fmt.Errorf("legacywire: DEL_GAME length %d", len(data))
		}
		control.Kind, control.BattleID = ControlDeleteGame, binary.LittleEndian.Uint32(data[24:28])
		return control, nil
	case messageGM2GLChangeSeat, messageGM2GLReplayMove, messageGM2GLPlayerMoneyLimit,
		messageGM2GLChangeRound, messageGM2GLWithholdMoney, messageGM2GLWithholdRefund,
		messageGM2GLTakePoints, messageGM2GLDelOneGame, messageGM2GLGameMessage:
		control.Kind = ControlUnsupported
		return control, nil
	}
	if len(data) < controlGLHeaderSize || binary.LittleEndian.Uint16(data[24:26]) != controlGLHeaderSize {
		return LegacyControl{}, fmt.Errorf("legacywire: control GLHeader length")
	}
	control.BattleID = binary.LittleEndian.Uint32(data[26:30])
	control.UserID = binary.LittleEndian.Uint32(data[30:34])
	switch header.Type {
	case messageGM2GLInitGame:
		if len(data) < 144 {
			return LegacyControl{}, fmt.Errorf("legacywire: INIT_GAME length %d", len(data))
		}
		control.Kind = ControlInitGame
		control.ProductID = binary.LittleEndian.Uint32(data[34:38])
		control.MatchID = binary.LittleEndian.Uint32(data[46:50])
		control.RoundID = binary.LittleEndian.Uint32(data[56:60])
		control.GameID = binary.LittleEndian.Uint32(data[64:68])
		control.Fee = int32(binary.LittleEndian.Uint32(data[104:108]))
		control.ScoreBase = int32(binary.LittleEndian.Uint32(data[108:112]))
		control.ScoreDenominator = int32(binary.LittleEndian.Uint32(data[112:116]))
		control.MaxGameNum = binary.LittleEndian.Uint32(data[116:120])
		control.MaxSubgameNum = binary.LittleEndian.Uint32(data[120:124])
		control.RoundUniCode, err = suffixString(data, 136, 144)
		if err != nil {
			return LegacyControl{}, err
		}
		return control, nil
	case messageGM2GLUpdatePlayer:
		if len(data) < 46 {
			return LegacyControl{}, fmt.Errorf("legacywire: UPDATE_PLAYER length %d", len(data))
		}
		control.Kind = ControlUpdatePlayers
		count := binary.LittleEndian.Uint32(data[34:38])
		suffix, err := suffixBytes(data, 38, 46)
		if err != nil || uint64(count) > uint64((maxFrameSize-46)/controlPlayerSize) || uint64(count)*controlPlayerSize != uint64(len(suffix)) {
			return LegacyControl{}, fmt.Errorf("legacywire: UPDATE_PLAYER players")
		}
		control.Players = make([]LegacyPlayer, count)
		for index := range control.Players {
			control.Players[index] = decodeLegacyPlayer(suffix[index*controlPlayerSize : (index+1)*controlPlayerSize])
		}
		return control, nil
	case messageGM2GLCommand:
		if len(data) != 38 {
			return LegacyControl{}, fmt.Errorf("legacywire: COMMAND length %d", len(data))
		}
		control.Kind, control.Command = ControlCommand, int32(binary.LittleEndian.Uint32(data[34:38]))
		return control, nil
	case messageGM2GLUpdateGame:
		if len(data) != 42 {
			return LegacyControl{}, fmt.Errorf("legacywire: UPDATE_GAME length %d", len(data))
		}
		control.Kind, control.GameNum, control.SubgameNum = ControlUpdateGame, binary.LittleEndian.Uint32(data[34:38]), binary.LittleEndian.Uint32(data[38:42])
		return control, nil
	case messageGM2GLStartNewGame:
		if len(data) < 50 {
			return LegacyControl{}, fmt.Errorf("legacywire: START_NEW_GAME length %d", len(data))
		}
		control.Kind = ControlStartNewGame
		control.SecRoundTotal, control.SecRoundUsed = binary.LittleEndian.Uint32(data[34:38]), binary.LittleEndian.Uint32(data[38:42])
		control.RoomInfo, err = suffixString(data, 42, 50)
		if err != nil {
			return LegacyControl{}, err
		}
		return control, nil
	case messageGM2GLDress:
		if len(data) < 42 {
			return LegacyControl{}, fmt.Errorf("legacywire: DRESS length %d", len(data))
		}
		control.Kind = ControlDress
		control.Dress, err = suffixString(data, 34, 42)
		if err != nil {
			return LegacyControl{}, err
		}
		return control, nil
	case messageGM2GLPlayerExit:
		if len(data) != 35 {
			return LegacyControl{}, fmt.Errorf("legacywire: PLAYER_EXIT length %d", len(data))
		}
		control.Kind, control.ForceExit = ControlPlayerExit, data[34]
		return control, nil
	case messageGM2GLSettlementAck:
		if len(data) < 67 {
			return LegacyControl{}, fmt.Errorf("legacywire: settlement length %d", len(data))
		}
		control.Kind = ControlSettlementAck
		control.SettlementSuccess = data[34] != 0
		control.ResultType = int32(binary.LittleEndian.Uint32(data[35:39]))
		control.ResultCount = int32(binary.LittleEndian.Uint32(data[39:43]))
		control.PlayerCount = int32(binary.LittleEndian.Uint32(data[43:47]))
		control.TeamCount = int32(binary.LittleEndian.Uint32(data[47:51]))
		control.ResultSuffix, err = suffixBytes(data, 51, 59)
		if err != nil {
			return LegacyControl{}, err
		}
		control.PlayerResultSuffix, err = suffixBytes(data, 59, 67)
		if err != nil {
			return LegacyControl{}, err
		}
		return control, nil
	default:
		return LegacyControl{}, fmt.Errorf("%w: %#x", errUnsupportedControl, header.Type)
	}
}

func suffixBytes(data []byte, indexOffset, fixed int) ([]byte, error) {
	if indexOffset+controlSuffixSize > len(data) {
		return nil, fmt.Errorf("legacywire: suffix index outside frame")
	}
	offset, size := binary.LittleEndian.Uint32(data[indexOffset:indexOffset+4]), binary.LittleEndian.Uint32(data[indexOffset+4:indexOffset+8])
	if uint64(offset)+uint64(size) != uint64(len(data)) || int(offset) < fixed {
		return nil, fmt.Errorf("legacywire: suffix boundary")
	}
	end := offset + size
	return append([]byte(nil), data[int(offset):int(end)]...), nil
}

func suffixString(data []byte, indexOffset, fixed int) (string, error) {
	value, err := suffixBytes(data, indexOffset, fixed)
	if err != nil {
		return "", err
	}
	for len(value) > 0 && value[len(value)-1] == 0 {
		value = value[:len(value)-1]
	}
	return string(value), nil
}

func decodeLegacyPlayer(data []byte) LegacyPlayer {
	return LegacyPlayer{UserID: binary.LittleEndian.Uint32(data[0:4]), ClientID: binary.LittleEndian.Uint32(data[4:8]), ConnectionID: binary.LittleEndian.Uint32(data[8:12]), SeatID: data[12], Score: int32(binary.LittleEndian.Uint32(data[13:17])), Flag: binary.LittleEndian.Uint32(data[17:21]), IsAI: data[21] != 0, ScoreChangeReason: int32(binary.LittleEndian.Uint32(data[22:26])), PlayerState: int32(binary.LittleEndian.Uint32(data[26:30])), PlayerFlag: int32(binary.LittleEndian.Uint32(data[30:34])), ScoreChange: int32(binary.LittleEndian.Uint32(data[34:38])), Exp: int32(binary.LittleEndian.Uint32(data[38:42])), Nickname: string(trimZero(data[42:90]))}
}

func trimZero(data []byte) []byte {
	end := len(data)
	for end > 0 && data[end-1] == 0 {
		end--
	}
	return data[:end]
}
