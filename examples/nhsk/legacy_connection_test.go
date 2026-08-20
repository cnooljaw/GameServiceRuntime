package nhsk

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestLegacyConnectionCachesCreatedBattleRouteUntilDelete(t *testing.T) {
	battleRef := gsr.ServiceRef{Node: "nhsk", ID: 9}
	runtime := &connectionRoutingRuntime{battleRef: battleRef}
	connection, err := NewLegacyGMConnection(LegacyGMConnectionConfig{
		Address: "gm-test", DialTimeout: time.Second, OriginTimeout: time.Second,
		InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
		BackoffMultiplier: 2, Jitter: .2, StableReset: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.AttachRouting(runtime, gsr.ServiceRef{Node: "nhsk", ID: 1}); err != nil {
		t.Fatal(err)
	}
	session := &legacyConnectionSession{generation: 3, frames: make(chan []byte, 1), outputs: make(chan GameOutputBatch, 1), done: make(chan struct{})}
	connection.session = session

	newGame := controlTestFrame(0x86c1, 44, func(data []byte) {
		putControl32(data, 24, 1234)
		putControl32(data, 28, 82)
		putControl32(data, 40, NHSKDescriptor.GameID)
	})
	if err := connection.config.OnFrame(context.Background(), 3, legacywire.Frame{Type: 0x86c1, Bytes: newGame}); err != nil {
		t.Fatal(err)
	}
	if ack := <-session.frames; len(ack) != 29 || ack[28] != 1 {
		t.Fatalf("NEW_GAME ack = %x", ack)
	}

	init := controlTestFrame(0x8600, 144, func(data []byte) {
		putControlGLHeader(data, 1234, 0)
		putControl32(data, 34, 7)
		putControl32(data, 46, 88)
		putControl32(data, 56, 9)
		putControl32(data, 64, NHSKDescriptor.GameID)
		putControl32(data, 116, 4)
		putControl32(data, 120, 8)
		putControlSuffix(data, 136, 144, nil)
	})
	if err := connection.config.OnFrame(context.Background(), 3, legacywire.Frame{Type: 0x8600, Bytes: init}); err != nil {
		t.Fatal(err)
	}
	if err := connection.config.OnFrame(context.Background(), 3, legacywire.Frame{Type: 0x8605, Bytes: decodeLegacyMapperGolden(t, legacyOutCardRelayGolden)}); err != nil {
		t.Fatal(err)
	}

	deleteGame := controlTestFrame(0x86c2, 28, func(data []byte) { putControl32(data, 24, 1234) })
	if err := connection.config.OnFrame(context.Background(), 3, legacywire.Frame{Type: 0x86c2, Bytes: deleteGame}); err != nil {
		t.Fatal(err)
	}
	if err := connection.config.OnFrame(context.Background(), 3, legacywire.Frame{Type: 0x8600, Bytes: init}); err == nil {
		t.Fatal("Battle control used a route after DEL_GAME invalidated it")
	}
	if runtime.resolveCalls != 0 {
		t.Fatalf("Host ResolveBattle calls = %d, want 0", runtime.resolveCalls)
	}
	if got := runtime.sendTargets(); len(got) != 3 || got[0] != battleRef || got[1] != battleRef || got[2].ID != 1 {
		t.Fatalf("Send targets = %#v", got)
	}
}

type connectionRoutingRuntime struct {
	mu                sync.Mutex
	battleRef         gsr.ServiceRef
	operationBattleID game.BattleID
	resolveCalls      int
	targets           []gsr.ServiceRef
}

func (runtime *connectionRoutingRuntime) Send(target gsr.ServiceRef, _ gsr.CommandID, _ any) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.targets = append(runtime.targets, target)
	return nil
}

func (runtime *connectionRoutingRuntime) Call(_ context.Context, _ gsr.ServiceRef, command gsr.CommandID, payload any) (any, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if command == ResolveBattleCommand {
		runtime.resolveCalls++
		request := payload.(ResolveBattleRequest)
		return ResolveBattleResult{BattleID: request.BattleID, Ref: runtime.battleRef}, nil
	}
	request := payload.(CreateBattleRequest)
	battleID := runtime.operationBattleID
	if battleID == 0 {
		battleID = request.BattleID
	}
	return CreateBattleOperation{OperationID: 1, BattleID: battleID, Phase: HostOperationCompleted, Ref: runtime.battleRef}, nil
}

func (runtime *connectionRoutingRuntime) sendTargets() []gsr.ServiceRef {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return append([]gsr.ServiceRef(nil), runtime.targets...)
}

func TestLegacyGMConnectionPerformsOriginQueuesOutputAndReportsGeneration(t *testing.T) {
	serverReady := make(chan net.Conn, 1)
	frames := make(chan legacywire.Frame, 1)
	connection, err := NewLegacyGMConnection(LegacyGMConnectionConfig{
		Address:           "gm-test",
		DialTimeout:       time.Second,
		OriginTimeout:     time.Second,
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        time.Millisecond,
		BackoffMultiplier: 2,
		Jitter:            0.2,
		StableReset:       time.Second,
		OutputQueue:       2,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			serverReady <- server
			return client, nil
		},
		OnFrame: func(_ context.Context, generation ConnectionGeneration, frame legacywire.Frame) error {
			frames <- legacywire.Frame{Type: frame.Type, Origin: uint16(generation), Bytes: frame.Bytes}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := connection.Start(ctx); err != nil {
		t.Fatal(err)
	}
	server := <-serverReady
	defer server.Close()
	origin, err := legacywire.ReadFrame(server)
	if err != nil {
		t.Fatal(err)
	}
	if origin.Type != 0x0600 || origin.Origin != 107 {
		t.Fatalf("GameLogic origin = %#v", origin)
	}
	if _, err := server.Write(originFrame(100)); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Write(testFrame(0x9999)); err != nil {
		t.Fatal(err)
	}
	frame := <-frames
	if frame.Type != 0x9999 || frame.Origin != 1 {
		t.Fatalf("received frame = %#v", frame)
	}
	if err := connection.SubmitFrame(legacywire.EncodeNewGameAck(1, true)); err != nil {
		t.Fatal(err)
	}
	ack, err := legacywire.ReadFrame(server)
	if err != nil || ack.Type != 0x800086c0 || len(ack.Bytes) != 29 {
		t.Fatalf("NEW_GAME ack = %#v, %v", ack, err)
	}
	if err := connection.Submit(GameOutputBatch{BattleID: 1, MatchID: 2, ProductID: 82, Ref: testServiceRef(), ConnectionGeneration: 1, Outputs: []GameOutput{GameStartedOutput{ReplayName: "NHSK.xml"}}}); err != nil {
		t.Fatal(err)
	}
	output, err := legacywire.ReadFrame(server)
	if err != nil {
		t.Fatal(err)
	}
	if output.Type != 0x8654 || len(output.Bytes) != 115 {
		t.Fatalf("GAME_STARTED output = type=%#x len=%d", output.Type, len(output.Bytes))
	}
	if err := connection.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func originFrame(origin uint16) []byte {
	return testHeader(0x0600, origin)
}

func testFrame(message uint32) []byte { return testHeader(message, 0) }

func testHeader(message uint32, origin uint16) []byte {
	frame := make([]byte, 24)
	binary.LittleEndian.PutUint16(frame[8:10], origin)
	binary.LittleEndian.PutUint32(frame[12:16], message)
	binary.LittleEndian.PutUint32(frame[20:24], uint32(len(frame)))
	return frame
}

func testServiceRef() (ref gsr.ServiceRef) { return gsr.ServiceRef{Node: "nhsk", ID: 1} }
