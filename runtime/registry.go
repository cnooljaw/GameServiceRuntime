package gsr

import (
	"sync"
	"time"
)

type localRegistry struct {
	mu             sync.Mutex
	services       map[ServiceRef]*serviceInstance
	names          map[ServiceName]ServiceRef
	tombstones     map[ServiceRef]time.Time
	tombstoneTTL   time.Duration
	tombstoneLimit int
	now            func() time.Time
}

func newLocalRegistry(ttl time.Duration, limit int, now func() time.Time) *localRegistry {
	return &localRegistry{services: make(map[ServiceRef]*serviceInstance), names: make(map[ServiceName]ServiceRef), tombstones: make(map[ServiceRef]time.Time), tombstoneTTL: ttl, tombstoneLimit: limit, now: now}
}

func (r *localRegistry) add(instance *serviceInstance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupLocked()
	if instance.name != "" {
		if _, exists := r.names[instance.name]; exists {
			return ErrServiceNameConflict
		}
		r.names[instance.name] = instance.ref
	}
	r.services[instance.ref] = instance
	return nil
}

func (r *localRegistry) get(ref ServiceRef) (*serviceInstance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupLocked()
	if instance := r.services[ref]; instance != nil {
		return instance, nil
	}
	if _, exists := r.tombstones[ref]; exists {
		return nil, ErrServiceClosed
	}
	return nil, ErrServiceNotFound
}

func (r *localRegistry) resolve(name ServiceName) (ServiceRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ref, exists := r.names[name]
	if !exists {
		return ServiceRef{}, ErrServiceNotFound
	}
	return ref, nil
}

func (r *localRegistry) remove(ref ServiceRef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	instance := r.services[ref]
	delete(r.services, ref)
	if instance != nil && instance.name != "" {
		delete(r.names, instance.name)
	}
	r.tombstones[ref] = r.now().Add(r.tombstoneTTL)
	r.cleanupLocked()
}

func (r *localRegistry) snapshot() []*serviceInstance {
	r.mu.Lock()
	defer r.mu.Unlock()
	instances := make([]*serviceInstance, 0, len(r.services))
	for _, instance := range r.services {
		instances = append(instances, instance)
	}
	return instances
}

func (r *localRegistry) clear() {
	r.mu.Lock()
	r.services = make(map[ServiceRef]*serviceInstance)
	r.names = make(map[ServiceName]ServiceRef)
	r.tombstones = make(map[ServiceRef]time.Time)
	r.mu.Unlock()
}

func (r *localRegistry) cleanupLocked() {
	now := r.now()
	for ref, expires := range r.tombstones {
		if !expires.After(now) {
			delete(r.tombstones, ref)
		}
	}
	for len(r.tombstones) > r.tombstoneLimit {
		for ref := range r.tombstones {
			delete(r.tombstones, ref)
			break
		}
	}
}
