package nhsk

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type replayXMLNode struct {
	name       string
	attributes map[string]string
	children   []*replayXMLNode
}

func marshalReplayMovesXML(document ReplayDocument) ([]byte, error) {
	moves, err := buildReplayMovesNode(document)
	if err != nil {
		return nil, err
	}
	return marshalReplayXMLNode(moves)
}

func marshalReplayDocumentXML(document ReplayDocument) ([]byte, error) {
	root, err := buildReplayDocumentNode(document)
	if err != nil {
		return nil, err
	}
	return marshalReplayXMLNode(root)
}

func buildReplayDocumentNode(document ReplayDocument) (*replayXMLNode, error) {
	if !document.start.Valid() || document.end == nil || document.end.endedAt.IsZero() {
		return nil, fmt.Errorf("nhsk: incomplete replay document")
	}
	root := newReplayXMLNode("Game")
	root.addAttribute("ID", strconv.FormatUint(uint64(NHSKDescriptor.GameID), 10))
	root.addAttribute("GameName", NHSKDescriptor.GameName)
	root.children = append(root.children, buildReplayInfoNode(document.start))
	moves, err := buildReplayMovesNode(document)
	if err != nil {
		return nil, err
	}
	root.children = append(root.children, moves, buildReplayGameOverNode(document.start, *document.end), buildReplaySummaryNode(document.start, *document.end), buildReplayDressNode(document.start), buildReplayOtherNode(document.start, *document.end))
	return root, nil
}

func buildReplayInfoNode(start ReplayStartSnapshot) *replayXMLNode {
	info := newReplayXMLNode("Info")
	info.addAttribute("OwnerID", strconv.FormatUint(uint64(start.ReplayMetadata.CreatorID), 10))
	info.addAttribute("RoomID", strconv.FormatUint(uint64(start.ReplayMetadata.RoomID), 10))
	info.addAttribute("RoundUniCode", start.Identity.RoundUniCode)
	info.addAttribute("UniCode", start.ReplayUID)

	match := newReplayXMLNode("MatchInfo")
	addReplayInt(match, "GameType", int64(start.ReplayMetadata.GameType))
	addReplayInt(match, "MPID", int64(start.Identity.ProductID))
	addReplayInt(match, "MatchID", int64(start.Identity.MatchID))
	addReplayInt(match, "RoundID", int64(start.Identity.RoundID))
	addReplayInt(match, "SecRoundTotal", int64(start.RoundContext.SecRoundTotal))
	addReplayInt(match, "SecRoundUsed", int64(start.RoundContext.SecRoundUsed))
	match.addAttribute("ServiceName", replayUTF8OrGBK(start.ReplayMetadata.MatchName))
	if start.RoundContext.RoomInfo != "" {
		room := newReplayXMLNode("RoomInfo")
		room.addAttribute("Json", start.RoundContext.RoomInfo)
		match.children = append(match.children, room)
	}

	gameInfo := newReplayXMLNode("GameInfo")
	addReplayInt(gameInfo, "BaseScore", int64(start.ScoreBase))
	if start.ScoreDenominator > 0 {
		addReplayInt(gameInfo, "BaseScoreDenominator", int64(start.ScoreDenominator))
	}
	addReplayInt(gameInfo, "Fee", int64(start.Fee))
	addReplayInt(gameInfo, "GameNum", int64(start.GameNum))
	addReplayInt(gameInfo, "MaxGameNum", int64(start.MaxGameNum))
	addReplayInt(gameInfo, "MaxSubGameNum", int64(start.MaxSubgameNum))
	addReplayInt(gameInfo, "ScoreMode", int64(start.ReplayMetadata.ScoreMode))
	addReplayInt(gameInfo, "ScoreType", int64(start.ReplayMetadata.ScoreType))
	addReplayInt(gameInfo, "StartTime", start.StartedAt.Unix())
	addReplayInt(gameInfo, "SubGameNum", int64(start.SubgameNum))
	rule := newReplayXMLNode("GameRule")
	addReplayInt(rule, "GameNumToRandomSeat", int64(start.ReplayRules.GameNumToRandomSeat))
	if start.RoundContext.SecRoundTotal == 0 {
		addReplayInt(rule, "GameNum", int64(start.MaxGameNum))
	} else {
		addReplayInt(rule, "GameTime", int64(start.RoundContext.SecRoundTotal))
	}
	addReplayInt(rule, "PlayerNum", 4)
	addReplayBool(rule, "RandomSeatRoundStart", start.ReplayRules.RandomSeatRoundStart)
	addReplayBool(rule, "TimeOutAutoMove", start.Rules.TimeoutAutoMove)
	addReplayBool(rule, "TimeOutOver", start.ReplayRules.TimeOutOver)
	addReplayBool(rule, "VoiceMode", start.ReplayRules.VoiceMode)
	setting := newReplayXMLNode("GameSetting")
	addReplayInt(setting, "MSecFirstOutCard", start.Rules.MsFirstOutCard.Milliseconds())
	addReplayInt(setting, "MSecOutCard", start.Rules.MsOutCard.Milliseconds())
	gameInfo.children = append(gameInfo.children, rule, setting)

	players := newReplayXMLNode("Players")
	addReplayInt(players, "Count", 4)
	for seat, player := range start.Players {
		node := newReplayXMLNode("Player" + strconv.Itoa(seat))
		addReplayInt(node, "ChairID", int64(seat))
		addReplayInt(node, "InitScore", int64(player.InitScore))
		addReplayInt(node, "Platform", int64(player.Platform))
		addReplayInt(node, "UserID", int64(player.UserID))
		node.addAttribute("UserName", replayUTF8OrGBK(player.Nickname))
		players.children = append(players.children, node)
	}
	info.children = append(info.children, match, gameInfo, players)
	return info
}

