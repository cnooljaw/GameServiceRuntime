package tcp

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestHandshakeRejectsVersionAndTargetMismatch(t *testing.T) {
	t.Run("version", func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		serverResult := make(chan error, 1)
		go func() {
			_, err := acceptHandshake(server, "node-b", protocolVersion, time.Second)
			serverResult <- err
		}()
		_, err := initiateHandshake(client, "node-a", "node-b", protocolVersion+1, time.Second)
		if !errors.Is(err, ErrProtocolVersion) {
			t.Fatalf("client handshake error = %v", err)
		}
		if err := <-serverResult; !errors.Is(err, ErrProtocolVersion) {
			t.Fatalf("server handshake error = %v", err)
		}
	})

	t.Run("target", func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		serverResult := make(chan error, 1)
		go func() {
			_, err := acceptHandshake(server, "node-c", protocolVersion, time.Second)
			serverResult <- err
		}()
		_, err := initiateHandshake(client, "node-a", "node-b", protocolVersion, time.Second)
		if !errors.Is(err, ErrPeerIdentity) {
			t.Fatalf("client handshake error = %v", err)
		}
		if err := <-serverResult; err != nil {
			t.Fatalf("server handshake error = %v", err)
		}
	})
}

func TestTransportUsesConnectionBidirectionally(t *testing.T) {
	receivedA := make(chan receivedEnvelope, 1)
	receivedB := make(chan receivedEnvelope, 1)
	unavailableA := make(chan gsr.NodeID, 1)

	transportB := New(Config{ListenAddress: "127.0.0.1:0"})
	startTransport(t, transportB, "node-b", gsr.ClusterEvents{
		Receive: func(peer gsr.NodeID, envelope gsr.WireEnvelope) {
			receivedB <- receivedEnvelope{peer: peer, envelope: envelope}
		},
		Unavailable: func(gsr.NodeID) {},
	})
	transportA := New(Config{
		ListenAddress: "127.0.0.1:0",
		Peers:         map[gsr.NodeID]string{"node-b": transportB.Address()},
	})
	startTransport(t, transportA, "node-a", gsr.ClusterEvents{
		Receive: func(peer gsr.NodeID, envelope gsr.WireEnvelope) {
			receivedA <- receivedEnvelope{peer: peer, envelope: envelope}
		},
		Unavailable: func(peer gsr.NodeID) { unavailableA <- peer },
	})

	request := gsr.WireEnvelope{
		Source:  gsr.ServiceRef{Node: "node-a"},
		Target:  gsr.ServiceRef{Node: "node-b", ID: 1},
		Command: 1,
		Payload: []byte("request"),
	}
	if err := transportA.Send("node-b", request); err != nil {
		t.Fatal(err)
	}
	assertReceived(t, receivedB, "node-a", request)

	reply := gsr.WireEnvelope{
		Source:   request.Target,
		Target:   request.Source,
		Session:  1,
		Command:  1,
		Payload:  []byte("reply"),
		Response: true,
	}
	if err := transportB.Send("node-a", reply); err != nil {
		t.Fatal(err)
	}
	assertReceived(t, receivedA, "node-b", reply)

	if err := transportB.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case peer := <-unavailableA:
		if peer != "node-b" {
			t.Fatalf("unavailable peer = %q", peer)
		}
	case <-time.After(time.Second):
		t.Fatal("peer disconnect was not reported")
	}
}

func TestTransportKeepsDeterministicConnectionWhenBothNodesDial(t *testing.T) {
	transportA := New(Config{ListenAddress: "127.0.0.1:0"})
	transportB := New(Config{ListenAddress: "127.0.0.1:0"})
	startTransport(t, transportA, "node-a", discardClusterEvents())
	startTransport(t, transportB, "node-b", discardClusterEvents())
	transportA.config.Peers["node-b"] = transportB.Address()
	transportB.config.Peers["node-a"] = transportA.Address()

	var wait sync.WaitGroup
	wait.Add(2)
	errorsByNode := make(chan error, 2)
	go func() {
		defer wait.Done()
		_, err := transportA.connect("node-b")
		errorsByNode <- err
	}()
	go func() {
		defer wait.Done()
		_, err := transportB.connect("node-a")
		errorsByNode <- err
	}()
	wait.Wait()
	close(errorsByNode)
	for err := range errorsByNode {
		if err != nil {
			t.Fatal(err)
		}
	}

	eventuallyTCP(t, func() bool {
		connA := transportA.connection("node-b")
		connB := transportB.connection("node-a")
		return connA != nil && connB != nil && connA.outbound && !connB.outbound
	})
}

