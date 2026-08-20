package nhsk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestHostCreatesResolvesAndStopsBattleThroughFactoryService(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk-host-test", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	factory, err := NewBattleFactoryService(runtime, runtime)
	if err != nil {
		t.Fatal(err)
	}
	factoryRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "nhsk-factory", Service: factory})
	if err != nil {
		t.Fatal(err)
	}
	host, err := NewNHSKHostService(NHSKHostConfig{MaxActiveBattles: 2, FactoryRef: factoryRef})
	if err != nil {
		t.Fatal(err)
	}
	hostRef, err := runtime.CreateService(gsr.ServiceSpec{Name: ".nhsk-game-host", Service: host})
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Call(context.Background(), hostRef, BeginCreateBattleCommand, CreateBattleRequest{BattleID: 33})
	if err != nil {
		t.Fatal(err)
	}
	operation := value.(CreateBattleOperation)
	if operation.Phase != HostOperationCreating || operation.OperationID == 0 {
		t.Fatalf("create operation = %#v", operation)
	}
	if !waitForBattleRef(t, runtime, hostRef, 33) {
		t.Fatal("battle was not created")
	}
	resolved, err := runtime.Call(context.Background(), hostRef, ResolveBattleCommand, ResolveBattleRequest{BattleID: 33})
	if err != nil {
		t.Fatal(err)
	}
	ref := resolved.(ResolveBattleResult).Ref
	if ref.ID == 0 || ref.Node != "nhsk-host-test" {
		t.Fatalf("resolved ref = %#v", ref)
	}
	if _, err := runtime.Call(context.Background(), hostRef, RequestDeleteBattleCommand, RequestDeleteBattleRequest{BattleID: 33, Ref: ref}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, err := runtime.Call(context.Background(), hostRef, ResolveBattleCommand, ResolveBattleRequest{BattleID: 33})
		if err == gsr.ErrServiceNotFound || err == gsr.ErrServiceClosed {
			return
		}
	}
	t.Fatal("battle was not stopped")
}

func TestHostCreateReturnsBeforeLifecycleRunnerFinishes(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk-async-create", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	creator := &blockingBattleCreator{started: make(chan struct{}), release: make(chan struct{}), ref: gsr.ServiceRef{Node: "nhsk-async-create", ID: 99}}
	released := false
	defer func() {
		if !released {
			close(creator.release)
		}
	}()
	factory, err := NewBattleFactoryService(creator, runtime)
	if err != nil {
		t.Fatal(err)
	}
	factoryRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "factory", Service: factory})
	if err != nil {
		t.Fatal(err)
	}
	host, err := NewNHSKHostService(NHSKHostConfig{MaxActiveBattles: 1, FactoryRef: factoryRef})
	if err != nil {
		t.Fatal(err)
	}
	hostRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "host", Service: host})
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Call(context.Background(), hostRef, BeginCreateBattleCommand, CreateBattleRequest{BattleID: 91})
	if err != nil {
		t.Fatal(err)
	}
	operation := value.(CreateBattleOperation)
	if operation.Phase != HostOperationCreating {
		t.Fatalf("operation = %#v", operation)
	}
	select {
	case <-creator.started:
	case <-time.After(time.Second):
		t.Fatal("lifecycle runner did not start create")
	}
	resolveCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := runtime.Call(resolveCtx, hostRef, ResolveBattleCommand, ResolveBattleRequest{BattleID: 91}); !errors.Is(err, gsr.ErrServiceNotFound) {
		t.Fatalf("Resolve while creating = %v", err)
	}
	close(creator.release)
	released = true
	if !waitForBattleRef(t, runtime, hostRef, 91) {
		t.Fatal("async create result was not committed")
	}
}

func TestHostCoalescesOnlyIdenticalCreateWhileCreating(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk-create-coalesce", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	creator := &blockingBattleCreator{started: make(chan struct{}), release: make(chan struct{}), ref: gsr.ServiceRef{Node: "nhsk-create-coalesce", ID: 99}}
	released := false
	defer func() {
		if !released {
			close(creator.release)
		}
	}()
	_, hostRef := createHostFixture(t, runtime, creator, 1)

	request := CreateBattleRequest{BattleID: 93, IsNewbie: true, ConnectionGeneration: 7}
	first := beginCreateOperation(t, runtime, hostRef, request)
	select {
	case <-creator.started:
	case <-time.After(time.Second):
		t.Fatal("lifecycle runner did not start create")
	}
	duplicate := beginCreateOperation(t, runtime, hostRef, request)
	if duplicate != first {
		t.Fatalf("duplicate operation = %#v, want %#v", duplicate, first)
	}
	conflict := beginCreateOperation(t, runtime, hostRef, CreateBattleRequest{BattleID: 93, ConnectionGeneration: 7})
	if conflict.OperationID != 0 || conflict.Rejection != errBattleIDInUse.Error() {
		t.Fatalf("conflicting operation = %#v", conflict)
	}
	close(creator.release)
	released = true
	if !waitForBattleRef(t, runtime, hostRef, 93) {
		t.Fatal("async create result was not committed")
	}
}