func buildReplayGameOverNode(start ReplayStartSnapshot, end replayEndSnapshot) *replayXMLNode {
	node := newReplayXMLNode("GameOver")
	addReplayInt(node, "EndReason", int64(end.result.reason))
	addReplayInt(node, "EndTime", end.endedAt.Unix())
	addReplayInt(node, "GameResult", int64(end.result.result))
	addReplayInt(node, "OverCode", 0)
	addReplayInt(node, "OverStatus", 0)
	addReplayInt(node, "OverUserID", 0)
	node.addAttribute("Reason", "Success")
	addReplayInt(node, "RecordValid", 1)
	node.addAttribute("ResultType", replayResultTypeName(end.result.result))
	node.addAttribute("Scale", "Game")
	for seat, player := range end.players {
		chair := newReplayXMLNode("Chair" + strconv.Itoa(seat))
		addReplayInt(chair, "CatchScore", int64(end.result.points[seat]))
		addReplayBool(chair, "IsAuto", end.result.automated[seat])
		addReplayBool(chair, "IsBreak", player.IsBreak)
		addReplayBool(chair, "IsSeal", player.IsSeal)
		addReplayBool(chair, "IsWin", end.result.multiples[seat] > 0)
		addReplayInt(chair, "Multiple", int64(end.result.multiples[seat]))
		addReplayInt(chair, "Result", int64(end.result.outcomes[seat]))
		addReplayInt(chair, "Score", int64(replayConvertedScore(start, end.result.multiples[seat])))
		addReplayInt(chair, "TotalScore", int64(end.finalScores[seat]))
		addReplayInt(chair, "UserID", int64(player.UserID))
		node.children = append(node.children, chair)
	}
	return node
}

