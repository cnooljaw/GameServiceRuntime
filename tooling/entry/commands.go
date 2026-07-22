package entry

import (
	"errors"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const issueTicketCommand gsr.CommandID = 0x02000401

type issueTicketRequest struct{ Issue IssueTicket }

type ticketResponse struct {
	Ticket LoginTicket
	Error  responseError
}

type responseError uint8

const (
	responseOK responseError = iota
	responseInvalidIdentity
	responseInvalidTicket
	responseUnauthorized
	responseTicketExpired
	responseSessionRevoked
	responseInvalidProof
	responseProofReplay
	responseSessionCapacity
)

func responseFromError(err error) responseError {
	switch {
	case err == nil:
		return responseOK
	case errors.Is(err, ErrInvalidIdentity):
		return responseInvalidIdentity
	case errors.Is(err, ErrInvalidTicket):
		return responseInvalidTicket
	case errors.Is(err, ErrUnauthorized):
		return responseUnauthorized
	case errors.Is(err, ErrTicketExpired):
		return responseTicketExpired
	case errors.Is(err, ErrSessionRevoked):
		return responseSessionRevoked
	case errors.Is(err, ErrInvalidProof):
		return responseInvalidProof
	case errors.Is(err, ErrProofReplay):
		return responseProofReplay
	default:
		return responseSessionCapacity
	}
}

func errorFromResponse(code responseError) error {
	switch code {
	case responseOK:
		return nil
	case responseInvalidIdentity:
		return ErrInvalidIdentity
	case responseInvalidTicket:
		return ErrInvalidTicket
	case responseUnauthorized:
		return ErrUnauthorized
	case responseTicketExpired:
		return ErrTicketExpired
	case responseSessionRevoked:
		return ErrSessionRevoked
	case responseInvalidProof:
		return ErrInvalidProof
	case responseProofReplay:
		return ErrProofReplay
	default:
		return ErrSessionCapacity
	}
}
