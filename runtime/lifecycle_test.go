package gsr_test

import (
	"context"
	"errors"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestStopRejectsNewSend(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: &recordingService{}})
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1, nil); !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("err = %v", err)
	}
}
