package main

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func BenchmarkKickSingleBattle(b *testing.B) {
	runtime, refs := newBenchmarkBattles(b, 1)
	request := KickRequest{Player: "alice", Shrew: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := runtime.Call(context.Background(), refs[0], KickCommand, request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkKickAcrossBattles(b *testing.B) {
	runtime, refs := newBenchmarkBattles(b, 64)
	request := KickRequest{Player: "alice", Shrew: 1}
	var next atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			ref := refs[next.Add(1)%uint64(len(refs))]
			if _, err := runtime.Call(context.Background(), ref, KickCommand, request); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func newBenchmarkBattles(b *testing.B, count int) (*gsr.Runtime, []gsr.ServiceRef) {
	b.Helper()
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "whack-benchmark", Workers: 4})
	b.Cleanup(func() { _ = runtime.Close(context.Background()) })
	refs := make([]gsr.ServiceRef, count)
	for index := range refs {
		battle, err := game.NewBattleService(game.BattleConfig{
			ID:           game.BattleID(index + 1),
			Participants: []game.Participant{{Player: "alice"}},
			Logic:        newWhackMoleLogic(uint64(index + 1)),
		})
		if err != nil {
			b.Fatal(err)
		}
		ref, err := runtime.CreateService(gsr.ServiceSpec{Service: battle})
		if err != nil {
			b.Fatal(err)
		}
		if err := startWhackMole(runtime, ref); err != nil {
			b.Fatal(err)
		}
		refs[index] = ref
	}
	return runtime, refs
}
