package gsr

import "sync"

type readyQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []ServiceRef
	closed bool
}

func newReadyQueue() *readyQueue { q := &readyQueue{}; q.cond = sync.NewCond(&q.mu); return q }
func (q *readyQueue) push(ref ServiceRef) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	q.items = append(q.items, ref)
	q.cond.Signal()
	return true
}
func (q *readyQueue) pop() (ServiceRef, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if q.closed {
		return ServiceRef{}, false
	}
	ref := q.items[0]
	copy(q.items, q.items[1:])
	q.items[len(q.items)-1] = ServiceRef{}
	q.items = q.items[:len(q.items)-1]
	return ref, true
}
func (q *readyQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.items = nil
	q.cond.Broadcast()
	q.mu.Unlock()
}
