package main

import (
	"context"
	"fmt"
	"log"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/discovery"
	clustertcp "github.com/lijiawang/GameServiceRuntime/transport/tcp"
)

func main() {
	transportB := clustertcp.New(clustertcp.Config{ListenAddress: "127.0.0.1:0"})
	nodeB, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-b", Workers: 2}, transportB, discovery.NewCodec(nil))
	if err != nil {
		log.Fatal(err)
	}
	defer nodeB.Close(context.Background())

	service, err := discovery.NewService(discovery.Config{LeaseTTL: time.Minute})
	if err != nil {
		log.Fatal(err)
	}
	discoveryRef, err := nodeB.CreateService(gsr.ServiceSpec{Name: discovery.DefaultServiceName, Service: service})
	if err != nil {
		log.Fatal(err)
	}
	configRef, err := nodeB.CreateService(gsr.ServiceSpec{Name: ".config", Service: configService{}})
	if err != nil {
		log.Fatal(err)
	}
	local, err := discovery.NewClient(nodeB, discoveryRef)
	if err != nil {
		log.Fatal(err)
	}
	lease, err := local.RegisterNode(context.Background(), "node-b", transportB.Address())
	if err != nil {
		log.Fatal(err)
	}
	if err := local.RegisterName(context.Background(), lease, ".config", configRef); err != nil {
		log.Fatal(err)
	}

	transportA := clustertcp.New(clustertcp.Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	nodeA, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a", Workers: 2}, transportA, discovery.NewCodec(nil))
	if err != nil {
		log.Fatal(err)
	}
	defer nodeA.Close(context.Background())
	remote, err := discovery.NewClient(nodeA, discoveryRef)
	if err != nil {
		log.Fatal(err)
	}
	resolved, err := remote.ResolveName(context.Background(), ".config")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf(".config -> %s/%d\n", resolved.Node, resolved.ID)
}

type configService struct{}

func (configService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (configService) Init(gsr.ServiceContext) error { return nil }
func (configService) Handle(gsr.CommandContext, gsr.Command) error {
	return nil
}
func (configService) Stop(context.Context) error { return nil }
func (configService) Close() error               { return nil }
