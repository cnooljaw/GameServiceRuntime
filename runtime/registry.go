package gsr

import (
	"sync"
	"sync/atomic"
)

type serviceInstance struct {
	ref     ServiceRef
	service Service
	mailbox *mailbox
	policy  ServicePolicy
	status  atomic.Int32
	ready   atomic.Bool
}

func (i *serviceInstance) setStatus(status ServiceStatus) { i.status.Store(int32(status)) }

type localRegistry struct {
	mu       sync.RWMutex
	services map[ServiceRef]*serviceInstance
	closed   map[ServiceRef]struct{}
}

func newLocalRegistry() *localRegistry {
	return &localRegistry{services: make(map[ServiceRef]*serviceInstance), closed: make(map[ServiceRef]struct{})}
}
func (r *localRegistry) add(instance *serviceInstance) {
	r.mu.Lock()
	r.services[instance.ref] = instance
	r.mu.Unlock()
}
func (r *localRegistry) get(ref ServiceRef) (*serviceInstance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if instance := r.services[ref]; instance != nil {
		return instance, nil
	}
	if _, ok := r.closed[ref]; ok {
		return nil, ErrServiceClosed
	}
	return nil, ErrServiceNotFound
}
func (r *localRegistry) remove(ref ServiceRef) {
	r.mu.Lock()
	delete(r.services, ref)
	r.closed[ref] = struct{}{}
	r.mu.Unlock()
}
