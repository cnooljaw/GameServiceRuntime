package nhsk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestFileReplayWriterWritesCompleteArtifactBelowRoot(t *testing.T) {
	root := t.TempDir()
	artifact := testReplayArtifact()
	artifact.XML = []byte("<GameRecord><Moves/></GameRecord>")
	if err := (FileReplayWriter{Root: root}).WriteReplay(context.Background(), artifact); err != nil {
		t.Fatalf("WriteReplay: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "FuPan", "20260103", "00", artifact.ReplayName))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !reflect.DeepEqual(got, artifact.XML) {
		t.Fatalf("file = %q, want %q", got, artifact.XML)
	}
}

func TestFileReplayWriterRejectsEscapingPath(t *testing.T) {
	artifact := testReplayArtifact()
	artifact.RelativePath = "../outside"
	if err := (FileReplayWriter{Root: t.TempDir()}).WriteReplay(context.Background(), artifact); !errors.Is(err, errInvalidReplayWrite) {
		t.Fatalf("WriteReplay error = %v, want %v", err, errInvalidReplayWrite)
	}
}

func TestReplayWriterRunnerReportsExactIdentityAndOwnsXMLCopy(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	receiver := &replayRunnerResultService{sent: make(chan replayWriteResult, 1)}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: receiver})
	if err != nil {
		t.Fatal(err)
	}
	writer := &recordingReplayWriter{written: make(chan ReplayArtifact, 1)}
	runner, err := NewReplayWriterRunner(runtime, writer, ReplayWriterRunnerConfig{QueueSize: 1, Workers: 1})
	if err != nil {
		t.Fatalf("NewReplayWriterRunner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	artifact := testReplayArtifact()
	artifact.Target = ref
	wantXML := append([]byte(nil), artifact.XML...)
	if err := runner.SubmitReplay(artifact); err != nil {
		t.Fatalf("SubmitReplay: %v", err)
	}
	artifact.XML[0] = 'X'
	select {
	case got := <-writer.written:
		if !reflect.DeepEqual(got.XML, wantXML) {
			t.Fatalf("writer XML = %q, want immutable %q", got.XML, wantXML)
		}
	case <-time.After(time.Second):
		t.Fatal("writer did not receive artifact")
	}
	select {
	case got := <-receiver.sent:
		if got.BattleID != 7 || got.GameNum != 2 || got.SubgameNum != 3 || got.ReplayName != "NHSK.xml" || got.Error != "" {
			t.Fatalf("result = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not receive result")
	}
}

func TestReplayWriterRunnerBoundsQueueWithoutBlockingBattle(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	receiver := &replayRunnerResultService{sent: make(chan replayWriteResult, 3)}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: receiver})
	if err != nil {
		t.Fatal(err)
	}
	writer := replayWriterFunc(func(context.Context, ReplayArtifact) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})
	runner, err := NewReplayWriterRunner(runtime, writer, ReplayWriterRunnerConfig{QueueSize: 1, Workers: 1})
	if err != nil {
		t.Fatalf("NewReplayWriterRunner: %v", err)
	}
	artifact := testReplayArtifact()
	artifact.Target = ref
	if err := runner.SubmitReplay(artifact); err != nil {
		t.Fatalf("submit active: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	if err := runner.SubmitReplay(artifact); err != nil {
		t.Fatalf("submit queued: %v", err)
	}
	if err := runner.SubmitReplay(artifact); !errors.Is(err, errReplayQueueFull) {
		t.Fatalf("third submit error = %v, want %v", err, errReplayQueueFull)
	}
	close(release)
	if err := runner.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func testReplayArtifact() ReplayArtifact {
	return ReplayArtifact{
		Target: gsr.ServiceRef{Node: "node-a", ID: 9}, BattleID: game.BattleID(7), GameNum: 2, SubgameNum: 3,
		ReplayName: "NHSK.xml", RelativePath: "FuPan/20260103/00", XML: []byte("<GameRecord/>"),
	}
}

type replayRunnerResultService struct{ sent chan replayWriteResult }

func (*replayRunnerResultService) Init(gsr.ServiceContext) error { return nil }
func (service *replayRunnerResultService) Handle(_ gsr.CommandContext, command gsr.Command) error {
	if command.ID != applyReplayResultCommand {
		return errors.New("unexpected command")
	}
	result, ok := command.Payload.(gsr.RunnerResult[replayWriteResult])
	if !ok || result.Err != nil {
		return errors.New("unexpected replay runner payload")
	}
	service.sent <- result.Value
	return nil
}
func (*replayRunnerResultService) Stop(context.Context) error { return nil }
func (*replayRunnerResultService) Close() error               { return nil }

type recordingReplayWriter struct{ written chan ReplayArtifact }

func (writer *recordingReplayWriter) WriteReplay(_ context.Context, artifact ReplayArtifact) error {
	writer.written <- artifact
	return nil
}

type replayWriterFunc func(context.Context, ReplayArtifact) error

func (write replayWriterFunc) WriteReplay(ctx context.Context, artifact ReplayArtifact) error {
	return write(ctx, artifact)
}
