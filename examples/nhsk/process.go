package nhsk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	Replay           ReplayProcessConfig
	AI               AIProcessConfig
	Diagnostic       DiagnosticProcessConfig
}

// AIProcessConfig selects the process-owned AI provider. Local is the default
// and has no external dependency; http enables the old RobotTran adapter.
type AIProcessConfig struct {
	Provider string
	URL      string
	Timeout  time.Duration
}

// ReplayProcessConfig configures bounded replay serialization output owned by
// the GameLogic process rather than by Battle Services.
type ReplayProcessConfig struct {
	Root string
}

// DiagnosticProcessConfig configures atomic local quarantine evidence.
type DiagnosticProcessConfig struct {
	Root        string
	AdminSocket string
	QueueSize   int
	Workers     int
}

// CustomDeckProcessConfig configures the optional Legacy compatibility bridge
// used by the independently deployed example process. Direct Cluster callers
// provide canonical catalogs through ProvideCustomDeckCommand and do not need
// this bridge.
type CustomDeckProcessConfig struct {
	Enabled         bool
	Source          string
	FilePath        string
	AllowAnyAccount bool
	AllowedAccounts []uint32
	QueueSize       int
	Workers         int
	LoadTimeout     time.Duration
	Redis           RedisCustomDeckConfig
}

// RedisCustomDeckConfig configures the standard-library Redis GET adapter.
// The generic process Redis settings are mapped here at config-load time.
type RedisCustomDeckConfig struct {
	Address       string
	Password      string
	DB            int
	DialTimeout   time.Duration
	MaxValueBytes int64
}

// GameLogicProcess owns the Runtime, Host, Factory and single Legacy connection.
type GameLogicProcess struct {
	runtime          *gsr.Runtime
	hostRef          gsr.ServiceRef
	factoryRef       gsr.ServiceRef
	connection       *LegacyGMConnection
	customDeckRunner *CustomDeckRunner
	replayWriter     *ReplayWriterRunner
	aiRunner         *AIRunner
	diagnosticRunner *DiagnosticRunner
	diagnosticRoot   string
	adminServer      *DiagnosticAdminServer
	closeOnce        bool
	outputMu         sync.Mutex
	outputRef        gsr.ServiceRef
}

// GameLogicHealthSnapshot separates GM-link readiness from retained Battle defects.
type GameLogicHealthSnapshot struct {
	Readiness          string
	GMLinkReady        bool
	QuarantinedBattles int64
}

