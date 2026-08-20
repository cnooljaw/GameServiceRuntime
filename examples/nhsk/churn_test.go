package nhsk

import (
	"context"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const nhskBattleChurnCount = 100_000

type churnRandom struct{}

func (churnRandom) Intn(n int) int {
	if n <= 0 {
		panic("invalid random bound")
	}
	return 0
}

func (churnRandom) Shuffle(int, func(int, int)) {}

func TestBattleServiceChurnReturnsRuntimeResourcesToBaseline(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{
		NodeID:          "nhsk-churn",
		Workers:         2,
		MailboxSize:     8,
		ShutdownTimeout: 30 * time.Second,
	})
	t.Cleanup(func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Errorf("close Runtime: %v", err)
		}
	})

	goruntime.GC()
	baselineGoroutines := goruntime.NumGoroutine()
	var firstRef, lastRef gsr.ServiceRef
	for index := 0; index < nhskBattleChurnCount; index++ {
		battle, err := NewBattleService(NHSKBattleConfig{
			ID:     game.BattleID(index%10_000 + 1),
			Random: churnRandom{},
		})
		if err != nil {
			t.Fatalf("create Battle %d: %v", index, err)
		}
		ref, err := runtime.CreateService(gsr.ServiceSpec{Service: battle})
		if err != nil {
			t.Fatalf("register Battle %d: %v", index, err)
		}
		if index == 0 {
			firstRef = ref
		}
		lastRef = ref
		if err := runtime.Stop(context.Background(), ref); err != nil {
			t.Fatalf("stop Battle %d: %v", index, err)
		}
	}
	if firstRef == lastRef || lastRef.ID <= firstRef.ID {
		t.Fatalf("ServiceRef did not advance across churn: first=%v last=%v", firstRef, lastRef)
	}

	goruntime.GC()
	inspection := runtime.Inspect()
	if len(inspection.Services) != 0 || len(inspection.Tasks) != 0 || inspection.PendingCalls != 0 || inspection.Timers != 0 {
		t.Fatalf("Runtime resources did not return to baseline: services=%d tasks=%d pending=%d timers=%d", len(inspection.Services), len(inspection.Tasks), inspection.PendingCalls, inspection.Timers)
	}
	if got := goruntime.NumGoroutine(); got > baselineGoroutines+2 {
		t.Fatalf("goroutines after churn = %d, baseline = %d", got, baselineGoroutines)
	}
}
