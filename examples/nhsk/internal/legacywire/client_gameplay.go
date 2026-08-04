package legacywire

import "fmt"

// ClientGameplayMessage identifies one retained Legacy NHSK client request.
type ClientGameplayMessage uint32

const (
	// ClientGameplayOutCard identifies 0x7701 NHSK OUT_CARD.
	ClientGameplayOutCard ClientGameplayMessage = ClientGameplayMessage(messageNHSKOutCard)
	// ClientGameplayCardAction identifies 0x7702 NHSK CARD_ACTION.
	ClientGameplayCardAction ClientGameplayMessage = ClientGameplayMessage(messageNHSKCardAction)
	// ClientGameplayUserStateChange identifies 0x720A USER_STATE_CHANGE.
	ClientGameplayUserStateChange ClientGameplayMessage = ClientGameplayMessage(messageGameUserStateChange)
)

// DecodeClientGameplayMessage reads the MessageID from one complete client payload.
func DecodeClientGameplayMessage(data []byte) (ClientGameplayMessage, error) {
	if len(data) < headerSize || len(data) > maxFrameSize {
		return 0, fmt.Errorf("legacywire: invalid client gameplay frame length %d", len(data))
	}
	header, err := decodeHeader(data)
	if err != nil {
		return 0, fmt.Errorf("legacywire: decode client gameplay header: %w", err)
	}
	if header.Length == 0 || uint64(header.Length) != uint64(len(data)) {
		return 0, fmt.Errorf("legacywire: client gameplay header length %d does not match %d", header.Length, len(data))
	}
	return ClientGameplayMessage(header.Type), nil
}
