package entry

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"reflect"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// TicketRegistry atomically issues a ticket and, when supplied, revokes its preceding generation.
type TicketRegistry interface {
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
	ticket, err := s.issue(request.Issue)
	return context.Reply(ticketResponse{Ticket: ticket, Error: responseFromError(err)})
}

func (*loginService) Stop(context.Context) error { return nil }

func (s *loginService) Close() error {
	s.context = nil
	s.current = nil
	return nil
}

func (s *loginService) issue(request IssueTicket) (LoginTicket, error) {
	if !validIdentity(request.Identity) || request.SecretRef == "" || request.ExpiresAt.IsZero() {
		return LoginTicket{}, ErrInvalidTicket
	}
	if !request.ExpiresAt.After(s.context.Now()) {
		return LoginTicket{}, ErrTicketExpired
	}
	key := accountServerKey{account: request.Identity.AccountID, server: request.Identity.Server}
	previous, hasPrevious := s.current[key]
	generation := uint64(1)
	if hasPrevious {
		generation = previous.Generation + 1
		if generation == 0 {
			generation++
		}
	}
	uid, err := newOpaqueID()
	if err != nil {
		return LoginTicket{}, err
	}
	subID, err := newOpaqueID()
	if err != nil {
		return LoginTicket{}, err
	}
	ticket := LoginTicket{UID: uid, SubID: subID, Server: request.Identity.Server, SecretRef: request.SecretRef, Generation: generation, ExpiresAt: request.ExpiresAt}
	var previousPtr *LoginTicket
	if hasPrevious {
		previousPtr = &previous
	}
	if _, err := s.registry.Replace(ticket, request.Identity, previousPtr); err != nil {
		return LoginTicket{}, err
	}
	s.current[key] = ticket
	return ticket, nil
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