func buildReplaySummaryNode(start ReplayStartSnapshot, end replayEndSnapshot) *replayXMLNode {
	summary := newReplayXMLNode("Summary")
	addReplayInt(summary, "Count", 5)
	keys := [6]string{"BuChu", "DanZhang", "DuiZi", "SanZhang", "FuLu", "ZhaDan"}
	var totalOut, totalAuto uint32
	var totalMS uint64
	for seat, player := range end.players {
		node := newReplayXMLNode("S" + strconv.Itoa(seat))
		addReplayInt(node, "AutoOutCount", int64(end.autoCount[seat]))
		addReplayInt(node, "ChairID", int64(seat))
		addReplayInt(node, "MoveTime", int64(end.moveMilliseconds[seat]))
		addReplayInt(node, "OutCount", int64(end.moveCount[seat]))
		addReplayInt(node, "TotalGameScore", int64(end.finalScores[seat]))
		addReplayInt(node, "UserID", int64(player.UserID))
		addReplayInt(node, "WangCount", 0)
		actions := newReplayXMLNode("Actions")
		for index, key := range keys {
			action := newReplayXMLNode("a" + strconv.Itoa(index))
			addReplayInt(action, "Count", int64(end.actions[seat].counts[index]))
			action.addAttribute("Key", key)
			addReplayInt(action, "Multiple", 0)
			actions.children = append(actions.children, action)
		}
		paiXing := newReplayXMLNode("PaiXing")
		if end.result.multiples[seat] > 0 {
			item := newReplayXMLNode("px0")
			addReplayInt(item, "Count", 1)
			key := "DanKou"
			if end.result.result == SubgameResultDouble {
				key = "ShuangKou"
			}
			item.addAttribute("Key", key)
			addReplayInt(item, "Value", int64(end.result.multiples[seat]))
			paiXing.children = append(paiXing.children, item)
		}
		roundStat := newReplayXMLNode("RoundStat")
		addReplayInt(roundStat, "Count", 3)
		values := [3]int32{end.finalScores[seat], 0, 0}
		if end.result.outcomes[seat] == PlayerOutcomeWin {
			values[1] = 1
		}
		if end.result.result == SubgameResultDouble && end.result.multiples[seat] > 0 {
			values[2] = 1
		}
		exegesis := [3]string{"SumScore", "WinCount", "Double"}
		for index := range values {
			stat := newReplayXMLNode("r" + strconv.Itoa(index))
			stat.addAttribute("Exegesis", exegesis[index])
			addReplayInt(stat, "Key", int64(index+1))
			addReplayInt(stat, "Value", int64(values[index]))
			roundStat.children = append(roundStat.children, stat)
		}
		node.children = append(node.children, actions, paiXing, roundStat)
		summary.children = append(summary.children, node)
		totalOut += end.moveCount[seat]
		totalAuto += end.autoCount[seat]
		totalMS += uint64(end.moveMilliseconds[seat])
	}
	total := newReplayXMLNode("S4")
	addReplayInt(total, "TotalAutoOutCount", int64(totalAuto))
	addReplayInt(total, "TotalMoveTime", int64(totalMS))
	addReplayInt(total, "TotalOutCount", int64(totalOut))
	summary.children = append(summary.children, total)
	return summary
}

func buildReplayDressNode(start ReplayStartSnapshot) *replayXMLNode {
	dress := newReplayXMLNode("Dress")
	for seat, player := range start.Players {
		node := newReplayXMLNode("D" + strconv.Itoa(seat+1))
		node.addAttribute("Data", player.Dress)
		addReplayInt(node, "UserID", int64(player.UserID))
		dress.children = append(dress.children, node)
	}
	return dress
}

func buildReplayOtherNode(start ReplayStartSnapshot, end replayEndSnapshot) *replayXMLNode {
	other := newReplayXMLNode("Other")
	cardDetail := newReplayXMLNode("CardDetail")
	for seat, player := range start.Players {
		node := newReplayXMLNode("P" + strconv.Itoa(seat))
		addReplayInt(node, "ChairID", int64(seat))
		addReplayInt(node, "DetailCount", int64(len(end.cardDetails[seat])))
		addReplayInt(node, "UserID", int64(player.UserID))
		for _, detail := range end.cardDetails[seat] {
			item := newReplayXMLNode("CD")
			item.addAttribute("Cards", replayCardsString(detail.cards))
			addReplayInt(item, "Count", int64(len(detail.cards)))
			item.addAttribute("Type", detail.cardType)
			node.children = append(node.children, item)
		}
		cardDetail.children = append(cardDetail.children, node)
	}
	other.children = append(other.children, cardDetail)
	return other
}

func replayConvertedScore(start ReplayStartSnapshot, multiple int32) int32 {
	if start.ScoreDenominator <= 0 {
		return 0
	}
	return multiple * start.ScoreBase / start.ScoreDenominator
}

func replayResultTypeName(result SubgameResult) string {
	switch result {
	case SubgameResultSingle:
		return "DanKou_1"
	case SubgameResultDouble:
		return "ShuangKou_2"
	case SubgameResultPeace:
		return "PingJu"
	default:
		return ""
	}
}

func addReplayInt(node *replayXMLNode, name string, value int64) {
	node.addAttribute(name, strconv.FormatInt(value, 10))
}

func addReplayBool(node *replayXMLNode, name string, value bool) {
	if value {
		addReplayInt(node, name, 1)
		return
	}
	addReplayInt(node, name, 0)
}

