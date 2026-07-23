package drain

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	commandGuardExternal gsr.CommandID = 0x7f270201
	commandGuardInternal gsr.CommandID = 0x7f270202
	commandGuardBegin    gsr.CommandID = 0x7f270203
	commandGuardBadBegin gsr.CommandID = 0x7f270204
)

func TestDecorateRejectsInvalidGuardConfiguration(t *testing.T) {
	validController := gsr.ServiceRef{Node: "guard-node", ID: 1}
	validService := &guardTargetService{}

	tests := []struct {
		name    string
		service gsr.Service
		config  GuardConfig
	}{
		{name: "nil service", service: nil, config: GuardConfig{Controller: validController, ExternalCommands: []gsr.CommandID{commandGuardExternal}}},
		{name: "missing command declarer", service: noCommandGuardService{}, config: GuardConfig{Controller: validController, ExternalCommands: []gsr.CommandID{commandGuardExternal}}},
		{name: "invalid controller", service: validService, config: GuardConfig{ExternalCommands: []gsr.CommandID{commandGuardExternal}}},
		{name: "empty external commands", service: validService, config: GuardConfig{Controller: validController}},
		{name: "duplicate external command", service: validService, config: GuardConfig{Controller: validController, ExternalCommands: []gsr.CommandID{commandGuardExternal, commandGuardExternal}}},
		{name: "undeclared external command", service: validService, config: GuardConfig{Controller: validController, ExternalCommands: []gsr.CommandID{99}}},
		{name: "begin command collision", service: &guardCommandsService{commands: []gsr.CommandID{BeginDrainCommand}}, config: GuardConfig{Controller: validController, ExternalCommands: []gsr.CommandID{BeginDrainCommand}}},
		{name: "status command collision", service: &guardCommandsService{commands: []gsr.CommandID{GetDrainStatusCommand}}, config: GuardConfig{Controller: validController, ExternalCommands: []gsr.CommandID{GetDrainStatusCommand}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decorate(test.service, test.config); !errors.Is(err, ErrInvalidGuard) {
				t.Fatalf("Decorate() error = %v, want ErrInvalidGuard", err)
			}
		})
	}

	decorated, err := Decorate(validService, GuardConfig{
		Controller:       validController,
		ExternalCommands: []gsr.CommandID{commandGuardExternal},
	})
	if err != nil {
		t.Fatal(err)
	}
	declarer, ok := decorated.(gsr.CommandDeclarer)
	if !ok {
		t.Fatal("decorated Service does not declare Commands")
	}
	commands := declarer.Commands()
	if !sameCommandIDs(commands, []gsr.CommandID{commandGuardExternal, commandGuardInternal, BeginDrainCommand, GetDrainStatusCommand}) {
		t.Fatalf("Commands() = %#v", commands)
	}
	commands[0] = 99
	if again := declarer.Commands(); again[0] != commandGuardExternal {
		t.Fatalf("Commands() returned shared backing storage: %#v", again)
	}

	selfConfigured, err := Decorate(&guardTargetService{}, GuardConfig{
		Controller:       gsr.ServiceRef{Node: "guard-node", ID: 9},
		ExternalCommands: []gsr.CommandID{commandGuardExternal},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := selfConfigured.Init(guardInitContext{self: gsr.ServiceRef{Node: "guard-node", ID: 9}}); !errors.Is(err, ErrInvalidGuard) {
		t.Fatalf("Init(self controller) error = %v, want ErrInvalidGuard", err)
	}
	if _, err := NewGuardClient(nil, gsr.ServiceRef{Node: "guard-node", ID: 1}); !errors.Is(err, ErrInvalidCaller) {
		t.Fatalf("NewGuardClient(nil) error = %v, want ErrInvalidCaller", err)
	}
	if _, err := NewGuardClient(invalidVisitorCaller{}, gsr.ServiceRef{}); !errors.Is(err, ErrInvalidGuard) {
		t.Fatalf("NewGuardClient(invalid target) error = %v, want ErrInvalidGuard", err)
	}
	client, err := NewGuardClient(invalidVisitorCaller{}, gsr.ServiceRef{Node: "guard-node", ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Status(context.Background()); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("GuardClient.Status(invalid response) error = %v, want ErrInvalidResponse", err)
	}
}

func TestDrainGuardRejectsOnlyDeclaredExternalWorkAfterAuthorizedBegin(t *testing.T) {
	fixture := newGuardFixture(t)

	if got := fixture.callTarget(t, commandGuardExternal); got != "external" {
		t.Fatalf("external result before Begin = %#v", got)
	}
	if fixture.target.external.Load() != 1 {
		t.Fatalf("external calls before Begin = %d, want 1", fixture.target.external.Load())
	}
	before, err := fixture.client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status(before Begin) error = %v", err)
	}
	if before.Draining || !before.StartedAt.IsZero() {
		t.Fatalf("Status(before Begin) = %#v", before)
	}

	if _, err := fixture.client.Begin(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Begin(node caller) error = %v, want ErrUnauthorized", err)
	}
	if _, err := fixture.runtime.Call(context.Background(), fixture.other, commandGuardBegin, struct{}{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Begin(other Service) error = %v, want ErrUnauthorized", err)
	}
	if _, err := fixture.runtime.Call(context.Background(), fixture.controller, commandGuardBadBegin, struct{}{}); !errors.Is(err, ErrInvalidGuard) {
		t.Fatalf("Begin(invalid payload) error = %v, want ErrInvalidGuard", err)
	}

	value, err := fixture.runtime.Call(context.Background(), fixture.controller, commandGuardBegin, struct{}{})
	if err != nil {
		t.Fatalf("Begin(controller) error = %v", err)
	}
	started, ok := value.(DrainStatus)
	if !ok || !started.Draining || !started.StartedAt.Equal(fixture.clock.Now()) {
		t.Fatalf("Begin(controller) = %#v, want draining at %v", value, fixture.clock.Now())
	}
	if _, err := fixture.runtime.Call(context.Background(), fixture.targetRef, commandGuardExternal, struct{}{}); !errors.Is(err, ErrDraining) {
		t.Fatalf("external Call after Begin error = %v, want ErrDraining", err)
	}
	if fixture.target.external.Load() != 1 {
		t.Fatalf("rejected external Command reached inner Service: calls=%d", fixture.target.external.Load())
	}
	if got := fixture.callTarget(t, commandGuardInternal); got != "internal" {
		t.Fatalf("internal result after Begin = %#v", got)
	}
	if fixture.target.internal.Load() != 1 {
		t.Fatalf("internal calls after Begin = %d, want 1", fixture.target.internal.Load())
	}

	value, err = fixture.runtime.Call(context.Background(), fixture.controller, commandGuardBegin, struct{}{})
	if err != nil {
		t.Fatalf("duplicate Begin error = %v", err)
	}
	duplicate, ok := value.(DrainStatus)
	if !ok || duplicate != started {
		t.Fatalf("duplicate Begin = %#v, want %#v", value, started)
	}
	after, err := fixture.client.Status(context.Background())
	if err != nil || after != started {
		t.Fatalf("Status(after Begin) = %#v, %v; want %#v", after, err, started)
	}
	metrics := fixture.runtime.Inspect().Metrics
	if got := metrics.Counter("drain_guard_begun_total"); got != 1 {
		t.Fatalf("drain_guard_begun_total = %d, want 1", got)
	}
	if got := metrics.Counter("drain_guard_begin_duplicate_total"); got != 1 {
		t.Fatalf("drain_guard_begin_duplicate_total = %d, want 1", got)
	}
	if got := metrics.Counter("drain_guard_rejected_total"); got != 1 {
		t.Fatalf("drain_guard_rejected_total = %d, want 1", got)
	}
}

func TestDrainGuardForwardsLifecycleAndStartupCommand(t *testing.T) {
	startup := &guardStartupService{handled: make(chan struct{}, 1)}
	decorated, err := Decorate(startup, GuardConfig{
		Controller:       gsr.ServiceRef{Node: "guard-node", ID: 99},
		ExternalCommands: []gsr.CommandID{commandGuardExternal},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "guard-node", Workers: 1})
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: decorated})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-startup.handled:
	case <-time.After(time.Second):
		t.Fatal("wrapped StartupCommand was not handled")
	}
	if err := runtime.Stop(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if startup.stopCalls.Load() != 1 {
		t.Fatalf("inner Stop calls = %d, want 1", startup.stopCalls.Load())
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if startup.closeCalls.Load() != 1 {
		t.Fatalf("inner Close calls = %d, want 1", startup.closeCalls.Load())
	}
}

func TestDrainGuardCommitsAuthorizedBeginBeforeReplyFailure(t *testing.T) {
	controller := gsr.ServiceRef{Node: "guard-node", ID: 2}
	decorated, err := Decorate(&guardTargetService{}, GuardConfig{
		Controller:       controller,
		ExternalCommands: []gsr.CommandID{commandGuardExternal},
	})
	if err != nil {
		t.Fatal(err)
	}
	guard := decorated.(*guardDecorator)
	if err := guard.Init(guardInitContext{self: gsr.ServiceRef{Node: "guard-node", ID: 1}}); err != nil {
		t.Fatal(err)
	}
	invalid := &guardReplyContext{source: controller}
	if err := guard.Handle(invalid, gsr.Command{ID: BeginDrainCommand, Payload: struct{}{}}); err != nil {
		t.Fatalf("invalid Begin handler error = %v", err)
	}
	if guard.draining {
		t.Fatal("invalid Begin changed Drain state")
	}
	failed := &guardReplyContext{source: controller, replyErr: gsr.ErrReplyExpired}
	if err := guard.Handle(failed, gsr.Command{ID: BeginDrainCommand, Payload: beginDrainRequest{}}); !errors.Is(err, gsr.ErrReplyExpired) {
		t.Fatalf("Begin reply error = %v, want ErrReplyExpired", err)
	}
	if status := guard.currentStatus(); !status.Draining || status.StartedAt.IsZero() {
		t.Fatalf("Begin reply failure lost committed state: %#v", status)
	}
}

func TestDrainGuardHonorsExternalCommandsAlreadyQueuedBeforeBegin(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "guard-node", Workers: 2})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	controllerService := &guardBeginService{}
	controller, err := runtime.CreateService(gsr.ServiceSpec{Service: controllerService})
	if err != nil {
		t.Fatal(err)
	}
	blocking := &guardBlockingService{entered: make(chan struct{}), release: make(chan struct{})}
	released := false
	defer func() {
		if !released {
			close(blocking.release)
		}
	}()
	decorated, err := Decorate(blocking, GuardConfig{
		Controller:       controller,
		ExternalCommands: []gsr.CommandID{commandGuardExternal},
	})
	if err != nil {
		t.Fatal(err)
	}
	target, err := runtime.CreateService(gsr.ServiceSpec{Service: decorated})
	if err != nil {
		t.Fatal(err)
	}
	controllerService.target = target
	if err := runtime.Send(target, commandGuardExternal, struct{}{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocking.entered:
	case <-time.After(time.Second):
		t.Fatal("queued external Command did not begin")
	}
	beginResult := make(chan error, 1)
	go func() {
		_, err := runtime.Call(context.Background(), controller, commandGuardBegin, struct{}{})
		beginResult <- err
	}()
	select {
	case err := <-beginResult:
		t.Fatalf("Begin completed before earlier external Command: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(blocking.release)
	released = true
	if err := <-beginResult; err != nil {
		t.Fatalf("Begin after earlier external Command error = %v", err)
	}
	if blocking.external.Load() != 1 {
		t.Fatalf("queued external Commands handled = %d, want 1", blocking.external.Load())
	}
	if _, err := runtime.Call(context.Background(), target, commandGuardExternal, struct{}{}); !errors.Is(err, ErrDraining) {
		t.Fatalf("external Command after Begin error = %v, want ErrDraining", err)
	}
}

type guardFixture struct {
	runtime    *gsr.Runtime
	clock      *visitorTestClock
	client     *GuardClient
	target     *guardTargetService
	targetRef  gsr.ServiceRef
	controller gsr.ServiceRef
	other      gsr.ServiceRef
}

func newGuardFixture(t *testing.T) guardFixture {
	t.Helper()
	clock := &visitorTestClock{now: time.Date(2026, 7, 23, 13, 0, 0, 0, time.UTC)}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "guard-node", Workers: 2, Now: clock.Now})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	controllerService := &guardBeginService{}
	controller, err := runtime.CreateService(gsr.ServiceSpec{Service: controllerService})
	if err != nil {
		t.Fatal(err)
	}
	target := &guardTargetService{}
	decorated, err := Decorate(target, GuardConfig{
		Controller:       controller,
		ExternalCommands: []gsr.CommandID{commandGuardExternal},
	})
	if err != nil {
		t.Fatal(err)
	}
	targetRef, err := runtime.CreateService(gsr.ServiceSpec{Service: decorated})
	if err != nil {
		t.Fatal(err)
	}
	controllerService.target = targetRef
	other, err := runtime.CreateService(gsr.ServiceSpec{Service: &guardBeginService{target: targetRef}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewGuardClient(runtime, targetRef)
	if err != nil {
		t.Fatal(err)
	}
	return guardFixture{
		runtime:    runtime,
		clock:      clock,
		client:     client,
		target:     target,
		targetRef:  targetRef,
		controller: controller,
		other:      other,
	}
}

func (f guardFixture) callTarget(t *testing.T, command gsr.CommandID) any {
	t.Helper()
	value, err := f.runtime.Call(context.Background(), f.targetRef, command, struct{}{})
	if err != nil {
		t.Fatalf("target Command %d error = %v", command, err)
	}
	return value
}

type noCommandGuardService struct{}

func (noCommandGuardService) Init(gsr.ServiceContext) error                { return nil }
func (noCommandGuardService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (noCommandGuardService) Stop(context.Context) error                   { return nil }
func (noCommandGuardService) Close() error                                 { return nil }

type guardCommandsService struct{ commands []gsr.CommandID }

func (s *guardCommandsService) Commands() []gsr.CommandID {
	return append([]gsr.CommandID(nil), s.commands...)
}
func (*guardCommandsService) Init(gsr.ServiceContext) error                { return nil }
func (*guardCommandsService) Handle(gsr.CommandContext, gsr.Command) error { return nil }
func (*guardCommandsService) Stop(context.Context) error                   { return nil }
func (*guardCommandsService) Close() error                                 { return nil }

type guardTargetService struct {
	external atomic.Int32
	internal atomic.Int32
}

func (*guardTargetService) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandGuardExternal, commandGuardInternal}
}
func (*guardTargetService) Init(gsr.ServiceContext) error { return nil }
func (s *guardTargetService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	if _, ok := command.Payload.(struct{}); !ok {
		return ErrInvalidGuard
	}
	switch command.ID {
	case commandGuardExternal:
		s.external.Add(1)
		return commandContext.Reply("external")
	case commandGuardInternal:
		s.internal.Add(1)
		return commandContext.Reply("internal")
	default:
		return gsr.ErrCommandNotRegistered
	}
}
func (*guardTargetService) Stop(context.Context) error { return nil }
func (*guardTargetService) Close() error               { return nil }

type guardBlockingService struct {
	entered  chan struct{}
	release  chan struct{}
	external atomic.Int32
}

func (*guardBlockingService) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandGuardExternal}
}
func (*guardBlockingService) Init(gsr.ServiceContext) error { return nil }
func (s *guardBlockingService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandGuardExternal {
		return gsr.ErrCommandNotRegistered
	}
	if _, ok := command.Payload.(struct{}); !ok {
		return ErrInvalidGuard
	}
	s.external.Add(1)
	close(s.entered)
	<-s.release
	return nil
}
func (*guardBlockingService) Stop(context.Context) error { return nil }
func (*guardBlockingService) Close() error               { return nil }