func TestFactoryStopsCreateThatFinishesAfterConnectionGenerationClosed(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk-create-generation", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	creator := &blockingBattleCreator{started: make(chan struct{}), release: make(chan struct{}), ref: gsr.ServiceRef{Node: "nhsk-create-generation", ID: 99}}
	released := false
	defer func() {
		if !released {
			close(creator.release)
		}
	}()
	factoryRef, hostRef := createHostFixture(t, runtime, creator, 1)
	operation := beginCreateOperation(t, runtime, hostRef, CreateBattleRequest{BattleID: 94, ConnectionGeneration: 8})
	select {
	case <-creator.started:
	case <-time.After(time.Second):
		t.Fatal("lifecycle runner did not start create")
	}
	if err := runtime.Send(factoryRef, stopGenerationCommand, stopGenerationInternal{Generation: 8}); err != nil {
		t.Fatal(err)
	}
	close(creator.release)
	released = true
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		value, err := runtime.Call(context.Background(), hostRef, GetCreateBattleOperationCommand, GetCreateBattleOperationRequest{OperationID: operation.OperationID})
		if err != nil {
			t.Fatal(err)
		}
		current := value.(CreateBattleOperation)
		if current.Phase == HostOperationFailed {
			if current.Rejection != errConnectionGenerationClosed.Error() {
				t.Fatalf("operation = %#v", current)
			}
			if _, err := runtime.Call(context.Background(), hostRef, ResolveBattleCommand, ResolveBattleRequest{BattleID: 94}); !errors.Is(err, gsr.ErrServiceNotFound) {
				t.Fatalf("Resolve closed-generation Battle = %v", err)
			}
			return
		}
	}
	t.Fatal("closed-generation create did not fail")
}

func TestHostLegacyActiveSameIDStopsThenCreatesNewRef(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk-replace", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	factory, err := NewBattleFactoryService(runtime, runtime)
	if err != nil {
		t.Fatal(err)
	}
	factoryRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "factory", Service: factory})
	if err != nil {
		t.Fatal(err)
	}
	host, err := NewNHSKHostService(NHSKHostConfig{MaxActiveBattles: 1, FactoryRef: factoryRef})
	if err != nil {
		t.Fatal(err)
	}
	hostRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "host", Service: host})
	if err != nil {
		t.Fatal(err)
	}
	first := beginAndWaitBattle(t, runtime, hostRef, CreateBattleRequest{BattleID: 92, ConnectionGeneration: 1})
	value, err := runtime.Call(context.Background(), hostRef, BeginCreateBattleCommand, CreateBattleRequest{BattleID: 92, ConnectionGeneration: 2})
	if err != nil {
		t.Fatal(err)
	}
	operation := value.(CreateBattleOperation)
	if operation.Phase != HostOperationStopping || operation.OperationID == 0 {
		t.Fatalf("replacement operation = %#v", operation)
	}
	if err := waitLegacyCreate(context.Background(), runtime, hostRef, operation); err != nil {
		t.Fatal(err)
	}
	resolved, err := runtime.Call(context.Background(), hostRef, ResolveBattleCommand, ResolveBattleRequest{BattleID: 92})
	if err != nil {
		t.Fatal(err)
	}
	second := resolved.(ResolveBattleResult).Ref
	if second == first {
		t.Fatalf("replacement reused ServiceRef %#v", second)
	}
	value, err = runtime.Call(context.Background(), hostRef, BeginCreateBattleCommand, CreateBattleRequest{BattleID: 92})
	if err != nil {
		t.Fatal(err)
	}
	if operation := value.(CreateBattleOperation); operation.Rejection != errBattleIDInUse.Error() {
		t.Fatalf("Cluster same-ID create = %#v", operation)
	}
}

