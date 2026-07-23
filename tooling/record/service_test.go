package record

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const commandRecordTestIncrement gsr.CommandID = 0x7f280101

func TestRecorderDecoratorRecordsMailboxInputsWithoutChangingDelegate(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "record-node", Workers: 1, MailboxSize: 32})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	recorder, err := NewRecorderService(RecorderConfig{MaxEntries: 8})
	if err != nil {
		t.Fatal(err)
	}
	recorderRef, err := runtime.CreateService(gsr.ServiceSpec{Service: recorder})
	if err != nil {
		t.Fatal(err)
	}
	target := &recordTargetService{}
	decorated, err := NewDecorator(target, recorderRef, "battle:42", jsonCommandCodec{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	targetRef, err := runtime.CreateService(gsr.ServiceSpec{Service: decorated})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(runtime, recorderRef)
	if err != nil {
		t.Fatal(err)
	}

	for _, amount := range []int{3, 5} {
		if _, err := runtime.Call(context.Background(), targetRef, commandRecordTestIncrement, amount); err != nil {
			t.Fatalf("Call(%d) error = %v", amount, err)
		}
	}
	if got := target.total.Load(); got != 8 {
		t.Fatalf("delegate total = %d, want 8", got)
	}

	entries := eventuallyRecords(t, client, "battle:42", 0, 8)
	if len(entries) != 2 {
		t.Fatalf("record count = %d, want 2", len(entries))
	}
	for index, entry := range entries {
		if entry.FormatVersion != FormatVersion || entry.TargetKey != "battle:42" || entry.Target != targetRef || entry.Sequence != Sequence(index+1) || entry.Command != commandRecordTestIncrement || entry.RecordedAt.IsZero() {
			t.Fatalf("entry[%d] = %#v", index, entry)
		}
		var amount int
		if err := json.Unmarshal(entry.Payload, &amount); err != nil || amount != []int{3, 5}[index] {
			t.Fatalf("entry[%d] payload = %q, %v", index, entry.Payload, err)
		}
	}
	entries[0].Payload[0] = '!'
	again, err := client.List(context.Background(), recorderRef, "battle:42", 0, 8)
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Payload[0] == '!' {
		t.Fatal("List returned shared payload storage")
	}
}

func TestRecorderRetainsBoundedWindowAndSupportsCursorAndClear(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "record-node", Workers: 1})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	recorder, err := NewRecorderService(RecorderConfig{MaxEntries: 2, Now: func() time.Time { return time.Unix(100, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Service: recorder})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(runtime, ref)
	if err != nil {
		t.Fatal(err)
	}
	for sequence := Sequence(1); sequence <= 3; sequence++ {
		if err := client.Append(context.Background(), ref, RecordEntry{FormatVersion: FormatVersion, TargetKey: "battle:42", Target: gsr.ServiceRef{Node: "record-node", ID: 99}, Sequence: sequence, RecordedAt: time.Unix(100, 0), Command: commandRecordTestIncrement, Payload: []byte{byte(sequence)}}); err != nil {
			t.Fatalf("Append(%d) error = %v", sequence, err)
		}
	}
	entries, err := client.List(context.Background(), ref, "battle:42", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Sequence != 2 || entries[1].Sequence != 3 {
		t.Fatalf("List() = %#v, want sequences 2,3", entries)
	}
	cursor, err := client.List(context.Background(), ref, "battle:42", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(cursor) != 1 || cursor[0].Sequence != 3 {
		t.Fatalf("List(after 2) = %#v", cursor)
	}
	if err := client.Clear(context.Background(), ref, "battle:42"); err != nil {
		t.Fatal(err)
	}
	empty, err := client.List(context.Background(), ref, "battle:42", 0, 2)
	if err != nil || len(empty) != 0 {
		t.Fatalf("List(after Clear) = %#v, %v", empty, err)
	}
}

func TestDecoratorFailureModeControlsDelegate(t *testing.T) {
	recorderRef := gsr.ServiceRef{Node: "record-node", ID: 1}
	normalTarget := &recordTargetService{}
	normal, err := NewDecorator(normalTarget, recorderRef, "battle:42", failingCommandCodec{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := normal.Init(recordServiceContext{}); err != nil {
		t.Fatal(err)
	}
	if err := normal.Handle(recordCommandContext{}, gsr.Command{ID: commandRecordTestIncrement, Payload: 1}); err != nil {
		t.Fatalf("normal Handle() error = %v", err)
	}
	if normalTarget.total.Load() != 1 {
		t.Fatal("normal recording failure blocked delegate")
	}

	strictTarget := &recordTargetService{}
	strict, err := NewDecorator(strictTarget, recorderRef, "battle:42", failingCommandCodec{}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := strict.Init(recordServiceContext{}); err != nil {
		t.Fatal(err)
	}
	if err := strict.Handle(recordCommandContext{}, gsr.Command{ID: commandRecordTestIncrement, Payload: 1}); !errors.Is(err, errRecordCodec) {
		t.Fatalf("strict Handle() error = %v, want codec failure", err)
	}
	if strictTarget.total.Load() != 0 {
		t.Fatal("strict recording failure reached delegate")
	}
}

func TestReplayValidatesBundleAndSendsDecodedInputsToIsolatedTarget(t *testing.T) {
	bundle := RecordBundle{FormatVersion: FormatVersion, TargetKey: "battle:42", Entries: []RecordEntry{
		{FormatVersion: FormatVersion, TargetKey: "battle:42", Target: gsr.ServiceRef{Node: "old", ID: 10}, Sequence: 1, RecordedAt: time.Unix(1, 0), Command: commandRecordTestIncrement, Payload: []byte("3")},
		{FormatVersion: FormatVersion, TargetKey: "battle:42", Target: gsr.ServiceRef{Node: "old", ID: 10}, Sequence: 2, RecordedAt: time.Unix(2, 0), Command: commandRecordTestIncrement, Payload: []byte("5")},
	}}
	runtime := &replayRuntime{}
	factoryCalls := 0
	err := Replay(context.Background(), bundle, jsonCommandCodec{}, func(context.Context, RecordBundle) (ReplayTarget, error) {
		factoryCalls++
		return ReplayTarget{Runtime: runtime, Ref: gsr.ServiceRef{Node: "isolated", ID: 1}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if factoryCalls != 1 || len(runtime.sent) != 2 || runtime.sent[0].target.Node != "isolated" || runtime.sent[0].payload != 3 || runtime.sent[1].payload != 5 {
		t.Fatalf("Replay sends = %#v, factory calls = %d", runtime.sent, factoryCalls)
	}
	bundle.Entries[1].Sequence = 3
	if err := Replay(context.Background(), bundle, jsonCommandCodec{}, func(context.Context, RecordBundle) (ReplayTarget, error) { return ReplayTarget{}, nil }); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("Replay(non-contiguous) error = %v, want ErrInvalidBundle", err)
	}
}

func eventuallyRecords(t *testing.T, client Client, targetKey StableKey, after Sequence, limit int) []RecordEntry {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries, err := client.List(context.Background(), gsr.ServiceRef{Node: "record-node", ID: 1}, targetKey, after, limit)
		if err == nil && len(entries) > 0 {
			return entries
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("records were not available before timeout")
	return nil
}

type recordTargetService struct{ total atomic.Int64 }

func (*recordTargetService) Commands() []gsr.CommandID {
	return []gsr.CommandID{commandRecordTestIncrement}
}
func (*recordTargetService) Init(gsr.ServiceContext) error { return nil }
func (s *recordTargetService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	if command.ID != commandRecordTestIncrement {
		return gsr.ErrCommandNotRegistered
	}
	amount, ok := command.Payload.(int)
	if !ok {
		return errRecordCodec
	}
	s.total.Add(int64(amount))
	return ctx.Reply(s.total.Load())
}
func (*recordTargetService) Stop(context.Context) error { return nil }
func (*recordTargetService) Close() error               { return nil }

type jsonCommandCodec struct{}

func (jsonCommandCodec) Encode(command gsr.CommandID, payload any) ([]byte, error) {
	if command != commandRecordTestIncrement {
		return nil, errRecordCodec
	}
	return json.Marshal(payload)
}
func (jsonCommandCodec) Decode(command gsr.CommandID, payload []byte) (any, error) {
	if command != commandRecordTestIncrement {
		return nil, errRecordCodec
	}
	var result int
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

var errRecordCodec = errors.New("record test codec error")

type failingCommandCodec struct{}

func (failingCommandCodec) Encode(gsr.CommandID, any) ([]byte, error) { return nil, errRecordCodec }
func (failingCommandCodec) Decode(gsr.CommandID, []byte) (any, error) { return nil, errRecordCodec }

type recordServiceContext struct{}

func (recordServiceContext) Self() gsr.ServiceRef                          { return gsr.ServiceRef{Node: "record-node", ID: 2} }
func (recordServiceContext) Send(gsr.ServiceRef, gsr.CommandID, any) error { return nil }
func (recordServiceContext) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}
func (recordServiceContext) After(time.Duration, gsr.CommandID, any) (gsr.TimerID, error) {
	return 0, nil
}
func (recordServiceContext) Now() time.Time { return time.Unix(1, 0) }
func (recordServiceContext) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
func (recordServiceContext) Metrics() gsr.Metrics { return recordMetrics{} }

type recordMetrics struct{}

func (recordMetrics) Inc(string)                    {}
func (recordMetrics) Add(string, uint64)            {}
func (recordMetrics) SetGauge(string, int64)        {}
func (recordMetrics) Observe(string, time.Duration) {}

type recordCommandContext struct{}

func (recordCommandContext) Self() gsr.ServiceRef   { return gsr.ServiceRef{Node: "record-node", ID: 2} }
func (recordCommandContext) Source() gsr.ServiceRef { return gsr.ServiceRef{} }
func (recordCommandContext) Reply(any) error        { return nil }

type replayRuntime struct{ sent []replaySent }
type replaySent struct {
	target  gsr.ServiceRef
	command gsr.CommandID
	payload any
}

func (r *replayRuntime) Send(target gsr.ServiceRef, command gsr.CommandID, payload any) error {
	r.sent = append(r.sent, replaySent{target: target, command: command, payload: payload})
	return nil
}