// NewGameLogicProcess creates and composes a complete process owner.
func NewGameLogicProcess(config GameLogicProcessConfig) (*GameLogicProcess, error) {
	if config.NodeID == "" || config.Workers <= 0 || config.MaxActiveBattles == 0 {
		return nil, errors.New("nhsk: invalid GameLogic process config")
	}
	runtime := gsr.NewRuntime(gsr.Config{NodeID: config.NodeID, Workers: config.Workers})
	diagnosticRoot := strings.TrimSpace(config.Diagnostic.Root)
	if diagnosticRoot == "" {
		diagnosticRoot = "diagnostics"
	}
	diagnosticRunner, err := NewDiagnosticRunner(runtime, LocalDiagnosticExporter{Root: diagnosticRoot}, DiagnosticRunnerConfig{QueueSize: config.Diagnostic.QueueSize, Workers: config.Diagnostic.Workers})
	if err != nil {
		_ = runtime.Close(context.Background())
		return nil, err
	}
	replayRoot := strings.TrimSpace(config.Replay.Root)
	if replayRoot == "" {
		replayRoot = "replays"
	}
	replayWriter, err := NewReplayWriterRunner(runtime, FileReplayWriter{Root: replayRoot}, ReplayWriterRunnerConfig{})
	if err != nil {
		_ = diagnosticRunner.Close()
		_ = runtime.Close(context.Background())
		return nil, err
	}
	aiProvider, err := aiProviderFromConfig(config.AI)
	if err != nil {
		_ = replayWriter.Close()
		_ = diagnosticRunner.Close()
		_ = runtime.Close(context.Background())
		return nil, err
	}
	aiRunner, err := NewAIRunner(runtime, aiProvider)
	if err != nil {
		_ = replayWriter.Close()
		_ = diagnosticRunner.Close()
		_ = runtime.Close(context.Background())
		return nil, err
	}
	var customDeckRunner *CustomDeckRunner
	if config.CustomDeck.Enabled {
		provider, providerErr := customDeckProviderFromConfig(config.CustomDeck)
		if providerErr != nil {
			_ = aiRunner.Close()
			_ = replayWriter.Close()
			_ = diagnosticRunner.Close()
			_ = runtime.Close(context.Background())
			return nil, providerErr
		}
		customDeckRunner, err = NewCustomDeckRunner(runtime, provider, CustomDeckRunnerConfig{Enabled: true, AllowAnyAccount: config.CustomDeck.AllowAnyAccount, AllowedAccounts: append([]uint32(nil), config.CustomDeck.AllowedAccounts...), QueueSize: config.CustomDeck.QueueSize, Workers: config.CustomDeck.Workers, LoadTimeout: config.CustomDeck.LoadTimeout})
		if err != nil {
			_ = aiRunner.Close()
			_ = replayWriter.Close()
			_ = diagnosticRunner.Close()
			_ = runtime.Close(context.Background())
			return nil, err
		}
	}
	factory, err := NewBattleFactoryService(runtime, runtime, BattleFactoryConfig{ReplaySubmitter: replayWriter, AISubmitter: aiRunner})
	if err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = aiRunner.Close()
		_ = replayWriter.Close()
		_ = diagnosticRunner.Close()
		_ = runtime.Close(context.Background())
		return nil, err
	}
	factoryRef, err := runtime.CreateService(gsr.ServiceSpec{Name: ".nhsk-battle-factory", Service: factory})
	if err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = aiRunner.Close()
		_ = replayWriter.Close()
		_ = diagnosticRunner.Close()
		_ = runtime.Close(context.Background())
		return nil, err
	}
	host, err := NewNHSKHostService(NHSKHostConfig{MaxActiveBattles: config.MaxActiveBattles, FactoryRef: factoryRef, Diagnostic: diagnosticRunner})
	if err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = aiRunner.Close()
		_ = replayWriter.Close()
		_ = diagnosticRunner.Close()
		_ = runtime.Close(context.Background())
		return nil, err
	}
	hostRef, err := runtime.CreateService(gsr.ServiceSpec{Name: ".nhsk-game-host", Service: host})
	if err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = aiRunner.Close()
		_ = replayWriter.Close()
		_ = diagnosticRunner.Close()
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
	connectionConfig.OnSubgamePrepared = func(ctx context.Context, _ ConnectionGeneration, battleID game.BattleID, gameNum, subgameNum uint16) {
		if process != nil {
			process.submitLegacyCustomDeck(ctx, battleID, gameNum, subgameNum)
		}
	}
	connection, err = NewLegacyGMConnection(connectionConfig)
	if err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = aiRunner.Close()
		_ = replayWriter.Close()
		_ = diagnosticRunner.Close()
		_ = runtime.Close(context.Background())
		return nil, fmt.Errorf("nhsk: create Legacy connection: %w", err)
	}
	if err := connection.AttachRouting(runtime, hostRef); err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = aiRunner.Close()
		_ = replayWriter.Close()
		_ = diagnosticRunner.Close()
		_ = runtime.Close(context.Background())
		return nil, err
	}
	adminSocket := strings.TrimSpace(config.Diagnostic.AdminSocket)
	if adminSocket == "" {
		adminSocket = filepath.Join(diagnosticRoot, "nhsk-admin.sock")
	}
	adminServer, err := NewDiagnosticAdminServer(adminSocket, NewQuarantineAdmin(runtime, hostRef, diagnosticRoot))
	if err != nil {
		if customDeckRunner != nil {
			_ = customDeckRunner.Close()
		}
		_ = aiRunner.Close()
		_ = replayWriter.Close()
		_ = diagnosticRunner.Close()
		_ = runtime.Close(context.Background())
		return nil, err
	}
	process = &GameLogicProcess{runtime: runtime, hostRef: hostRef, factoryRef: factoryRef, connection: connection, customDeckRunner: customDeckRunner, replayWriter: replayWriter, aiRunner: aiRunner, diagnosticRunner: diagnosticRunner, diagnosticRoot: diagnosticRoot, adminServer: adminServer}
	return process, nil
}

