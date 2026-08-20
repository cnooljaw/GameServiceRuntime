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
		actor, err := replaySourceActor(move.Source)
		if err != nil {
			return nil, err
		}
		if actor != "" {
			node.addAttribute("Actor", actor)
		}
		moves.children = append(moves.children, node)
	}
	return moves, nil
}

func replaySourceActor(source ReplayMoveSource) (string, error) {
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
