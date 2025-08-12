package domain

import (
	"fmt"
	"sync"
	"time"
)

// HealthDetails represents structured health details
type HealthDetails struct {
	// Resource utilization
	CPUUsage    float64 `json:"cpu_usage,omitempty"`
	MemoryUsage int64   `json:"memory_usage,omitempty"`
	DiskUsage   int64   `json:"disk_usage,omitempty"`
	NetworkIO   int64   `json:"network_io,omitempty"`

	// Performance metrics
	Latency     time.Duration `json:"latency,omitempty"`
	Throughput  float64       `json:"throughput,omitempty"`
	QueueSize   int           `json:"queue_size,omitempty"`
	ActiveConns int           `json:"active_connections,omitempty"`

	// Component-specific metrics
	EventRate   float64 `json:"event_rate,omitempty"`
	ErrorRate   float64 `json:"error_rate,omitempty"`
	SuccessRate float64 `json:"success_rate,omitempty"`

	// Health check details
	Dependencies map[string]string `json:"dependencies,omitempty"`
	ConfigHash   string            `json:"config_hash,omitempty"`
	FeatureFlags []string          `json:"feature_flags,omitempty"`

	// Runtime information
	StartTime    time.Time `json:"start_time,omitempty"`
	RestartCount int32     `json:"restart_count,omitempty"`
	PID          int32     `json:"pid,omitempty"`

	// Storage and database health
	DBConnections int     `json:"db_connections,omitempty"`
	CacheHitRate  float64 `json:"cache_hit_rate,omitempty"`
	StorageHealth string  `json:"storage_health,omitempty"`

	// Component statuses (for aggregators)
	Components   map[string]*HealthStatus `json:"components,omitempty"`
	ChecksTotal  int                      `json:"checks_total,omitempty"`
	ChecksPassed int                      `json:"checks_passed,omitempty"`
	ChecksFailed int                      `json:"checks_failed,omitempty"`

	// Additional structured data
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// HealthStatus represents the health status of a component
type HealthStatus struct {
	// Core fields
	Status    HealthStatusValue `json:"status"`
	Message   string            `json:"message"`
	Timestamp time.Time         `json:"timestamp"`

	// Component information
	Component string `json:"component,omitempty"`
	Version   string `json:"version,omitempty"`

	// Runtime information
	Uptime        time.Duration `json:"uptime,omitempty"`
	LastHealthy   time.Time     `json:"last_healthy,omitempty"`
	LastError     error         `json:"-"` // Don't expose error directly in JSON
	LastErrorText string        `json:"last_error,omitempty"`

	// Metrics
	EventsEmitted int64 `json:"events_emitted,omitempty"`
	EventsDropped int64 `json:"events_dropped,omitempty"`
	ErrorCount    int64 `json:"error_count,omitempty"`

	// Additional details for specific components
	Details *HealthDetails `json:"details,omitempty"`
}

// HealthStatusValue represents the health state
type HealthStatusValue string

const (
	HealthHealthy   HealthStatusValue = "healthy"
	HealthDegraded  HealthStatusValue = "degraded"
	HealthUnhealthy HealthStatusValue = "unhealthy"
	HealthUnknown   HealthStatusValue = "unknown"
)

// String returns the string representation of the health status
func (h HealthStatusValue) String() string {
	return string(h)
}

// IsHealthy returns true if the status represents a healthy state
func (h HealthStatusValue) IsHealthy() bool {
	return h == HealthHealthy
}

// NewHealthStatus creates a new health status with the given values
func NewHealthStatus(status HealthStatusValue, message string) *HealthStatus {
	return &HealthStatus{
		Status:    status,
		Message:   message,
		Timestamp: time.Now(),
		Details:   &HealthDetails{},
	}
}

// NewHealthyStatus creates a healthy status
func NewHealthyStatus(message string) *HealthStatus {
	return NewHealthStatus(HealthHealthy, message)
}

// NewUnhealthyStatus creates an unhealthy status
func NewUnhealthyStatus(message string, err error) *HealthStatus {
	hs := NewHealthStatus(HealthUnhealthy, message)
	if err != nil {
		hs.LastError = err
		hs.LastErrorText = err.Error()
		hs.ErrorCount = 1
	}
	return hs
}

// SetError updates the health status with an error
func (h *HealthStatus) SetError(err error) {
	if err != nil {
		h.LastError = err
		h.LastErrorText = err.Error()
		h.ErrorCount++
		h.Status = HealthUnhealthy
	}
}

// SetDetail adds a detail to the health status
func (h *HealthStatus) SetDetail(key string, value interface{}) {
	if h.Details == nil {
		h.Details = &HealthDetails{}
	}

	// Set common details based on key
	switch key {
	case "components":
		if components, ok := value.(map[string]*HealthStatus); ok {
			h.Details.Components = components
		}
	case "unhealthy_count":
		if count, ok := value.(int); ok {
			h.Details.ChecksFailed = count
		}
	case "degraded_count":
		if count, ok := value.(int); ok {
			h.Details.ChecksFailed += count // Add to failed count
		}
	case "total_components":
		if count, ok := value.(int); ok {
			h.Details.ChecksTotal = count
		}
	default:
		// For unknown keys, store in labels
		if h.Details.Labels == nil {
			h.Details.Labels = make(map[string]string)
		}
		if str, ok := value.(string); ok {
			h.Details.Labels[key] = str
		} else {
			h.Details.Labels[key] = fmt.Sprintf("%v", value)
		}
	}
}

// IsHealthy returns true if the status is healthy
func (h *HealthStatus) IsHealthy() bool {
	return h.Status.IsHealthy()
}

// HealthChecker is an interface for components that can report health
type HealthChecker interface {
	// Health returns the current health status
	Health() *HealthStatus
}

// HealthAggregator aggregates health from multiple sources
type HealthAggregator struct {
	components map[string]HealthChecker
	mu         sync.RWMutex
}

// NewHealthAggregator creates a new health aggregator
func NewHealthAggregator() *HealthAggregator {
	return &HealthAggregator{
		components: make(map[string]HealthChecker),
	}
}

// Register adds a component to the aggregator
func (h *HealthAggregator) Register(name string, checker HealthChecker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.components[name] = checker
}

// Unregister removes a component from the aggregator
func (h *HealthAggregator) Unregister(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.components, name)
}

// AggregateHealth returns the overall health status
func (h *HealthAggregator) AggregateHealth() *HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	overall := NewHealthyStatus("All components healthy")
	componentStatuses := make(map[string]*HealthStatus)

	unhealthyCount := 0
	degradedCount := 0

	for name, checker := range h.components {
		status := checker.Health()
		componentStatuses[name] = status

		switch status.Status {
		case HealthUnhealthy:
			unhealthyCount++
		case HealthDegraded:
			degradedCount++
		case HealthUnknown:
			// Don't count unknown as unhealthy
		}
	}

	// Determine overall status
	if unhealthyCount > 0 {
		overall.Status = HealthUnhealthy
		overall.Message = fmt.Sprintf("%d components unhealthy", unhealthyCount)
	} else if degradedCount > 0 {
		overall.Status = HealthDegraded
		overall.Message = fmt.Sprintf("%d components degraded", degradedCount)
	}

	// Add component details
	overall.SetDetail("components", componentStatuses)
	overall.SetDetail("unhealthy_count", unhealthyCount)
	overall.SetDetail("degraded_count", degradedCount)
	overall.SetDetail("total_components", len(h.components))

	return overall
}
