package nhsk

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
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
	if process.replayWriter == nil || process.diagnosticRunner == nil || process.adminServer == nil {
		t.Fatal("process-owned runners or diagnostic admin were not composed")
	}
	if health := process.Health(); health.Readiness != string(nodeNotReady) || health.GMLinkReady || health.QuarantinedBattles != 0 {
		t.Fatalf("initial health = %#v", health)
	}
	if err := process.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNewGameLogicProcessWiresLocalCustomDeckRunner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-deck.txt")
	if err := os.WriteFile(path, []byte("{\n"+sequentialCustomDeckLine()+"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := GameLogicProcessConfig{
		NodeID: "nhsk-process-custom", Workers: 1, MaxActiveBattles: 2,
		CustomDeck: CustomDeckProcessConfig{Enabled: true, FilePath: path, AllowAnyAccount: true},
		Legacy:     LegacyGMConnectionConfig{Address: "gm-test", DialTimeout: time.Second, OriginTimeout: time.Second, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, BackoffMultiplier: 2, Jitter: .2, StableReset: time.Second, Dial: func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("not started") }},
	}
	process, err := NewGameLogicProcess(config)
	if err != nil {
		t.Fatal(err)
	}
	if process.customDeckRunner == nil {
		t.Fatal("custom deck runner was not composed")
	}
	if err := process.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCustomDeckProviderFromConfigSelectsRedisAdapter(t *testing.T) {
	provider, err := customDeckProviderFromConfig(CustomDeckProcessConfig{Source: "redis", Redis: RedisCustomDeckConfig{Address: "redis-test", DB: 2}})
	if err != nil {
		t.Fatal(err)
	}
	redisProvider, ok := provider.(RedisCustomDeckProvider)
	if !ok {
		t.Fatalf("provider = %T, want RedisCustomDeckProvider", provider)
	}
	getter, ok := redisProvider.Getter.(TCPRedisStringGetter)
	if !ok || getter.Address != "redis-test" || getter.DB != 2 {
		t.Fatalf("getter = %#v", redisProvider.Getter)
	}
}

func TestCustomDeckProviderFromConfigRejectsInvalidRedisConfig(t *testing.T) {
	if _, err := customDeckProviderFromConfig(CustomDeckProcessConfig{Source: "redis", Redis: RedisCustomDeckConfig{Address: "redis-test", DB: -1}}); err == nil {
		t.Fatal("negative Redis DB should fail")
	}
	if _, err := customDeckProviderFromConfig(CustomDeckProcessConfig{Source: "file"}); err == nil {
		t.Fatal("empty file path should fail")
	}
}

func TestGameLogicProcessRoutesNewGameAckAndStopsConnectionGeneration(t *testing.T) {
	serverReady := make(chan net.Conn, 1)
	config := GameLogicProcessConfig{
		NodeID: "nhsk-process-wire", Workers: 2, MaxActiveBattles: 8,
		Diagnostic: DiagnosticProcessConfig{Root: t.TempDir(), AdminSocket: shortAdminSocket(t)},
		Legacy: LegacyGMConnectionConfig{
			Address: "gm-test", DialTimeout: time.Second, OriginTimeout: time.Second,
			InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, BackoffMultiplier: 2,
			Jitter: .2, StableReset: time.Second,
			Dial: func(context.Context, string, string) (net.Conn, error) {
				client, server := net.Pipe()
				serverReady <- server
				return client, nil
			},
		},
	}
	process, err := NewGameLogicProcess(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := process.Start(ctx); err != nil {
		t.Fatal(err)
	}
	server := <-serverReady
	defer server.Close()
	if _, err := legacywire.ReadFrame(server); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Write(processOriginFrame()); err != nil {
		t.Fatal(err)
	}
	newGame := make([]byte, 44)
	binary.LittleEndian.PutUint32(newGame[12:16], 0x86c1)
	binary.LittleEndian.PutUint32(newGame[20:24], uint32(len(newGame)))
	binary.LittleEndian.PutUint32(newGame[24:28], 101)
	binary.LittleEndian.PutUint32(newGame[28:32], 7)
	binary.LittleEndian.PutUint32(newGame[40:44], NHSKDescriptor.GameID)
	if _, err := server.Write(newGame); err != nil {
		t.Fatal(err)
	}
	ack, err := legacywire.ReadFrame(server)
	if err != nil || ack.Type != 0x800086c0 || len(ack.Bytes) != 29 || ack.Bytes[28] != 1 {
		t.Fatalf("NEW_GAME ack = %#v, %v", ack, err)
	}
	_ = server.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		value, callErr := process.Runtime().Call(context.Background(), process.HostRef(), GetHostSnapshotCommand, nil)
		if callErr == nil {
			if snapshot, ok := value.(HostSnapshot); ok && len(snapshot.ActiveBattles) == 0 {
				_ = process.Close(context.Background())
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	_ = process.Close(context.Background())
	t.Fatal("connection generation battle was not stopped")
}

func processOriginFrame() []byte {
	frame := make([]byte, 24)
	binary.LittleEndian.PutUint16(frame[8:10], 100)
	binary.LittleEndian.PutUint32(frame[12:16], 0x0600)
	binary.LittleEndian.PutUint32(frame[20:24], 24)
	return frame
}
