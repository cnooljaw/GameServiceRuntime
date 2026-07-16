package gsr_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestCallReturnsReply(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: replyService{}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := rt.Call(context.Background(), ref, 10, "ping")
	if err != nil || got != "pong" {
		t.Fatalf("got %v, err %v", got, err)
	}
}

func TestCallTimesOut(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: noReplyService{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = rt.Call(ctx, ref, 10, nil)
	if !errors.Is(err, gsr.ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}

func TestReplyTwiceFails(t *testing.T) {
	rt := gsr.NewRuntime(gsr.Config{NodeID: "local"})
	defer rt.Close(context.Background())
	ref, err := rt.CreateService(gsr.ServiceSpec{Service: twiceReplyService{}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := rt.Call(context.Background(), ref, 10, nil)
	if err != nil || got != "first" {
		t.Fatalf("got %v, err %v", got, err)
	}
	if err := <-twiceReplyError; !errors.Is(err, gsr.ErrReplyTwice) {
		t.Fatalf("second reply = %v", err)
	}
}

type replyService struct{}

func (replyService) Commands() []gsr.CommandID                          { return []gsr.CommandID{10} }
func (replyService) Init(gsr.ServiceContext) error                      { return nil }
func (replyService) Handle(ctx gsr.CommandContext, _ gsr.Command) error { return ctx.Reply("pong") }
func (replyService) Stop(context.Context) error                         { return nil }
func (replyService) Close() error                                       { return nil }

type noReplyService struct{ replyService }

func (noReplyService) Handle(gsr.CommandContext, gsr.Command) error { return nil }

var twiceReplyError = make(chan error, 1)

type twiceReplyService struct{ replyService }

func (twiceReplyService) Handle(ctx gsr.CommandContext, _ gsr.Command) error {
	_ = ctx.Reply("first")
	twiceReplyError <- ctx.Reply("second")
	return nil
}
