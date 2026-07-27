package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"strconv"
	"sync"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/snapshot"
	"github.com/lijiawang/GameServiceRuntime/tooling/supervisor"
)

const (
	valueCommand gsr.CommandID = 0x7f000201 + iota
	panicCommand
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (result error) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 2})
	store := snapshot.NewMemoryStore()
	snapshots, err := snapshot.NewManager(runtime, store, snapshot.Config{})
	if err != nil {
		return err
	}
	key := supervisor.ServiceKey{Namespace: "player", ID: "42"}
	publisher := newBindingPublisher()
	factory := supervisor.ServiceFactoryFunc(func(ctx context.Context, key supervisor.ServiceKey, _ uint64) (gsr.ServiceSpec, error) {
		loaded, err := snapshots.Load(ctx, snapshot.Key{Namespace: key.Namespace, ID: key.ID})
		if err != nil {
			if errors.Is(err, snapshot.ErrSnapshotNotFound) {
				return gsr.ServiceSpec{}, errors.Join(supervisor.ErrSnapshotNotFound, err)
			}
			return gsr.ServiceSpec{}, err
		}
		service, err := newCounterService(key, loaded.State)
		if err != nil {
			return gsr.ServiceSpec{}, err
		}
		return gsr.ServiceSpec{Service: service}, nil
	})
	launcher, err := supervisor.NewRuntimeLauncher(runtime, factory, publisher)
	if err != nil {
		return err
	}
	runner, err := supervisor.NewRunner(runtime, launcher, supervisor.RunnerConfig{
		Workers: 1, QueueSize: 8, AttemptTimeout: time.Second,
		ResultTimeout: time.Second, ResultRetryInterval: time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		return err
	}
	defer func() {
		result = errors.Join(result, runner.Close(context.Background()), runtime.Close(context.Background()))
	}()

	supervisorService, err := supervisor.NewService(runner)
	if err != nil {
		return err
	}
	supervisorRef, err := runtime.CreateService(gsr.ServiceSpec{Service: supervisorService})
	if err != nil {
		return err
	}
	initial := &counterService{key: key, value: 2, revision: 2}
	decorated, err := supervisor.Decorate(initial, supervisor.DecoratorConfig{
		Key: key, Generation: 1, Supervisor: supervisorRef,
	})
	if err != nil {
		return err
	}
	oldRef, err := runtime.CreateService(gsr.ServiceSpec{Service: decorated})
	if err != nil {
		return err
	}
	client, err := supervisor.NewClient(runtime, supervisorRef)
	if err != nil {
		return err
	}
	policy := supervisor.RestartPolicy{
		Strategy: supervisor.RestartOnFailure, MaxAttempts: 3, MaxRestarts: 2,
		Window: time.Minute, MinBackoff: time.Millisecond, MaxBackoff: 4 * time.Millisecond,
	}
	if err := client.Register(context.Background(), supervisor.Registration{
		Key: key, Ref: oldRef, Generation: 1, Policy: policy,
	}); err != nil {
		return err
	}
	if err := publisher.Publish(context.Background(), key, oldRef); err != nil {
		return err
	}
	if _, err := snapshots.Capture(context.Background(), oldRef, snapshot.Key{Namespace: key.Namespace, ID: key.ID}); err != nil {
		return err
	}
	if _, err := runtime.Call(context.Background(), oldRef, panicCommand, nil); !errors.Is(err, gsr.ErrServiceFailed) {
		return fmt.Errorf("panic Call: %w", err)
	}
	record, err := waitForRecovery(client, key)
	if err != nil {
		return err
	}
	value, err := runtime.Call(context.Background(), record.Registration.Ref, valueCommand, nil)
	if err != nil {
		return err
	}
	restored, ok := value.(counterValue)
	if !ok {
		return supervisor.ErrInvalidResponse
	}
	fmt.Printf("old=%d new=%d generation=1->%d revision=%d value=%d\n",
		oldRef.ID, record.Registration.Ref.ID, record.Registration.Generation, restored.Revision, restored.Value)
	return nil
}

func waitForRecovery(client *supervisor.Client, key supervisor.ServiceKey) (supervisor.Record, error) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record, err := client.Get(context.Background(), key)
		if err != nil {
			return supervisor.Record{}, err
		}
		if record.Status == supervisor.ServiceRunning && record.Registration.Generation == 2 {
			return record, nil
		}
		time.Sleep(time.Millisecond)
	}
	return supervisor.Record{}, context.DeadlineExceeded
}

type counterValue struct {
	Value    int
	Revision uint64
}

type counterService struct {
	key      supervisor.ServiceKey
	value    int
	revision uint64
}

func newCounterService(key supervisor.ServiceKey, state snapshot.State) (*counterService, error) {
	value, err := strconv.Atoi(string(state.Payload))
	if err != nil || state.Schema != "counter" || state.Version != 1 || state.Revision == 0 {
		return nil, snapshot.ErrInvalidState
	}
	return &counterService{key: key, value: value, revision: state.Revision}, nil
}

func (*counterService) Init(gsr.ServiceContext) error { return nil }
func (s *counterService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case valueCommand:
		return ctx.Reply(counterValue{Value: s.value, Revision: s.revision})
	case panicCommand:
		panic("counter failed")
	case snapshot.CaptureCommand:
		request, ok := command.Payload.(snapshot.CaptureRequest)
		key := snapshot.Key{Namespace: s.key.Namespace, ID: s.key.ID}
		if !ok || request.Key != key {
			return snapshot.ErrInvalidKey
		}
		return ctx.Reply(snapshot.CaptureResponse{
			Key:   key,
			State: snapshot.State{Schema: "counter", Version: 1, Revision: s.revision, Payload: []byte(strconv.Itoa(s.value))},
		})
	default:
		return gsr.ErrUnknownCommand
	}
}
func (*counterService) Stop(context.Context) error { return nil }
func (*counterService) Close() error               { return nil }

type bindingPublisher struct {
	mu       sync.Mutex
	bindings map[supervisor.ServiceKey]gsr.ServiceRef
}

func newBindingPublisher() *bindingPublisher {
	return &bindingPublisher{bindings: make(map[supervisor.ServiceKey]gsr.ServiceRef)}
}

func (p *bindingPublisher) Publish(_ context.Context, key supervisor.ServiceKey, ref gsr.ServiceRef) error {
	p.mu.Lock()
	p.bindings[key] = ref
	p.mu.Unlock()
	return nil
}

func (p *bindingPublisher) Withdraw(_ context.Context, key supervisor.ServiceKey, ref gsr.ServiceRef) error {
	p.mu.Lock()
	if p.bindings[key] == ref {
		delete(p.bindings, key)
	}
	p.mu.Unlock()
	return nil
}
