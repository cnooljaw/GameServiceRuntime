package control

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
	"github.com/lijiawang/GameServiceRuntime/tooling/drain"
	"github.com/lijiawang/GameServiceRuntime/tooling/servicegroup"
)

const (
	commandDrainGateway gsr.CommandID = 0x7f250301
	commandDrainWork    gsr.CommandID = 0x7f250302
	commandDrainVisitor gsr.CommandID = 0x7f250303
)

func TestDrainCoordinatorRejectsInvalidConfigAndUnauthorizedRequests(t *testing.T) {
	valid := DrainCoordinatorConfig{
		Gateway:           gsr.ServiceRef{Node: "node-a", ID: 1},
		AllowedPrincipals: []Principal{"ops"},
		Directory:         gsr.ServiceRef{Node: "node-a", ID: 2},
		VisitorRegistry:   gsr.ServiceRef{Node: "node-a", ID: 3},
	}
	invalid := []DrainCoordinatorConfig{
		{},
		{Gateway: valid.Gateway, Directory: valid.Directory, VisitorRegistry: valid.VisitorRegistry},
		{Gateway: valid.Gateway, AllowedPrincipals: []Principal{"ops", "ops"}, Directory: valid.Directory, VisitorRegistry: valid.VisitorRegistry},
		{Gateway: valid.Gateway, AllowedPrincipals: valid.AllowedPrincipals, Directory: valid.Directory, VisitorRegistry: valid.VisitorRegistry, CallTimeout: -time.Second},
		{Gateway: valid.Gateway, AllowedPrincipals: valid.AllowedPrincipals, Directory: valid.Directory, VisitorRegistry: valid.VisitorRegistry, AuditLimit: -1},
	}
	for _, config := range invalid {
		if _, err := NewDrainCoordinatorService(config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewDrainCoordinatorService(%#v) error = %v, want ErrInvalidConfig", config, err)
		}
	}
	if _, err := NewDrainClient((*nilCaller)(nil), valid.Gateway); !errors.Is(err, ErrInvalidCaller) {
		t.Fatalf("NewDrainClient(nil) error = %v, want ErrInvalidCaller", err)
	}
	if _, err := NewDrainClient(testCaller{}, gsr.ServiceRef{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewDrainClient(invalid target) error = %v, want ErrInvalidConfig", err)
	}

	fixture := newDrainFixture(t, []Principal{"ops"})
	direct, err := NewDrainClient(fixture.runtime, fixture.coordinator)
	if err != nil {
		t.Fatal(err)
	}
	request := fixture.request("direct-denied", "ops")
	if _, err := direct.Start(context.Background(), request); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("direct Start() error = %v, want ErrUnauthorized", err)
	}
	current := fixture.getDirectory(t)
	if current.Version != fixture.initial.Version {
		t.Fatalf("unauthorized Start changed Directory: %#v, initial %#v", current, fixture.initial)
	}
	if _, err := fixture.client.Start(context.Background(), fixture.request("principal-denied", "not-allowed")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("gateway Start(denied principal) error = %v, want ErrUnauthorized", err)
	}
	audit, err := fixture.client.ListAudit(context.Background(), "ops")
	if err != nil {
		t.Fatalf("ListAudit() error = %v", err)
	}
	if len(audit) != 1 || audit[0].Principal != "not-allowed" || audit[0].Outcome != "denied" {
		t.Fatalf("denied audit = %#v", audit)
	}
}

func TestDrainCoordinatorPublishesGuardsWaitsAndIsIdempotent(t *testing.T) {
	fixture := newDrainFixture(t, []Principal{"ops", "reader"})
	lease := fixture.acquireStrong(t)
	request := fixture.request("drain-1", "ops")

	operation, err := fixture.client.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if operation.Phase != DrainWaitingVisitors || len(operation.Targets) != 1 || operation.Targets[0].Ref != fixture.old || !operation.Targets[0].Guarded || operation.Targets[0].StrongVisitorCount != 1 {
		t.Fatalf("Start() = %#v", operation)
	}
	if operation.Original.Version != fixture.initial.Version || operation.Published.Version.Revision != fixture.initial.Version.Revision+1 || len(operation.Published.Refs) != 1 || operation.Published.Refs[0] != fixture.next {
		t.Fatalf("Start() versions and sets = %#v", operation)
	}
	if _, err := fixture.runtime.Call(context.Background(), fixture.old, commandDrainWork, struct{}{}); !errors.Is(err, drain.ErrDraining) {
		t.Fatalf("old work after Start error = %v, want ErrDraining", err)
	}

	duplicate, err := fixture.client.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("duplicate Start() error = %v", err)
	}
	if duplicate.Phase != DrainWaitingVisitors || duplicate.Published.Version != operation.Published.Version {
		t.Fatalf("duplicate Start() = %#v, want %#v", duplicate, operation)
	}
	if current := fixture.getDirectory(t); current.Version != operation.Published.Version {
		t.Fatalf("duplicate Start published again: %#v", current)
	}
	conflict := request
	conflict.NextTags = map[string]string{"changed": "yes"}
	if _, err := fixture.client.Start(context.Background(), conflict); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("conflicting Start() error = %v, want ErrRequestConflict", err)
	}
	if _, err := fixture.client.Get(context.Background(), request.RequestID, "reader"); !errors.Is(err, ErrOperationOwnerMismatch) {
		t.Fatalf("Get(other principal) error = %v, want ErrOperationOwnerMismatch", err)
	}

	operation.Targets[0].Guarded = false
	operation.Published.Tags["caller"] = "mutation"
	stored, err := fixture.client.Get(context.Background(), request.RequestID, request.Principal)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !stored.Targets[0].Guarded || stored.Published.Tags["caller"] != "stable" {
		t.Fatalf("Get returned caller-mutated operation: %#v", stored)
	}

	if err := fixture.release(t, lease); err != nil {
		t.Fatalf("release strong visitor error = %v", err)
	}
	ready, err := fixture.client.Resolve(context.Background(), request.RequestID, request.Principal)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if ready.Phase != DrainReadyToStop || ready.Targets[0].StrongVisitorCount != 0 {
		t.Fatalf("Resolve() = %#v, want ReadyToStop", ready)
	}
	if timers := fixture.runtime.Inspect().Timers; timers != 1 {
		t.Fatalf("Coordinator added timers beyond the Visitor Registry sweep: %d, want 1", timers)
	}
}

