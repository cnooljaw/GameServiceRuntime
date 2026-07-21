package discovery

import (
	"context"
	"strconv"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func BenchmarkResolveName(b *testing.B) {
	runtime := gsr.NewRuntime(gsr.Config{NodeID: "discovery-node", Workers: 2})
	b.Cleanup(func() { _ = runtime.Close(context.Background()) })
	service, err := NewService(Config{LeaseTTL: time.Hour, SweepInterval: time.Hour})
	if err != nil {
		b.Fatal(err)
	}
	ref, err := runtime.CreateService(gsr.ServiceSpec{Name: DefaultServiceName, Service: service})
	if err != nil {
		b.Fatal(err)
	}
	client, err := NewClient(runtime, ref)
	if err != nil {
		b.Fatal(err)
	}
	lease, err := client.RegisterNode(context.Background(), "node-a", "node-a:9000")
	if err != nil {
		b.Fatal(err)
	}
	if err := client.RegisterName(context.Background(), lease, ".config", gsr.ServiceRef{Node: "node-a", ID: 10}); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := client.ResolveName(context.Background(), ".config"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPruneExpiredTenThousandNames(b *testing.B) {
	now := time.Date(2026, 7, 21, 16, 0, 0, 0, time.UTC)
	owner := leaseKey{node: "node-a", generation: 1}
	b.ReportAllocs()
	for iteration := 0; iteration < b.N; iteration++ {
		b.StopTimer()
		service := &service{
			nodes:        map[gsr.NodeID]NodeRecord{"node-a": {ID: "node-a", Generation: 1, ExpiresAt: now.Add(-time.Second)}},
			names:        make(map[gsr.ServiceName]nameBinding, 10_000),
			namesByLease: map[leaseKey]map[gsr.ServiceName]struct{}{owner: make(map[gsr.ServiceName]struct{}, 10_000)},
		}
		for index := range 10_000 {
			name := gsr.ServiceName(".service-" + strconv.Itoa(index))
			service.names[name] = nameBinding{ref: gsr.ServiceRef{Node: "node-a", ID: gsr.ServiceID(index + 1)}, owner: owner}
			service.namesByLease[owner][name] = struct{}{}
		}
		b.StartTimer()
		service.pruneExpired(now)
		b.StopTimer()
		if len(service.nodes) != 0 || len(service.names) != 0 || len(service.namesByLease) != 0 {
			b.Fatal("expired node state was not fully removed")
		}
		b.StartTimer()
	}
}
