package nhsk

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	applyBattleCreatedCommand   gsr.CommandID = 0x0410f001
	applyBattleStoppedCommand   gsr.CommandID = 0x0410f002
	createBattleInternalCommand gsr.CommandID = 0x0410f010
	stopBattleInternalCommand   gsr.CommandID = 0x0410f011
	bindOutputInternalCommand   gsr.CommandID = 0x0410f012
	unbindOutputInternalCommand gsr.CommandID = 0x0410f013
	stopGenerationCommand       gsr.CommandID = 0x0410f014
)

var (
	errInvalidHostConfig = errors.New("nhsk: invalid Host config")
	errBattleIDInUse     = errors.New("nhsk: battle id in use")
	errHostCapacity      = errors.New("nhsk: Host capacity exhausted")
	errInvalidBattleID   = errors.New("nhsk: invalid BattleID")
)

// NHSKHostConfig configures the stable `.nhsk-game-host` Service.
type NHSKHostConfig struct {
	MaxActiveBattles uint32
	FactoryRef       gsr.ServiceRef
}

// NHSKHostService owns BattleID-to-ServiceRef bindings and delegates lifecycle work to a factory Service.
type NHSKHostService struct {
	max        uint32
	factory    gsr.ServiceRef
	service    gsr.ServiceContext
	nextOp     HostOperationID
	active     map[game.BattleID]gsr.ServiceRef
	states     map[game.BattleID]HostOperationPhase
	operations map[HostOperationID]CreateBattleOperation
}

// NewNHSKHostService creates an empty Host index.
func NewNHSKHostService(config NHSKHostConfig) (*NHSKHostService, error) {
	if config.MaxActiveBattles == 0 || config.FactoryRef.Node == "" || config.FactoryRef.ID == 0 {
		return nil, errInvalidHostConfig
	}
	return &NHSKHostService{max: config.MaxActiveBattles, factory: config.FactoryRef, active: make(map[game.BattleID]gsr.ServiceRef), states: make(map[game.BattleID]HostOperationPhase), operations: make(map[HostOperationID]CreateBattleOperation)}, nil
}

// Init captures the Host Service capability.
func (host *NHSKHostService) Init(service gsr.ServiceContext) error {
	if service == nil {
		return errInvalidHostConfig
	}
	host.service = service
	return nil
}

// Handle applies one Host lifecycle or lookup Command.
func (host *NHSKHostService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case BeginCreateBattleCommand:
		return host.beginCreate(ctx, command.Payload)
	case GetCreateBattleOperationCommand:
		return host.getOperation(ctx, command.Payload)
	case ResolveBattleCommand:
		return host.resolve(ctx, command.Payload)
	case RequestDeleteBattleCommand:
		return host.delete(ctx, command.Payload)
	case GetHostSnapshotCommand:
		return host.reply(ctx, host.snapshot())
	case applyBattleCreatedCommand:
		return host.applyCreated(ctx, command.Payload)
	case applyBattleStoppedCommand:
		return host.applyStopped(ctx, command.Payload)
	default:
		return gsr.ErrUnknownCommand
	}
}

// Stop stops accepting lifecycle changes; Runtime owns actual Service cleanup.
func (*NHSKHostService) Stop(context.Context) error { return nil }

// Close releases the Host Service capability.
func (host *NHSKHostService) Close() error { host.service = nil; return nil }

type createBattleInternal struct {
	OperationID HostOperationID
	Request     CreateBattleRequest
	Host        gsr.ServiceRef
}
type applyBattleCreated struct {
	OperationID HostOperationID
	BattleID    game.BattleID
	Ref         gsr.ServiceRef
	Err         string
}
type stopBattleInternal struct {
	OperationID HostOperationID
	BattleID    game.BattleID
	Ref, Host   gsr.ServiceRef
}

type bindOutputInternal struct {
	Generation ConnectionGeneration
	Ref        gsr.ServiceRef
	Reporter   ConnectionFailureReporter
}

type unbindOutputInternal struct{ Generation ConnectionGeneration }

