package supervisor

import (
	"context"
	"errors"
	"sync"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestRuntimeLauncherPreparesDecoratedUnnamedService(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	received := make(chan receivedFailureNotice, 1)
	supervisorRef, err := runtime.CreateService(gsr.ServiceSpec{Service: &failureNoticeCaptureService{received: received}})
	if err != nil {
		t.Fatal(err)
	}
	factory := ServiceFactoryFunc(func(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error) {
		return gsr.ServiceSpec{Service: panicDecoratorService{}}, nil
	})
	launcher, err := NewRuntimeLauncher(runtime, factory, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := testLaunchRequest(supervisorRef)
	ref, err := launcher.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if ref == request.FailedRef {
		t.Fatalf("prepared Ref reused failed Ref: %v", ref)
	}
	if _, err := runtime.Call(context.Background(), ref, 10, nil); !errors.Is(err, gsr.ErrServiceFailed) {
		t.Fatalf("Call error = %v, want ErrServiceFailed", err)
	}
	got := <-received
	if got.notice.Generation != request.Generation || got.notice.Key != request.Key || got.source != ref {
		t.Fatalf("notice = %#v source=%v", got.notice, got.source)
	}
}

func TestRuntimeLauncherRejectsNamedFactorySpecAndClassifiesErrors(t *testing.T) {
	control := &launcherControl{}
	tests := []struct {
		name    string
		factory ServiceFactory
		want    error
	}{
		{
			name: "snapshot", want: ErrSnapshotNotFound,
			factory: ServiceFactoryFunc(func(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error) {
				return gsr.ServiceSpec{}, errors.Join(ErrSnapshotNotFound, errors.New("missing"))
			}),
		},
		{
			name: "named", want: ErrInvalidConfig,
			factory: ServiceFactoryFunc(func(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error) {
				return gsr.ServiceSpec{Name: ".player", Service: panicDecoratorService{}}, nil
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			launcher, err := NewRuntimeLauncher(control, test.factory, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := launcher.Prepare(context.Background(), testLaunchRequest(gsr.ServiceRef{Node: "node-a", ID: 1})); !errors.Is(err, test.want) {
				t.Fatalf("Prepare error = %v, want %v", err, test.want)
			}
		})
	}

	control.createErr = errors.New("create failed")
	launcher, err := NewRuntimeLauncher(control, ServiceFactoryFunc(func(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error) {
		return gsr.ServiceSpec{Service: panicDecoratorService{}}, nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := launcher.Prepare(context.Background(), testLaunchRequest(gsr.ServiceRef{Node: "node-a", ID: 1})); !errors.Is(err, ErrCreateFailed) {
		t.Fatalf("Prepare error = %v, want ErrCreateFailed", err)
	}
}

func TestRuntimeLauncherCommitAndAbortConvergeBindingBeforeStop(t *testing.T) {
	events := &runnerEvents{}
	control := &launcherControl{events: events, ref: gsr.ServiceRef{Node: "node-a", ID: 8}}
	publisher := &launcherPublisher{events: events}
	launcher, err := NewRuntimeLauncher(control, ServiceFactoryFunc(func(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error) {
		return gsr.ServiceSpec{Service: panicDecoratorService{}}, nil
	}), publisher)
	if err != nil {
		t.Fatal(err)
	}
	request := testLaunchRequest(gsr.ServiceRef{Node: "node-a", ID: 1})
	if err := launcher.Commit(context.Background(), request, control.ref); err != nil {
		t.Fatal(err)
	}
	if err := launcher.Abort(context.Background(), request, control.ref); err != nil {
		t.Fatal(err)
	}
	want := []string{"publish", "withdraw", "stop"}
	if got := events.snapshot(); !equalStrings(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestRuntimeLauncherAbortJoinsCleanupFailuresAndStillStops(t *testing.T) {
	events := &runnerEvents{}
	control := &launcherControl{events: events, stopErr: errors.New("stop failed")}
	publisher := &launcherPublisher{events: events, withdrawErr: errors.New("withdraw failed")}
	launcher, err := NewRuntimeLauncher(control, ServiceFactoryFunc(func(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error) {
		return gsr.ServiceSpec{Service: panicDecoratorService{}}, nil
	}), publisher)
	if err != nil {
		t.Fatal(err)
	}
	err = launcher.Abort(context.Background(), testLaunchRequest(gsr.ServiceRef{Node: "node-a", ID: 1}), gsr.ServiceRef{Node: "node-a", ID: 8})
	if !errors.Is(err, ErrAbortFailed) || !errors.Is(err, control.stopErr) || !errors.Is(err, publisher.withdrawErr) {
		t.Fatalf("Abort error = %v", err)
	}
	want := []string{"withdraw", "stop"}
	if got := events.snapshot(); !equalStrings(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestRuntimeLauncherRejectsDynamicNilDependencies(t *testing.T) {
	var control *launcherControl
	var factory ServiceFactoryFunc
	var publisher *launcherPublisher
	if _, err := NewRuntimeLauncher(control, ServiceFactoryFunc(func(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error) {
		return gsr.ServiceSpec{}, nil
	}), nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil control error = %v", err)
	}
	if _, err := NewRuntimeLauncher(&launcherControl{}, factory, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil factory error = %v", err)
	}
	if _, err := NewRuntimeLauncher(&launcherControl{}, ServiceFactoryFunc(func(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error) {
		return gsr.ServiceSpec{}, nil
	}), publisher); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil publisher error = %v", err)
	}
}

func testLaunchRequest(supervisor gsr.ServiceRef) LaunchRequest {
	return LaunchRequest{
		Supervisor: supervisor, Key: testServiceKey(), FailedRef: gsr.ServiceRef{Node: "node-a", ID: 7},
		Generation: 2, Attempt: 1,
	}
}

type launcherControl struct {
	mu        sync.Mutex
	events    *runnerEvents
	ref       gsr.ServiceRef
	createErr error
	stopErr   error
	stopCalls int
}

func (c *launcherControl) CreateService(gsr.ServiceSpec) (gsr.ServiceRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ref, c.createErr
}

func (c *launcherControl) Stop(context.Context, gsr.ServiceRef) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopCalls++
	c.events.add("stop")
	return c.stopErr
}

type launcherPublisher struct {
	events      *runnerEvents
	publishErr  error
	withdrawErr error
}

func (p *launcherPublisher) Publish(context.Context, ServiceKey, gsr.ServiceRef) error {
	p.events.add("publish")
	return p.publishErr
}

func (p *launcherPublisher) Withdraw(context.Context, ServiceKey, gsr.ServiceRef) error {
	p.events.add("withdraw")
	return p.withdrawErr
}