type guardBeginService struct {
	context gsr.ServiceContext
	target  gsr.ServiceRef
}

func (*guardBeginService) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandGuardBegin, commandGuardBadBegin}
}
func (s *guardBeginService) Init(context gsr.ServiceContext) error {
	s.context = context
	return nil
}
func (s *guardBeginService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	if _, ok := command.Payload.(struct{}); !ok {
		return ErrInvalidGuard
	}
	if command.ID == commandGuardBadBegin {
		value, err := s.context.Call(context.Background(), s.target, BeginDrainCommand, struct{}{})
		if err != nil {
			return err
		}
		response, ok := value.(drainStatusResponse)
		if !ok {
			return ErrInvalidResponse
		}
		return guardErrorFromCode(response.Error)
	}
	if command.ID != commandGuardBegin {
		return gsr.ErrCommandNotRegistered
	}
	client, err := NewGuardClient(s.context, s.target)
	if err != nil {
		return err
	}
	status, err := client.Begin(context.Background())
	if err != nil {
		return err
	}
	return commandContext.Reply(status)
}
func (*guardBeginService) Stop(context.Context) error { return nil }
func (*guardBeginService) Close() error               { return nil }

type guardStartupService struct {
	handled    chan struct{}
	stopCalls  atomic.Int32
	closeCalls atomic.Int32
}

