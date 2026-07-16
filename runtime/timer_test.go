package gsr_test

import (
	"context"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestAfterDeliversCommand(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	svc := &recordingService{}
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if _, err := rt.After(ref, 5*time.Millisecond, 20, "expired"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return svc.last() == "expired" })
}

func TestCancelTimerPreventsDelivery(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	svc := &recordingService{}
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
	id, _ := rt.After(ref, 30*time.Millisecond, 20, "expired")
	if err := rt.Cancel(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := svc.last(); got != nil {
		t.Fatalf("payload = %v, want nil", got)
	}
}
