package servicegroup

import (
	"context"
	"errors"
	"hash/fnv"
	"reflect"
	"sync"
	"testing"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestHashUsesFNV1aAndStableServiceSetOrder(t *testing.T) {
	set := routingTestSet()
	policy := Hash{}
	for _, key := range []RoutingKey{"player-1", "player-42", "房间-7"} {
		targets, err := policy.Pick(set, key)
		if err != nil {
			t.Fatalf("Hash.Pick(%q) error = %v", key, err)
		}
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(key))
		want := set.Refs[hash.Sum64()%uint64(len(set.Refs))]
		if len(targets) != 1 || targets[0] != want {
			t.Fatalf("Hash.Pick(%q) = %#v, want %#v", key, targets, want)
		}
	}
	if _, err := policy.Pick(set, ""); !errors.Is(err, ErrInvalidRoutingKey) {
		t.Fatalf("Hash.Pick(empty key) error = %v, want ErrInvalidRoutingKey", err)
	}
	empty := set
	empty.Refs = make([]gsr.ServiceRef, 0)
	if _, err := policy.Pick(empty, "player-1"); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("Hash.Pick(empty set) error = %v, want ErrNoRoute", err)
	}
}

func TestRoundRobinRotatesPerPolicyInstanceAndIsConcurrentSafe(t *testing.T) {
	set := routingTestSet()
	policy := &RoundRobin{}
	for index := 0; index < len(set.Refs)*2; index++ {
		targets, err := policy.Pick(set, "")
		if err != nil {
			t.Fatalf("RoundRobin.Pick() error = %v", err)
		}
		want := set.Refs[index%len(set.Refs)]
		if len(targets) != 1 || targets[0] != want {
			t.Fatalf("RoundRobin.Pick(%d) = %#v, want %#v", index, targets, want)
		}
	}
	fresh, err := (&RoundRobin{}).Pick(set, "")
	if err != nil {
		t.Fatalf("fresh RoundRobin.Pick() error = %v", err)
	}
	if fresh[0] != set.Refs[0] {
		t.Fatalf("fresh RoundRobin.Pick() = %#v, want first ref", fresh)
	}

	concurrent := &RoundRobin{}
	const calls = 600
	results := make(chan gsr.ServiceRef, calls)
	var wait sync.WaitGroup
	for index := 0; index < calls; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			targets, pickErr := concurrent.Pick(set, "")
			if pickErr != nil {
				t.Errorf("concurrent RoundRobin.Pick() error = %v", pickErr)
				return
			}
			results <- targets[0]
		}()
	}
	wait.Wait()
	close(results)
	counts := make(map[gsr.ServiceRef]int)
	for target := range results {
		counts[target]++
	}
	for _, ref := range set.Refs {
		if counts[ref] != calls/len(set.Refs) {
			t.Fatalf("RoundRobin count[%#v] = %d, want %d", ref, counts[ref], calls/len(set.Refs))
		}
	}
}

func TestBroadcastReturnsStableIndependentTargets(t *testing.T) {
	set := routingTestSet()
	targets, err := (Broadcast{}).Pick(set, "")
	if err != nil {
		t.Fatalf("Broadcast.Pick() error = %v", err)
	}
	if !reflect.DeepEqual(targets, set.Refs) {
		t.Fatalf("Broadcast.Pick() = %#v, want %#v", targets, set.Refs)
	}
	targets[0] = gsr.ServiceRef{Node: "mutated", ID: 99}
	if set.Refs[0].Node == "mutated" {
		t.Fatal("Broadcast.Pick() returned ServiceSet backing storage")
	}
}

func TestRouterSendsHashAndCallsExactlyOneTarget(t *testing.T) {
	dispatcher := &recordingDispatcher{callResult: "reply"}
	router, err := NewRouter(dispatcher)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	set := routingTestSet()
	policy := Hash{}
	targets, err := policy.Pick(set, "player-42")
	if err != nil {
		t.Fatal(err)
	}
	payload := &struct{ Value string }{Value: "payload"}
	if err := router.Send(set, policy, "player-42", 10, payload); err != nil {
		t.Fatalf("Router.Send() error = %v", err)
	}
	if len(dispatcher.sends) != 1 || dispatcher.sends[0].target != targets[0] || dispatcher.sends[0].payload != payload {
		t.Fatalf("Send records = %#v, want target %#v payload identity", dispatcher.sends, targets[0])
	}

	result, err := router.Call(context.Background(), set, policy, "player-42", 11, payload)
	if err != nil {
		t.Fatalf("Router.Call() error = %v", err)
	}
	if result != "reply" || len(dispatcher.calls) != 1 || dispatcher.calls[0].target != targets[0] {
		t.Fatalf("Call result=%#v records=%#v", result, dispatcher.calls)
	}
}

