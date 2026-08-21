package nhsk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	customDeckCardCount                            = 104
	customDeckSeatCount                            = 4
	defaultCustomDeckQueueSize                     = 128
	defaultCustomDeckWorkers                       = 1
	defaultCustomDeckLoadTimeout                   = 5 * time.Second
	applyCustomDeckLoadResultCommand gsr.CommandID = 0x0410f01c
)

var (
	errCustomDeckQueueFull      = errors.New("nhsk: custom deck queue full")
	errInvalidCustomDeckRequest = errors.New("nhsk: invalid custom deck request")
)

// CustomDeck is one immutable 104-card deal and its optional banker override.
// Card bytes intentionally retain the legacy debug grammar and are not
// validated as a standard NHSK deck.
type CustomDeck struct {
	Cards      []byte
	BankerSeat int
}

// CustomDeckCatalog is the canonical snapshot supplied to one subgame. The
// external compatibility bridge deep-copies it before sending the API
// Command, so a provider may reuse its own buffers after Load returns.
type CustomDeckCatalog struct {
	Decks []CustomDeck
}

// CustomDeckLookup identifies the legacy data-source selection inputs. The
// runner applies enable and account authorization before invoking a provider.
type CustomDeckLookup struct {
	GameID    uint32
	ProductID uint32
	UserIDs   []uint32
}

// CustomDeckProvider loads one immutable catalog outside a Battle Mailbox.
// Implementations may use a local file, Redis adapter, or another bounded
// source; they must honor context cancellation where the source permits it.
type CustomDeckProvider interface {
	Load(context.Context, CustomDeckLookup) (CustomDeckCatalog, error)
}

// LocalFileCustomDeckProvider is the dependency-light example provider. It
// reads the file once per submitted subgame and never observes a file while a
// subgame is running.
type LocalFileCustomDeckProvider struct {
	Path string
}

// Load reads and parses the configured legacy custom-deck text snapshot.
func (provider LocalFileCustomDeckProvider) Load(ctx context.Context, _ CustomDeckLookup) (CustomDeckCatalog, error) {
	if strings.TrimSpace(provider.Path) == "" {
		return CustomDeckCatalog{}, nil
	}
	if err := contextError(ctx); err != nil {
		return CustomDeckCatalog{}, err
	}
	data, err := os.ReadFile(provider.Path)
	if err != nil {
		return CustomDeckCatalog{}, fmt.Errorf("read custom deck: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return CustomDeckCatalog{}, err
	}
	return ParseCustomDeck(string(data))
}

// ParseCustomDeck preserves the old debug grammar used by NHSK MakecardConfig.
// Complete blocks with exactly 104 cards are accepted; incomplete blocks are
// ignored, excess cards are truncated, and invalid tokens fail the load.
func ParseCustomDeck(text string) (CustomDeckCatalog, error) {
	var catalog CustomDeckCatalog
	text = strings.TrimSpace(text)
	if text == "" {
		return catalog, nil
	}

	inBlock := false
	current := make([]byte, 0, customDeckCardCount)
	banker := -1
	appendBlock := func() {
		if len(current) != customDeckCardCount {
			return
		}
		catalog.Decks = append(catalog.Decks, CustomDeck{Cards: append([]byte(nil), current...), BankerSeat: banker})
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "{") {
			inBlock = true
			current = current[:0]
			banker = -1
			line = strings.TrimSpace(strings.TrimPrefix(line, "{"))
			if line == "" {
				continue
			}
		}
		if strings.HasPrefix(line, "}") {
			appendBlock()
			inBlock = false
			continue
		}
		if !inBlock {
			continue
		}
		if strings.HasPrefix(line, "@") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "@"))
			seat, err := strconv.Atoi(value)
			if err != nil {
				return CustomDeckCatalog{}, fmt.Errorf("invalid banker %q: %w", line, err)
			}
			if seat >= 0 && seat < customDeckSeatCount {
				banker = seat
			}
			continue
		}
		line = strings.TrimRight(line, ",")
		for _, part := range strings.Split(line, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			part = strings.TrimPrefix(strings.ToLower(part), "0x")
			value, err := strconv.ParseUint(part, 16, 8)
			if err != nil {
				return CustomDeckCatalog{}, fmt.Errorf("invalid card %q: %w", part, err)
			}
			if len(current) < customDeckCardCount {
				current = append(current, byte(value))
			}
		}
	}
	if inBlock {
		appendBlock()
	}
	return catalog, nil
}

func (catalog CustomDeckCatalog) clone() CustomDeckCatalog {
	copyCatalog := CustomDeckCatalog{Decks: make([]CustomDeck, len(catalog.Decks))}
	for index, deck := range catalog.Decks {
		copyCatalog.Decks[index] = CustomDeck{BankerSeat: deck.BankerSeat, Cards: append([]byte(nil), deck.Cards...)}
	}
	return copyCatalog
}

func (catalog CustomDeckCatalog) valid() bool {
	if len(catalog.Decks) == 0 {
		return false
	}
	for _, deck := range catalog.Decks {
		if len(deck.Cards) != customDeckCardCount {
			return false
		}
	}
	return true
}

// CustomDeckLoadRequest is the per-subgame request submitted by an outer
// compatibility bridge.
type CustomDeckLoadRequest struct {
	Target     gsr.ServiceRef
	BattleID   game.BattleID
	GameNum    uint16
	SubgameNum uint16
	Lookup     CustomDeckLookup
}