type stopGenerationInternal struct{ Generation ConnectionGeneration }
type applyBattleStopped struct {
	OperationID HostOperationID
	BattleID    game.BattleID
	Ref         gsr.ServiceRef
	Err         string
}

func (host *NHSKHostService) beginCreate(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(CreateBattleRequest)
	if !ok || request.BattleID == 0 {
		return host.reply(ctx, CreateBattleOperation{Phase: HostOperationFailed, Rejection: errInvalidBattleID.Error()})
	}
	if phase, exists := host.states[request.BattleID]; exists && phase != HostOperationFailed {
		return host.reply(ctx, CreateBattleOperation{BattleID: request.BattleID, Phase: phase, Rejection: errBattleIDInUse.Error()})
	}
	if uint32(len(host.active))+uint32(host.creatingCount()) >= host.max {
		return host.reply(ctx, CreateBattleOperation{BattleID: request.BattleID, Phase: HostOperationFailed, Rejection: errHostCapacity.Error()})
	}
	host.nextOp++
	operation := CreateBattleOperation{OperationID: host.nextOp, BattleID: request.BattleID, Phase: HostOperationCreating}
	host.operations[operation.OperationID] = operation
	host.states[request.BattleID] = HostOperationCreating
	if err := host.service.Send(host.factory, createBattleInternalCommand, createBattleInternal{OperationID: operation.OperationID, Request: request, Host: ctx.Self()}); err != nil {
		operation.Phase, operation.Rejection = HostOperationFailed, err.Error()
		host.operations[operation.OperationID] = operation
		delete(host.states, request.BattleID)
	}
	return host.reply(ctx, operation)
}

func (host *NHSKHostService) getOperation(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(GetCreateBattleOperationRequest)
	if !ok || request.OperationID == 0 {
		return gsr.ErrInvalidClusterEnvelope
	}
	operation, exists := host.operations[request.OperationID]
	if !exists {
		return gsr.ErrServiceNotFound
	}
	return host.reply(ctx, operation)
}

func (host *NHSKHostService) resolve(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(ResolveBattleRequest)
	if !ok || request.BattleID == 0 {
		return errInvalidBattleID
	}
	ref, exists := host.active[request.BattleID]
	if !exists {
		return gsr.ErrServiceNotFound
	}
	return host.reply(ctx, ResolveBattleResult{BattleID: request.BattleID, Ref: ref})
}

func (host *NHSKHostService) delete(ctx gsr.CommandContext, payload any) error {
	request, ok := payload.(RequestDeleteBattleRequest)
	if !ok || request.BattleID == 0 {
		return host.reply(ctx, HostCommandResult{Rejection: errInvalidBattleID.Error()})
	}
	ref, exists := host.active[request.BattleID]
	if !exists || (request.Ref.ID != 0 && request.Ref != ref) {
		return host.reply(ctx, HostCommandResult{Rejection: gsr.ErrServiceNotFound.Error()})
	}
	host.states[request.BattleID] = HostOperationCreating
	host.nextOp++
	if err := host.service.Send(host.factory, stopBattleInternalCommand, stopBattleInternal{OperationID: host.nextOp, BattleID: request.BattleID, Ref: ref, Host: ctx.Self()}); err != nil {
		return host.reply(ctx, HostCommandResult{Rejection: err.Error()})
	}
	return host.reply(ctx, HostCommandResult{Accepted: true, OperationID: host.nextOp})
}

func (host *NHSKHostService) applyCreated(ctx gsr.CommandContext, payload any) error {
	if ctx.Source() != host.factory {
		return game.ErrUnauthorized
	}
	result, ok := payload.(applyBattleCreated)
	if !ok {
		return gsr.ErrInvalidClusterEnvelope
	}
	operation, exists := host.operations[result.OperationID]
	if !exists {
		return nil
	}
	if result.Err != "" || result.Ref.ID == 0 {
		operation.Phase, operation.Rejection = HostOperationFailed, result.Err
		delete(host.states, result.BattleID)
	} else {
		operation.Phase, operation.Ref = HostOperationCompleted, result.Ref
		host.active[result.BattleID] = result.Ref
		host.states[result.BattleID] = HostOperationCompleted
	}
	host.operations[result.OperationID] = operation
	return nil
}

