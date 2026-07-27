package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/entry"
)

const echoCommand gsr.CommandID = 0x04000101

func main() {
	ctx := context.Background()
	runtime := gsr.NewRuntime(gsr.Config{})
	defer runtime.Close(ctx)

	received := make(chan entry.SessionIdentity, 1)
	target, err := runtime.CreateService(gsr.ServiceSpec{Service: &echoService{received: received}})
	must(err)
	registry, err := entry.NewInMemorySessionRegistry(entry.RegistryConfig{})
	must(err)
	loginService, err := entry.NewLoginService(entry.LoginServiceConfig{Registry: registry})
	must(err)
	loginRef, err := runtime.CreateService(gsr.ServiceSpec{Service: loginService})
	must(err)
	issuer, err := entry.NewLoginClient(runtime, loginRef)
	must(err)

	gatewayListener, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	gateway, err := entry.NewGatewayAdapter(entry.GatewayAdapterConfig{Listener: gatewayListener, Registry: registry, Mapper: echoMapper{target: target}, Dispatcher: runtime})
	must(err)
	must(gateway.Start())
	defer gateway.Close(ctx)

	loginListener, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	login, err := entry.NewLoginAdapter(entry.LoginAdapterConfig{
		Listener:         loginListener,
		Handshake:        exampleHandshake{},
		Registry:         registry,
		Issuer:           issuer,
		ConnectionCloser: gateway,
	})
	must(err)
	must(login.Start())
	defer login.Close(ctx)

	ticket := requestTicket(loginListener.Addr().String())
	connection, err := net.Dial("tcp", gatewayListener.Addr().String())
	must(err)
	defer connection.Close()
	secret := []byte("01234567890123456789012345678901")
	proof := entry.SignGatewayProof(secret, entry.GatewayProof{UID: ticket.uid, SubID: ticket.subID, Server: ticket.server, Generation: ticket.generation, Sequence: 1})
	authLine, err := entry.FormatAuthLine(proof)
	must(err)
	_, err = connection.Write([]byte(authLine))
	must(err)
	mustResponse(connection, "OK\n")
	_, err = connection.Write([]byte("PING\n"))
	must(err)
	mustResponse(connection, "OK\n")

	select {
	case identity := <-received:
		fmt.Printf("player=%s generation=%d\n", identity.PlayerID, identity.Generation)
	case <-time.After(time.Second):
		must(errors.New("timed out waiting for routed Command"))
	}
}

type exampleHandshake struct{}

func (exampleHandshake) Accept(context.Context, net.Conn) (entry.VerifiedLogin, error) {
	return entry.VerifiedLogin{
		Identity:  entry.AuthIdentity{AccountID: "account-1", PlayerID: "player-1", Server: "local"},
		Secret:    []byte("01234567890123456789012345678901"),
		ExpiresAt: time.Now().Add(time.Minute),
	}, nil
}

type echoMapper struct{ target gsr.ServiceRef }

func (m echoMapper) Map(identity entry.SessionIdentity, packet entry.ClientPacket) (entry.Route, error) {
	if string(packet.Payload) != "PING" {
		return entry.Route{}, errors.New("unknown client packet")
	}
	return entry.Route{Target: m.target, Command: echoCommand, Payload: identity}, nil
}

type echoService struct{ received chan<- entry.SessionIdentity }

func (*echoService) Init(gsr.ServiceContext) error { return nil }
func (s *echoService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	if command.ID != echoCommand {
		return gsr.ErrUnknownCommand
	}
	identity, ok := command.Payload.(entry.SessionIdentity)
	if !ok {
		return errors.New("missing session identity")
	}
	s.received <- identity
	return nil
}
func (*echoService) Stop(context.Context) error { return nil }
func (*echoService) Close() error               { return nil }

type clientTicket struct {
	uid        string
	subID      string
	server     string
	generation uint64
}

func requestTicket(address string) clientTicket {
	connection, err := net.Dial("tcp", address)
	must(err)
	defer connection.Close()
	line := readLine(connection)
	fields := strings.Split(strings.TrimSuffix(line, "\n"), " ")
	if len(fields) != 6 || fields[0] != "TICKET" {
		must(errors.New("invalid ticket response"))
	}
	decode := func(value string) string {
		decoded, err := base64.RawURLEncoding.DecodeString(value)
		must(err)
		return string(decoded)
	}
	generation, err := strconv.ParseUint(fields[4], 10, 64)
	must(err)
	if generation == 0 {
		must(errors.New("invalid ticket generation"))
	}
	return clientTicket{uid: decode(fields[1]), subID: decode(fields[2]), server: decode(fields[3]), generation: generation}
}

func mustResponse(connection net.Conn, expected string) {
	if line := readLine(connection); line != expected {
		must(fmt.Errorf("response = %q, want %q", line, expected))
	}
}

func readLine(connection net.Conn) string {
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	line, err := bufio.NewReader(connection).ReadString('\n')
	must(err)
	return line
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