// Start starts the single active Legacy connection and returns immediately.
func (process *GameLogicProcess) Start(ctx context.Context) error {
	if process == nil || process.connection == nil {
		return errors.New("nhsk: nil GameLogic process")
	}
	if err := process.adminServer.Start(); err != nil {
		return err
	}
	if err := process.connection.Start(ctx); err != nil {
		_ = process.adminServer.Close()
		return err
	}
	return nil
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
		if process.adminServer != nil {
			_ = process.adminServer.Close()
		}
		if process.customDeckRunner != nil {
			_ = process.customDeckRunner.Close()
		}
		if process.aiRunner != nil {
			_ = process.aiRunner.Close()
		}
		if process.replayWriter != nil {
			_ = process.replayWriter.Close()
		}
		if process.diagnosticRunner != nil {
			_ = process.diagnosticRunner.Close()
		}
		_ = process.runtime.Close(ctx)
		return err
	}
	if process.adminServer != nil {
		_ = process.adminServer.Close()
	}
	if process.customDeckRunner != nil {
		_ = process.customDeckRunner.Close()
	}
	if process.aiRunner != nil {
		_ = process.aiRunner.Close()
	}
	if process.replayWriter != nil {
		_ = process.replayWriter.Close()
	}
	if process.diagnosticRunner != nil {
		_ = process.diagnosticRunner.Close()
	}
	return process.runtime.Close(ctx)
}