func (host *NHSKHostService) applyStopped(ctx gsr.CommandContext, payload any) error {
	if ctx.Source() != host.factory {
		return game.ErrUnauthorized
	}
	result, ok := payload.(applyBattleStopped)
	if !ok {
		return gsr.ErrInvalidClusterEnvelope
	}
	if ref, exists := host.active[result.BattleID]; !exists || ref != result.Ref {
		return nil
	} else {
		delete(host.active, result.BattleID)
		delete(host.states, result.BattleID)
	}
	return nil
}

func (host *NHSKHostService) creatingCount() int {
	count := 0
	for _, phase := range host.states {
		if phase == HostOperationCreating {
			count++
		}
	}
	return count
}
func (host *NHSKHostService) snapshot() HostSnapshot {
	active := make(map[game.BattleID]gsr.ServiceRef, len(host.active))
	for id, ref := range host.active {
		active[id] = ref
	}
	return HostSnapshot{MaxActiveBattles: host.max, ActiveBattles: active}
}
func (host *NHSKHostService) reply(ctx gsr.CommandContext, value any) error {
	if err := ctx.Reply(value); err != nil && !errors.Is(err, gsr.ErrReplyUnavailable) {
		return err
	}
	return nil
}

// BattleStopper stops an exact Battle ServiceRef from a lifecycle runner.
type BattleStopper interface {
	Stop(context.Context, gsr.ServiceRef) error
}

// BattleFactoryService is a bounded lifecycle runner used by NHSKHostService.
type BattleFactoryService struct {
	creator             game.ServiceCreator
	stopper             BattleStopper
	customDeckRequester CustomDeckRequester
	service             gsr.ServiceContext
	stopQueue           chan stopBattleInternal
	stopDone            chan struct{}
	stopCancel          context.CancelFunc
	stopWG              sync.WaitGroup
	battles             map[game.BattleID]factoryBattle
	outputRef           gsr.ServiceRef
	outputGeneration    ConnectionGeneration
	outputReporter      ConnectionFailureReporter
}

type factoryBattle struct {
	ref        gsr.ServiceRef
	generation ConnectionGeneration
	host       gsr.ServiceRef
}

// NewBattleFactoryService creates a lifecycle runner backed by the Runtime composition root.
func NewBattleFactoryService(creator game.ServiceCreator, stopper BattleStopper) (*BattleFactoryService, error) {
	return NewBattleFactoryServiceWithCustomDeck(creator, stopper, nil)
}

// NewBattleFactoryServiceWithCustomDeck creates a factory with an optional
// external custom-deck requester injected at the composition boundary.
func NewBattleFactoryServiceWithCustomDeck(creator game.ServiceCreator, stopper BattleStopper, requester CustomDeckRequester) (*BattleFactoryService, error) {
	if creator == nil || stopper == nil {
		return nil, errInvalidHostConfig
	}
	return &BattleFactoryService{creator: creator, stopper: stopper, customDeckRequester: requester}, nil
}

// Init captures the factory capability.
func (factory *BattleFactoryService) Init(service gsr.ServiceContext) error {
	if service == nil {
		return errInvalidHostConfig
	}
	factory.service = service
	ctx, cancel := context.WithCancel(context.Background())
	factory.stopCancel = cancel
	factory.stopQueue = make(chan stopBattleInternal, 10000)
	factory.stopDone = make(chan struct{})
	factory.battles = make(map[game.BattleID]factoryBattle)
	factory.stopWG.Add(1)
	startFactoryStopRunner(factory, ctx)
	return nil
}

