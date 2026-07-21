package gsr_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestSlowCommandAndMailboxMetrics(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1, SlowCommandThreshold: time.Millisecond})
	defer rt.Close(context.Background())
	svc := &slowService{done: make(chan struct{})}
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-svc.done
	eventually(t, func() bool { return rt.Inspect().Metrics.Counter("slow_commands_total") == 1 })
	if got := rt.Inspect().Metrics.MailboxDepth(ref); got != 0 {
		t.Fatalf("mailbox depth = %d", got)
	}
	if got := rt.Inspect().Metrics.Duration("mailbox_wait_duration"); got <= 0 {
		t.Fatalf("mailbox wait = %v", got)
	}
}

func TestMailboxFullIsReportedAndObserved(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1, MailboxSize: 1})
	defer rt.Close(context.Background())
	svc := newSerialStopService()
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-svc.started
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1, nil); !errors.Is(err, gsr.ErrMailboxFull) {
		t.Fatalf("err = %v", err)
	}
	if got := rt.Inspect().Metrics.Counter("mailbox_rejected_total"); got != 1 {
		t.Fatalf("rejected = %d", got)
	}
	close(svc.release)
}
