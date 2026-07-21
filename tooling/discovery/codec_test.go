package discovery

import (
	"errors"
	"reflect"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestCodecRoundTripsDiscoveryRequestsAndReplies(t *testing.T) {
	now := time.Date(2026, 7, 21, 15, 0, 0, 0, time.UTC)
	lease := NodeLease{Node: "node-a", Generation: 7, ExpiresAt: now.Add(time.Minute)}
	node := NodeRecord{ID: "node-a", Address: "127.0.0.1:9001", Generation: 7, LastSeen: now, ExpiresAt: now.Add(time.Minute)}
	ref := gsr.ServiceRef{Node: "node-a", ID: 10}
	tests := []struct {
		name     string
		command  gsr.CommandID
		response bool
		value    any
	}{
		{name: "register node request", command: commandRegisterNode, value: registerNodeRequest{Node: "node-a", Address: "127.0.0.1:9001"}},
		{name: "register node response", command: commandRegisterNode, response: true, value: leaseResponse{Lease: lease}},
		{name: "heartbeat request", command: commandHeartbeat, value: heartbeatRequest{Lease: lease}},
		{name: "heartbeat response", command: commandHeartbeat, response: true, value: leaseResponse{Lease: lease, Error: errorLeaseExpired}},
		{name: "unregister node request", command: commandUnregisterNode, value: unregisterNodeRequest{Lease: lease}},
		{name: "unregister node response", command: commandUnregisterNode, response: true, value: emptyResponse{}},
		{name: "get node request", command: commandGetNode, value: getNodeRequest{Node: "node-a"}},
		{name: "get node response", command: commandGetNode, response: true, value: nodeResponse{Node: node}},
		{name: "list nodes request", command: commandListNodes, value: listNodesRequest{}},
		{name: "list nodes response", command: commandListNodes, response: true, value: nodesResponse{Nodes: []NodeRecord{node}}},
		{name: "register name request", command: commandRegisterName, value: registerNameRequest{Lease: lease, Name: ".config", Ref: newWireServiceRef(ref)}},
		{name: "register name response", command: commandRegisterName, response: true, value: emptyResponse{Error: errorNameConflict}},
		{name: "unregister name request", command: commandUnregisterName, value: unregisterNameRequest{Lease: lease, Name: ".config", Ref: newWireServiceRef(ref)}},
		{name: "unregister name response", command: commandUnregisterName, response: true, value: emptyResponse{}},
		{name: "resolve name request", command: commandResolveName, value: resolveNameRequest{Name: ".config"}},
		{name: "resolve name response", command: commandResolveName, response: true, value: refResponse{Ref: newWireServiceRef(ref)}},
	}

	codec := NewCodec(nil)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := codec.Encode(test.command, test.response, test.value)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := codec.Decode(test.command, test.response, payload)
			if err != nil {
				t.Fatal(err)
			}
			if reflect.TypeOf(decoded) != reflect.TypeOf(test.value) || !reflect.DeepEqual(decoded, test.value) {
				t.Fatalf("decoded = %#v (%T), want %#v (%T)", decoded, decoded, test.value, test.value)
			}
		})
	}
}

func TestCodecUsesStableWireFieldNames(t *testing.T) {
	codec := NewCodec(nil)
	payload, err := codec.Encode(commandRegisterNode, false, registerNodeRequest{Node: "node-a", Address: "127.0.0.1:9001"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(payload), `{"node":"node-a","address":"127.0.0.1:9001"}`; got != want {
		t.Fatalf("register node JSON = %s, want %s", got, want)
	}
	payload, err = codec.Encode(commandResolveName, true, refResponse{Ref: newWireServiceRef(gsr.ServiceRef{Node: "node-a", ID: 10})})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(payload), `{"ref":{"node":"node-a","id":10},"error":""}`; got != want {
		t.Fatalf("resolve name JSON = %s, want %s", got, want)
	}
}

func TestCodecIgnoresUnknownWireFields(t *testing.T) {
	codec := NewCodec(nil)
	value, err := codec.Decode(commandRegisterNode, false, []byte(`{"node":"node-a","address":"127.0.0.1:9001","future":true}`))
	if err != nil {
		t.Fatal(err)
	}
	want := registerNodeRequest{Node: "node-a", Address: "127.0.0.1:9001"}
	if value != want {
		t.Fatalf("decoded = %#v, want %#v", value, want)
	}
}

func TestCodecRejectsTrailingJSONValue(t *testing.T) {
	codec := NewCodec(nil)
	_, err := codec.Decode(commandRegisterNode, false, []byte(`{"node":"node-a","address":"127.0.0.1:9001"}{}`))
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Decode error = %v, want ErrInvalidResponse", err)
	}
}

func TestCodecDelegatesUnknownCommand(t *testing.T) {
	fallback := &recordingCodec{}
	codec := NewCodec(fallback)
	const command gsr.CommandID = 99

	payload, err := codec.Encode(command, false, "request")
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "fallback" || fallback.encodedCommand != command {
		t.Fatalf("fallback Encode = %q, command %d", payload, fallback.encodedCommand)
	}
	value, err := codec.Decode(command, true, []byte("reply"))
	if err != nil {
		t.Fatal(err)
	}
	if value != "fallback" || fallback.decodedCommand != command {
		t.Fatalf("fallback Decode = %#v, command %d", value, fallback.decodedCommand)
	}
}

func TestCodecRejectsUnknownCommandWithoutFallback(t *testing.T) {
	codec := NewCodec(nil)
	if _, err := codec.Encode(99, false, "request"); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode error = %v, want ErrUnsupportedCommand", err)
	}
	if _, err := codec.Decode(99, true, []byte("reply")); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedCommand", err)
	}
}

func TestCodecRejectsInternalSweepCommand(t *testing.T) {
	codec := NewCodec(&recordingCodec{})
	if _, err := codec.Encode(commandSweepExpired, false, nil); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Encode error = %v, want ErrUnsupportedCommand", err)
	}
	if _, err := codec.Decode(commandSweepExpired, false, nil); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedCommand", err)
	}
}

type recordingCodec struct {
	encodedCommand gsr.CommandID
	decodedCommand gsr.CommandID
}

func (c *recordingCodec) Encode(command gsr.CommandID, _ bool, _ any) ([]byte, error) {
	c.encodedCommand = command
	return []byte("fallback"), nil
}

func (c *recordingCodec) Decode(command gsr.CommandID, _ bool, _ []byte) (any, error) {
	c.decodedCommand = command
	return "fallback", nil
}