// CustomDeckRunnerConfig controls the external runner's authorization and
// bounded work queue. A non-positive queue or worker value uses a safe default.
type CustomDeckRunnerConfig struct {
	Enabled         bool
	AllowAnyAccount bool
	AllowedAccounts []uint32
	QueueSize       int
	Workers         int
	LoadTimeout     time.Duration
}

// CustomDeckRunner adapts legacy custom-deck I/O to the Runtime-owned Core Runner.
type CustomDeckRunner struct {
	runtime  *gsr.Runtime
	provider CustomDeckProvider
	config   CustomDeckRunnerConfig
	core     *gsr.Runner[CustomDeckLoadRequest, customDeckLoadResult]
}

type customDeckLoadResult struct {
	Provision ProvideCustomDeckRequest
	Available bool
}

// NewCustomDeckRunner creates and starts a bounded provider runner.
func NewCustomDeckRunner(runtime *gsr.Runtime, provider CustomDeckProvider, config CustomDeckRunnerConfig) (*CustomDeckRunner, error) {
	if runtime == nil || provider == nil {
		return nil, errInvalidCustomDeckRequest
	}
	if config.QueueSize <= 0 {
		config.QueueSize = defaultCustomDeckQueueSize
	}
	if config.Workers <= 0 {
		config.Workers = defaultCustomDeckWorkers
	}
	if config.LoadTimeout <= 0 {
		config.LoadTimeout = defaultCustomDeckLoadTimeout
	}
	config.AllowedAccounts = append([]uint32(nil), config.AllowedAccounts...)
	core, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "nhsk-custom-deck", Workers: config.Workers, QueueSize: config.QueueSize}, func(ctx context.Context, request CustomDeckLoadRequest) (customDeckLoadResult, error) {
		loadContext, cancel := context.WithTimeout(ctx, config.LoadTimeout)
		catalog, loadErr := provider.Load(loadContext, request.Lookup)
		cancel()
		result := customDeckLoadResult{}
		if catalog.valid() {
			result.Available = true
			result.Provision = ProvideCustomDeckRequest{BattleID: request.BattleID, GameNum: request.GameNum, SubgameNum: request.SubgameNum, Catalog: catalog.clone()}
		}
		return result, loadErr
	})
	if err != nil {
		return nil, err
	}
	return &CustomDeckRunner{runtime: runtime, provider: provider, config: config, core: core}, nil
}

// SubmitCustomDeck submits one non-blocking legacy compatibility load. Queue
// saturation is returned to the bridge; the Battle falls back to ordinary
// dealing when no provision arrives.
func (runner *CustomDeckRunner) SubmitCustomDeck(request CustomDeckLoadRequest) error {
	if runner == nil || request.Target.ID == 0 || request.BattleID == 0 || request.GameNum == 0 || request.SubgameNum == 0 {
		return errInvalidCustomDeckRequest
	}
	request.Lookup.UserIDs = append([]uint32(nil), request.Lookup.UserIDs...)
	if !runner.config.Enabled || !runner.authorized(request.Lookup.UserIDs) {
		return nil
	}
	err := runner.core.Submit(context.Background(), request.Target, applyCustomDeckLoadResultCommand, request)
	if errors.Is(err, gsr.ErrRunnerQueueFull) {
		return errCustomDeckQueueFull
	}
	if errors.Is(err, gsr.ErrRunnerClosed) {
		return context.Canceled
	}
	return err
}

// LoadAndProvide performs one bounded Legacy compatibility load and sends the
// public ProvideCustomDeck Command before returning. It is used only by the
// ordered Legacy UPDATE_GAME hook so an immediately following START cannot
// overtake the old Redis bridge; direct Cluster callers should provide the
// catalog themselves.
func (runner *CustomDeckRunner) LoadAndProvide(ctx context.Context, request CustomDeckLoadRequest) error {
	if runner == nil || request.Target.ID == 0 || request.BattleID == 0 || request.GameNum == 0 || request.SubgameNum == 0 {
		return errInvalidCustomDeckRequest
	}
	if !runner.config.Enabled || !runner.authorized(request.Lookup.UserIDs) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	loadCtx, cancel := context.WithTimeout(ctx, runner.config.LoadTimeout)
	catalog, err := runner.provider.Load(loadCtx, request.Lookup)
	cancel()
	if err != nil {
		return err
	}
	if !catalog.valid() {
		return nil
	}
	return runner.sendResult(request, catalog)
}

// Close cancels queued provider work and waits for every worker to return.
func (runner *CustomDeckRunner) Close() error {
	if runner == nil {
		return nil
	}
	return runner.core.Close(context.Background())
}

func (runner *CustomDeckRunner) authorized(userIDs []uint32) bool {
	if !runner.config.Enabled {
		return false
	}
	if runner.config.AllowAnyAccount {
		return true
	}
	for _, allowed := range runner.config.AllowedAccounts {
		if allowed == 0 {
			continue
		}
		for _, userID := range userIDs {
			if userID == allowed {
				return true
			}
		}
	}
	return false
}

func (runner *CustomDeckRunner) sendResult(request CustomDeckLoadRequest, catalog CustomDeckCatalog) error {
	return runner.runtime.Send(request.Target, ProvideCustomDeckCommand, ProvideCustomDeckRequest{
		BattleID: request.BattleID, GameNum: request.GameNum, SubgameNum: request.SubgameNum,
		Catalog: catalog.clone(),
	})
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	default:
		return nil
	}
}
