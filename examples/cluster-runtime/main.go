package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

const cmdHello gsr.CommandID = 1

func main() {
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 2}, transportB, textCodec{})
	if err != nil {
		log.Fatal(err)
	}
	defer nodeB.Close(context.Background())

	target, err := nodeB.CreateService(gsr.ServiceSpec{Service: helloService{}})
	if err != nil {
		log.Fatal(err)
	}

	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 2}, transportA, textCodec{})
	if err != nil {
		log.Fatal(err)
	}
	defer nodeA.Close(context.Background())

	result, err := nodeA.Call(context.Background(), target, cmdHello, "cluster")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result)
}

type textCodec struct{}

func (textCodec) Encode(_ gsr.CommandID, _ bool, value any) ([]byte, error) {
	text, ok := value.(string)
	if !ok {
		return nil, errors.New("text codec expects string")
	}
	return []byte(text), nil
}

func (textCodec) Decode(_ gsr.CommandID, _ bool, payload []byte) (any, error) {
	return string(payload), nil
}

type helloService struct{}

func (helloService) Init(gsr.ServiceContext) error { return nil }
func (helloService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	if command.ID != cmdHello {
		return gsr.ErrUnknownCommand
	}
	return ctx.Reply("hello " + command.Payload.(string))
}
func (helloService) Stop(context.Context) error { return nil }
func (helloService) Close() error               { return nil }
