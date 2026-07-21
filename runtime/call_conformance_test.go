package gsr_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestHandlerErrorFailsCallAndRecordsMetric(t *testing.T) {
	sentinel := errors.New("handler failed")
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: errorService{err: sentinel}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rt.Call(context.Background(), ref, 1, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("call err = %v", err)
	}
	if got := rt.Inspect().Metrics.Counter("handler_errors_total"); got != 1 {
		t.Fatalf("handler errors = %d", got)
	}
}

func TestSendReplyAndLateReplyHaveDistinctErrors(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 2})
	defer rt.Close(context.Background())
	sendReply := &replyErrorService{errors: make(chan error, 1)}
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: sendReply})
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-sendReply.errors; !errors.Is(err, gsr.ErrReplyUnavailable) {
		t.Fatalf("send reply err = %v", err)
	}
	delayed := &delayedReplyService{release: make(chan struct{}), replyErr: make(chan error, 1)}
	delayedRef, _ := rt.CreateService(gsr.ServiceSpec{Service: delayed})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := rt.Call(ctx, delayedRef, 1, nil); !errors.Is(err, gsr.ErrTimeout) {
		t.Fatalf("call err = %v", err)
	}
	close(delayed.release)
	if err := <-delayed.replyErr; !errors.Is(err, gsr.ErrReplyExpired) {
		t.Fatalf("late reply err = %v", err)
	}
	if got := rt.Inspect().Metrics.Counter("late_reply_total"); got != 1 {
		t.Fatalf("late replies = %d", got)
	}
}

func TestSelfCallIsRejected(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	defer rt.Close(context.Background())
	svc := &selfCallingService{}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	svc.self = ref
	_, err = rt.Call(context.Background(), ref, 1, nil)
	if !errors.Is(err, gsr.ErrCallCycle) {
		t.Fatalf("err = %v", err)
	}
}

func TestCrossServiceCallCycleIsRejected(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	defer rt.Close(context.Background())
	a := &callingService{}
	b := &callingService{}
	aRef, _ := rt.CreateService(gsr.ServiceSpec{Service: a})
	bRef, _ := rt.CreateService(gsr.ServiceSpec{Service: b})
	a.target, b.target = bRef, aRef
	_, err := rt.Call(context.Background(), aRef, 1, nil)
	if !errors.Is(err, gsr.ErrCallCycle) {
		t.Fatalf("err = %v", err)
	}
}

func TestHandlerPanicFailsCall(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: panicService{}})
	_, err := rt.Call(context.Background(), ref, 1, nil)
	if !errors.Is(err, gsr.ErrServiceFailed) {
		t.Fatalf("err = %v", err)
	}
}

func TestCompletedCallPathDoesNotAffectStopCall(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	defer rt.Close(context.Background())
	a := &stopCallingReplyService{result: make(chan error, 1)}
	aRef, _ := rt.CreateService(gsr.ServiceSpec{Service: a})
	b := &bridgeService{target: aRef}
	bRef, _ := rt.CreateService(gsr.ServiceSpec{Service: b})
	a.target = bRef
	if _, err := rt.Call(context.Background(), bRef, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := rt.Stop(context.Background(), aRef); err != nil {
		t.Fatal(err)
	}
	if err := <-a.result; err != nil {
		t.Fatalf("Stop call inherited an old CallPath: %v", err)
	}
}

func TestServiceContextCallOutsideSerialPathIsRejected(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	target, _ := rt.CreateService(gsr.ServiceSpec{Service: &fixedReplyService{reply: "pong"}})
	svc := &contextCaptureService{}
	if _, err := rt.CreateService(gsr.ServiceSpec{Service: svc}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ctx.Call(context.Background(), target, 1, nil); !errors.Is(err, gsr.ErrCallNotAllowed) {
		t.Fatalf("Call err = %v", err)
	}
}