func TestClusterServiceCannotClaimLegacyConnectionGeneration(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "nhsk-cluster-generation", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	_, hostRef := createHostFixture(t, runtime, runtime, 1)
	active := beginAndWaitBattle(t, runtime, hostRef, CreateBattleRequest{BattleID: 95})
	caller := &hostCreateCallerService{host: hostRef}
	callerRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "cluster-caller", Service: caller})
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Call(context.Background(), callerRef, 1, CreateBattleRequest{BattleID: 95, ConnectionGeneration: 9})
	if err != nil {
		t.Fatal(err)
	}
	operation := value.(CreateBattleOperation)
	if operation.Phase != HostOperationFailed || operation.Rejection != errConnectionGenerationSource.Error() {
		t.Fatalf("forged Legacy generation operation = %#v", operation)
	}
	resolved, err := runtime.Call(context.Background(), hostRef, ResolveBattleCommand, ResolveBattleRequest{BattleID: 95})
	if err != nil {
		t.Fatal(err)
	}
	if current := resolved.(ResolveBattleResult).Ref; current != active {
		t.Fatalf("active Ref changed to %#v, want %#v", current, active)
	}
}

func beginAndWaitBattle(t *testing.T, runtime *gsr.Runtime, host gsr.ServiceRef, request CreateBattleRequest) gsr.ServiceRef {
	t.Helper()
	operation := beginCreateOperation(t, runtime, host, request)
	if err := waitLegacyCreate(context.Background(), runtime, host, operation); err != nil {
		t.Fatal(err)
	}
	resolved, err := runtime.Call(context.Background(), host, ResolveBattleCommand, ResolveBattleRequest{BattleID: request.BattleID})
	if err != nil {
		t.Fatal(err)
	}
	return resolved.(ResolveBattleResult).Ref
}

func beginCreateOperation(t *testing.T, runtime *gsr.Runtime, host gsr.ServiceRef, request CreateBattleRequest) CreateBattleOperation {
	t.Helper()
	value, err := runtime.Call(context.Background(), host, BeginCreateBattleCommand, request)
	if err != nil {
		t.Fatal(err)
	}
	return value.(CreateBattleOperation)
}

func createHostFixture(t *testing.T, runtime *gsr.Runtime, creator game.ServiceCreator, capacity uint32) (gsr.ServiceRef, gsr.ServiceRef) {
	t.Helper()
	factory, err := NewBattleFactoryService(creator, runtime)
	if err != nil {
		t.Fatal(err)
	}
	factoryRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "factory", Service: factory})
	if err != nil {
		t.Fatal(err)
	}
	host, err := NewNHSKHostService(NHSKHostConfig{MaxActiveBattles: capacity, FactoryRef: factoryRef})
	if err != nil {
		t.Fatal(err)
	}
	hostRef, err := runtime.CreateService(gsr.ServiceSpec{Name: "host", Service: host})
	if err != nil {
		t.Fatal(err)
	}
	return factoryRef, hostRef
}

type blockingBattleCreator struct {
	started chan struct{}
	release chan struct{}
	ref     gsr.ServiceRef
}

type hostCreateCallerService struct {
	host    gsr.ServiceRef
	service gsr.ServiceContext
}

func (caller *hostCreateCallerService) Init(service gsr.ServiceContext) error {
	caller.service = service
	return nil
}

func (caller *hostCreateCallerService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	request, ok := command.Payload.(CreateBattleRequest)
	if !ok {
		return gsr.ErrInvalidClusterEnvelope
	}
	value, err := caller.service.Call(context.Background(), caller.host, BeginCreateBattleCommand, request)
	if err != nil {
		return err
	}
	return ctx.Reply(value)
}

func (*hostCreateCallerService) Stop(context.Context) error { return nil }
func (caller *hostCreateCallerService) Close() error {
	caller.service = nil
	return nil
}

func (creator *blockingBattleCreator) CreateService(gsr.ServiceSpec) (gsr.ServiceRef, error) {
	close(creator.started)
	<-creator.release
	return creator.ref, nil
}

func waitForBattleRef(t *testing.T, runtime *gsr.Runtime, host gsr.ServiceRef, battleID game.BattleID) bool {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		value, err := runtime.Call(context.Background(), host, ResolveBattleCommand, ResolveBattleRequest{BattleID: battleID})
		if err == nil && value.(ResolveBattleResult).Ref.ID != 0 {
			return true
		}
	}
	return false
}
