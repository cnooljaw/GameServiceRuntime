package main

import (
	"context"
	"fmt"
	"log"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const cmdEcho gsr.CommandID = 1

func main() {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 2, MailboxSize: 32})
	defer runtime.Close(context.Background())

	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: echoService{}})
	if err != nil {
		log.Fatal(err)
	}

	result, err := runtime.Call(context.Background(), ref, cmdEcho, "hello")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result)
}

type echoService struct{}

func (echoService) Init(gsr.ServiceContext) error { return nil }
func (echoService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	if command.ID == cmdEcho {
		return ctx.Reply(command.Payload)
	}
	return nil
}
func (echoService) Stop(context.Context) error { return nil }
func (echoService) Close() error               { return nil }
