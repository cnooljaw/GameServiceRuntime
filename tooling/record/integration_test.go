package record

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestReplaySendsOnlyToFactoryIsolatedRuntime(t *testing.T) {
	originalRuntime := gsr.NewRuntime(gsr.Config{NodeID: "original", Workers: 1})
	t.Cleanup(func() { _ = originalRuntime.Close(context.Background()) })
	original := &recordTargetService{}
	originalRef, err := originalRuntime.CreateService(gsr.ServiceSpec{Service: original})
	if err != nil {
		t.Fatal(err)
	}
	bundle := testRecordBundle()
	bundle.Entries[0].Target = originalRef
	bundle.Entries[1].Target = originalRef

	var isolatedRuntime *gsr.Runtime
	var isolated *recordTargetService
	err = Replay(context.Background(), bundle, jsonCommandCodec{}, func(context.Context, RecordBundle) (ReplayTarget, error) {
		isolatedRuntime = gsr.NewRuntime(gsr.Config{NodeID: "isolated", Workers: 1})
		isolated = &recordTargetService{}
		ref, createErr := isolatedRuntime.CreateService(gsr.ServiceSpec{Service: isolated})
		if createErr != nil {
			return ReplayTarget{}, createErr
		}
		return ReplayTarget{Runtime: isolatedRuntime, Ref: ref}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = isolatedRuntime.Close(context.Background()) })
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && isolated.total.Load() != 8 {
		time.Sleep(time.Millisecond)
	}
	if isolated.total.Load() != 8 {
		t.Fatalf("isolated replay total = %d, want 8", isolated.total.Load())
	}
	if original.total.Load() != 0 {
		t.Fatalf("original Runtime received replay input: total=%d", original.total.Load())
	}
}

func TestDecoratorRecordsTimerDeliveredCommand(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "record-node", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	recorder, err := NewRecorderService(RecorderConfig{MaxEntries: 4})
	if err != nil {
		t.Fatal(err)
	}
	recorderRef, err := runtime.CreateService(gsr.ServiceSpec{Service: recorder})
	if err != nil {
		t.Fatal(err)
	}
	target := &timerRecordTarget{}
	decorated, err := NewDecorator(target, recorderRef, "battle:timer", jsonCommandCodec{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.CreateService(gsr.ServiceSpec{Service: decorated}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(runtime)
	if err != nil {
		t.Fatal(err)
	}
	entries := eventuallyRecords(t, client, "battle:timer", 0, 4)
	if len(entries) != 1 || entries[0].Command != commandRecordTestIncrement || string(entries[0].Payload) != "4" || target.total.Load() != 4 {
		t.Fatalf("timer record = %#v, target total=%d", entries, target.total.Load())
	}
}

type timerRecordTarget struct {
	context gsr.ServiceContext
	total   atomic.Int64
}

const commandRecordTimerStart gsr.CommandID = 0x7f280102

func (s *timerRecordTarget) StartupCommand() (gsr.Command, bool) {
	return gsr.Command{ID: commandRecordTimerStart}, true
}
func (s *timerRecordTarget) Init(ctx gsr.ServiceContext) error { s.context = ctx; return nil }
func (s *timerRecordTarget) Handle(_ gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case commandRecordTimerStart:
		_, err := s.context.After(time.Millisecond, commandRecordTestIncrement, 4)
		return err
	case commandRecordTestIncrement:
		amount, ok := command.Payload.(int)
		if !ok {
			return errRecordCodec
		}
		s.total.Add(int64(amount))
		return nil
	default:
		return gsr.ErrUnknownCommand
	}
}
func (*timerRecordTarget) Stop(context.Context) error { return nil }
func (*timerRecordTarget) Close() error               { return nil }
