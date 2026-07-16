package gsr_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

func TestStopWaitsForCurrentHandler(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 2})
	defer rt.Close(context.Background())
	svc := newSerialStopService()
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-svc.started
	stopped := make(chan error, 1)
	go func() { stopped <- rt.Stop(context.Background(), ref) }()
	select {
	case <-svc.stopCalled:
		t.Fatal("Stop ran concurrently with Handle")
	case <-time.After(20 * time.Millisecond):
	}
	close(svc.release)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if svc.concurrent.Load() {
		t.Fatal("Stop observed Handle as active")
	}
}

func TestHandlerPanicIsIsolated(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	defer rt.Close(context.Background())
	bad, _ := rt.CreateService(gsr.ServiceSpec{Service: panicService{}})
	if err := rt.Send(bad, 1, nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return rt.MetricsSnapshot().Counter("service_panics_total") == 1 })
	good := &recordingService{}
	goodRef, err := rt.CreateService(gsr.ServiceSpec{Service: good})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(goodRef, 1001, "alive"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return good.last() == "alive" })
}

func TestStopTimeoutStillRemovesService(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	defer rt.Close(context.Background())
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: blockingStopService{}, Policy: gsr.ServicePolicy{StopTimeout: 20 * time.Millisecond, CloseTimeout: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = rt.Stop(context.Background(), ref)
	if !errors.Is(err, gsr.ErrStopTimeout) {
		t.Fatalf("err = %v, want ErrStopTimeout", err)
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatal("Stop remained blocked after timeout")
	}
	if err := rt.Send(ref, 1, nil); !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("send err = %v", err)
	}
}

func TestCommandRegistryRejectsUnknownCommand(t *testing.T) {
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

func TestRuntimeCloseStopsServicesAndWakesPendingCall(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	svc := &pendingService{handled: make(chan struct{}), stopped: make(chan struct{}), closed: make(chan struct{})}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := rt.Call(context.Background(), ref, 1, nil); result <- err }()
	<-svc.handled
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, gsr.ErrRuntimeClosed) {
		t.Fatalf("call err = %v", err)
	}
	select {
	case <-svc.stopped:
	default:
		t.Fatal("Service.Stop was not called")
	}
	select {
	case <-svc.closed:
	default:
		t.Fatal("Service.Close was not called")
	}
	if err := rt.Send(ref, 1, nil); !errors.Is(err, gsr.ErrRuntimeClosed) {
		t.Fatalf("send err = %v", err)
	}
}

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
	if got := rt.MetricsSnapshot().Counter("handler_errors_total"); got != 1 {
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
	if got := rt.MetricsSnapshot().Counter("late_reply_total"); got != 1 {
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

func TestSlowCommandAndMailboxMetrics(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1, SlowCommandThreshold: time.Millisecond})
	defer rt.Close(context.Background())
	svc := &slowService{done: make(chan struct{})}
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err := rt.Send(ref, 1, nil); err != nil {
		t.Fatal(err)
	}
	<-svc.done
	eventually(t, func() bool { return rt.MetricsSnapshot().Counter("slow_commands_total") == 1 })
	if got := rt.MetricsSnapshot().MailboxDepth(ref); got != 0 {
		t.Fatalf("mailbox depth = %d", got)
	}
	if got := rt.MetricsSnapshot().Duration("mailbox_wait_duration"); got <= 0 {
		t.Fatalf("mailbox wait = %v", got)
	}
}

func TestStopCancelsTargetTimer(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	svc := &recordingService{}
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if _, err := rt.After(ref, 30*time.Millisecond, 20, "expired"); err != nil {
		t.Fatal(err)
	}
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := svc.last(); got != nil {
		t.Fatalf("timer delivered %v", got)
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
	if got := rt.MetricsSnapshot().Counter("mailbox_rejected_total"); got != 1 {
		t.Fatalf("rejected = %d", got)
	}
	close(svc.release)
}

func TestStopWakesPendingCall(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	svc := &pendingService{handled: make(chan struct{}), stopped: make(chan struct{}), closed: make(chan struct{})}
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
	result := make(chan error, 1)
	go func() { _, err := rt.Call(context.Background(), ref, 1, nil); result <- err }()
	<-svc.handled
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("call err = %v", err)
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

func TestCloseCannotStartBusinessCall(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1})
	defer rt.Close(context.Background())
	target, _ := rt.CreateService(gsr.ServiceSpec{Service: &fixedReplyService{reply: "pong"}})
	svc := &closeCallingService{target: target, result: make(chan error, 1)}
	ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err := rt.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := <-svc.result; !errors.Is(err, gsr.ErrServiceClosed) {
		t.Fatalf("Close call err = %v", err)
	}
}