func buildReplayMovesNode(document ReplayDocument) (*replayXMLNode, error) {
	moves := newReplayXMLNode("Moves")
	moves.addAttribute("Count", strconv.Itoa(len(document.moves)))
	for index, move := range document.moves {
		node := newReplayXMLNode("M" + strconv.Itoa(index))
		node.addAttribute("Act", string(move.Kind))
		switch move.Kind {
		case ReplayMoveDeal:
			for seat := 0; seat < len(move.Hands); seat++ {
				deal := newReplayXMLNode("D" + strconv.Itoa(seat))
				deal.addAttribute("ChairID", strconv.Itoa(seat))
				deal.addAttribute("UserID", strconv.FormatUint(uint64(document.start.Players[seat].UserID), 10))
				deal.addAttribute("Cards", replayCardsString(move.Hands[seat]))
				node.children = append(node.children, deal)
			}
		case ReplayMoveCurrentPoint:
			node.addAttribute("Cards", replayCardsString(move.Cards))
			node.addAttribute("Point", strconv.FormatUint(uint64(move.Point), 10))
		case ReplayMoveOutCard:
			node.addAttribute("ChairID", strconv.FormatUint(uint64(move.SeatID), 10))
			node.addAttribute("Cards", replayCardsString(move.Cards))
			node.addAttribute("CardType", move.CardType)
			node.addAttribute("MSec", strconv.FormatUint(uint64(move.MoveMilliseconds), 10))
		case ReplayMoveCatchPoint:
			node.addAttribute("ChairID", strconv.FormatUint(uint64(move.SeatID), 10))
			node.addAttribute("Cards", replayCardsString(move.Cards))
			node.addAttribute("Point", strconv.FormatUint(uint64(move.Point), 10))
		case ReplayMoveTurnEnd:
			node.addAttribute("Scores", replayScoresString(move.Scores))
		default:
			return nil, fmt.Errorf("nhsk: unsupported replay move kind %q", move.Kind)
		}
		legacySource, err := replaySourceAttribute(move.Source)
		if err != nil {
			return nil, err
		}
		if legacySource != "" {
			node.addAttribute("Actor", legacySource)
		}
		moves.children = append(moves.children, node)
	}
	return moves, nil
}

func replaySourceAttribute(source ReplayMoveSource) (string, error) {
	switch source {
	case ReplayMoveSourceUnknown:
		return "", nil
	case ReplayMoveSourceSystem:
		return "系统", nil
	case ReplayMoveSourcePlayer:
		return "玩家", nil
	case ReplayMoveSourceAI:
		return "AI", nil
	case ReplayMoveSourceTimeout:
		return "超时", nil
	case ReplayMoveSourceAuto:
		return "托管", nil
	default:
		return "", fmt.Errorf("nhsk: unsupported replay move source %q", source)
	}
}

func replayCardsString(cards []byte) string {
	parts := make([]string, len(cards))
	for index, card := range cards {
		parts[index] = fmt.Sprintf("0x%02x", card)
	}
	return strings.Join(parts, ",")
}

func replayScoresString(scores [4]uint16) string {
	parts := make([]string, len(scores))
	for index, score := range scores {
		parts[index] = strconv.FormatUint(uint64(score), 10)
	}
	return strings.Join(parts, ",")
}

func newReplayXMLNode(name string) *replayXMLNode {
	return &replayXMLNode{name: name, attributes: make(map[string]string)}
}

func (node *replayXMLNode) addAttribute(name, value string) {
	node.attributes[name] = value
}

func marshalReplayXMLNode(root *replayXMLNode) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString(xml.Header)
	encoder := xml.NewEncoder(&buffer)
	encoder.Indent("", "\t")
	if err := root.encode(encoder); err != nil {
		return nil, err
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (node *replayXMLNode) encode(encoder *xml.Encoder) error {
	start := xml.StartElement{Name: xml.Name{Local: node.name}}
	keys := make([]string, 0, len(node.attributes))
	for key := range node.attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: key}, Value: node.attributes[key]})
	}
	if err := encoder.EncodeToken(start); err != nil {
		return err
	}
	for _, child := range node.children {
		if err := child.encode(encoder); err != nil {
			return err
		}
	}
	return encoder.EncodeToken(start.End())
}
