package main

import (
	"context"
	"log"
	"os"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/monitor"
)

const cmdEcho gsr.CommandID = 1

func main() {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	defer runtime.Close(context.Background())

	ref, err := runtime.CreateService(gsr.ServiceSpec{Name: "echo", Service: echoService{}})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := runtime.Call(context.Background(), ref, cmdEcho, "hello monitor"); err != nil {
		log.Fatal(err)
	}

	localMonitor, err := monitor.New(runtime)
	if err != nil {
		log.Fatal(err)
	}
	if err := localMonitor.WriteJSON(os.Stdout); err != nil {
		log.Fatal(err)
	}
}

type echoService struct{}

func (echoService) Commands() []gsr.CommandID { return []gsr.CommandID{cmdEcho} }
func (echoService) Init(context gsr.ServiceContext) error {
	context.Metrics().Inc("monitor_example_started_total")
	return nil
}
func (echoService) Handle(context gsr.CommandContext, command gsr.Command) error {
	return context.Reply(command.Payload)
}
func (echoService) Stop(context.Context) error { return nil }
func (echoService) Close() error               { return nil }
