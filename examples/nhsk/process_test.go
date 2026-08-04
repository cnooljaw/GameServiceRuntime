package nhsk

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestNewGameLogicProcessComposesHostFactoryAndConnection(t *testing.T) {
	config := GameLogicProcessConfig{
		NodeID: "nhsk-process-test", Workers: 1, MaxActiveBattles: 8,
		Legacy: LegacyGMConnectionConfig{Address: "gm-test", DialTimeout: time.Second, OriginTimeout: time.Second, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, BackoffMultiplier: 2, Jitter: .2, StableReset: time.Second, Dial: func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("not started") }},
	}
	process, err := NewGameLogicProcess(config)
	if err != nil {
		t.Fatal(err)
	}
	if process.HostRef().ID == 0 || process.Runtime() == nil {
		t.Fatalf("process refs = host=%#v runtime=%v", process.HostRef(), process.Runtime())
	}
	if err := process.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
