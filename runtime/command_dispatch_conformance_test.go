package gsr_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestUnknownCommandIsHandledInsideService(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	service := &unknownCommandService{handled: make(chan gsr.CommandID, 1)}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 9999, nil); err != nil {
		t.Fatalf("err = %v", err)
	}
	select {
	case command := <-service.handled:
		if command != 9999 {
			t.Fatalf("command = %d", command)
		}
	case <-time.After(time.Second):
		t.Fatal("unknown Command did not reach Handle")
	}
}

func TestServiceNameRegistrationLifecycle(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	ref, err := rt.CreateService(gsr.ServiceSpec{Name: ".echo", Service: &recordingService{}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := rt.Resolve(".echo")
	if err != nil || got != ref {
		t.Fatalf("got %v, err %v", got, err)
	}
	if _, err := rt.CreateService(gsr.ServiceSpec{Name: ".echo", Service: &recordingService{}}); !errors.Is(err, gsr.ErrServiceNameConflict) {
		t.Fatalf("duplicate err = %v", err)
	}
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Resolve(".echo"); !errors.Is(err, gsr.ErrServiceNotFound) {
		t.Fatalf("resolve err = %v", err)
	}
}

func TestTombstonesAreBounded(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", TombstoneTTL: time.Hour, TombstoneLimit: 2})
	defer rt.Close(context.Background())
	refs := make([]gsr.ServiceRef, 3)
	for i := range refs {
		refs[i], _ = rt.CreateService(gsr.ServiceSpec{Service: &recordingService{}})
		if err := rt.Stop(context.Background(), refs[i]); err != nil {
			t.Fatal(err)
		}
	}
	closed, missing := 0, 0
	for _, ref := range refs {
		err := rt.Send(ref, 1001, nil)
		if errors.Is(err, gsr.ErrServiceClosed) {
			closed++
		}
		if errors.Is(err, gsr.ErrServiceNotFound) {
			missing++
		}
	}
	if closed > 2 || missing == 0 {
		t.Fatalf("closed=%d missing=%d", closed, missing)
	}
}

func TestServiceDoesNotDeclareCommands(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: &unknownCommandService{}})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if _, err := rt.Call(context.Background(), ref, 9999, nil); !errors.Is(err, gsr.ErrUnknownCommand) {
		t.Fatalf("Call() error = %v, want ErrUnknownCommand", err)
	}
}

func TestAfterDoesNotPreflightServiceCommands(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	service := &unknownCommandService{handled: make(chan gsr.CommandID, 1)}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: service})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.After(ref, time.Millisecond, 7777, nil); err != nil {
		t.Fatalf("After() error = %v", err)
	}
	select {
	case command := <-service.handled:
		if command != 7777 {
			t.Fatalf("command = %d", command)
		}
	case <-time.After(time.Second):
		t.Fatal("timer Command did not reach Handle")
	}
}

type unknownCommandService struct {
	handled chan gsr.CommandID
}

func (*unknownCommandService) Init(gsr.ServiceContext) error { return nil }
func (s *unknownCommandService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	if s.handled != nil {
		s.handled <- command.ID
	}
	return gsr.ErrUnknownCommand
}
func (*unknownCommandService) Stop(context.Context) error { return nil }
func (*unknownCommandService) Close() error               { return nil }
