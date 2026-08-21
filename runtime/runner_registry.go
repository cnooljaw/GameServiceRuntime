package gsr

import (
	"context"
	"errors"
	"sort"
	"sync"
)

type runtimeRunner interface {
	Close(context.Context) error
	inspection() RunnerInspection
}

type runnerRegistry struct {
	mu      sync.RWMutex
	runners map[RunnerName]runtimeRunner
}

func newRunnerRegistry() *runnerRegistry {
	return &runnerRegistry{runners: make(map[RunnerName]runtimeRunner)}
}

func (r *runnerRegistry) add(name RunnerName, runner runtimeRunner) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runners[name]; exists {
		return ErrRunnerNameConflict
	}
	r.runners[name] = runner
	return nil
}

func (r *runnerRegistry) snapshot() []runtimeRunner {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]RunnerName, 0, len(r.runners))
	for name := range r.runners {
		names = append(names, name)
	}
	sort.Slice(names, func(left, right int) bool { return names[left] < names[right] })
	runners := make([]runtimeRunner, 0, len(names))
	for _, name := range names {
		runners = append(runners, r.runners[name])
	}
	return runners
}

func (r *runnerRegistry) inspections() []RunnerInspection {
	runners := r.snapshot()
	inspections := make([]RunnerInspection, 0, len(runners))
	for _, runner := range runners {
		inspections = append(inspections, runner.inspection())
	}
	return inspections
}

func (r *runnerRegistry) closeAll(ctx context.Context) error {
	var result error
	for _, runner := range r.snapshot() {
		if err := runner.Close(ctx); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}
