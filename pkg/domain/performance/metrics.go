package performance

import (
	"sync/atomic"
	"time"
)

// Metrics tracks performance metrics for collectors
type Metrics struct {
	// Event processing
	EventsProcessed atomic.Uint64
	EventsDropped   atomic.Uint64
	EventsQueued    atomic.Uint64

	// Batch processing
	BatchesProcessed atomic.Uint64
	BatchSize        atomic.Uint64

	// Buffer metrics
	BufferSize        atomic.Uint64
	BufferCapacity    uint64
	BufferUtilization atomic.Uint64

	// Pool metrics
	PoolAllocated atomic.Uint64
	PoolRecycled  atomic.Uint64
	PoolInUse     atomic.Uint64

	// Timing metrics
	ProcessingTime atomic.Uint64 // nanoseconds
	QueueTime      atomic.Uint64 // nanoseconds

	// Rates (calculated)
	EventsPerSecond atomic.Uint64
	BytesPerSecond  atomic.Uint64

	// Internal
	lastUpdate      time.Time
	lastEventsCount uint64
}

// NewMetrics creates a new metrics instance
func NewMetrics() *Metrics {
	return &Metrics{
		lastUpdate: time.Now(),
	}
}

// RecordEvent records a processed event
func (m *Metrics) RecordEvent() {
	m.EventsProcessed.Add(1)
}

// RecordDrop records a dropped event
func (m *Metrics) RecordDrop() {
	m.EventsDropped.Add(1)
}

// RecordBatch records a processed batch
func (m *Metrics) RecordBatch(size int) {
	m.BatchesProcessed.Add(1)
	m.BatchSize.Store(uint64(size))
}

// UpdateBufferStats updates buffer statistics
func (m *Metrics) UpdateBufferStats(size, capacity uint64) {
	m.BufferSize.Store(size)
	m.BufferCapacity = capacity
	if capacity > 0 {
		utilization := (size * 100) / capacity
		m.BufferUtilization.Store(utilization)
	}
}

// UpdatePoolStats updates pool statistics
func (m *Metrics) UpdatePoolStats(allocated, recycled, inUse int64) {
	m.PoolAllocated.Store(uint64(allocated))
	m.PoolRecycled.Store(uint64(recycled))
	m.PoolInUse.Store(uint64(inUse))
}

// RecordProcessingTime records event processing time
func (m *Metrics) RecordProcessingTime(start time.Time) {
	duration := time.Since(start).Nanoseconds()
	m.ProcessingTime.Store(uint64(duration))
}

// UpdateRates calculates and updates rate metrics
func (m *Metrics) UpdateRates() {
	now := time.Now()
	elapsed := now.Sub(m.lastUpdate).Seconds()

	if elapsed > 0 {
		currentEvents := m.EventsProcessed.Load()
		eventsDiff := currentEvents - m.lastEventsCount

		eventsPerSec := uint64(float64(eventsDiff) / elapsed)
		m.EventsPerSecond.Store(eventsPerSec)

		m.lastUpdate = now
		m.lastEventsCount = currentEvents
	}
}

// GetSnapshot returns a snapshot of all metrics
func (m *Metrics) GetSnapshot() MetricsSnapshot {
	m.UpdateRates()

	return MetricsSnapshot{
		EventsProcessed:   m.EventsProcessed.Load(),
		EventsDropped:     m.EventsDropped.Load(),
		EventsQueued:      m.EventsQueued.Load(),
		BatchesProcessed:  m.BatchesProcessed.Load(),
		BatchSize:         m.BatchSize.Load(),
		BufferSize:        m.BufferSize.Load(),
		BufferCapacity:    m.BufferCapacity,
		BufferUtilization: m.BufferUtilization.Load(),
		PoolAllocated:     m.PoolAllocated.Load(),
		PoolRecycled:      m.PoolRecycled.Load(),
		PoolInUse:         m.PoolInUse.Load(),
		ProcessingTime:    m.ProcessingTime.Load(),
		QueueTime:         m.QueueTime.Load(),
		EventsPerSecond:   m.EventsPerSecond.Load(),
		BytesPerSecond:    m.BytesPerSecond.Load(),
		Timestamp:         time.Now(),
	}
}

// MetricsSnapshot is a point-in-time snapshot of metrics
type MetricsSnapshot struct {
	EventsProcessed   uint64
	EventsDropped     uint64
	EventsQueued      uint64
	BatchesProcessed  uint64
	BatchSize         uint64
	BufferSize        uint64
	BufferCapacity    uint64
	BufferUtilization uint64
	PoolAllocated     uint64
	PoolRecycled      uint64
	PoolInUse         uint64
	ProcessingTime    uint64
	QueueTime         uint64
	EventsPerSecond   uint64
	BytesPerSecond    uint64
	Timestamp         time.Time
}