func TestSendAndStopHaveOneAcceptanceBoundary(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 4})
	defer rt.Close(context.Background())
	for i := 0; i < 50; i++ {
		svc := newAcceptanceService()
		ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.Send(ref, 1, nil); err != nil {
			t.Fatal(err)
		}
		<-svc.started
		gate := make(chan struct{})
		sendResult := make(chan error, 1)
		stopResult := make(chan error, 1)
		go func() { <-gate; sendResult <- rt.Send(ref, 1, nil) }()
		go func() { <-gate; stopResult <- rt.Stop(context.Background(), ref) }()
		close(gate)
		sendErr := <-sendResult
		close(svc.release)
		if err := <-stopResult; err != nil {
			t.Fatal(err)
		}
		switch {
		case sendErr == nil && svc.handled.Load() != 2:
			t.Fatalf("accepted command was not drained: handled=%d", svc.handled.Load())
		case errors.Is(sendErr, gsr.ErrServiceClosed) && svc.handled.Load() != 1:
			t.Fatalf("rejected command was handled: handled=%d", svc.handled.Load())
		case sendErr != nil && !errors.Is(sendErr, gsr.ErrServiceClosed):
			t.Fatalf("send err = %v", sendErr)
		}
	}
}

func TestLifecyclePanicsAreIsolated(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	if _, err := rt.CreateService(gsr.ServiceSpec{Service: lifecyclePanicService{phase: "init"}}); !errors.Is(err, gsr.ErrServiceFailed) {
		t.Fatalf("Init panic err = %v", err)
	}
	for _, phase := range []string{"stop", "close"} {
		ref, err := rt.CreateService(gsr.ServiceSpec{Service: lifecyclePanicService{phase: phase}})
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.Stop(context.Background(), ref); !errors.Is(err, gsr.ErrServiceFailed) {
			t.Fatalf("%s panic err = %v", phase, err)
		}
	}
	good := &recordingService{}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: good})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Send(ref, 1001, "alive"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return good.last() == "alive" })
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

