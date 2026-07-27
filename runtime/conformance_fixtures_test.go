package gsr_test

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type fixedReplyService struct{ reply any }

func (*fixedReplyService) Init(gsr.ServiceContext) error { return nil }

func (s *fixedReplyService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	return ctx.Reply(s.reply)
}

func (*fixedReplyService) Stop(context.Context) error { return nil }

func (*fixedReplyService) Close() error { return nil }

type callingService struct {
	ctx    gsr.ServiceContext
	target gsr.ServiceRef
}

func (s *callingService) Init(ctx gsr.ServiceContext) error { s.ctx = ctx; return nil }

func (s *callingService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	value, err := s.ctx.Call(context.Background(), s.target, 1, nil)
	if err != nil {
		return err
	}
	return ctx.Reply(value)
}

func (*callingService) Stop(context.Context) error { return nil }

func (*callingService) Close() error { return nil }

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

func (*acceptanceService) Init(gsr.ServiceContext) error { return nil }

func (s *acceptanceService) Handle(gsr.CommandContext, gsr.Command) error {
	s.handled.Add(1)
	s.once.Do(func() { close(s.started) })
	<-s.release
	return nil
}

func (*acceptanceService) Stop(context.Context) error { return nil }

func (*acceptanceService) Close() error { return nil }

type lifecyclePanicService struct{ phase string }

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

func (s *blockingInitService) Init(gsr.ServiceContext) error {
	close(s.started)
	<-s.release
	return nil
}

func (*blockingInitService) Handle(gsr.CommandContext, gsr.Command) error { return nil }

func (*blockingInitService) Stop(context.Context) error { return nil }

func (s *blockingInitService) Close() error {
	close(s.closed)
	return nil
}

type controlledStopService struct{ stopStarted, releaseStop, closed chan struct{} }

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

func (s *contextCaptureService) Init(ctx gsr.ServiceContext) error {
	s.ctx = ctx
	return nil
}

func (*contextCaptureService) Handle(gsr.CommandContext, gsr.Command) error { return nil }

func (*contextCaptureService) Stop(context.Context) error { return nil }

func (*contextCaptureService) Close() error { return nil }

type stopCallingReplyService struct {
	ctx    gsr.ServiceContext
	target gsr.ServiceRef
	result chan error
}

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

func (*bridgeService) Close() error { return nil }

func (s *closeCallingService) Init(ctx gsr.ServiceContext) error {
	s.ctx = ctx
	return nil
}

func (*closeCallingService) Handle(gsr.CommandContext, gsr.Command) error { return nil }

func (*closeCallingService) Stop(context.Context) error { return nil }

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

func (*fanoutService) Close() error { return nil }

type serialStopService struct {
	started, release, stopCalled chan struct{}
	active                       atomic.Bool
	concurrent                   atomic.Bool
	once                         sync.Once
}

func newSerialStopService() *serialStopService {
	return &serialStopService{started: make(chan struct{}), release: make(chan struct{}), stopCalled: make(chan struct{})}
}

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

func (panicService) Init(gsr.ServiceContext) error { return nil }

func (panicService) Handle(gsr.CommandContext, gsr.Command) error { panic("boom") }

func (panicService) Stop(context.Context) error { return nil }

func (panicService) Close() error { return nil }

type releasableStopService struct{ started, release chan struct{} }

func (*releasableStopService) Init(gsr.ServiceContext) error { return nil }

func (*releasableStopService) Handle(gsr.CommandContext, gsr.Command) error { return nil }

func (s *releasableStopService) Stop(context.Context) error {
	close(s.started)
	<-s.release
	return nil
}

func (*releasableStopService) Close() error { return nil }

type lifecycleErrorService struct{ stopErr, closeErr error }

func (lifecycleErrorService) Init(gsr.ServiceContext) error { return nil }

func (lifecycleErrorService) Handle(gsr.CommandContext, gsr.Command) error { return nil }

func (s lifecycleErrorService) Stop(context.Context) error { return s.stopErr }

func (s lifecycleErrorService) Close() error { return s.closeErr }

type pendingService struct{ handled, stopped, closed chan struct{} }

func (*pendingService) Init(gsr.ServiceContext) error { return nil }

func (s *pendingService) Handle(gsr.CommandContext, gsr.Command) error { close(s.handled); return nil }

func (s *pendingService) Stop(context.Context) error { close(s.stopped); return nil }

func (s *pendingService) Close() error { close(s.closed); return nil }

type errorService struct{ err error }

func (errorService) Init(gsr.ServiceContext) error { return nil }

func (s errorService) Handle(gsr.CommandContext, gsr.Command) error { return s.err }

func (errorService) Stop(context.Context) error { return nil }

func (errorService) Close() error { return nil }

type replyErrorService struct{ errors chan error }

func (*replyErrorService) Init(gsr.ServiceContext) error { return nil }

func (s *replyErrorService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	s.errors <- ctx.Reply(nil)
	return nil
}

func (*replyErrorService) Stop(context.Context) error { return nil }

func (*replyErrorService) Close() error { return nil }

type delayedReplyService struct {
	release  chan struct{}
	replyErr chan error
}

func (*delayedReplyService) Init(gsr.ServiceContext) error { return nil }

func (s *delayedReplyService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	<-s.release
	s.replyErr <- ctx.Reply(nil)
	return nil
}

func (*delayedReplyService) Stop(context.Context) error { return nil }

func (*delayedReplyService) Close() error { return nil }

type selfCallingService struct {
	ctx  gsr.ServiceContext
	self gsr.ServiceRef
}

func (s *selfCallingService) Init(ctx gsr.ServiceContext) error { s.ctx = ctx; return nil }

func (s *selfCallingService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	_, err := s.ctx.Call(context.Background(), s.self, 1, nil)
	if err != nil {
		return err
	}
	return ctx.Reply(nil)
}

func (*selfCallingService) Stop(context.Context) error { return nil }

func (*selfCallingService) Close() error { return nil }

type slowService struct{ done chan struct{} }

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

func (*slowService) Close() error { return nil }
