package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func main() {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "whackmole", Workers: 2})
	defer runtime.Close(context.Background())
	runner, err := game.NewLedgerRunner(runtime, game.LedgerRunnerConfig{Store: game.NewMemoryLedgerStore(), Workers: 1, QueueSize: 8, Timeout: time.Second})
	if err != nil {
		panic(err)
	}
	defer runner.Close(context.Background())
	wallet, err := game.NewWalletService(game.WalletConfig{Executor: runner, MaxPending: 8, RunnerNode: "whackmole"})
	if err != nil {
		panic(err)
	}
	walletRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "wallet", Service: wallet})
	if err != nil {
		panic(err)
	}
	battle, err := game.NewBattleService(game.BattleConfig{ID: "whackmole-demo", Participants: []game.Participant{{Player: "alice"}}, Wallet: walletRef, Logic: newWhackMoleLogic(7)})
	if err != nil {
		panic(err)
	}
	battleRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "battle", Service: battle})
	if err != nil {
		panic(err)
	}
	if _, err = runtime.Call(context.Background(), battleRef, game.StartBattleCommand, struct{}{}); err != nil {
		panic(err)
	}
	if _, err = runtime.Call(context.Background(), battleRef, StartCommand, struct{}{}); err != nil {
		panic(err)
	}
	value, err := runtime.Call(context.Background(), battleRef, KickCommand, KickRequest{Player: "alice", Shrew: 1, Epoch: 1})
	if err != nil {
		panic(err)
	}
	fmt.Printf("whackmole kick: %#v\n", value)
}
