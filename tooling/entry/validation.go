package entry

import (
	"strings"
	"unicode/utf8"
)

func validText(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != ""
}

func validIdentity(identity AuthIdentity) bool {
	return validText(identity.AccountID) && validText(identity.PlayerID) && validText(identity.Server)
}

func validTicket(ticket LoginTicket) bool {
	return validText(ticket.UID) && validText(ticket.SubID) && validText(ticket.Server) && ticket.SecretRef != "" && ticket.Generation != 0 && !ticket.ExpiresAt.IsZero()
}

func validProofFields(proof GatewayProof) bool {
	return validText(proof.UID) && validText(proof.SubID) && validText(proof.Server) && proof.Generation != 0 && proof.Sequence != 0
}
