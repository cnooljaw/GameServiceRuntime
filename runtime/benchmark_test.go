package gsr_test

import (
	"context"
	"fmt"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const (
	benchmarkCommandSend gsr.CommandID = iota + 1
	benchmarkCommandCall
)

func BenchmarkSend(b *testing.B) {
	rt, ref, svc := newBenchmarkRuntime(b, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := rt.Send(ref, benchmarkCommandSend, nil); err != nil {
			b.Fatal(err)
		}
		<-svc.handled
	}
}

func BenchmarkCallReply(b *testing.B) {
	rt, ref, _ := newBenchmarkRuntime(b, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rt.Call(context.Background(), ref, benchmarkCommandCall, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkManyServices(b *testing.B) {
	for _, serviceCount := range []int{1, 100, 1000} {
		b.Run(fmt.Sprintf("services_%d", serviceCount), func(b *testing.B) {
			rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 4})
			b.Cleanup(func() { _ = rt.Close(context.Background()) })
			refs := make([]gsr.ServiceRef, serviceCount)
			for i := range refs {
				ref, err := rt.CreateService(gsr.ServiceSpec{Service: &benchmarkService{handled: make(chan struct{})}})
				if err != nil {
					b.Fatal(err)
				}
				refs[i] = ref
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := rt.Call(context.Background(), refs[i%len(refs)], benchmarkCommandCall, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTimerDelivery(b *testing.B) {
	rt, ref, svc := newBenchmarkRuntime(b, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rt.After(ref, 0, benchmarkCommandSend, nil); err != nil {
			b.Fatal(err)
		}
		<-svc.handled
	}
}

func newBenchmarkRuntime(b *testing.B, workers int) (*gsr.Runtime, gsr.ServiceRef, *benchmarkService) {
	b.Helper()
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: workers, MailboxSize: 64})
	svc := &benchmarkService{handled: make(chan struct{})}
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: svc})
	if err != nil {
		rt.Close(context.Background())
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = rt.Close(context.Background()) })
	return rt, ref, svc
}

type benchmarkService struct{ handled chan struct{} }

func (*benchmarkService) Init(gsr.ServiceContext) error { return nil }
func (s *benchmarkService) Handle(ctx gsr.CommandContext, command gsr.Command) error {
	switch command.ID {
	case benchmarkCommandSend:
		s.handled <- struct{}{}
		return nil
	case benchmarkCommandCall:
		return ctx.Reply("pong")
	default:
		return fmt.Errorf("unexpected benchmark command %d", command.ID)
	}
}
func (*benchmarkService) Stop(context.Context) error { return nil }
func (*benchmarkService) Close() error               { return nil }
