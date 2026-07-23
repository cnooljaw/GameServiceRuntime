package entry

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"reflect"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// TicketRegistry recovers the current ticket and atomically replaces it with a new generation.
// Implementations must be local and non-blocking because LoginService calls them from Handle.
type TicketRegistry interface {
	Current(AuthIdentity) (LoginTicket, bool)
	Replace(LoginTicket, AuthIdentity, *LoginTicket) (ConnectionID, error)
}

// LoginServiceConfig configures LoginService ticket policy dependencies.
type LoginServiceConfig struct {
	Registry TicketRegistry
}

// IssueTicket is the verified, secret-free input accepted by LoginService.
type IssueTicket struct {
	Identity  AuthIdentity
	SecretRef SecretRef
	ExpiresAt time.Time
}

type loginService struct {
	registry TicketRegistry
	context  gsr.ServiceContext
	current  map[accountServerKey]LoginTicket
}

type accountServerKey struct {
	account string
	server  string
}

// NewLoginService creates a Mailbox-serialized SingleSession ticket issuer.
func NewLoginService(config LoginServiceConfig) (gsr.Service, error) {
	if nilInterface(config.Registry) {
		return nil, ErrInvalidConfig
	}
	return &loginService{registry: config.Registry, current: make(map[accountServerKey]LoginTicket)}, nil
}

func (*loginService) Commands() []gsr.CommandID { return []gsr.CommandID{issueTicketCommand} }

func (s *loginService) Init(context gsr.ServiceContext) error {
	if nilInterface(context) {
		return ErrInvalidConfig
	}
	s.context = context
	return nil
}

func (s *loginService) Handle(context gsr.CommandContext, command gsr.Command) error {
	if command.ID != issueTicketCommand {
		return gsr.ErrCommandNotRegistered
	}
	request, ok := command.Payload.(issueTicketRequest)
	if !ok {
		return context.Reply(ticketResponse{Error: responseInvalidTicket})
	}
	issue, err := s.issue(request.Issue)
	return context.Reply(ticketResponse{Issue: issue, Error: responseFromError(err)})
}

func (*loginService) Stop(context.Context) error { return nil }

func (s *loginService) Close() error {
	s.context = nil
	s.current = nil
	return nil
}

func (s *loginService) issue(request IssueTicket) (TicketIssue, error) {
	if !validIdentity(request.Identity) || request.SecretRef == "" || request.ExpiresAt.IsZero() {
		return TicketIssue{}, ErrInvalidTicket
	}
	if !request.ExpiresAt.After(s.context.Now()) {
		return TicketIssue{}, ErrTicketExpired
	}
	key := accountServerKey{account: request.Identity.AccountID, server: request.Identity.Server}
	previous, hasPrevious := s.current[key]
	if !hasPrevious {
		previous, hasPrevious = s.registry.Current(request.Identity)
	}
	generation := uint64(1)
	if hasPrevious {
		generation = previous.Generation + 1
		if generation == 0 {
			generation++
		}
	}
	uid, err := newOpaqueID()
	if err != nil {
		return TicketIssue{}, err
	}
	subID, err := newOpaqueID()
	if err != nil {
		return TicketIssue{}, err
	}
	ticket := LoginTicket{UID: uid, SubID: subID, Server: request.Identity.Server, SecretRef: request.SecretRef, Generation: generation, ExpiresAt: request.ExpiresAt}
	var previousPtr *LoginTicket
	if hasPrevious {
		previousPtr = &previous
	}
	replaced, err := s.registry.Replace(ticket, request.Identity, previousPtr)
	if err != nil {
		return TicketIssue{}, err
	}
	s.current[key] = ticket
	return TicketIssue{Ticket: ticket, ReplacedConnectionID: replaced}, nil
}

func newOpaqueID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