func TestDrainCoordinatorDoesNotGuardRetainedRefAndStopsOnSupersededDirectory(t *testing.T) {
	fixture := newDrainFixture(t, []Principal{"ops"})
	request := fixture.request("retain-old", "ops")
	request.NextRefs = []gsr.ServiceRef{fixture.old, fixture.next}
	operation, err := fixture.client.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start(retain old) error = %v", err)
	}
	if operation.Phase != DrainReadyToStop || len(operation.Targets) != 0 {
		t.Fatalf("Start(retain old) = %#v", operation)
	}
	if _, err := fixture.runtime.Call(context.Background(), fixture.old, commandDrainWork, struct{}{}); err != nil {
		t.Fatalf("retained old work error = %v", err)
	}

	fixture = newDrainFixture(t, []Principal{"ops"})
	lease := fixture.acquireStrong(t)
	request = fixture.request("superseded", "ops")
	if _, err := fixture.client.Start(context.Background(), request); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	directory, err := servicegroup.NewClient(fixture.runtime, fixture.directory)
	if err != nil {
		t.Fatal(err)
	}
	current := fixture.getDirectory(t)
	if _, err := directory.Publish(context.Background(), request.Group, current.Version, []gsr.ServiceRef{fixture.old}, map[string]string{"rollback": "manual"}); err != nil {
		t.Fatalf("manual Publish() error = %v", err)
	}
	if err := fixture.release(t, lease); err != nil {
		t.Fatal(err)
	}
	operation, err = fixture.client.Resolve(context.Background(), request.RequestID, request.Principal)
	if err != nil {
		t.Fatalf("Resolve(superseded) error = %v", err)
	}
	if operation.Phase != DrainSuperseded {
		t.Fatalf("Resolve(superseded) = %#v", operation)
	}
}