func TestRuntimeCloseWaitsForServiceInitialization(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", ShutdownTimeout: time.Second})
	svc := &blockingInitService{started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	created := make(chan error, 1)
	go func() {
		_, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
		created <- err
	}()
	<-svc.started
	closed := make(chan error, 1)
	go func() { closed <- rt.Close(context.Background()) }()
	select {
	case err := <-closed:
		t.Fatalf("Runtime.Close returned during Init: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(svc.release)
	if err := <-created; !errors.Is(err, gsr.ErrRuntimeClosed) {
		t.Fatalf("CreateService err = %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	select {
	case <-svc.closed:
	default:
		t.Fatal("partially initialized Service was not closed")
	}
}

func TestRuntimeCloseJoinsExistingServiceStop(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", ShutdownTimeout: time.Second})
	svc := &controlledStopService{stopStarted: make(chan struct{}), releaseStop: make(chan struct{}), closed: make(chan struct{})}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- rt.Stop(context.Background(), ref) }()
	<-svc.stopStarted
	runtimeClosed := make(chan error, 1)
	go func() { runtimeClosed <- rt.Close(context.Background()) }()
	select {
	case err := <-runtimeClosed:
		t.Fatalf("Runtime.Close returned during Service.Stop: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(svc.releaseStop)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if err := <-runtimeClosed; err != nil {
		t.Fatal(err)
	}
	select {
	case <-svc.closed:
	default:
		t.Fatal("Service.Close was skipped")
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

type fixedReplyService struct{ reply any }

func (*fixedReplyService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (*fixedReplyService) Init(gsr.ServiceContext) error { return nil }
func (s *fixedReplyService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	return ctx.Reply(s.reply)
}
func (*fixedReplyService) Stop(context.Context) error { return nil }
func (*fixedReplyService) Close() error               { return nil }

type callingService struct {
	ctx    gsr.ServiceContext
	target gsr.ServiceRef
}

func (*callingService) Commands() []gsr.CommandID           { return []gsr.CommandID{1} }
func (s *callingService) Init(ctx gsr.ServiceContext) error { s.ctx = ctx; return nil }
func (s *callingService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	value, err := s.ctx.Call(context.Background(), s.target, 1, nil)
	if err != nil {
		return err
	}
	return ctx.Reply(value)
}
func (*callingService) Stop(context.Context) error { return nil }
func (*callingService) Close() error               { return nil }

type closeCallingService struct {
	ctx    gsr.ServiceContext
	target gsr.ServiceRef
	result chan error
}

type acceptanceService struct {
	started chan struct{}
	release chan struct{}
	handled atomic.Int32
	once    sync.Once
}

func newAcceptanceService() *acceptanceService {
	return &acceptanceService{started: make(chan struct{}), release: make(chan struct{})}
}
func (*acceptanceService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (*acceptanceService) Init(gsr.ServiceContext) error { return nil }
func (s *acceptanceService) Handle(gsr.CommandContext, gsr.Command) error {
	s.handled.Add(1)
	s.once.Do(func() { close(s.started) })
	<-s.release
	return nil
}
func (*acceptanceService) Stop(context.Context) error { return nil }
func (*acceptanceService) Close() error               { return nil }

type lifecyclePanicService struct{ phase string }

func (lifecyclePanicService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (s lifecyclePanicService) Init(gsr.ServiceContext) error {
	if s.phase == "init" {
		panic("init")
	}
	return nil
}
func (lifecyclePanicService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (s lifecyclePanicService) Stop(context.Context) error {
	if s.phase == "stop" {
		panic("stop")
	}
	return nil
}
func (s lifecyclePanicService) Close() error {
	if s.phase == "close" {
		panic("close")
	}
	return nil
}

type blockingInitService struct{ started, release, closed chan struct{} }

func (*blockingInitService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (s *blockingInitService) Init(gsr.ServiceContext) error {
	close(s.started)
	<-s.release
	return nil
}
func (*blockingInitService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (*blockingInitService) Stop(context.Context) error                   { return nil }
func (s *blockingInitService) Close() error {
	close(s.closed)
	return nil
}

type controlledStopService struct{ stopStarted, releaseStop, closed chan struct{} }

func (*controlledStopService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (*controlledStopService) Init(gsr.ServiceContext) error { return nil }
func (*controlledStopService) Handle(gsr.CommandContext, gsr.Command) error {
	return nil
}
func (s *controlledStopService) Stop(context.Context) error {
	close(s.stopStarted)
	<-s.releaseStop
	return nil
}
func (s *controlledStopService) Close() error {
	close(s.closed)
	return nil
}

type contextCaptureService struct{ ctx gsr.ServiceContext }

func (*contextCaptureService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (s *contextCaptureService) Init(ctx gsr.ServiceContext) error {
	s.ctx = ctx
	return nil
}
func (*contextCaptureService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (*contextCaptureService) Stop(context.Context) error                   { return nil }
func (*contextCaptureService) Close() error                                 { return nil }

type stopCallingReplyService struct {
	ctx    gsr.ServiceContext
	target gsr.ServiceRef
	result chan error
}

func (*stopCallingReplyService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (s *stopCallingReplyService) Init(ctx gsr.ServiceContext) error {
	s.ctx = ctx
	return nil
}
func (*stopCallingReplyService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	return ctx.Reply("pong")
}
func (s *stopCallingReplyService) Stop(context.Context) error {
	_, err := s.ctx.Call(context.Background(), s.target, 2, nil)
	s.result <- err
	return err
}
func (*stopCallingReplyService) Close() error { return nil }

type bridgeService struct {
	ctx    gsr.ServiceContext
	target gsr.ServiceRef
}

func (*bridgeService) Commands() []gsr.CommandID { return []gsr.CommandID{1, 2} }
func (s *bridgeService) Init(ctx gsr.ServiceContext) error {
	s.ctx = ctx
	return nil
}
func (s *bridgeService) Handle(ctx gsr.CommandContext, cmd gsr.Command) error {
	if cmd.ID == 2 {
		return ctx.Reply("stop-pong")
	}
	value, err := s.ctx.Call(context.Background(), s.target, 1, nil)
	if err != nil {
		return err
	}
	return ctx.Reply(value)
}
func (*bridgeService) Stop(context.Context) error { return nil }
func (*bridgeService) Close() error               { return nil }

func (*closeCallingService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (s *closeCallingService) Init(ctx gsr.ServiceContext) error {
	s.ctx = ctx
	return nil
}
func (*closeCallingService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (*closeCallingService) Stop(context.Context) error                   { return nil }
func (s *closeCallingService) Close() error {
	_, err := s.ctx.Call(context.Background(), s.target, 1, nil)
	s.result <- err
	return nil
}

type fanoutService struct {
	ctx     gsr.ServiceContext
	targets []gsr.ServiceRef
	done    chan error
}

func (*fanoutService) Commands() []gsr.CommandID           { return []gsr.CommandID{1} }
func (s *fanoutService) Init(ctx gsr.ServiceContext) error { s.ctx = ctx; return nil }
func (s *fanoutService) Handle(gsr.CommandContext, gsr.Command) error {
	for _, target := range s.targets {
		if err := s.ctx.Send(target, 1001, nil); err != nil {
			s.done <- err
			return nil
		}
	}
	s.done <- nil
	return nil
}
func (*fanoutService) Stop(context.Context) error { return nil }
func (*fanoutService) Close() error               { return nil }

type serialStopService struct {
	started, release, stopCalled chan struct{}
	active                       atomic.Bool
	concurrent                   atomic.Bool
	once                         sync.Once
}

func newSerialStopService() *serialStopService {
	return &serialStopService{started: make(chan struct{}), release: make(chan struct{}), stopCalled: make(chan struct{})}
}
func (*serialStopService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (*serialStopService) Init(gsr.ServiceContext) error { return nil }
func (s *serialStopService) Handle(gsr.CommandContext, gsr.Command) error {
	s.active.Store(true)
	s.once.Do(func() { close(s.started) })
	<-s.release
	s.active.Store(false)
	return nil
}
func (s *serialStopService) Stop(context.Context) error {
	s.concurrent.Store(s.active.Load())
	close(s.stopCalled)
	return nil
}
func (*serialStopService) Close() error { return nil }

type panicService struct{}

func (panicService) Commands() []gsr.CommandID                    { return []gsr.CommandID{1} }
func (panicService) Init(gsr.ServiceContext) error                { return nil }
func (panicService) Handle(gsr.CommandContext, gsr.Command) error { panic("boom") }
func (panicService) Stop(context.Context) error                   { return nil }
func (panicService) Close() error                                 { return nil }

type blockingStopService struct{}

func (blockingStopService) Commands() []gsr.CommandID                    { return []gsr.CommandID{1} }
func (blockingStopService) Init(gsr.ServiceContext) error                { return nil }
func (blockingStopService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (blockingStopService) Stop(context.Context) error                   { select {} }
func (blockingStopService) Close() error                                 { return nil }

type pendingService struct{ handled, stopped, closed chan struct{} }

func (*pendingService) Commands() []gsr.CommandID                      { return []gsr.CommandID{1} }
func (*pendingService) Init(gsr.ServiceContext) error                  { return nil }
func (s *pendingService) Handle(gsr.CommandContext, gsr.Command) error { close(s.handled); return nil }
func (s *pendingService) Stop(context.Context) error                   { close(s.stopped); return nil }
func (s *pendingService) Close() error                                 { close(s.closed); return nil }

type errorService struct{ err error }

func (errorService) Commands() []gsr.CommandID                      { return []gsr.CommandID{1} }
func (errorService) Init(gsr.ServiceContext) error                  { return nil }
func (s errorService) Handle(gsr.CommandContext, gsr.Command) error { return s.err }
func (errorService) Stop(context.Context) error                     { return nil }
func (errorService) Close() error                                   { return nil }

type replyErrorService struct{ errors chan error }

func (*replyErrorService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (*replyErrorService) Init(gsr.ServiceContext) error { return nil }
func (s *replyErrorService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	s.errors <- ctx.Reply(nil)
	return nil
}
func (*replyErrorService) Stop(context.Context) error { return nil }
func (*replyErrorService) Close() error               { return nil }

type delayedReplyService struct {
	release  chan struct{}
	replyErr chan error
}

func (*delayedReplyService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (*delayedReplyService) Init(gsr.ServiceContext) error { return nil }
func (s *delayedReplyService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	<-s.release
	s.replyErr <- ctx.Reply(nil)
	return nil
}
func (*delayedReplyService) Stop(context.Context) error { return nil }
func (*delayedReplyService) Close() error               { return nil }

type selfCallingService struct {
	ctx  gsr.ServiceContext
	self gsr.ServiceRef
}

func (*selfCallingService) Commands() []gsr.CommandID           { return []gsr.CommandID{1} }
func (s *selfCallingService) Init(ctx gsr.ServiceContext) error { s.ctx = ctx; return nil }
func (s *selfCallingService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	_, err := s.ctx.Call(context.Background(), s.self, 1, nil)
	if err != nil {
		return err
	}
	return ctx.Reply(nil)
}
func (*selfCallingService) Stop(context.Context) error { return nil }
func (*selfCallingService) Close() error               { return nil }

type slowService struct{ done chan struct{} }

func (*slowService) Commands() []gsr.CommandID { return []gsr.CommandID{1} }
func (*slowService) Init(ctx gsr.ServiceContext) error {
	_ = ctx.Now()
	_ = ctx.Logger()
	ctx.Metrics().Inc("service_context_metrics_total")
	return nil
}
func (s *slowService) Handle(gsr.CommandContext, gsr.Command) error {
	time.Sleep(5 * time.Millisecond)
	close(s.done)
	return nil
}
func (*slowService) Stop(context.Context) error { return nil }
func (*slowService) Close() error               { return nil }

type undeclaredService struct{}

func (undeclaredService) Init(gsr.ServiceContext) error                { return nil }
func (undeclaredService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (undeclaredService) Stop(context.Context) error                   { return nil }
func (undeclaredService) Close() error                                 { return nil }

type duplicateCommandService struct{ undeclaredService }

func (duplicateCommandService) Commands() []gsr.CommandID { return []gsr.CommandID{1, 1} }
