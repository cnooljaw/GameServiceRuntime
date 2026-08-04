package nhsk

import (
	"context"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestHostCreatesResolvesAndStopsBattleThroughFactoryService(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk-host-test", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	factory, err := NewBattleFactoryService(runtime, runtime)
	if err != nil {
		t.Fatal(err)
	}
	factoryRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "nhsk-factory", Service: factory})
	if err != nil {
		t.Fatal(err)
	}
	host, err := NewNHSKHostService(NHSKHostConfig{MaxActiveBattles: 2, FactoryRef: factoryRef})
	if err != nil {
		t.Fatal(err)
	}
	hostRef, err := runtime.CreateService(gsr.ServiceSpec{Name: ".nhsk-game-host", Service: host})
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Call(context.Background(), hostRef, BeginCreateBattleCommand, CreateBattleRequest{BattleID: 33})
	if err != nil {
		t.Fatal(err)
	}
	operation := value.(CreateBattleOperation)
	if operation.Phase != HostOperationCreating || operation.OperationID == 0 {
		t.Fatalf("create operation = %#v", operation)
	}
	if !waitForBattleRef(t, runtime, hostRef, 33) {
		t.Fatal("battle was not created")
	}
	resolved, err := runtime.Call(context.Background(), hostRef, ResolveBattleCommand, ResolveBattleRequest{BattleID: 33})
	if err != nil {
		t.Fatal(err)
	}
	ref := resolved.(ResolveBattleResult).Ref
	if ref.ID == 0 || ref.Node != "nhsk-host-test" {
		t.Fatalf("resolved ref = %#v", ref)
	}
	if _, err := runtime.Call(context.Background(), hostRef, RequestDeleteBattleCommand, RequestDeleteBattleRequest{BattleID: 33, Ref: ref}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, err := runtime.Call(context.Background(), hostRef, ResolveBattleCommand, ResolveBattleRequest{BattleID: 33})
		if err == gsr.ErrServiceNotFound || err == gsr.ErrServiceClosed {
			return
		}
	}
	t.Fatal("battle was not stopped")
}

func waitForBattleRef(t *testing.T, runtime *gsr.Runtime, host gsr.ServiceRef, battleID game.BattleID) bool {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		value, err := runtime.Call(context.Background(), host, ResolveBattleCommand, ResolveBattleRequest{BattleID: battleID})
		if err == nil && value.(ResolveBattleResult).Ref.ID != 0 {
			return true
		}
	}
	return false
}
