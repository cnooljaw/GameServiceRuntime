package nhsk

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

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
