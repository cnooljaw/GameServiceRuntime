package gsr

import "sync"

type scheduler struct {
	runtime  *Runtime
	ready    chan ServiceRef
	maxBatch int
	done     chan struct{}
	once     sync.Once
	workers  sync.WaitGroup
}

func newScheduler(runtime *Runtime, workers, maxBatch int) *scheduler {
	s := &scheduler{runtime: runtime, ready: make(chan ServiceRef, workers*4), maxBatch: maxBatch, done: make(chan struct{})}
	for i := 0; i < workers; i++ {
		s.workers.Add(1)
		go s.worker()
	}
	return s
}
func (s *scheduler) schedule(instance *serviceInstance) {
	if !instance.ready.CompareAndSwap(false, true) {
		return
	}
	select {
	case s.ready <- instance.ref:
	case <-s.done:
		instance.ready.Store(false)
	}
}
func (s *scheduler) worker() {
	defer s.workers.Done()
	for {
		select {
		case ref := <-s.ready:
			if instance, err := s.runtime.registry.get(ref); err == nil {
				s.process(instance)
			}
		case <-s.done:
			return
		}
	}
}
func (s *scheduler) process(instance *serviceInstance) {
	for n := 0; n < s.maxBatch; n++ {
		envelope, ok := instance.mailbox.pop()
		if !ok {
			break
		}
		instance.service.Handle(commandContext{self: instance.ref}, Command{ID: envelope.Command, Payload: envelope.Payload})
	}
	instance.ready.Store(false)
	if instance.mailbox.notEmpty() {
		s.schedule(instance)
	}
}
func (s *scheduler) close() { s.once.Do(func() { close(s.done); s.workers.Wait() }) }
