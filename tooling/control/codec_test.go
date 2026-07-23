package control

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestCodecEncodesControlPayloadAndDelegatesFallback(t *testing.T) {
	fallback := &recordingCodec{}
	codec := NewCodec(fallback)
	request := refreshNodeRequest{Node: "node-b"}
	payload, err := codec.Encode(commandRefreshNode, false, request)
	if err != nil {
		t.Fatalf("Encode(control) error = %v", err)
	}
	decoded, err := codec.Decode(commandRefreshNode, false, payload)
	if err != nil {
		t.Fatalf("Decode(control) error = %v", err)
	}
	if got, ok := decoded.(refreshNodeRequest); !ok || got != request {
		t.Fatalf("decoded request = %#v, want %#v", decoded, request)
	}
	if _, err := codec.Encode(99, false, "fallback"); err != nil {
		t.Fatalf("Encode(fallback) error = %v", err)
	}
	if fallback.encoded != 1 {
		t.Fatalf("fallback Encode calls = %d, want 1", fallback.encoded)
	}
}

func TestCodecRejectsInvalidPayloadAndTrailingJSON(t *testing.T) {
	codec := NewCodec(nil)
	if _, err := codec.Encode(commandListNodes, false, refreshNodeRequest{}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Encode(wrong type) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(commandRefreshNode, false, []byte(`{"node":"node-b"} {}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(trailing JSON) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Decode(commandRefreshNode, true, []byte(`{"detail":{},"error":""}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode(invalid success response) error = %v, want ErrInvalidResponse", err)
	}
	if _, err := codec.Encode(99, false, nil); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode(no fallback) error = %v, want ErrUnsupportedCommand", err)
	}
	if _, err := codec.Encode(commandRegisterNodeLease, false, nil); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode(private lease command) error = %v, want ErrUnsupportedCommand", err)
	}
}

func TestCodecEncodesNodeStopRequestsAndKeepsRunnerResultPrivate(t *testing.T) {
	codec := NewCodec(nil)
	task := validNodeStopTaskFor(gsr.ServiceRef{Node: "node-b", ID: 7})
	payload, err := codec.Encode(commandBeginNodeStop, false, beginNodeStopRequest{Task: task})
	if err != nil {
		t.Fatalf("Encode(BeginNodeStop) error = %v", err)
	}
	decoded, err := codec.Decode(commandBeginNodeStop, false, payload)
	if err != nil {
		t.Fatalf("Decode(BeginNodeStop) error = %v", err)
	}
	request, ok := decoded.(beginNodeStopRequest)
	if !ok || !sameNodeStopTask(request.Task, task) {
		t.Fatalf("decoded request = %#v, want %#v", decoded, task)
	}
	receipt := NodeStopReceipt{RequestID: task.RequestID, Target: task.Target, State: StopTargetQueued, UpdatedAt: time.Now()}
	if _, err := codec.Encode(commandGetNodeStopReceipt, true, nodeStopReceiptResponse{Receipt: receipt}); err != nil {
		t.Fatalf("Encode(GetNodeStopReceipt response) error = %v", err)
	}
	if _, err := codec.Encode(commandRecordNodeStopResult, false, nodeStopResult{}); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode(private runner result) error = %v, want ErrUnsupportedCommand", err)
	}
}

func TestControlConfigRejectsInvalidTargetsAndClient(t *testing.T) {
	validTarget := NodeTarget{Config: NodeConfig{ID: "node-b", Address: "127.0.0.1:9000", Enabled: true}, Agent: gsr.ServiceRef{Node: "node-b", ID: 1}}
	if _, err := NewClusterObserverService(ObserverConfig{Nodes: []NodeTarget{validTarget, validTarget}}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewClusterObserverService(duplicate) error = %v, want ErrInvalidConfig", err)
	}
	invalidAgent := validTarget
	invalidAgent.Agent = gsr.ServiceRef{Node: "node-a", ID: 1}
	if _, err := NewClusterObserverService(ObserverConfig{Nodes: []NodeTarget{invalidAgent}}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewClusterObserverService(wrong agent node) error = %v, want ErrInvalidConfig", err)
	}
	if _, err := NewClusterObserverService(ObserverConfig{Nodes: []NodeTarget{validTarget}, CallTimeout: -time.Second}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewClusterObserverService(negative timeout) error = %v, want ErrInvalidConfig", err)
	}
	if _, err := NewClient((*nilCaller)(nil), gsr.ServiceRef{Node: "node-a", ID: 1}); !errors.Is(err, ErrInvalidCaller) {
		t.Fatalf("NewClient(nil caller) error = %v, want ErrInvalidCaller", err)
	}
	if _, err := NewClient(testCaller{}, gsr.ServiceRef{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewClient(invalid target) error = %v, want ErrInvalidConfig", err)
	}
	reporter := &countingReporter{report: testReport("node-a")}
	if _, err := NewNodeAgentService(NodeAgentConfig{Reporter: reporter, ObserverNode: "node-a"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewNodeAgentService(missing discovery) error = %v, want ErrInvalidConfig", err)
	}
	invalidInterval := testNodeAgentConfig(reporter)
	invalidInterval.HeartbeatInterval = -time.Second
	if _, err := NewNodeAgentService(invalidInterval); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewNodeAgentService(negative interval) error = %v, want ErrInvalidConfig", err)
	}
}

type recordingCodec struct{ encoded int }

func (c *recordingCodec) Encode(gsr.CommandID, bool, any) ([]byte, error) {
	c.encoded++
	return []byte("fallback"), nil
}

func (*recordingCodec) Decode(gsr.CommandID, bool, []byte) (any, error) { return "fallback", nil }

type nilCaller struct{}

func (*nilCaller) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}

type testCaller struct{}

func (testCaller) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}

func TestCodecDoesNotMutateInputPayload(t *testing.T) {
	codec := NewCodec(nil)
	input := refreshNodeRequest{Node: "node-b"}
	payload, err := codec.Encode(commandRefreshNode, false, input)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if bytes.Contains(payload, []byte("node-a")) {
		t.Fatalf("payload = %q, contains unrelated node", payload)
	}
}