func TestDrainCoordinatorConfirmsLostPublishReplyWithoutRepublishing(t *testing.T) {
	directory, err := servicegroup.NewDirectoryService(servicegroup.DirectoryConfig{PublisherNode: "node-a", WatchTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	lostReply := &lostPublishReplyService{inner: directory}
	fixture := newDrainFixtureWithDirectory(t, []Principal{"ops"}, lostReply, 20*time.Millisecond)
	lostReply.drop.Store(true)
	request := fixture.request("publish-unknown", "ops")

	unknown, err := fixture.client.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if unknown.Phase != DrainPublishUnknown {
		t.Fatalf("Start() = %#v, want PublishUnknown", unknown)
	}
	if publishes := lostReply.publishes.Load(); publishes != 2 { // initial fixture publish plus operation publish
		t.Fatalf("Directory Publish calls = %d, want 2", publishes)
	}
	if current := fixture.getDirectory(t); current.Version.Revision != fixture.initial.Version.Revision+1 {
		t.Fatalf("lost-reply Publish did not commit: %#v", current)
	}

	confirmed, err := fixture.client.Resolve(context.Background(), request.RequestID, request.Principal)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if confirmed.Phase != DrainReadyToStop || len(confirmed.Targets) != 1 || !confirmed.Targets[0].Guarded {
		t.Fatalf("Resolve() = %#v, want guarded ReadyToStop", confirmed)
	}
	if publishes := lostReply.publishes.Load(); publishes != 2 {
		t.Fatalf("Resolve republished unknown operation: calls=%d, want 2", publishes)
	}
}

func TestDrainCoordinatorMarksUnknownPublishSupersededWhenDirectoryAdvances(t *testing.T) {
	directory, err := servicegroup.NewDirectoryService(servicegroup.DirectoryConfig{PublisherNode: "node-a", WatchTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	lostReply := &lostPublishReplyService{inner: directory}
	fixture := newDrainFixtureWithDirectory(t, []Principal{"ops"}, lostReply, 20*time.Millisecond)
	lostReply.drop.Store(true)
	request := fixture.request("publish-superseded", "ops")
	if operation, err := fixture.client.Start(context.Background(), request); err != nil || operation.Phase != DrainPublishUnknown {
		t.Fatalf("Start() = %#v, %v; want PublishUnknown", operation, err)
	}

	lostReply.drop.Store(false)
	directoryClient, err := servicegroup.NewClient(fixture.runtime, fixture.directory)
	if err != nil {
		t.Fatal(err)
	}
	current := fixture.getDirectory(t)
	if _, err := directoryClient.Publish(context.Background(), request.Group, current.Version, []gsr.ServiceRef{fixture.next}, map[string]string{"manual": "replacement"}); err != nil {
		t.Fatalf("manual Publish() error = %v", err)
	}
	operation, err := fixture.client.Resolve(context.Background(), request.RequestID, request.Principal)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if operation.Phase != DrainSuperseded || operation.Published.Name != "" || operation.Published.Version != (servicegroup.ServiceSetVersion{}) || operation.Published.Refs != nil || operation.Published.Tags != nil || len(operation.Targets) != 0 {
		t.Fatalf("Resolve() = %#v, want unconfirmed Publish superseded", operation)
	}
	if _, err := fixture.runtime.Call(context.Background(), fixture.old, commandDrainWork, struct{}{}); err != nil {
		t.Fatalf("unknown superseded operation began Guard: %v", err)
	}
}

func TestDrainCoordinatorConfirmsLostGuardReplyThroughStatus(t *testing.T) {
	directory, err := servicegroup.NewDirectoryService(servicegroup.DirectoryConfig{PublisherNode: "node-a", WatchTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	lostGuard := &lostGuardBeginReplyService{}
	fixture := newDrainFixtureWithOldWrapper(t, []Principal{"ops"}, directory, 20*time.Millisecond, func(service gsr.Service) gsr.Service {
		lostGuard.inner = service
		return lostGuard
	})
	lostGuard.drop.Store(true)
	operation, err := fixture.client.Start(context.Background(), fixture.request("guard-reply-lost", "ops"))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if operation.Phase != DrainReadyToStop || len(operation.Targets) != 1 || !operation.Targets[0].Guarded {
		t.Fatalf("Start() = %#v, want Guard-confirmed ReadyToStop", operation)
	}
	if begins := lostGuard.begins.Load(); begins != 1 {
		t.Fatalf("Guard Begin calls = %d, want 1", begins)
	}
	if _, err := fixture.runtime.Call(context.Background(), fixture.old, commandDrainWork, struct{}{}); !errors.Is(err, drain.ErrDraining) {
		t.Fatalf("old work after lost Begin reply error = %v, want ErrDraining", err)
	}
}

type drainFixture struct {
	runtime            *gsr.Runtime
	client             *DrainClient
	coordinator        gsr.ServiceRef
	coordinatorService *drainCoordinatorService
	directory          gsr.ServiceRef
	registry           gsr.ServiceRef
	old                gsr.ServiceRef
	next               gsr.ServiceRef
	initial            servicegroup.ServiceSet
	visitor            gsr.ServiceRef
}

func newDrainFixture(t *testing.T, principals []Principal) drainFixture {
	t.Helper()
	directoryService, err := servicegroup.NewDirectoryService(servicegroup.DirectoryConfig{PublisherNode: "node-a", WatchTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return newDrainFixtureWithDirectory(t, principals, directoryService, time.Second)
}

func newDrainFixtureWithDirectory(t *testing.T, principals []Principal, directoryService gsr.Service, callTimeout time.Duration) drainFixture {
	return newDrainFixtureWithOldWrapper(t, principals, directoryService, callTimeout, nil)
}

func newDrainFixtureWithOldWrapper(t *testing.T, principals []Principal, directoryService gsr.Service, callTimeout time.Duration, wrapOld func(gsr.Service) gsr.Service) drainFixture {
	t.Helper()
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a", Workers: 4})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	directory, err := runtime.CreateService(gsr.ServiceSpec{Service: directoryService})
	if err != nil {
		t.Fatal(err)
	}
	registryService, err := drain.NewVisitorRegistryService(drain.VisitorRegistryConfig{LeaseTTL: time.Minute, SweepInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := runtime.CreateService(gsr.ServiceSpec{Service: registryService})
	if err != nil {
		t.Fatal(err)
	}
	gateway := &drainGatewayService{}
	gatewayRef, err := runtime.CreateService(gsr.ServiceSpec{Service: gateway})
	if err != nil {
		t.Fatal(err)
	}
	coordinatorService, err := NewDrainCoordinatorService(DrainCoordinatorConfig{
		Gateway:           gatewayRef,
		AllowedPrincipals: principals,
		Directory:         directory,
		VisitorRegistry:   registry,
		CallTimeout:       callTimeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := runtime.CreateService(gsr.ServiceSpec{Service: coordinatorService})
	if err != nil {
		t.Fatal(err)
	}
	gateway.target = coordinator

	oldService, err := drain.Decorate(drainWorkService{}, drain.GuardConfig{Controller: coordinator, ExternalCommands: []gsr.CommandID{commandDrainWork}})
	if err != nil {
		t.Fatal(err)
	}
	if wrapOld != nil {
		oldService = wrapOld(oldService)
	}
	old, err := runtime.CreateService(gsr.ServiceSpec{Service: oldService})
	if err != nil {
		t.Fatal(err)
	}
	next, err := runtime.CreateService(gsr.ServiceSpec{Service: drainWorkService{}})
	if err != nil {
		t.Fatal(err)
	}
	directoryClient, err := servicegroup.NewClient(runtime, directory)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := directoryClient.Publish(context.Background(), "match", servicegroup.ServiceSetVersion{}, []gsr.ServiceRef{old}, map[string]string{"caller": "stable"})
	if err != nil {
		t.Fatal(err)
	}
	visitorService := &drainVisitorService{registry: registry}
	visitor, err := runtime.CreateService(gsr.ServiceSpec{Service: visitorService})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewDrainClient(runtime, gatewayRef)
	if err != nil {
		t.Fatal(err)
	}
	return drainFixture{runtime: runtime, client: client, coordinator: coordinator, coordinatorService: coordinatorService.(*drainCoordinatorService), directory: directory, registry: registry, old: old, next: next, initial: initial, visitor: visitor}
}

func (f drainFixture) request(id RequestID, principal Principal) StartDrainRequest {
	return StartDrainRequest{
		RequestID: id,
		Principal: principal,
		Group:     f.initial.Name,
		Expected:  f.initial.Version,
		NextRefs:  []gsr.ServiceRef{f.next},
		NextTags:  map[string]string{"caller": "stable"},
	}
}

func (f drainFixture) getDirectory(t *testing.T) servicegroup.ServiceSet {
	t.Helper()
	client, err := servicegroup.NewClient(f.runtime, f.directory)
	if err != nil {
		t.Fatal(err)
	}
	set, err := client.Get(context.Background(), f.initial.Name)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func (f drainFixture) acquireStrong(t *testing.T) drain.VisitorLease {
	t.Helper()
	value, err := f.runtime.Call(context.Background(), f.visitor, commandDrainVisitor, drainVisitorAction{target: f.old})
	if err != nil {
		t.Fatal(err)
	}
	lease, ok := value.(drain.VisitorLease)
	if !ok {
		t.Fatalf("Acquire result = %#v", value)
	}
	return lease
}

func (f drainFixture) release(t *testing.T, lease drain.VisitorLease) error {
	t.Helper()
	_, err := f.runtime.Call(context.Background(), f.visitor, commandDrainVisitor, drainVisitorAction{lease: lease})
	return err
}

type drainGatewayService struct {
	context gsr.ServiceContext
	target  gsr.ServiceRef
}

func (*drainGatewayService) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandStartDrainOperation, commandResolveDrainOperation, commandGetDrainOperation, commandListDrainAudit, commandBeginDrainStop, commandResolveDrainStop, commandGetDrainStop, commandBeginRecovery, commandConfirmRecovery, commandResolveRecovery, commandGetRecovery, commandAbandonRecovery}
}
func (s *drainGatewayService) Init(context gsr.ServiceContext) error { s.context = context; return nil }
func (s *drainGatewayService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	value, err := s.context.Call(context.Background(), s.target, command.ID, command.Payload)
	if err != nil {
		return err
	}
	return commandContext.Reply(value)
}
func (*drainGatewayService) Stop(context.Context) error { return nil }
func (s *drainGatewayService) Close() error             { s.context = nil; return nil }

type drainWorkService struct{}

func (drainWorkService) Commands() []gsr.CommandID     { return []gsr.CommandID{commandDrainWork} }
func (drainWorkService) Init(gsr.ServiceContext) error { return nil }
func (drainWorkService) Handle(context gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandDrainWork {
		return gsr.ErrCommandNotRegistered
	}
	return context.Reply("worked")
}
func (drainWorkService) Stop(context.Context) error { return nil }
func (drainWorkService) Close() error               { return nil }

type drainVisitorAction struct {
	target gsr.ServiceRef
	lease  drain.VisitorLease
}

type drainVisitorService struct {
	context  gsr.ServiceContext
	registry gsr.ServiceRef
	client   *drain.Client
}

func (*drainVisitorService) Commands() []gsr.CommandID { return []gsr.CommandID{commandDrainVisitor} }
func (s *drainVisitorService) Init(context gsr.ServiceContext) error {
	client, err := drain.NewClient(context, s.registry)
	if err != nil {
		return err
	}
	s.context = context
	s.client = client
	return nil
}
func (s *drainVisitorService) Handle(context gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandDrainVisitor {
		return gsr.ErrCommandNotRegistered
	}
	action, ok := command.Payload.(drainVisitorAction)
	if !ok {
		return drain.ErrInvalidLease
	}
	if action.lease != (drain.VisitorLease{}) {
		if err := s.client.Release(contextBackground, action.lease); err != nil {
			return err
		}
		return context.Reply(struct{}{})
	}
	lease, err := s.client.Acquire(contextBackground, action.target, context.Self(), false)
	if err != nil {
		return err
	}
	return context.Reply(lease)
}
func (*drainVisitorService) Stop(context.Context) error { return nil }
func (s *drainVisitorService) Close() error {
	s.context = nil
	s.client = nil
	return nil
}

var contextBackground = context.Background()

type lostPublishReplyService struct {
	inner     gsr.Service
	publishes atomic.Int64
	drop      atomic.Bool
}

func (s *lostPublishReplyService) Commands() []gsr.CommandID {
	declarer, ok := s.inner.(gsr.CommandDeclarer)
	if !ok {
		return nil
	}
	return declarer.Commands()
}
func (s *lostPublishReplyService) Init(context gsr.ServiceContext) error {
	return s.inner.Init(context)
}
func (s *lostPublishReplyService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	if command.ID != 0x02600101 {
		return s.inner.Handle(commandContext, command)
	}
	s.publishes.Add(1)
	if !s.drop.Load() {
		return s.inner.Handle(commandContext, command)
	}
	_ = s.inner.Handle(lostReplyCommandContext{CommandContext: commandContext}, command)
	return nil
}
func (s *lostPublishReplyService) Stop(context context.Context) error { return s.inner.Stop(context) }
func (s *lostPublishReplyService) Close() error                       { return s.inner.Close() }

type lostReplyCommandContext struct{ gsr.CommandContext }

func (lostReplyCommandContext) Reply(any) error { return gsr.ErrReplyExpired }

type lostGuardBeginReplyService struct {
	inner  gsr.Service
	begins atomic.Int64
	drop   atomic.Bool
}

func (s *lostGuardBeginReplyService) Commands() []gsr.CommandID {
	declarer, ok := s.inner.(gsr.CommandDeclarer)
	if !ok {
		return nil
	}
	return declarer.Commands()
}
func (s *lostGuardBeginReplyService) Init(context gsr.ServiceContext) error {
	return s.inner.Init(context)
}
func (s *lostGuardBeginReplyService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
	if command.ID != drain.BeginDrainCommand || !s.drop.Load() {
		return s.inner.Handle(commandContext, command)
	}
	s.begins.Add(1)
	_ = s.inner.Handle(lostReplyCommandContext{CommandContext: commandContext}, command)
	return nil
}
func (s *lostGuardBeginReplyService) Stop(context context.Context) error {
	return s.inner.Stop(context)
}
func (s *lostGuardBeginReplyService) Close() error { return s.inner.Close() }
