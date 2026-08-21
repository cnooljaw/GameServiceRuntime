package nhsk

import (
	"context"
	"errors"
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

// AIRunner adapts NHSK AI requests to the Runtime-owned Core Runner.
type AIRunner struct {
	core *gsr.Runner[AIRequest, aiResult]
}

// NewAIRunner starts the fixed-capacity AI runner.
func NewAIRunner(runtime *gsr.Runtime, provider AIProvider) (*AIRunner, error) {
	if runtime == nil || provider == nil {
		return nil, errInvalidAIRequest
	}
	core, err := gsr.NewRunner(runtime, gsr.RunnerConfig{Name: "nhsk-ai", Workers: 1, QueueSize: 128}, func(ctx context.Context, request AIRequest) (aiResult, error) {
		cards, moveErr := provider.Move(ctx, request)
		result := aiResult{BattleID: request.BattleID, GameNum: request.GameNum, SubgameNum: request.SubgameNum, UserID: request.UserID, SeatID: request.SeatID, TurnRevision: request.TurnRevision, VerifyCode: request.VerifyCode, StartedAt: request.StartedAt, Cards: append([]byte(nil), cards...)}
		if moveErr != nil {
			result.Error = moveErr.Error()
		}
		return result, moveErr
	})
	if err != nil {
		return nil, err
	}
	return &AIRunner{core: core}, nil
}

// SubmitAI deep-copies and queues one request.
func (runner *AIRunner) SubmitAI(request AIRequest) error {
	if runner == nil || !validAIRequest(request) {
		return errInvalidAIRequest
	}
	request = cloneAIRequest(request)
	err := runner.core.Submit(context.Background(), request.Target, applyAIResultCommand, request)
	if errors.Is(err, gsr.ErrRunnerQueueFull) {
		return errAIQueueFull
	}
	if errors.Is(err, gsr.ErrRunnerClosed) {
		return context.Canceled
	}
	return err
}

// Close cancels provider work and waits for the worker to return.
func (runner *AIRunner) Close() error {
	if runner == nil {
		return nil
	}
	return runner.core.Close(context.Background())
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
