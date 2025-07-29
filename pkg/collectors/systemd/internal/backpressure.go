package internal

import (
	"sync"
	"time"
)

// BackpressureController manages load shedding based on system load
type BackpressureController struct {
	mu               sync.Mutex
	currentLoad      float64
	maxQueueSize     int
	currentQueueSize int
	droppedEvents    uint64
	processedEvents  uint64
	lastLoadCheck    time.Time
	loadThresholds   LoadThresholds
}

// LoadThresholds defines thresholds for different load levels
type LoadThresholds struct {
	Normal   float64 // 0-60% - accept all events
	High     float64 // 60-80% - drop low priority events
	Critical float64 // 80-100% - drop all but critical events
}

// EventPriority defines event priority levels
type EventPriority int

const (
	PriorityLow EventPriority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

// NewBackpressureController creates a new backpressure controller
func NewBackpressureController(maxQueueSize int) *BackpressureController {
	return &BackpressureController{
		maxQueueSize: maxQueueSize,
		loadThresholds: LoadThresholds{
			Normal:   0.6,
			High:     0.8,
			Critical: 0.95,
		},
		lastLoadCheck: time.Now(),
	}
}

// ShouldAccept determines if an event should be accepted based on load
func (b *BackpressureController) ShouldAccept(priority EventPriority) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Update load metrics
	b.currentLoad = float64(b.currentQueueSize) / float64(b.maxQueueSize)

	// Make decision based on load
	switch {
	case b.currentLoad >= b.loadThresholds.Critical:
		// Only accept critical events
		if priority < PriorityCritical {
			b.droppedEvents++
			return false
		}
	case b.currentLoad >= b.loadThresholds.High:
		// Drop low priority events
		if priority < PriorityNormal {
			b.droppedEvents++
			return false
		}
	case b.currentLoad >= b.loadThresholds.Normal:
		// Drop only low priority events
		if priority < PriorityLow {
			b.droppedEvents++
			return false
		}
	}

	b.processedEvents++
	return true
}

// UpdateQueueSize updates the current queue size
func (b *BackpressureController) UpdateQueueSize(size int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentQueueSize = size
}

// GetLoadLevel returns the current load level as a string
func (b *BackpressureController) GetLoadLevel() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch {
	case b.currentLoad >= b.loadThresholds.Critical:
		return "critical"
	case b.currentLoad >= b.loadThresholds.High:
		return "high"
	case b.currentLoad >= b.loadThresholds.Normal:
		return "normal"
	default:
		return "low"
	}
}

// Metrics returns backpressure metrics
func (b *BackpressureController) Metrics() map[string]interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()

	return map[string]interface{}{
		"current_load":     b.currentLoad,
		"load_level":       b.GetLoadLevel(),
		"queue_size":       b.currentQueueSize,
		"max_queue_size":   b.maxQueueSize,
		"dropped_events":   b.droppedEvents,
		"processed_events": b.processedEvents,
		"drop_rate":        float64(b.droppedEvents) / float64(b.processedEvents+b.droppedEvents+1),
	}
}
