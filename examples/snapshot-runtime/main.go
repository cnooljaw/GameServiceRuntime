package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/snapshot"
)

const (
	commandIncrement gsr.CommandID = 1
	commandGet       gsr.CommandID = 2
)

func main() {
	ctx := context.Background()
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "snapshot-example", Workers: 2})
	defer runtime.Close(ctx)
	store := snapshot.NewMemoryStore()
	manager, err := snapshot.NewManager(runtime, store, snapshot.Config{})
	if err != nil {
		log.Fatal(err)
	}

	key := snapshot.Key{Namespace: "counter", ID: "example"}
	oldRef, err := runtime.CreateService(gsr.ServiceSpec{Service: &counterService{key: key, value: 1, revision: 1}})
	if err != nil {
		log.Fatal(err)
	}
	if err := runtime.Send(oldRef, commandIncrement, 1); err != nil {
		log.Fatal(err)
	}
	if _, err := manager.Capture(ctx, oldRef, key); err != nil {
		log.Fatal(err)
	}
	if err := runtime.Stop(ctx, oldRef); err != nil {
		log.Fatal(err)
	}

	loaded, err := manager.Load(ctx, key)
	if err != nil {
		log.Fatal(err)
	}
	restored, err := newCounterServiceFromSnapshot(loaded)
	if err != nil {
		log.Fatal(err)
	}
	newRef, err := runtime.CreateService(gsr.ServiceSpec{Service: restored})
	if err != nil {
		log.Fatal(err)
	}
	if newRef == oldRef {
		log.Fatal("restored Service reused the old ServiceRef")
	}
	value, err := runtime.Call(ctx, newRef, commandGet, struct{}{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(value)
}

type counterState struct {
	Value int `json:"value"`
}

type counterService struct {
	key      snapshot.Key
	value    int
	revision uint64
}

func newCounterServiceFromSnapshot(saved snapshot.Snapshot) (*counterService, error) {
	if saved.Key.Namespace == "" || saved.Key.ID == "" {
		return nil, snapshot.ErrInvalidKey
	}
	state := saved.State
	if state.Schema != "counter" || state.Version != 1 || state.Revision == 0 {
		return nil, snapshot.ErrInvalidState
	}
	var payload counterState
	if err := json.Unmarshal(state.Payload, &payload); err != nil {
		return nil, err
	}
	return &counterService{key: saved.Key, value: payload.Value, revision: state.Revision}, nil
}

func (*counterService) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandIncrement, commandGet, snapshot.CaptureCommand}
}

func (*counterService) Init(gsr.ServiceContext) error { return nil }

func (s *counterService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case commandIncrement:
		amount, ok := command.Payload.(int)
		if !ok {
			return snapshot.ErrInvalidState
		}
		s.value += amount
		s.revision++
		return nil
	case commandGet:
		return ctx.Reply(s.value)
	case snapshot.CaptureCommand:
		request, ok := command.Payload.(snapshot.CaptureRequest)
		if !ok {
			return snapshot.ErrInvalidResponse
		}
		if request.Key != s.key {
			return snapshot.ErrInvalidKey
		}
		payload, err := json.Marshal(counterState{Value: s.value})
		if err != nil {
			return err
		}
		return ctx.Reply(snapshot.CaptureResponse{Key: s.key, State: snapshot.State{
			Schema: "counter", Version: 1, Revision: s.revision, Payload: payload,
		}})
	default:
		return gsr.ErrCommandNotRegistered
	}
}

func (*counterService) Stop(context.Context) error { return nil }
func (*counterService) Close() error               { return nil }
