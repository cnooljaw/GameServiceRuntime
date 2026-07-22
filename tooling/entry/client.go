package entry

import (
	"context"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// CommandCaller is the narrow Runtime Call capability used by LoginClient.
type CommandCaller interface {
	Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

// LoginClient provides typed ticket issuance to one LoginService.
type LoginClient struct {
	caller CommandCaller
	target gsr.ServiceRef
}

// NewLoginClient binds a LoginClient to a concrete LoginService reference.
func NewLoginClient(caller CommandCaller, target gsr.ServiceRef) (*LoginClient, error) {
	if nilInterface(caller) || target.Node == "" || target.ID == 0 {
		return nil, ErrInvalidConfig
	}
	return &LoginClient{caller: caller, target: target}, nil
}

// IssueTicket submits verified, secret-free login material and returns the ticket plus any revoked Gateway connection.
func (c *LoginClient) IssueTicket(ctx context.Context, issue IssueTicket) (TicketIssue, error) {
	if ctx == nil || !validIdentity(issue.Identity) || issue.SecretRef == "" || issue.ExpiresAt.IsZero() {
		return TicketIssue{}, ErrInvalidTicket
	}
	value, err := c.caller.Call(ctx, c.target, issueTicketCommand, issueTicketRequest{Issue: issue})
	if err != nil {
		return TicketIssue{}, err
	}
	response, ok := value.(ticketResponse)
	if !ok {
		return TicketIssue{}, ErrUnauthorized
	}
	if err := errorFromResponse(response.Error); err != nil {
		return TicketIssue{}, err
	}
	if !validTicket(response.Issue.Ticket) || response.Issue.Ticket.Server != issue.Identity.Server || response.Issue.Ticket.SecretRef != issue.SecretRef || !response.Issue.Ticket.ExpiresAt.Equal(issue.ExpiresAt) {
		return TicketIssue{}, ErrUnauthorized
	}
	return response.Issue, nil
}