// Handle creates or stops exact Battle ServiceRefs and reports results to Host.
func (factory *BattleFactoryService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case createBattleInternalCommand:
		request, ok := command.Payload.(createBattleInternal)
		if !ok {
			return gsr.ErrInvalidClusterEnvelope
		}
		outputRef, outputGeneration, outputReporter := factory.outputRef, factory.outputGeneration, factory.outputReporter
		if outputGeneration != request.Request.ConnectionGeneration {
			outputRef, outputGeneration, outputReporter = gsr.ServiceRef{}, 0, nil
		}
		battle, err := NewBattleService(NHSKBattleConfig{ID: request.Request.BattleID, OutputRef: outputRef, IsNewbie: request.Request.IsNewbie, ConnectionGeneration: outputGeneration, OutputReporter: outputReporter, CustomDeckRequester: factory.customDeckRequester})
		if err == nil {
			var ref gsr.ServiceRef
			ref, err = factory.creator.CreateService(gsr.ServiceSpec{Name: gsr.ServiceName(fmt.Sprintf("nhsk-battle/%d", request.Request.BattleID)), Service: battle})
			if err == nil {
				factory.battles[request.Request.BattleID] = factoryBattle{ref: ref, generation: request.Request.ConnectionGeneration, host: request.Host}
				return factory.service.Send(request.Host, applyBattleCreatedCommand, applyBattleCreated{OperationID: request.OperationID, BattleID: request.Request.BattleID, Ref: ref})
			}
		}
		return factory.service.Send(request.Host, applyBattleCreatedCommand, applyBattleCreated{OperationID: request.OperationID, BattleID: request.Request.BattleID, Err: err.Error()})
	case stopBattleInternalCommand:
		request, ok := command.Payload.(stopBattleInternal)
		if !ok {
			return gsr.ErrInvalidClusterEnvelope
		}
		select {
		case factory.stopQueue <- request:
			delete(factory.battles, request.BattleID)
			return nil
		default:
			return gsr.ErrMailboxFull
		}
	case bindOutputInternalCommand:
		request, ok := command.Payload.(bindOutputInternal)
		if !ok || request.Generation == 0 || request.Ref.ID == 0 || request.Reporter == nil {
			return gsr.ErrInvalidClusterEnvelope
		}
		factory.outputRef, factory.outputGeneration, factory.outputReporter = request.Ref, request.Generation, request.Reporter
		return nil
	case unbindOutputInternalCommand:
		request, ok := command.Payload.(unbindOutputInternal)
		if !ok {
			return gsr.ErrInvalidClusterEnvelope
		}
		if request.Generation == factory.outputGeneration {
			factory.outputRef, factory.outputGeneration, factory.outputReporter = gsr.ServiceRef{}, 0, nil
		}
		return nil
	case stopGenerationCommand:
		request, ok := command.Payload.(stopGenerationInternal)
		if !ok || request.Generation == 0 {
			return gsr.ErrInvalidClusterEnvelope
		}
		for battleID, battle := range factory.battles {
			if battle.generation != request.Generation {
				continue
			}
			select {
			case factory.stopQueue <- stopBattleInternal{OperationID: 0, BattleID: battleID, Ref: battle.ref, Host: battle.host}:
				delete(factory.battles, battleID)
			default:
				return gsr.ErrMailboxFull
			}
		}
		return nil
	default:
		return gsr.ErrUnknownCommand
	}
}

// Stop stops the runner's command intake.
func (*BattleFactoryService) Stop(context.Context) error { return nil }

// Close releases the runner capability.
func (factory *BattleFactoryService) Close() error {
	if factory.stopCancel != nil {
		factory.stopCancel()
	}
	if factory.stopDone != nil {
		<-factory.stopDone
	}
	factory.service = nil
	return nil
}

func (factory *BattleFactoryService) stopLoop(ctx context.Context) {
	defer factory.stopWG.Done()
	defer close(factory.stopDone)
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-factory.stopQueue:
			err := factory.stopper.Stop(ctx, request.Ref)
			if factory.service != nil {
				_ = factory.service.Send(request.Host, applyBattleStoppedCommand, applyBattleStopped{OperationID: request.OperationID, BattleID: request.BattleID, Ref: request.Ref, Err: errorText(err)})
			}
		}
	}
}

// startFactoryStopRunner owns the bounded lifecycle worker outside the Service Handler.
func startFactoryStopRunner(factory *BattleFactoryService, ctx context.Context) {
	go factory.stopLoop(ctx)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Compile-time checks keep lifecycle services on the Runtime Service boundary.
var _ gsr.Service = (*NHSKHostService)(nil)
var _ gsr.Service = (*BattleFactoryService)(nil)
