package gsr_test

import (
	"context"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestServiceCallYieldsExecutionPermit(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	defer rt.Close(context.Background())
	target, err := rt.CreateService(gsr.ServiceSpec{Service: &fixedReplyService{reply: "pong"}})
	if err != nil {
		t.Fatal(err)
	}
	caller := &callingService{target: target}
	callerRef, err := rt.CreateService(gsr.ServiceSpec{Service: caller})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := rt.Call(ctx, callerRef, 1, nil)
	if err != nil || got != "pong" {
		t.Fatalf("got %v, err %v", got, err)
	}
}

func TestHandlerSendDoesNotBlockOnReadyQueue(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1, MailboxSize: 64})
	defer rt.Close(context.Background())
	targets := make([]gsr.ServiceRef, 32)
	for i := range targets {
		ref, err := rt.CreateService(gsr.ServiceSpec{Service: &recordingService{}})
		if err != nil {
			t.Fatal(err)
		}
		targets[i] = ref
	}
	sender := &fanoutService{targets: targets, done: make(chan error, 1)}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: sender})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-sender.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("handler blocked while scheduling ready services")
	}
}
