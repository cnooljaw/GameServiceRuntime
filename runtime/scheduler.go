package gsr

import (
	"context"
	"sync"
)

type scheduler struct {
	runtime   *Runtime
	queue     *readyQueue
	permits   chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	dispatch  sync.WaitGroup
	tasks     sync.WaitGroup
	maxBatch  int
}

func newScheduler(runtime *Runtime, workers, maxBatch int) *scheduler {
	s := &scheduler{runtime: runtime, queue: newReadyQueue(), permits: make(chan struct{}, workers), done: make(chan struct{}), maxBatch: maxBatch}
	s.dispatch.Add(1)
	go s.dispatchLoop()
	return s
}

func (s *scheduler) schedule(instance *serviceInstance) {
	if !instance.ready.CompareAndSwap(false, true) {
		return
	}
	if !s.queue.push(instance.ref) {
		instance.ready.Store(false)
	}
}

func (s *scheduler) dispatchLoop() {
	defer s.dispatch.Done()
	for {
		ref, ok := s.queue.pop()
		if !ok {
			return
		}
		if !s.acquire() {
			return
		}
		instance, err := s.runtime.registry.get(ref)
		if err != nil {
			s.release()
			continue
		}
		instance.permitHeld.Store(true)
		s.tasks.Add(1)
		task := s.runtime.tasks.begin(instance.ref, runtimeTaskDispatch, nil)
		go func() {
			defer s.tasks.Done()
			defer s.runtime.tasks.finish(task)
			s.process(instance)
		}()
	}
}

func (s *scheduler) process(instance *serviceInstance) {
	defer func() {
		if instance.permitHeld.CompareAndSwap(true, false) {
			s.release()
		}
	}()
	for n := 0; n < s.maxBatch; n++ {
		item, ok := instance.mailbox.pop()
		if !ok {
			break
		}
		if item.stop != nil {
			s.runtime.executeStop(instance, item.stop)
			instance.ready.Store(false)
			return
		}
		s.runtime.executeEnvelope(instance, *item.envelope)
		if instance.finalized.Load() {
			instance.ready.Store(false)
			return
		}
	}
	instance.ready.Store(false)
	if instance.mailbox.notEmpty() && !instance.finalized.Load() {
		s.schedule(instance)
	}
}

func (s *scheduler) yield(instance *serviceInstance) bool {
	if !instance.permitHeld.CompareAndSwap(true, false) {
		return false
	}
	s.release()
	return true
}

func (s *scheduler) resume(instance *serviceInstance) error {
	if !s.acquire() {
		return ErrRuntimeClosed
	}
	instance.permitHeld.Store(true)
	return nil
}

func (s *scheduler) acquire() bool {
	select {
	case s.permits <- struct{}{}:
		return true
	case <-s.done:
		return false
	}
}
func (s *scheduler) release() {
	select {
	case <-s.permits:
	default:
	}
}

func (s *scheduler) close(ctx context.Context) error {
	s.closeOnce.Do(func() { close(s.done); s.queue.close() })
	s.dispatch.Wait()
	finished := make(chan struct{})
	go func() { s.tasks.Wait(); close(finished) }()
	select {
	case <-finished:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}
