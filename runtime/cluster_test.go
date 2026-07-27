package gsr_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestRemoteSendAndCallMatchLocalBehavior(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntime(t, "node-b", network)

	recorder := &clusterRecordingService{received: make(chan string, 1)}
	recordRef := createClusterService(t, nodeB, recorder)
	if err := nodeA.Send(recordRef, 1, "hello"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-recorder.received:
		if got != "hello" {
			t.Fatalf("remote Send payload = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("remote Send was not delivered")
	}

	replyRef := createClusterService(t, nodeB, clusterReplyService{})
	got, err := nodeA.Call(context.Background(), replyRef, 1, "cluster")
	if err != nil {
		t.Fatal(err)
	}
	if got != "reply: cluster" {
		t.Fatalf("remote Call reply = %#v", got)
	}
}

func TestLocalRuntimeRejectsRemoteTarget(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	err := runtime.Send(gsr.ServiceRef{Node: "node-b", ID: 1}, 1, nil)
	if !errors.Is(err, gsr.ErrRemoteUnavailable) {
		t.Fatalf("Send error = %v", err)
	}
}

func TestClusterRuntimeStartFailureClosesTransport(t *testing.T) {
	cleanupErr := errors.New("transport cleanup failed")
	transport := &failingStartTransport{closeErr: cleanupErr}
	runtime, err := gsr.NewClusterRuntime(gsr.Config{NodeID: "node-a"}, transport, testClusterCodec{})
	if runtime != nil {
		t.Fatal("NewClusterRuntime returned a partial Runtime")
	}
	if !errors.Is(err, gsr.ErrClusterStart) {
		t.Fatalf("NewClusterRuntime error = %v", err)
	}
	if !transport.closed {
		t.Fatal("failed ClusterTransport was not closed")
	}
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("NewClusterRuntime error = %v, want cleanup error", err)
	}
}

func TestRemoteCallReturnsDeliveryError(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	newTestClusterRuntime(t, "node-b", network)

	_, err := nodeA.Call(context.Background(), gsr.ServiceRef{Node: "node-b", ID: 999}, 1, nil)
	if !errors.Is(err, gsr.ErrServiceNotFound) {
		t.Fatalf("Call error = %v", err)
	}
}

func TestRemoteCallRecordsOneResultMetric(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntime(t, "node-b", network)

	successRef := createClusterService(t, nodeB, clusterReplyService{})
	if _, err := nodeA.Call(context.Background(), successRef, 1, "ok"); err != nil {
		t.Fatal(err)
	}
	metrics := nodeA.Inspect().Metrics
	if got := metrics.Counter("remote_calls_succeeded_total"); got != 1 {
		t.Fatalf("remote_calls_succeeded_total = %d, want 1", got)
	}
	if got := metrics.Counter("remote_calls_failed_total"); got != 0 {
		t.Fatalf("remote_calls_failed_total = %d, want 0", got)
	}

	errorRef := createClusterService(t, nodeB, clusterErrorService{})
	if _, err := nodeA.Call(context.Background(), errorRef, 1, nil); err == nil {
		t.Fatal("remote handler error Call succeeded")
	}
	if _, err := nodeA.Call(context.Background(), gsr.ServiceRef{Node: "missing-node", ID: 1}, 1, nil); !errors.Is(err, gsr.ErrRemoteUnavailable) {
		t.Fatalf("unavailable remote Call error = %v, want ErrRemoteUnavailable", err)
	}
	blocking := &clusterBlockingReplyService{started: make(chan struct{}), release: make(chan struct{})}
	blockingRef := createClusterService(t, nodeB, blocking)
	timeoutContext, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	if _, err := nodeA.Call(timeoutContext, blockingRef, 1, nil); !errors.Is(err, gsr.ErrTimeout) {
		cancel()
		close(blocking.release)
		t.Fatalf("timed out remote Call error = %v, want ErrTimeout", err)
	}
	cancel()
	close(blocking.release)
	metrics = nodeA.Inspect().Metrics
	if got := metrics.Counter("remote_calls_succeeded_total"); got != 1 {
		t.Fatalf("remote_calls_succeeded_total after failures = %d, want 1", got)
	}
	if got := metrics.Counter("remote_calls_failed_total"); got != 3 {
		t.Fatalf("remote_calls_failed_total = %d, want 3", got)
	}
}

func TestRemoteCallMetricsIgnoreLocalCall(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	localRef := createClusterService(t, nodeA, clusterReplyService{})

	if _, err := nodeA.Call(context.Background(), localRef, 1, "local"); err != nil {
		t.Fatal(err)
	}
	metrics := nodeA.Inspect().Metrics
	if got := metrics.Counter("remote_calls_succeeded_total"); got != 0 {
		t.Fatalf("remote_calls_succeeded_total = %d, want 0", got)
	}
	if got := metrics.Counter("remote_calls_failed_total"); got != 0 {
		t.Fatalf("remote_calls_failed_total = %d, want 0", got)
	}
}

func TestRemoteSendDeliveryFailureIsObservedByReceiver(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntime(t, "node-b", network)

	if err := nodeA.Send(gsr.ServiceRef{Node: "node-b", ID: 999}, 1, nil); err != nil {
		t.Fatalf("asynchronous remote Send error = %v", err)
	}
	eventually(t, func() bool {
		return nodeB.Inspect().Metrics.Counter("cluster_delivery_errors_total") == 1
	})
}

func TestClusterRejectsEnvelopeWhoseSourceDoesNotMatchPeer(t *testing.T) {
	network := newMemoryCluster()
	newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntime(t, "node-b", network)
	recorder := &clusterRecordingService{received: make(chan string, 1)}
	target := createClusterService(t, nodeB, recorder)

	network.deliver("node-a", "node-b", gsr.WireEnvelope{
		Source:  gsr.ServiceRef{Node: "forged", ID: 1},
		Target:  target,
		Command: 1,
		Payload: encodeTestPayload("forged"),
	})
	select {
	case value := <-recorder.received:
		t.Fatalf("invalid envelope delivered payload %q", value)
	case <-time.After(20 * time.Millisecond):
	}
	if got := nodeB.Inspect().Metrics.Counter("cluster_invalid_envelopes_total"); got != 1 {
		t.Fatalf("invalid envelope metric = %d", got)
	}
}

func TestRemoteHandlerErrorReturnsRemoteError(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntime(t, "node-b", network)
	target := createClusterService(t, nodeB, clusterErrorService{})

	_, err := nodeA.Call(context.Background(), target, 1, nil)
	var remoteErr *gsr.RemoteError
	if !errors.As(err, &remoteErr) {
		t.Fatalf("Call error = %T %v, want *RemoteError", err, err)
	}
	if remoteErr.Message != "cluster handler failed" {
		t.Fatalf("RemoteError message = %q", remoteErr.Message)
	}
}

func TestRemoteReplyEncodeFailureReturnsPayloadErrorToBothNodes(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntimeWithCodec(t, "node-b", network, replyEncodeFailCodec{testClusterCodec{}})
	service := &clusterReplyErrorService{replyErr: make(chan error, 1)}
	target := createClusterService(t, nodeB, service)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := nodeA.Call(ctx, target, 1, nil)
	if !errors.Is(err, gsr.ErrPayloadEncode) {
		t.Fatalf("caller error = %v, want ErrPayloadEncode", err)
	}
	if err := <-service.replyErr; !errors.Is(err, gsr.ErrPayloadEncode) {
		t.Fatalf("responder Reply error = %v, want ErrPayloadEncode", err)
	}
}

func TestRemoteRuntimeErrorSurvivesIntermediateNode(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntime(t, "node-b", network)
	service := &clusterCallingService{target: gsr.ServiceRef{Node: "node-c", ID: 1}}
	target := createClusterService(t, nodeB, service)

	_, err := nodeA.Call(context.Background(), target, 1, nil)
	if !errors.Is(err, gsr.ErrRemoteUnavailable) {
		t.Fatalf("caller error = %T %v, want ErrRemoteUnavailable", err, err)
	}
}

func TestRemoteReplyRequiresOriginalResponder(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntime(t, "node-b", network)
	service := &clusterBlockingReplyService{started: make(chan struct{}), release: make(chan struct{})}
	target := createClusterService(t, nodeB, service)

	result := make(chan error, 1)
	go func() {
		value, err := nodeA.Call(context.Background(), target, 1, nil)
		if err == nil && value != "right" {
			err = fmt.Errorf("reply = %#v", value)
		}
		result <- err
	}()
	<-service.started
	request := network.lastCommand("node-a", "node-b")
	network.deliver("node-b", "node-a", gsr.WireEnvelope{
		Source:   gsr.ServiceRef{Node: "node-b", ID: target.ID + 1},
		Target:   request.Source,
		Session:  request.Session,
		Command:  request.Command,
		Payload:  encodeTestPayload("forged"),
		Response: true,
	})
	select {
	case err := <-result:
		t.Fatalf("forged responder completed Call: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(service.release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCallFailsWhenNodeDisconnects(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntime(t, "node-b", network)
	service := &clusterBlockingReplyService{started: make(chan struct{}), release: make(chan struct{})}
	target := createClusterService(t, nodeB, service)

	result := make(chan error, 1)
	go func() {
		_, err := nodeA.Call(context.Background(), target, 1, nil)
		result <- err
	}()
	<-service.started
	network.disconnect("node-b")
	select {
	case err := <-result:
		if !errors.Is(err, gsr.ErrRemoteUnavailable) {
			t.Fatalf("Call error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Call did not fail after node disconnect")
	}
	close(service.release)
}

func TestRemoteLateReplyIsDiscarded(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntime(t, "node-b", network)
	service := &clusterBlockingReplyService{started: make(chan struct{}), release: make(chan struct{})}
	target := createClusterService(t, nodeB, service)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := nodeA.Call(ctx, target, 1, nil); !errors.Is(err, gsr.ErrTimeout) {
		t.Fatalf("Call error = %v", err)
	}
	close(service.release)
	eventually(t, func() bool { return nodeA.Inspect().Metrics.Counter("late_reply_total") == 1 })
}

func TestRemoteCallPathRejectsCrossNodeCycle(t *testing.T) {
	network := newMemoryCluster()
	nodeA := newTestClusterRuntime(t, "node-a", network)
	nodeB := newTestClusterRuntime(t, "node-b", network)
	serviceA := &clusterCallingService{}
	serviceB := &clusterCallingService{}
	refA := createClusterService(t, nodeA, serviceA)
	refB := createClusterService(t, nodeB, serviceB)
	serviceA.target = refB
	serviceB.target = refA

	_, err := nodeA.Call(context.Background(), refA, 1, nil)
	if !errors.Is(err, gsr.ErrCallCycle) {
		t.Fatalf("Call error = %v, want ErrCallCycle", err)
	}
}

func newTestClusterRuntime(t *testing.T, node gsr.NodeID, network *memoryCluster) *gsr.Runtime {
	return newTestClusterRuntimeWithCodec(t, node, network, testClusterCodec{})
}

func newTestClusterRuntimeWithCodec(t *testing.T, node gsr.NodeID, network *memoryCluster, codec gsr.ClusterCodec) *gsr.Runtime {
	t.Helper()
	runtime, err := gsr.NewClusterRuntime(gsr.Config{NodeID: node, Workers: 2}, network.transport(), codec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	return runtime
}

func createClusterService(t *testing.T, runtime *gsr.Runtime, service gsr.Service) gsr.ServiceRef {
	t.Helper()
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

type testClusterCodec struct{}

func (testClusterCodec) Encode(_ gsr.CommandID, _ bool, value any) ([]byte, error) {
	switch value := value.(type) {
	case nil:
		return []byte{0}, nil
	case string:
		return encodeTestPayload(value), nil
	default:
		return nil, fmt.Errorf("unsupported payload %T", value)
	}
}

func (testClusterCodec) Decode(_ gsr.CommandID, _ bool, payload []byte) (any, error) {
	if len(payload) == 1 && payload[0] == 0 {
		return nil, nil
	}
	if len(payload) == 0 || payload[0] != 1 {
		return nil, errors.New("invalid test payload")
	}
	return string(payload[1:]), nil
}

func encodeTestPayload(value string) []byte { return append([]byte{1}, value...) }

type replyEncodeFailCodec struct{ testClusterCodec }

func (replyEncodeFailCodec) Encode(_ gsr.CommandID, response bool, value any) ([]byte, error) {
	if response {
		return nil, errors.New("reply encode failed")
	}
	return testClusterCodec{}.Encode(0, false, value)
}

type memoryCluster struct {
	mu         sync.Mutex
	nodes      map[gsr.NodeID]*memoryTransport
	lastByLink map[string]gsr.WireEnvelope
}

func newMemoryCluster() *memoryCluster {
	return &memoryCluster{nodes: make(map[gsr.NodeID]*memoryTransport), lastByLink: make(map[string]gsr.WireEnvelope)}
}

func (c *memoryCluster) transport() *memoryTransport { return &memoryTransport{network: c} }

func (c *memoryCluster) deliver(from, target gsr.NodeID, envelope gsr.WireEnvelope) error {
	c.mu.Lock()
	transport := c.nodes[target]
	if !envelope.Response {
		c.lastByLink[string(from)+"\x00"+string(target)] = envelope
	}
	c.mu.Unlock()
	if transport == nil {
		return gsr.ErrRemoteUnavailable
	}
	transport.events.Receive(from, envelope)
	return nil
}

func (c *memoryCluster) lastCommand(from, target gsr.NodeID) gsr.WireEnvelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastByLink[string(from)+"\x00"+string(target)]
}

func (c *memoryCluster) disconnect(node gsr.NodeID) {
	c.mu.Lock()
	delete(c.nodes, node)
	peers := make([]*memoryTransport, 0, len(c.nodes))
	for _, transport := range c.nodes {
		peers = append(peers, transport)
	}
	c.mu.Unlock()
	for _, transport := range peers {
		transport.events.Unavailable(node)
	}
}

type memoryTransport struct {
	network *memoryCluster
	local   gsr.NodeID
	events  gsr.ClusterEvents
}

type failingStartTransport struct {
	closed   bool
	closeErr error
}

func (*failingStartTransport) Start(gsr.NodeID, gsr.ClusterEvents) error {
	return errors.New("start failed")
}
func (*failingStartTransport) Send(gsr.NodeID, gsr.WireEnvelope) error { return nil }
func (t *failingStartTransport) Close(context.Context) error {
	t.closed = true
	return t.closeErr
}

func (t *memoryTransport) Start(local gsr.NodeID, events gsr.ClusterEvents) error {
	t.local = local
	t.events = events
	t.network.mu.Lock()
	defer t.network.mu.Unlock()
	if t.network.nodes[local] != nil {
		return errors.New("duplicate node")
	}
	t.network.nodes[local] = t
	return nil
}

func (t *memoryTransport) Send(target gsr.NodeID, envelope gsr.WireEnvelope) error {
	return t.network.deliver(t.local, target, envelope)
}

func (t *memoryTransport) Close(context.Context) error {
	t.network.mu.Lock()
	if t.network.nodes[t.local] == t {
		delete(t.network.nodes, t.local)
	}
	t.network.mu.Unlock()
	return nil
}

type clusterRecordingService struct{ received chan string }

func (*clusterRecordingService) Init(gsr.ServiceContext) error { return nil }
func (s *clusterRecordingService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	s.received <- command.Payload.(string)
	return nil
}
func (*clusterRecordingService) Stop(context.Context) error { return nil }
func (*clusterRecordingService) Close() error               { return nil }

type clusterReplyService struct{}

func (clusterReplyService) Init(gsr.ServiceContext) error { return nil }
func (clusterReplyService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	return ctx.Reply("reply: " + command.Payload.(string))
}
func (clusterReplyService) Stop(context.Context) error { return nil }
func (clusterReplyService) Close() error               { return nil }

type clusterErrorService struct{}

func (clusterErrorService) Init(gsr.ServiceContext) error { return nil }
func (clusterErrorService) Handle(gsr.CommandContext, gsr.Command) error {
	return errors.New("cluster handler failed")
}
func (clusterErrorService) Stop(context.Context) error { return nil }
func (clusterErrorService) Close() error               { return nil }

type clusterReplyErrorService struct{ replyErr chan error }

func (*clusterReplyErrorService) Init(gsr.ServiceContext) error { return nil }
func (s *clusterReplyErrorService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	err := ctx.Reply("reply")
	s.replyErr <- err
	return err
}
func (*clusterReplyErrorService) Stop(context.Context) error { return nil }
func (*clusterReplyErrorService) Close() error               { return nil }

type clusterBlockingReplyService struct {
	started chan struct{}
	release chan struct{}
}

func (*clusterBlockingReplyService) Init(gsr.ServiceContext) error { return nil }
func (s *clusterBlockingReplyService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	close(s.started)
	<-s.release
	return ctx.Reply("right")
}
func (*clusterBlockingReplyService) Stop(context.Context) error { return nil }
func (*clusterBlockingReplyService) Close() error               { return nil }

type clusterCallingService struct {
	ctx    gsr.ServiceContext
	target gsr.ServiceRef
}

func (s *clusterCallingService) Init(ctx gsr.ServiceContext) error { s.ctx = ctx; return nil }
func (s *clusterCallingService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	value, err := s.ctx.Call(context.Background(), s.target, 1, nil)
	if err != nil {
		return err
	}
	return ctx.Reply(value)
}
func (*clusterCallingService) Stop(context.Context) error { return nil }
func (*clusterCallingService) Close() error               { return nil }
