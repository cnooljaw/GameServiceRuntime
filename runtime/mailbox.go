package gsr

import (
	"sync"
	"time"
)

type mailboxItem struct {
	envelope   *Envelope
	stop       *stopRequest
	enqueuedAt time.Time
}

type mailbox struct {
	mu       sync.Mutex
	items    []mailboxItem
	capacity int
	metrics  *metricCollector
	now      func() time.Time
	depthKey string
	closed   bool
}

func newMailbox(size int, metrics *metricCollector, now func() time.Time, depthKey string) *mailbox {
	return &mailbox{capacity: size, metrics: metrics, now: now, depthKey: depthKey}
}

func (m *mailbox) pushEnvelope(envelope Envelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrServiceClosed
	}
	if len(m.items) >= m.capacity {
		m.metrics.Inc("mailbox_rejected_total")
		return ErrMailboxFull
	}
	m.items = append(m.items, mailboxItem{envelope: &envelope, enqueuedAt: m.now()})
	m.metrics.SetGauge(m.depthKey, int64(len(m.items)))
	return nil
}

func (m *mailbox) pushStop(request *stopRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrServiceClosed
	}
	m.items = append(m.items, mailboxItem{stop: request, enqueuedAt: m.now()})
	m.metrics.SetGauge(m.depthKey, int64(len(m.items)))
	return nil
}

func (m *mailbox) pop() (mailboxItem, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.items) == 0 {
		return mailboxItem{}, false
	}
	item := m.items[0]
	copy(m.items, m.items[1:])
	m.items[len(m.items)-1] = mailboxItem{}
	m.items = m.items[:len(m.items)-1]
	m.metrics.SetGauge(m.depthKey, int64(len(m.items)))
	m.metrics.Observe("mailbox_wait_duration", m.now().Sub(item.enqueuedAt))
	return item, true
}

func (m *mailbox) notEmpty() bool { m.mu.Lock(); defer m.mu.Unlock(); return len(m.items) > 0 }
func (m *mailbox) discard() {
	m.mu.Lock()
	m.items = nil
	m.metrics.SetGauge(m.depthKey, 0)
	m.mu.Unlock()
}
func (m *mailbox) close() {
	m.mu.Lock()
	m.closed = true
	m.items = nil
	m.mu.Unlock()
	m.metrics.deleteGauge(m.depthKey)
}
