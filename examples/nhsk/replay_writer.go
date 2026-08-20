package nhsk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

var (
	errInvalidReplayWrite = errors.New("nhsk: invalid replay write")
	errReplayQueueFull    = errors.New("nhsk: replay writer queue full")
)

// ReplayArtifact is one fully serialized immutable replay file.
type ReplayArtifact struct {
	Target       gsr.ServiceRef
	BattleID     game.BattleID
	GameNum      uint16
	SubgameNum   uint16
	ReplayName   string
	RelativePath string
	XML          []byte
}

// ReplaySubmitter accepts a replay artifact without blocking a Battle Mailbox.
type ReplaySubmitter interface {
	SubmitReplay(ReplayArtifact) error
}

// ReplayArtifactWriter performs the external write for one immutable artifact.
type ReplayArtifactWriter interface {
	WriteReplay(context.Context, ReplayArtifact) error
}

// FileReplayWriter writes artifacts beneath one configured root directory.
type FileReplayWriter struct{ Root string }

// WriteReplay creates the fixed relative directory and writes the complete XML
// using checkable MkdirAll/Open/Write/Close operations.
func (writer FileReplayWriter) WriteReplay(_ context.Context, artifact ReplayArtifact) error {
	if strings.TrimSpace(writer.Root) == "" || !validReplayArtifact(artifact) || filepath.IsAbs(artifact.RelativePath) || strings.Contains(artifact.RelativePath, "..") || filepath.Base(artifact.ReplayName) != artifact.ReplayName {
		return errInvalidReplayWrite
	}
	directory := filepath.Join(writer.Root, filepath.FromSlash(artifact.RelativePath))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(directory, artifact.ReplayName), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := io.Copy(file, bytes.NewReader(artifact.XML))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// ReplayWriterRunnerConfig bounds replay I/O concurrency and queued artifacts.
type ReplayWriterRunnerConfig struct {
	QueueSize int
	Workers   int
}

// ReplayWriterRunner owns fixed external I/O workers and reports completion to
// the exact Battle ServiceRef carried by each artifact.
type ReplayWriterRunner struct {
	runtime   game.CommandRuntime
	writer    ReplayArtifactWriter
	queue     chan ReplayArtifact
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
	lifecycle sync.RWMutex
}

// NewReplayWriterRunner starts a bounded replay writer runner.
func NewReplayWriterRunner(runtime game.CommandRuntime, writer ReplayArtifactWriter, config ReplayWriterRunnerConfig) (*ReplayWriterRunner, error) {
	if runtime == nil || writer == nil {
		return nil, errInvalidReplayWrite
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 128
	}
	if config.Workers <= 0 {
		config.Workers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := &ReplayWriterRunner{runtime: runtime, writer: writer, queue: make(chan ReplayArtifact, config.QueueSize), ctx: ctx, cancel: cancel}
	runner.wg.Add(config.Workers)
	for index := 0; index < config.Workers; index++ {
		go runner.worker()
	}
	return runner, nil
}

// SubmitReplay copies and queues one artifact without blocking the caller.
func (runner *ReplayWriterRunner) SubmitReplay(artifact ReplayArtifact) error {
	if runner == nil || !validReplayArtifact(artifact) {
		return errInvalidReplayWrite
	}
	runner.lifecycle.RLock()
	defer runner.lifecycle.RUnlock()
	artifact.XML = append([]byte(nil), artifact.XML...)
	select {
	case <-runner.ctx.Done():
		return context.Canceled
	case runner.queue <- artifact:
		return nil
	default:
		return errReplayQueueFull
	}
}

// Close cancels queued writes and waits for active workers to return.
func (runner *ReplayWriterRunner) Close() error {
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

func (runner *ReplayWriterRunner) worker() {
	defer runner.wg.Done()
	for {
		select {
		case <-runner.ctx.Done():
			return
		case artifact := <-runner.queue:
			if runner.ctx.Err() != nil {
				return
			}
			err := runner.writer.WriteReplay(runner.ctx, artifact)
			result := replayWriteResult{BattleID: artifact.BattleID, GameNum: artifact.GameNum, SubgameNum: artifact.SubgameNum, ReplayName: artifact.ReplayName}
			if err != nil {
				result.Error = err.Error()
			}
			_ = runner.runtime.Send(artifact.Target, applyReplayResultCommand, result)
		}
	}
}

func validReplayArtifact(artifact ReplayArtifact) bool {
	return artifact.Target.Node != "" && artifact.Target.ID != 0 && artifact.BattleID != 0 && artifact.GameNum != 0 && artifact.SubgameNum != 0 && artifact.ReplayName != "" && artifact.RelativePath != "" && len(artifact.XML) > 0
}
