package gsr

import (
	"sync"
	"sync/atomic"
	"time"
)

type timerEntry struct {
	target ServiceRef
	timer  *time.Timer
}
type timerManager struct {
	next   atomic.Uint64
	mu     sync.Mutex
	timers map[TimerID]timerEntry
}

func newTimerManager() *timerManager { return &timerManager{timers: make(map[TimerID]timerEntry)} }
func (m *timerManager) add(target ServiceRef, delay time.Duration, fire func()) TimerID {
	id := TimerID(m.next.Add(1))
	m.mu.Lock()
	timer := time.AfterFunc(delay, func() {
		m.mu.Lock()
		_, ok := m.timers[id]
		delete(m.timers, id)
		m.mu.Unlock()
		if ok {
			fire()
		}
	})
	m.timers[id] = timerEntry{target: target, timer: timer}
	m.mu.Unlock()
	return id
}
func (m *timerManager) cancel(id TimerID) {
	m.mu.Lock()
	entry, ok := m.timers[id]
	if ok {
		delete(m.timers, id)
	}
	m.mu.Unlock()
	if ok {
		entry.timer.Stop()
	}
}
func (m *timerManager) cancelTarget(target ServiceRef) {
	m.mu.Lock()
	entries := make([]timerEntry, 0)
	for id, entry := range m.timers {
		if entry.target == target {
			delete(m.timers, id)
			entries = append(entries, entry)
		}
	}
	m.mu.Unlock()
	for _, entry := range entries {
		entry.timer.Stop()
	}
}

func (m *timerManager) cancelAll() {
	m.mu.Lock()
	entries := make([]timerEntry, 0, len(m.timers))
	for id, entry := range m.timers {
		delete(m.timers, id)
		entries = append(entries, entry)
	}
	m.mu.Unlock()
	for _, entry := range entries {
		entry.timer.Stop()
	}
}