func TestTransportDialToOneNodeDoesNotBlockAnotherNode(t *testing.T) {
	slowListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slowListener.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := slowListener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()

	received := make(chan receivedEnvelope, 1)
	healthy := New(Config{ListenAddress: "127.0.0.1:0"})
	startTransport(t, healthy, "node-b", gsr.ClusterEvents{
		Receive: func(peer gsr.NodeID, envelope gsr.WireEnvelope) {
			received <- receivedEnvelope{peer: peer, envelope: envelope}
		},
		Unavailable: func(gsr.NodeID) {},
	})

	transport := New(Config{
		ListenAddress:    "127.0.0.1:0",
		HandshakeTimeout: 2 * time.Second,
		Peers: map[gsr.NodeID]string{
			"node-slow": slowListener.Addr().String(),
			"node-b":    healthy.Address(),
		},
	})
	startTransport(t, transport, "node-a", discardClusterEvents())

	slowResult := make(chan error, 1)
	go func() {
		slowResult <- transport.Send("node-slow", gsr.WireEnvelope{
			Source:  gsr.ServiceRef{Node: "node-a"},
			Target:  gsr.ServiceRef{Node: "node-slow", ID: 1},
			Command: 1,
		})
	}()

	var slowConn net.Conn
	select {
	case slowConn = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("slow peer was not dialed")
	}
	defer slowConn.Close()

	healthyEnvelope := gsr.WireEnvelope{
		Source:  gsr.ServiceRef{Node: "node-a"},
		Target:  gsr.ServiceRef{Node: "node-b", ID: 1},
		Command: 1,
	}
	healthyResult := make(chan error, 1)
	go func() { healthyResult <- transport.Send("node-b", healthyEnvelope) }()

	select {
	case err := <-healthyResult:
		if err != nil {
			t.Fatal(err)
		}
		assertReceived(t, received, "node-a", healthyEnvelope)
	case <-time.After(300 * time.Millisecond):
		_ = slowConn.Close()
		<-slowResult
		<-healthyResult
		t.Fatal("dial to healthy node was blocked by another node's handshake")
	}

	_ = slowConn.Close()
	if err := <-slowResult; err == nil {
		t.Fatal("slow handshake unexpectedly succeeded")
	}
}

func TestTransportRejectsEnvelopeIdentityMismatch(t *testing.T) {
	transport := New(Config{ListenAddress: "127.0.0.1:0"})
	startTransport(t, transport, "node-a", discardClusterEvents())
	err := transport.Send("node-b", gsr.WireEnvelope{
		Source: gsr.ServiceRef{Node: "forged"},
		Target: gsr.ServiceRef{Node: "node-b", ID: 1},
	})
	if !errors.Is(err, ErrPeerIdentity) {
		t.Fatalf("Send error = %v", err)
	}
}

func startTransport(t *testing.T, transport *Transport, node gsr.NodeID, events gsr.ClusterEvents) {
	t.Helper()
	if err := transport.Start(node, events); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
}

type receivedEnvelope struct {
	peer     gsr.NodeID
	envelope gsr.WireEnvelope
}

func assertReceived(t *testing.T, input <-chan receivedEnvelope, peer gsr.NodeID, envelope gsr.WireEnvelope) {
	t.Helper()
	select {
	case got := <-input:
		if got.peer != peer {
			t.Fatalf("peer = %q, want %q", got.peer, peer)
		}
		if string(got.envelope.Payload) != string(envelope.Payload) || got.envelope.Source != envelope.Source || got.envelope.Target != envelope.Target || got.envelope.Response != envelope.Response {
			t.Fatalf("envelope = %#v, want %#v", got.envelope, envelope)
		}
	case <-time.After(time.Second):
		t.Fatal("envelope was not received")
	}
}

func discardClusterEvents() gsr.ClusterEvents {
	return gsr.ClusterEvents{
		Receive:     func(gsr.NodeID, gsr.WireEnvelope) {},
		Unavailable: func(gsr.NodeID) {},
	}
}

func eventuallyTCP(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
