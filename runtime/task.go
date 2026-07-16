package gsr

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type runtimeTaskID uint64
type runtimeTaskKind string

const (
	runtimeTaskInit     runtimeTaskKind = "init"
	runtimeTaskDispatch runtimeTaskKind = "dispatch"
	runtimeTaskStop     runtimeTaskKind = "stop"
	runtimeTaskClose    runtimeTaskKind = "close"
)

type runtimeTaskHandle struct {
	id   runtimeTaskID
	done <-chan struct{}
}

type runtimeTask struct {
	id       runtimeTaskID
	owner    ServiceRef
	kind     runtimeTaskKind
	started  time.Time
	cancel   context.CancelFunc
	done     chan struct{}
	timedOut bool
}

type runtimeTaskSnapshot struct {
	id       runtimeTaskID
	owner    ServiceRef
	kind     runtimeTaskKind
	started  time.Time
	timedOut bool
}

type taskTracker struct {
	next    atomic.Uint64
	mu      sync.Mutex
	tasks   map[runtimeTaskID]*runtimeTask
	metrics *metricCollector
	now     func() time.Time
}

func newTaskTracker(metrics *metricCollector, now func() time.Time) *taskTracker {
	return &taskTracker{tasks: make(map[runtimeTaskID]*runtimeTask), metrics: metrics, now: now}
}

func (t *taskTracker) begin(owner ServiceRef, kind runtimeTaskKind, cancel context.CancelFunc) runtimeTaskHandle {
	id := runtimeTaskID(t.next.Add(1))
	done := make(chan struct{})
	t.mu.Lock()
	t.tasks[id] = &runtimeTask{id: id, owner: owner, kind: kind, started: t.now(), cancel: cancel, done: done}
	t.metrics.SetGauge("runtime_tasks_active", int64(len(t.tasks)))
	t.mu.Unlock()
	return runtimeTaskHandle{id: id, done: done}
}

func (t *taskTracker) finish(handle runtimeTaskHandle) {
	t.mu.Lock()
	task := t.tasks[handle.id]
	if task != nil {
		delete(t.tasks, handle.id)
		close(task.done)
		t.metrics.SetGauge("runtime_tasks_active", int64(len(t.tasks)))
	}
	t.mu.Unlock()
}

func (t *taskTracker) timeout(handle runtimeTaskHandle) {
	t.mu.Lock()
	task := t.tasks[handle.id]
	cancel := t.markTimedOutLocked(task)
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (t *taskTracker) timeoutOwner(owner ServiceRef) {
	t.mu.Lock()
	cancels := make([]context.CancelFunc, 0)
	for _, task := range t.tasks {
		if task.owner == owner {
			if cancel := t.markTimedOutLocked(task); cancel != nil {
				cancels = append(cancels, cancel)
			}
		}
	}
	t.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (t *taskTracker) markTimedOutLocked(task *runtimeTask) context.CancelFunc {
	if task == nil || task.timedOut {
		return nil
	}
	task.timedOut = true
	t.metrics.Inc("runtime_task_timeouts_total")
	return task.cancel
}

func (t *taskTracker) active() []runtimeTaskSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	tasks := make([]runtimeTaskSnapshot, 0, len(t.tasks))
	for _, task := range t.tasks {
		tasks = append(tasks, runtimeTaskSnapshot{
			id:       task.id,
			owner:    task.owner,
			kind:     task.kind,
			started:  task.started,
			timedOut: task.timedOut,
		})
	}
	return tasks
}

func (r *Runtime) invokeTask(owner ServiceRef, kind runtimeTaskKind, cancel context.CancelFunc, fn func() error) (runtimeTaskHandle, <-chan error) {
	handle := r.tasks.begin(owner, kind, cancel)
	result := make(chan error, 1)
	go func() {
		err := invokeService(fn)
		r.tasks.finish(handle)
		result <- err
	}()
	return handle, result
}

func (r *Runtime) invokeInlineTask(owner ServiceRef, kind runtimeTaskKind, fn func() error) error {
	handle := r.tasks.begin(owner, kind, nil)
	defer r.tasks.finish(handle)
	return invokeService(fn)
}

func (r *Runtime) reportActiveTasks() {
	tasks := r.tasks.active()
	if len(tasks) == 0 {
		return
	}
	r.metrics.Add("runtime_tasks_abandoned_total", uint64(len(tasks)))
	for _, task := range tasks {
		r.logger.Error("runtime task still active",
			"task", task.id,
			"service", task.owner,
			"kind", task.kind,
			"duration", r.now().Sub(task.started),
			"timed_out", task.timedOut,
		)
	}
}
