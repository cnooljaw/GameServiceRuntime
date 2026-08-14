package nhsk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk/internal/legacywire"
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// GameLogicProcessConfig configures one independently deployable NHSK GameLogic process.
type GameLogicProcessConfig struct {
	NodeID           gsr.NodeID
	Workers          int
	MaxActiveBattles uint32
	Legacy           LegacyGMConnectionConfig
	CustomDeck       CustomDeckProcessConfig
}

// CustomDeckProcessConfig configures the dependency-light local custom-deck
// provider used by the independently deployed example process.
type CustomDeckProcessConfig struct {
	Enabled         bool
	FilePath        string
	AllowAnyAccount bool
	AllowedAccounts []uint32
	QueueSize       int
	Workers         int
	LoadTimeout     time.Duration
}

// GameLogicProcess owns the Runtime, Host, Factory and single Legacy connection.
type GameLogicProcess struct {
	runtime          *gsr.Runtime
	hostRef          gsr.ServiceRef
	factoryRef       gsr.ServiceRef
	connection       *LegacyGMConnection
	customDeckRunner *CustomDeckRunner
	closeOnce        bool
	outputMu         sync.Mutex
	outputRef        gsr.ServiceRef
}

// NewGameLogicProcess creates and composes a complete process owner.
func NewGameLogicProcess(config GameLogicProcessConfig) (*GameLogicProcess, error) {
	if config.NodeID == "" || config.Workers <= 0 || config.MaxActiveBattles == 0 {
		return nil, errors.New("nhsk: invalid GameLogic process config")
	}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: config.NodeID, Workers: config.Workers})
	var customDeckRunner *CustomDeckRunner
	var err error
	if config.CustomDeck.Enabled {
		if config.CustomDeck.FilePath == "" {
			_ = runtime.Close(context.Background())
			return nil, errors.New("nhsk: custom deck file path is required when enabled")
		}
		customDeckRunner, err = NewCustomDeckRunner(runtime, LocalFileCustomDeckProvider{Path: config.CustomDeck.FilePath}, CustomDeckRunnerConfig{Enabled: true, AllowAnyAccount: config.CustomDeck.AllowAnyAccount, AllowedAccounts: append([]uint32(nil), config.CustomDeck.AllowedAccounts...), QueueSize: config.CustomDeck.QueueSize, Workers: config.CustomDeck.Workers, LoadTimeout: config.CustomDeck.LoadTimeout})
		if err != nil {
			_ = runtime.Close(context.Background())
			return nil, err
		}
	}
	factory, err := NewBattleFactoryServiceWithCustomDeck(runtime, runtime, customDeckRunner)
	if err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = runtime.Close(context.Background())
		return nil, err
	}
	factoryRef, err := runtime.CreateService(gsr.ServiceSpec{Name: ".nhsk-battle-factory", Service: factory})
	if err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = runtime.Close(context.Background())
		return nil, err
	}
	host, err := NewNHSKHostService(NHSKHostConfig{MaxActiveBattles: config.MaxActiveBattles, FactoryRef: factoryRef})
	if err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = runtime.Close(context.Background())
		return nil, err
	}
	hostRef, err := runtime.CreateService(gsr.ServiceSpec{Name: ".nhsk-game-host", Service: host})
	if err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = runtime.Close(context.Background())
		return nil, err
	}
	connectionConfig := config.Legacy
	var connection *LegacyGMConnection
	var process *GameLogicProcess
	connectionConfig.OnConnected = func(generation ConnectionGeneration) error {
		outputSpec, err := newGameOutputServiceSpec(generation, connection, connection)
		if err != nil {
			return err
		}
		ref, err := runtime.CreateService(outputSpec)
		if err != nil {
			return err
		}
		if err := runtime.Send(factoryRef, bindOutputInternalCommand, bindOutputInternal{Generation: generation, Ref: ref, Reporter: connection}); err != nil {
			_ = runtime.Stop(context.Background(), ref)
			return err
		}
		process.outputMu.Lock()
		process.outputRef = ref
		process.outputMu.Unlock()
		return nil
	}
	connectionConfig.OnDisconnected = func(generation ConnectionGeneration) {
		_ = runtime.Send(factoryRef, unbindOutputInternalCommand, unbindOutputInternal{Generation: generation})
		_ = runtime.Send(factoryRef, stopGenerationCommand, stopGenerationInternal{Generation: generation})
		process.outputMu.Lock()
		ref := process.outputRef
		process.outputRef = gsr.ServiceRef{}
		process.outputMu.Unlock()
		if ref.ID != 0 {
			_ = runtime.Stop(context.Background(), ref)
		}
	}
	connection, err = NewLegacyGMConnection(connectionConfig)
	if err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = runtime.Close(context.Background())
		return nil, fmt.Errorf("nhsk: create Legacy connection: %w", err)
	}
	if err := connection.AttachRouting(runtime, hostRef); err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = runtime.Close(context.Background())
		return nil, err
	}
	process = &GameLogicProcess{runtime: runtime, hostRef: hostRef, factoryRef: factoryRef, connection: connection, customDeckRunner: customDeckRunner}
	return process, nil
}