func TestRouterBroadcastAttemptsAllTargetsAndAggregatesFailures(t *testing.T) {
	firstError := errors.New("first failed")
	lastError := errors.New("last failed")
	set := routingTestSet()
	dispatcher := &recordingDispatcher{sendErrors: map[gsr.ServiceRef]error{
		set.Refs[0]: firstError,
		set.Refs[2]: lastError,
	}}
	router, err := NewRouter(dispatcher)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	err = router.Send(set, Broadcast{}, "", 10, "payload")
	var broadcastError *BroadcastError
	if !errors.As(err, &broadcastError) {
		t.Fatalf("Router.Send(Broadcast) error = %v, want BroadcastError", err)
	}
	if !errors.Is(err, firstError) || !errors.Is(err, lastError) {
		t.Fatalf("BroadcastError does not unwrap target errors: %v", err)
	}
	if len(dispatcher.sends) != len(set.Refs) {
		t.Fatalf("Send attempts = %d, want %d", len(dispatcher.sends), len(set.Refs))
	}
	for index, record := range dispatcher.sends {
		if record.target != set.Refs[index] {
			t.Fatalf("Send target[%d] = %#v, want %#v", index, record.target, set.Refs[index])
		}
	}
	wantFailures := []DeliveryFailure{
		{Target: set.Refs[0], Err: firstError},
		{Target: set.Refs[2], Err: lastError},
	}
	if !reflect.DeepEqual(broadcastError.Failures, wantFailures) {
		t.Fatalf("BroadcastError.Failures = %#v, want %#v", broadcastError.Failures, wantFailures)
	}
	if _, err := router.Call(context.Background(), set, Broadcast{}, "", 11, nil); !errors.Is(err, ErrMultipleTargets) {
		t.Fatalf("Router.Call(Broadcast) error = %v, want ErrMultipleTargets", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("Call attempts = %d, want 0", len(dispatcher.calls))
	}
}

func TestRouterRejectsInvalidPolicyResultsBeforeDispatch(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	router, err := NewRouter(dispatcher)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	set := routingTestSet()
	cases := []struct {
		name    string
		targets []gsr.ServiceRef
	}{
		{name: "empty"},
		{name: "duplicate", targets: []gsr.ServiceRef{set.Refs[0], set.Refs[0]}},
		{name: "outside", targets: []gsr.ServiceRef{{Node: "node-z", ID: 99}}},
		{name: "invalid", targets: []gsr.ServiceRef{{Node: "node-a"}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := router.Send(set, fixedPolicy{targets: test.targets}, "", 10, nil)
			if !errors.Is(err, ErrInvalidRoutingResult) {
				t.Fatalf("Router.Send() error = %v, want ErrInvalidRoutingResult", err)
			}
		})
	}
	if len(dispatcher.sends) != 0 {
		t.Fatalf("Send attempts = %d, want 0", len(dispatcher.sends))
	}

	invalidSet := set
	invalidSet.Refs = append(invalidSet.Refs, invalidSet.Refs[0])
	if err := router.Send(invalidSet, Broadcast{}, "", 10, nil); !errors.Is(err, ErrInvalidServiceSet) {
		t.Fatalf("Router.Send(invalid set) error = %v, want ErrInvalidServiceSet", err)
	}
	if _, err := NewRouter((*nilDispatcher)(nil)); !errors.Is(err, ErrInvalidCaller) {
		t.Fatalf("NewRouter(nil) error = %v, want ErrInvalidCaller", err)
	}
	var nilPolicy *RoundRobin
	if err := router.Send(set, nilPolicy, "", 10, nil); !errors.Is(err, ErrInvalidRoutingResult) {
		t.Fatalf("Router.Send(nil policy) error = %v, want ErrInvalidRoutingResult", err)
	}
	policyError := errors.New("policy failed")
	if err := router.Send(set, fixedPolicy{err: policyError}, "", 10, nil); !errors.Is(err, policyError) {
		t.Fatalf("Router.Send(policy error) error = %v, want original error", err)
	}
}

func routingTestSet() ServiceSet {
	return ServiceSet{
		Name:    "match",
		Version: ServiceSetVersion{AuthorityEpoch: 1, Revision: 1},
		Refs: []gsr.ServiceRef{
			{Node: "node-a", ID: 1},
			{Node: "node-a", ID: 2},
			{Node: "node-b", ID: 1},
		},
		Tags: map[string]string{"version": "blue"},
	}
}

type fixedPolicy struct {
	targets []gsr.ServiceRef
	err     error
}

func (p fixedPolicy) Pick(ServiceSet, RoutingKey) ([]gsr.ServiceRef, error) {
	return append([]gsr.ServiceRef(nil), p.targets...), p.err
}

type dispatchRecord struct {
	target  gsr.ServiceRef
	command gsr.CommandID
	payload any
}

type recordingDispatcher struct {
	sends      []dispatchRecord
	calls      []dispatchRecord
	sendErrors map[gsr.ServiceRef]error
	callResult any
	callError  error
}

func (d *recordingDispatcher) Send(target gsr.ServiceRef, command gsr.CommandID, payload any) error {
	d.sends = append(d.sends, dispatchRecord{target: target, command: command, payload: payload})
	return d.sendErrors[target]
}

func (d *recordingDispatcher) Call(_ context.Context, target gsr.ServiceRef, command gsr.CommandID, payload any) (any, error) {
	d.calls = append(d.calls, dispatchRecord{target: target, command: command, payload: payload})
	return d.callResult, d.callError
}

type nilDispatcher struct{}

func (*nilDispatcher) Send(gsr.ServiceRef, gsr.CommandID, any) error { return nil }
func (*nilDispatcher) Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) {
	return nil, nil
}
