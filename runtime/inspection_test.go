package gsr_test

import (
	"context"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestRuntimeInspectReportsIdentityAndStatus(t *testing.T) {
	now := time.Date(2026, 7, 17, 18, 0, 0, 0, time.UTC)
	runtime := gsr.NewRuntime(gsr.Config{
		NodeID: "node-a",
		Now:    func() time.Time { return now },
	})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	inspection := runtime.Inspect()
	if inspection.Node != "node-a" {
		t.Fatalf("Node = %q, want node-a", inspection.Node)
	}
	if inspection.Status != gsr.RuntimeRunning {
		t.Fatalf("Status = %v, want RuntimeRunning", inspection.Status)
	}
	if !inspection.CapturedAt.Equal(now) {
		t.Fatalf("CapturedAt = %v, want %v", inspection.CapturedAt, now)
	}
}

func TestRuntimeInspectReturnsServicesInStableOrder(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	refs := make([]gsr.ServiceRef, 0, 3)
	for _, name := range []gsr.ServiceName{"third", "first", "second"} {
		ref, err := runtime.CreateService(gsr.ServiceSpec{Name: name, Service: inspectionService{}})
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
	}

	inspection := runtime.Inspect()
	if len(inspection.Services) != len(refs) {
		t.Fatalf("Services length = %d, want %d", len(inspection.Services), len(refs))
	}
	for index, service := range inspection.Services {
		if service.Ref != refs[index] {
			t.Fatalf("Services[%d].Ref = %v, want %v", index, service.Ref, refs[index])
		}
		if service.Status != gsr.ServiceRunning {
			t.Fatalf("Services[%d].Status = %v, want ServiceRunning", index, service.Status)
		}
		if service.MailboxDepth != 0 {
			t.Fatalf("Services[%d].MailboxDepth = %d, want 0", index, service.MailboxDepth)
		}
	}
	if inspection.Services[0].Name != "third" || inspection.Services[1].Name != "first" || inspection.Services[2].Name != "second" {
		t.Fatalf("service names are not paired with stable refs: %#v", inspection.Services)
	}
}

func TestRuntimeInspectReturnsIndependentCopies(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	ref, err := runtime.CreateService(gsr.ServiceSpec{Name: "original", Service: inspectionService{}})
	if err != nil {
		t.Fatal(err)
	}

	first := runtime.Inspect()
	first.Services[0].Ref = gsr.ServiceRef{Node: "changed", ID: 99}
	first.Services[0].Name = "changed"
	first.Services = append(first.Services, gsr.ServiceInspection{})

	second := runtime.Inspect()
	if len(second.Services) != 1 {
		t.Fatalf("Services length = %d, want 1", len(second.Services))
	}
	if second.Services[0].Ref != ref || second.Services[0].Name != "original" {
		t.Fatalf("second inspection was changed through first copy: %#v", second.Services[0])
	}
}

func TestRuntimeInspectWorksAfterClose(t *testing.T) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "node-a"})
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	inspection := runtime.Inspect()
	if inspection.Status != gsr.RuntimeClosed {
		t.Fatalf("Status = %v, want RuntimeClosed", inspection.Status)
	}
}

type inspectionService struct{}

func (inspectionService) Commands() []gsr.CommandID     { return []gsr.CommandID{1} }
func (inspectionService) Init(gsr.ServiceContext) error { return nil }
func (inspectionService) Handle(gsr.CommandContext, gsr.Command) error {
	return nil
}
func (inspectionService) Stop(context.Context) error { return nil }
func (inspectionService) Close() error               { return nil }
