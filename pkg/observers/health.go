package observers

import (
	"sync/atomic"
	"time"
)

// HealthStatus provides structured health information for observers
type HealthStatus struct {
	// Healthy indicates if the observer is functioning properly
	Healthy bool `json:"healthy"`

	// LastEventTime when the observer last processed an event
	LastEventTime time.Time `json:"last_event_time"`

	// EventsObserved total number of events successfully processed
	EventsObserved uint64 `json:"events_observed"`

	// EventsDropped total number of events dropped due to buffer overflow
	EventsDropped uint64 `json:"events_dropped"`

	// ErrorCount total number of errors encountered
	ErrorCount uint64 `json:"error_count"`

	// StartTime when the observer was started
	StartTime time.Time `json:"start_time"`

	// ComponentHealth provides health status for individual components
	ComponentHealth map[string]bool `json:"component_health,omitempty"`

	// ResourceUsage provides resource utilization information
	ResourceUsage ResourceUsage `json:"resource_usage,omitempty"`
}

// ResourceUsage tracks observer resource utilization
type ResourceUsage struct {
	// MemoryUsageBytes approximate memory usage in bytes
	MemoryUsageBytes uint64 `json:"memory_usage_bytes"`

	// CPUUsagePercent approximate CPU usage as percentage
	CPUUsagePercent float64 `json:"cpu_usage_percent"`

	// BufferUtilization channel buffer utilization percentage
	BufferUtilization float64 `json:"buffer_utilization"`

	// GoroutineCount number of goroutines used by observer
	GoroutineCount int `json:"goroutine_count"`

	// FileDescriptorCount number of file descriptors open
	FileDescriptorCount int `json:"file_descriptor_count,omitempty"`
}

// HealthTracker provides atomic operations for tracking observer health
type HealthTracker struct {
	healthy        int64     // 0=unhealthy, 1=healthy
	eventsObserved uint64    // atomic counter
	eventsDropped  uint64    // atomic counter
	errorCount     uint64    // atomic counter
	lastEventTime  int64     // unix nanoseconds, atomic
	startTime      time.Time // immutable after initialization
}

// NewHealthTracker creates a new health tracker
func NewHealthTracker() *HealthTracker {
	return &HealthTracker{
		healthy:   1, // Start healthy
		startTime: time.Now(),
	}
}

// SetHealthy atomically sets the healthy status
func (h *HealthTracker) SetHealthy(healthy bool) {
	var value int64
	if healthy {
		value = 1
	}
	atomic.StoreInt64(&h.healthy, value)
}

// IsHealthy atomically reads the healthy status
func (h *HealthTracker) IsHealthy() bool {
	return atomic.LoadInt64(&h.healthy) == 1
}

// IncrementEventsObserved atomically increments the events observed counter
func (h *HealthTracker) IncrementEventsObserved() {
	atomic.AddUint64(&h.eventsObserved, 1)
	atomic.StoreInt64(&h.lastEventTime, time.Now().UnixNano())
}

// IncrementEventsDropped atomically increments the events dropped counter
func (h *HealthTracker) IncrementEventsDropped() {
	atomic.AddUint64(&h.eventsDropped, 1)
}

// IncrementErrorCount atomically increments the error counter
func (h *HealthTracker) IncrementErrorCount() {
	atomic.AddUint64(&h.errorCount, 1)
}

// GetEventsObserved atomically reads the events observed counter
func (h *HealthTracker) GetEventsObserved() uint64 {
	return atomic.LoadUint64(&h.eventsObserved)
}

// GetEventsDropped atomically reads the events dropped counter
func (h *HealthTracker) GetEventsDropped() uint64 {
	return atomic.LoadUint64(&h.eventsDropped)
}

// GetErrorCount atomically reads the error counter
func (h *HealthTracker) GetErrorCount() uint64 {
	return atomic.LoadUint64(&h.errorCount)
}

// GetLastEventTime atomically reads the last event time
func (h *HealthTracker) GetLastEventTime() time.Time {
	nanos := atomic.LoadInt64(&h.lastEventTime)
	if nanos == 0 {
		return time.Time{} // Zero time if no events processed
	}
	return time.Unix(0, nanos)
}

// GetStartTime returns the start time (immutable)
func (h *HealthTracker) GetStartTime() time.Time {
	return h.startTime
}

// GetHealthStatus returns the current health status snapshot
func (h *HealthTracker) GetHealthStatus() HealthStatus {
	return HealthStatus{
		Healthy:        h.IsHealthy(),
		LastEventTime:  h.GetLastEventTime(),
		EventsObserved: h.GetEventsObserved(),
		EventsDropped:  h.GetEventsDropped(),
		ErrorCount:     h.GetErrorCount(),
		StartTime:      h.GetStartTime(),
	}
}

// GetHealthStatusWithComponents returns health status with component details
func (h *HealthTracker) GetHealthStatusWithComponents(components map[string]bool, usage ResourceUsage) HealthStatus {
	status := h.GetHealthStatus()
	status.ComponentHealth = components
	status.ResourceUsage = usage
	return status
}

// ObserverWithHealth extends the base Observer interface with structured health reporting
type ObserverWithHealth interface {
	Observer

	// GetHealthStatus returns structured health information
	GetHealthStatus() HealthStatus
}