func (*guardStartupService) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandGuardExternal, commandGuardInternal}
}
func (*guardStartupService) StartupCommand() (gsr.Command, bool) {
	return gsr.Command{ID: commandGuardInternal, Payload: struct{}{}}, true
}
func (*guardStartupService) Init(gsr.ServiceContext) error { return nil }
func (s *guardStartupService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandGuardInternal {
		return gsr.ErrCommandNotRegistered
	}
	s.handled <- struct{}{}
	return nil
}
func (s *guardStartupService) Stop(context.Context) error {
	s.stopCalls.Add(1)
	return nil
}
func (s *guardStartupService) Close() error {
	s.closeCalls.Add(1)
	return nil
}

type guardInitContext struct{ self gsr.ServiceRef }

func (c guardInitContext) Self() gsr.ServiceRef                        { return c.self }
func (guardInitContext) Send(gsr.ServiceRef, gsr.CommandID, any) error { return nil }
func (guardInitContext) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}
func (guardInitContext) After(time.Duration, gsr.CommandID, any) (gsr.TimerID, error) {
	return 0, nil
}
func (guardInitContext) Now() time.Time       { return time.Now() }
func (guardInitContext) Logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func (guardInitContext) Metrics() gsr.Metrics { return noOpMetrics{} }

type guardReplyContext struct {
	source   gsr.ServiceRef
	replyErr error
}

func (*guardReplyContext) Self() gsr.ServiceRef     { return gsr.ServiceRef{Node: "guard-node", ID: 1} }
func (c *guardReplyContext) Source() gsr.ServiceRef { return c.source }
func (c *guardReplyContext) Reply(any) error        { return c.replyErr }

func sameCommandIDs(left, right []gsr.CommandID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
