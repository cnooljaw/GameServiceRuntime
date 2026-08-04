package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk"
)

func main() {
	configPath := flag.String("config", "examples/nhsk/config.example.json", "NHSK GameLogic JSON config")
	flag.Parse()
	config, err := nhsk.LoadGameLogicProcessConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	process, err := nhsk.NewGameLogicProcess(config)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := process.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
