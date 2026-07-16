package gsr_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestPrivateCommandSetRejectsUnknownCommand(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: &recordingService{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 9999, nil); !errors.Is(err, gsr.ErrCommandNotRegistered) {
		t.Fatalf("err = %v", err)
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

func TestCommandDeclarationsAreRequiredAndUnique(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	if _, err := rt.CreateService(gsr.ServiceSpec{Service: undeclaredService{}}); !errors.Is(err, gsr.ErrInvalidServiceSpec) {
		t.Fatalf("missing declaration err = %v", err)
	}
	if _, err := rt.CreateService(gsr.ServiceSpec{Service: duplicateCommandService{}}); !errors.Is(err, gsr.ErrCommandAlreadyRegistered) {
		t.Fatalf("duplicate declaration err = %v", err)
	}
}
