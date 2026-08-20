package nhsk

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

var (
	errInvalidAIRequest = errors.New("nhsk: invalid AI request")
	errAIQueueFull      = errors.New("nhsk: AI queue full")
)

// AIScene is the immutable minimum scene consumed by an NHSK AI provider.
type AIScene struct {
	ActiveSeat     uint8
	FirstOutSeat   uint8
	HandCounts     [4]uint8
	Hand           []byte
	CapturedPoints [4]uint16
	Ranks          [4]uint8
	TrickPoint     uint32
	Leading        bool
	OutedCards     [][]byte
}

// AIRequest identifies one exact action opportunity and its immutable scene.
type AIRequest struct {
	Target       gsr.ServiceRef
	BattleID     game.BattleID
	ProductID    uint32
	MatchID      uint32
	RoundID      uint32
	GameNum      uint16
	SubgameNum   uint16
	UserID       uint32
	SeatID       uint8
	TurnRevision uint64
	VerifyCode   uint32
	StartedAt    time.Time
	MoveMS       uint32
	ActionMS     uint32
	Scene        AIScene
}

// AIProvider computes one candidate move outside a Battle Mailbox.
type AIProvider interface {
	Move(context.Context, AIRequest) ([]byte, error)
}

// AISubmitter accepts an immutable AI request without blocking a Battle.
type AISubmitter interface {
	SubmitAI(AIRequest) error
}

// LocalAIProvider implements the reference fallback: lead the smallest single
// and pass when following.
type LocalAIProvider struct{}

// Move returns one deterministic local candidate.
func (LocalAIProvider) Move(_ context.Context, request AIRequest) ([]byte, error) {
	if !validAIRequest(request) {
		return nil, errInvalidAIRequest
	}
	if len(request.Scene.Hand) == 0 {
		return nil, errInvalidAIRequest
	}
	if !request.Scene.Leading {
		return nil, nil
	}
	card := request.Scene.Hand[0]
	for _, candidate := range request.Scene.Hand[1:] {
		candidateValue, cardValue := cardLogicValue(int(candidate&0x0f)), cardLogicValue(int(card&0x0f))
		if candidateValue < cardValue || candidateValue == cardValue && candidate < card {
			card = candidate
		}
	}
	return []byte{card}, nil
}

type aiResult struct {
	BattleID     game.BattleID
	GameNum      uint16
	SubgameNum   uint16
	UserID       uint32
	SeatID       uint8
	TurnRevision uint64
	VerifyCode   uint32
	StartedAt    time.Time
	Cards        []byte
	Error        string
}

// AIRunner owns a fixed worker pool for provider calls.
type AIRunner struct {
	runtime   game.CommandRuntime
	provider  AIProvider
	queue     chan AIRequest
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
	lifecycle sync.RWMutex
}

// NewAIRunner starts the fixed-capacity AI runner.
func NewAIRunner(runtime game.CommandRuntime, provider AIProvider) (*AIRunner, error) {
	if runtime == nil || provider == nil {
		return nil, errInvalidAIRequest
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := &AIRunner{runtime: runtime, provider: provider, queue: make(chan AIRequest, 128), ctx: ctx, cancel: cancel}
	runner.wg.Add(1)
	go runner.worker()
	return runner, nil
}

// SubmitAI deep-copies and queues one request.
func (runner *AIRunner) SubmitAI(request AIRequest) error {
	if runner == nil || !validAIRequest(request) {
		return errInvalidAIRequest
	}
	runner.lifecycle.RLock()
	defer runner.lifecycle.RUnlock()
	request = cloneAIRequest(request)
	select {
	case <-runner.ctx.Done():
		return context.Canceled
	case runner.queue <- request:
		return nil
	default:
		return errAIQueueFull
	}
}

// Close cancels provider work and waits for the worker to return.
func (runner *AIRunner) Close() error {
	if runner == nil {
		return nil
	}
	runner.closeOnce.Do(func() {
		runner.lifecycle.Lock()
		defer runner.lifecycle.Unlock()
		runner.cancel()
		runner.wg.Wait()
	})
	return nil
}

func (runner *AIRunner) worker() {
	defer runner.wg.Done()
	for {
		select {
		case <-runner.ctx.Done():
			return
		case request := <-runner.queue:
			if runner.ctx.Err() != nil {
				return
			}
			cards, err := runner.provider.Move(runner.ctx, request)
			result := aiResult{BattleID: request.BattleID, GameNum: request.GameNum, SubgameNum: request.SubgameNum, UserID: request.UserID, SeatID: request.SeatID, TurnRevision: request.TurnRevision, VerifyCode: request.VerifyCode, StartedAt: request.StartedAt, Cards: append([]byte(nil), cards...)}
			if err != nil {
				result.Error = err.Error()
			}
			_ = runner.runtime.Send(request.Target, applyAIResultCommand, result)
		}
	}
}

func validAIRequest(request AIRequest) bool {
	return request.Target.Node != "" && request.Target.ID != 0 && request.BattleID != 0 && request.ProductID != 0 && request.MatchID != 0 && request.GameNum != 0 && request.SubgameNum != 0 && request.UserID != 0 && request.SeatID < 4 && request.TurnRevision != 0 && request.VerifyCode != 0 && !request.StartedAt.IsZero() && len(request.Scene.Hand) > 0
}

func cloneAIRequest(request AIRequest) AIRequest {
	request.Scene.Hand = append([]byte(nil), request.Scene.Hand...)
	outedCards := request.Scene.OutedCards
	request.Scene.OutedCards = make([][]byte, len(outedCards))
	for index, cards := range outedCards {
		request.Scene.OutedCards[index] = append([]byte(nil), cards...)
	}
	return request
}
