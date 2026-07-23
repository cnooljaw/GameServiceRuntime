package servicegroup

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync/atomic"
	"unicode/utf8"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

// Hash selects one ServiceRef using FNV-1a 64 over the exact RoutingKey bytes.
type Hash struct{}

// Pick returns the stable modulo target for key in set.
func (Hash) Pick(set ServiceSet, key RoutingKey) ([]gsr.ServiceRef, error) {
	if err := validateRoutingSet(set); err != nil {
		return nil, err
	}
	if key == "" || !utf8.ValidString(string(key)) {
		return nil, ErrInvalidRoutingKey
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key))
	index := hash.Sum64() % uint64(len(set.Refs))
	return []gsr.ServiceRef{set.Refs[index]}, nil
}

// RoundRobin selects successive ServiceRefs with a counter scoped to this policy instance.
type RoundRobin struct {
	next atomic.Uint64
}

// Pick returns the next ServiceRef in stable ServiceSet order.
func (p *RoundRobin) Pick(set ServiceSet, _ RoutingKey) ([]gsr.ServiceRef, error) {
	if p == nil {
		return nil, ErrInvalidRoutingResult
	}
	if err := validateRoutingSet(set); err != nil {
		return nil, err
	}
	index := (p.next.Add(1) - 1) % uint64(len(set.Refs))
	return []gsr.ServiceRef{set.Refs[index]}, nil
}

// Broadcast selects every ServiceRef in stable ServiceSet order.
type Broadcast struct{}

// Pick returns an independent copy of every ServiceSet member.
func (Broadcast) Pick(set ServiceSet, _ RoutingKey) ([]gsr.ServiceRef, error) {
	if err := validateRoutingSet(set); err != nil {
		return nil, err
	}
	targets := make([]gsr.ServiceRef, len(set.Refs))
	copy(targets, set.Refs)
	return targets, nil
}

// DeliveryFailure records one failed target from a multi-target Send.
type DeliveryFailure struct {
	Target gsr.ServiceRef
	Err    error
}

// BroadcastError reports the stable ordered failures from a multi-target Send.
// Targets absent from Failures accepted the Command; their delivery is not rolled back.
type BroadcastError struct {
	Failures []DeliveryFailure
}

// Error implements error.
func (e *BroadcastError) Error() string {
	if e == nil {
		return "servicegroup: broadcast failed"
	}
	return fmt.Sprintf("servicegroup: broadcast failed for %d target(s)", len(e.Failures))
}

// Unwrap exposes all target errors to errors.Is and errors.As.
func (e *BroadcastError) Unwrap() []error {
	if e == nil {
		return nil
	}
	errors := make([]error, 0, len(e.Failures))
	for _, failure := range e.Failures {
		if failure.Err != nil {
			errors = append(errors, failure.Err)
		}
	}
	return errors
}

// NewRouter binds routing to a Runtime or ServiceContext dispatcher.
func NewRouter(dispatcher CommandDispatcher) (*Router, error) {
	if isNil(dispatcher) {
		return nil, ErrInvalidCaller
	}
	return &Router{dispatcher: dispatcher}, nil
}

// Send selects targets from set and attempts each target in policy order.
func (r *Router) Send(set ServiceSet, policy RoutingPolicy, key RoutingKey, command gsr.CommandID, payload any) error {
	targets, err := selectRoutingTargets(set, policy, key)
	if err != nil {
		return err
	}
	if len(targets) == 1 {
		return r.dispatcher.Send(targets[0], command, payload)
	}
	failures := make([]DeliveryFailure, 0)
	for _, target := range targets {
		if err := r.dispatcher.Send(target, command, payload); err != nil {
			failures = append(failures, DeliveryFailure{Target: target, Err: err})
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return &BroadcastError{Failures: failures}
}

// Call selects exactly one target from set and waits for its Reply.
func (r *Router) Call(ctx context.Context, set ServiceSet, policy RoutingPolicy, key RoutingKey, command gsr.CommandID, payload any) (any, error) {
	targets, err := selectRoutingTargets(set, policy, key)
	if err != nil {
		return nil, err
	}
	if len(targets) != 1 {
		return nil, ErrMultipleTargets
	}
	return r.dispatcher.Call(ctx, targets[0], command, payload)
}

func validateRoutingSet(set ServiceSet) error {
	if !validServiceSet(set) {
		return ErrInvalidServiceSet
	}
	if len(set.Refs) == 0 {
		return ErrNoRoute
	}
	return nil
}

func selectRoutingTargets(set ServiceSet, policy RoutingPolicy, key RoutingKey) ([]gsr.ServiceRef, error) {
	if err := validateRoutingSet(set); err != nil {
		return nil, err
	}
	if isNil(policy) {
		return nil, ErrInvalidRoutingResult
	}
	targets, err := policy.Pick(set, key)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, ErrInvalidRoutingResult
	}
	members := make(map[gsr.ServiceRef]struct{}, len(set.Refs))
	for _, ref := range set.Refs {
		members[ref] = struct{}{}
	}
	seen := make(map[gsr.ServiceRef]struct{}, len(targets))
	result := make([]gsr.ServiceRef, len(targets))
	for index, target := range targets {
		if !validServiceRef(target) {
			return nil, ErrInvalidRoutingResult
		}
		if _, exists := members[target]; !exists {
			return nil, ErrInvalidRoutingResult
		}
		if _, exists := seen[target]; exists {
			return nil, ErrInvalidRoutingResult
		}
		seen[target] = struct{}{}
		result[index] = target
	}
	return result, nil
}
