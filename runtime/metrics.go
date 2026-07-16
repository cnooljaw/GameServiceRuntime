package gsr

import (
	"strconv"
	"sync"
	"time"
)

// Metrics records Runtime and Service measurements.
type Metrics interface {
	Inc(string)
	Add(string, uint64)
	SetGauge(string, int64)
	Observe(string, time.Duration)
}

// MetricsSnapshot is an immutable copy of collected measurements.
type MetricsSnapshot struct {
	counters  map[string]uint64
	gauges    map[string]int64
	durations map[string]time.Duration
}

// Counter returns a counter value.
func (s MetricsSnapshot) Counter(name string) uint64 { return s.counters[name] }

// Gauge returns a gauge value.
func (s MetricsSnapshot) Gauge(name string) int64 { return s.gauges[name] }

// Duration returns the accumulated duration for a metric.
func (s MetricsSnapshot) Duration(name string) time.Duration { return s.durations[name] }

// MailboxDepth returns the queued item count for a Service.
func (s MetricsSnapshot) MailboxDepth(ref ServiceRef) int64 { return s.Gauge(mailboxDepthMetric(ref)) }

func mailboxDepthMetric(ref ServiceRef) string {
	return "mailbox_depth." + string(ref.Node) + "." + strconv.FormatUint(uint64(ref.ID), 10)
}

type metricCollector struct {
	mu        sync.RWMutex
	counters  map[string]uint64
	gauges    map[string]int64
	durations map[string]time.Duration
}

func newMetricCollector() *metricCollector {
	return &metricCollector{counters: make(map[string]uint64), gauges: make(map[string]int64), durations: make(map[string]time.Duration)}
}
func (m *metricCollector) Inc(name string) { m.Add(name, 1) }
func (m *metricCollector) Add(name string, delta uint64) {
	m.mu.Lock()
	m.counters[name] += delta
	m.mu.Unlock()
}
func (m *metricCollector) SetGauge(name string, value int64) {
	m.mu.Lock()
	m.gauges[name] = value
	m.mu.Unlock()
}
func (m *metricCollector) deleteGauge(name string) {
	m.mu.Lock()
	delete(m.gauges, name)
	m.mu.Unlock()
}
func (m *metricCollector) Observe(name string, value time.Duration) {
	m.mu.Lock()
	m.durations[name] += value
	m.mu.Unlock()
}
func (m *metricCollector) snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot := MetricsSnapshot{counters: make(map[string]uint64, len(m.counters)), gauges: make(map[string]int64, len(m.gauges)), durations: make(map[string]time.Duration, len(m.durations))}
	for key, value := range m.counters {
		snapshot.counters[key] = value
	}
	for key, value := range m.gauges {
		snapshot.gauges[key] = value
	}
	for key, value := range m.durations {
		snapshot.durations[key] = value
	}
	return snapshot
}