// QuarantineAdmin returns node-local diagnostic operations for this process.
func (process *GameLogicProcess) QuarantineAdmin() *QuarantineAdmin {
	if process == nil {
		return nil
	}
	return NewQuarantineAdmin(process.runtime, process.hostRef, process.diagnosticRoot)
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

// Health returns a process-owned readiness projection without mutating Services.
func (process *GameLogicProcess) Health() GameLogicHealthSnapshot {
	if process == nil || process.runtime == nil {
		return GameLogicHealthSnapshot{Readiness: string(nodeNotReady)}
	}
	ready := process.connection != nil && process.connection.Ready()
	quarantined := process.runtime.Inspect().Metrics.Gauge("nhsk_quarantined_battles")
	readiness := nodeReady
	if !ready {
		readiness = nodeNotReady
	} else if quarantined > 0 {
		readiness = nodeDegraded
	}
	return GameLogicHealthSnapshot{Readiness: string(readiness), GMLinkReady: ready, QuarantinedBattles: quarantined}
}

func (process *GameLogicProcess) NodeHealth() nodeHealthSnapshot {
	health := process.Health()
	return nodeHealthSnapshot{GMLinkReady: health.GMLinkReady, QuarantinedBattles: int(health.QuarantinedBattles)}
}

// submitLegacyCustomDeck translates the old Redis-backed lookup into the
// public ProvideCustomDeck Command. It is an outer compatibility bridge; the
// Battle itself never owns or invokes the provider.
func (process *GameLogicProcess) submitLegacyCustomDeck(ctx context.Context, battleID game.BattleID, gameNum, subgameNum uint16) {
	if process == nil || process.customDeckRunner == nil || process.runtime == nil || process.hostRef.ID == 0 {
		return
	}
	value, err := process.runtime.Call(ctx, process.hostRef, ResolveBattleCommand, ResolveBattleRequest{BattleID: battleID})
	if err != nil {
		return
	}
	resolved, ok := value.(ResolveBattleResult)
	if !ok || resolved.BattleID != battleID || resolved.Ref.ID == 0 {
		return
	}
	snapshotValue, err := process.runtime.Call(ctx, resolved.Ref, GetNHSKBattleSnapshotCommand, nil)
	if err != nil {
		return
	}
	snapshot, ok := snapshotValue.(NHSKBattleSnapshot)
	if !ok || snapshot.BattleID != battleID || snapshot.Identity.ProductID == 0 {
		return
	}
	userIDs := make([]uint32, 0, len(snapshot.Players))
	for _, player := range snapshot.Players {
		if player.UserID != 0 {
			userIDs = append(userIDs, player.UserID)
		}
	}
	_ = process.customDeckRunner.LoadAndProvide(ctx, CustomDeckLoadRequest{
		Target: resolved.Ref, BattleID: battleID, GameNum: gameNum, SubgameNum: subgameNum,
		Lookup: CustomDeckLookup{GameID: NHSKDescriptor.GameID, ProductID: snapshot.Identity.ProductID, UserIDs: userIDs},
	})
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
	return GameLogicProcessConfig{NodeID: gsr.NodeID(config.Node.ID), Workers: config.Node.Workers, MaxActiveBattles: config.Node.MaxActiveBattles, Legacy: LegacyGMConnectionConfig{Address: config.LegacyGM.Address, DialTimeout: connection.DialTimeout, OriginTimeout: connection.OriginTimeout, InitialBackoff: connection.InitialBackoff, MaxBackoff: connection.MaxBackoff, BackoffMultiplier: connection.BackoffMultiplier, Jitter: connection.JitterRatio, StableReset: connection.StableResetAfter}, CustomDeck: CustomDeckProcessConfig{Enabled: config.CustomDeck.Enabled, Source: config.CustomDeck.Source, FilePath: config.CustomDeck.FilePath, AllowAnyAccount: config.CustomDeck.AllowAnyAccount, AllowedAccounts: append([]uint32(nil), config.CustomDeck.AllowedAccounts...), QueueSize: config.CustomDeck.QueueSize, Workers: config.CustomDeck.Workers, LoadTimeout: time.Duration(config.CustomDeck.LoadTimeout), Redis: RedisCustomDeckConfig{Address: config.Redis.Address, Password: config.Redis.Password, DB: config.Redis.DB, DialTimeout: connection.DialTimeout}}, Replay: ReplayProcessConfig{Root: config.Replay.Root}, AI: AIProcessConfig{Provider: config.AI.Provider, URL: config.AI.URL, Timeout: time.Duration(config.AI.Timeout)}, Diagnostic: DiagnosticProcessConfig{Root: config.Diagnostic.Root, AdminSocket: config.Diagnostic.AdminSocket, QueueSize: config.Diagnostic.QueueSize, Workers: config.Diagnostic.Workers}}, nil
}

func aiProviderFromConfig(config AIProcessConfig) (AIProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(config.Provider))
	if provider == "" || provider == "local" {
		return LocalAIProvider{}, nil
	}
	if provider == "http" {
		if strings.TrimSpace(config.URL) == "" || config.Timeout <= 0 {
			return nil, errors.New("nhsk: invalid Legacy HTTP AI config")
		}
		return LegacyHTTPAIProvider{URL: config.URL, Client: &http.Client{Timeout: config.Timeout}, GameID: NHSKDescriptor.GameID}, nil
	}
	return nil, fmt.Errorf("nhsk: unsupported AI provider %q", config.Provider)
}

func customDeckProviderFromConfig(config CustomDeckProcessConfig) (CustomDeckProvider, error) {
	source := strings.ToLower(strings.TrimSpace(config.Source))
	if source == "" || source == "file" {
		if strings.TrimSpace(config.FilePath) == "" {
			return nil, errors.New("nhsk: custom deck file path is required for file source")
		}
		return LocalFileCustomDeckProvider{Path: config.FilePath}, nil
	}
	if source == "redis" {
		if strings.TrimSpace(config.Redis.Address) == "" {
			return nil, errors.New("nhsk: Redis address is required for custom deck source")
		}
		if config.Redis.DB < 0 {
			return nil, errors.New("nhsk: Redis DB must not be negative")
		}
		return RedisCustomDeckProvider{Getter: TCPRedisStringGetter{Address: config.Redis.Address, Password: config.Redis.Password, DB: config.Redis.DB, DialTimeout: config.Redis.DialTimeout, MaxValueBytes: config.Redis.MaxValueBytes}}, nil
	}
	return nil, fmt.Errorf("nhsk: unsupported custom deck source %q", config.Source)
}

var _ game.CommandRuntime = (*gsr.Runtime)(nil)
