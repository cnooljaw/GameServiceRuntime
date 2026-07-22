package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	testCommandIncrement gsr.CommandID = 0x03000101
	testCommandGet       gsr.CommandID = 0x03000102
)

func TestSnapshotRestoreCreatesNewServiceInstance(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	store := NewMemoryStore()
	manager, err := NewManager(runtime, store, Config{})
	if err != nil {
		t.Fatal(err)
	}

	oldRef, err := runtime.CreateService(gsr.ServiceSpec{Service: &integrationCounterService{value: 1, revision: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Send(oldRef, testCommandIncrement, 1); err != nil {
		t.Fatal(err)
	}
	key := Key{Namespace: "counter", ID: "example"}
	captured, err := manager.Capture(context.Background(), oldRef, key)
	if err != nil {
		t.Fatal(err)
	}
	if captured.State.Revision != 2 {
		t.Fatalf("captured revision = %d, want 2", captured.State.Revision)
	}
	if err := runtime.Stop(context.Background(), oldRef); err != nil {
		t.Fatal(err)
	}

	loaded, err := manager.Load(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := newIntegrationCounterService(loaded.State)
	if err != nil {
		t.Fatal(err)
	}
	newRef, err := runtime.CreateService(gsr.ServiceSpec{Service: restored})
	if err != nil {
		t.Fatal(err)
	}
	if newRef == oldRef {
		t.Fatalf("new ServiceRef = old ServiceRef %v", oldRef)
	}
	if _, err := runtime.Call(context.Background(), oldRef, testCommandGet, struct{}{}); !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("Call old Service error = %v, want ErrServiceClosed", err)
	}
	value, err := runtime.Call(context.Background(), newRef, testCommandGet, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if value != 2 {
		t.Fatalf("restored value = %v, want 2", value)
	}
}

type integrationCounterState struct {
	Value int `json:"value"`
}

type integrationCounterService struct {
	value    int
	revision uint64
}

func newIntegrationCounterService(state State) (*integrationCounterService, error) {
	if state.Schema != "counter" || state.Version != 1 || state.Revision == 0 {
		return nil, ErrInvalidState
	}
	var payload integrationCounterState
	if err := json.Unmarshal(state.Payload, &payload); err != nil {
		return nil, err
	}
	return &integrationCounterService{value: payload.Value, revision: state.Revision}, nil
}

func (*integrationCounterService) Commands() []gsr.CommandID {
	return []gsr.CommandID{testCommandIncrement, testCommandGet, CaptureCommand}
}

func (*integrationCounterService) Init(gsr.ServiceContext) error { return nil }

func (s *integrationCounterService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case testCommandIncrement:
		amount, ok := command.Payload.(int)
		if !ok {
			return ErrInvalidState
		}
		s.value += amount
		s.revision++
		return nil
	case testCommandGet:
		return ctx.Reply(s.value)
	case CaptureCommand:
		if _, ok := command.Payload.(CaptureRequest); !ok {
			return ErrInvalidResponse
		}
		payload, err := json.Marshal(integrationCounterState{Value: s.value})
		if err != nil {
			return err
		}
		return ctx.Reply(CaptureResponse{State: State{
			Schema: "counter", Version: 1, Revision: s.revision, Payload: payload,
		}})
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (*integrationCounterService) Stop(context.Context) error { return nil }
func (*integrationCounterService) Close() error               { return nil }
