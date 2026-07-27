package main

import (
	"context"
	"fmt"
	"log"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/drain"
)

const commandAcquireVisitorLease gsr.CommandID = 1

func main() {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer func() { _ = runtime.Close(context.Background()) }()

	registry, err := drain.NewVisitorRegistryService(drain.VisitorRegistryConfig{})
	if err != nil {
		log.Fatal(err)
	}
	registryRef, err := runtime.CreateService(gsr.ServiceSpec{
		Name:    drain.DefaultVisitorRegistryName,
		Service: registry,
	})
	if err != nil {
		log.Fatal(err)
	}
	targetRef, err := runtime.CreateService(gsr.ServiceSpec{Service: targetService{}})
	if err != nil {
		log.Fatal(err)
	}
	visitorRef, err := runtime.CreateService(gsr.ServiceSpec{Service: &visitorService{
		registry: registryRef,
		target:   targetRef,
	}})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := runtime.Call(context.Background(), visitorRef, commandAcquireVisitorLease, struct{}{}); err != nil {
		log.Fatal(err)
	}

	client, err := drain.NewClient(runtime, registryRef)
	if err != nil {
		log.Fatal(err)
	}
	visitors, err := client.List(context.Background(), targetRef)
	if err != nil {
		log.Fatal(err)
	}
	strong := 0
	for _, visitor := range visitors {
		if !visitor.Weak {
			strong++
		}
	}
	fmt.Printf("target=%s/%d strong=%d\n", targetRef.Node, targetRef.ID, strong)
}

type targetService struct{}

func (targetService) Init(gsr.ServiceContext) error {
	return nil
}
func (targetService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (targetService) Stop(context.Context) error                   { return nil }
func (targetService) Close() error                                 { return nil }

type visitorService struct {
	context  gsr.ServiceContext
	registry gsr.ServiceRef
	target   gsr.ServiceRef
}

func (s *visitorService) Init(serviceContext gsr.ServiceContext) error {
	s.context = serviceContext
	return nil
}

func (s *visitorService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandAcquireVisitorLease {
		return gsr.ErrUnknownCommand
	}
	if _, ok := command.Payload.(struct{}); !ok {
		return drain.ErrInvalidLease
	}
	client, err := drain.NewClient(s.context, s.registry)
	if err != nil {
		return err
	}
	lease, err := client.Acquire(context.Background(), s.target, commandContext.Self(), false)
	if err != nil {
		return err
	}
	return commandContext.Reply(lease)
}

func (*visitorService) Stop(context.Context) error { return nil }
func (*visitorService) Close() error               { return nil }