// Start starts the single active Legacy connection and returns immediately.
func (process *GameLogicProcess) Start(ctx context.Context) error {
	if process == nil || process.connection == nil {
		return errors.New("nhsk: nil GameLogic process")
	}
	return process.connection.Start(ctx)
}

// Run starts the process and waits for its context to finish before closing all owners.
func (process *GameLogicProcess) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := process.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return process.Close(context.Background())
}

// Close closes the connection first, then the Runtime and all Services.
func (process *GameLogicProcess) Close(ctx context.Context) error {
	if process == nil || process.closeOnce {
		return nil
	}
	process.closeOnce = true
	if ctx == nil {
		ctx = context.Background()
	}
	if err := process.connection.Close(ctx); err != nil {
		if process.customDeckRunner != nil {
			_ = process.customDeckRunner.Close()
		}
		_ = process.runtime.Close(ctx)
		return err
	}
	if process.customDeckRunner != nil {
		_ = process.customDeckRunner.Close()
	}
	return process.runtime.Close(ctx)
}

// HostRef returns the stable Host ServiceRef for in-process composition tests.
func (process *GameLogicProcess) HostRef() gsr.ServiceRef {
	if process == nil {
		return gsr.ServiceRef{}
	}
	return process.hostRef
}

// Runtime returns the process Runtime for composition-root integration tests.
func (process *GameLogicProcess) Runtime() *gsr.Runtime {
	if process == nil {
		return nil
	}
	return process.runtime
}

// LoadGameLogicProcessConfig loads the existing JSON config without exposing internal config structs.
func LoadGameLogicProcessConfig(path string) (GameLogicProcessConfig, error) {
	if path == "" {
		return GameLogicProcessConfig{}, errors.New("nhsk: empty config path")
	}
	if _, err := os.Stat(path); err != nil {
		return GameLogicProcessConfig{}, err
	}
	config, err := loadConfig(path)
	if err != nil {
		return GameLogicProcessConfig{}, err
	}
	connection := legacywire.ConnectionConfig{
		DialTimeout:       time.Duration(config.LegacyGM.DialTimeout),
		OriginTimeout:     time.Duration(config.LegacyGM.OriginTimeout),
		InitialBackoff:    time.Duration(config.LegacyGM.InitialBackoff),
		MaxBackoff:        time.Duration(config.LegacyGM.MaxBackoff),
		BackoffMultiplier: config.LegacyGM.BackoffMultiplier,
		JitterRatio:       config.LegacyGM.Jitter,
		StableResetAfter:  time.Duration(config.LegacyGM.StableReset),
	}
	return GameLogicProcessConfig{NodeID: gsr.NodeID(config.Node.ID), Workers: config.Node.Workers, MaxActiveBattles: config.Node.MaxActiveBattles, Legacy: LegacyGMConnectionConfig{Address: config.LegacyGM.Address, DialTimeout: connection.DialTimeout, OriginTimeout: connection.OriginTimeout, InitialBackoff: connection.InitialBackoff, MaxBackoff: connection.MaxBackoff, BackoffMultiplier: connection.BackoffMultiplier, Jitter: connection.JitterRatio, StableReset: connection.StableResetAfter}, CustomDeck: CustomDeckProcessConfig{Enabled: config.CustomDeck.Enabled, FilePath: config.CustomDeck.FilePath, AllowAnyAccount: config.CustomDeck.AllowAnyAccount, AllowedAccounts: append([]uint32(nil), config.CustomDeck.AllowedAccounts...), QueueSize: config.CustomDeck.QueueSize, Workers: config.CustomDeck.Workers, LoadTimeout: time.Duration(config.CustomDeck.LoadTimeout)}}, nil
}

var _ game.CommandRuntime = (*gsr.Runtime)(nil)
