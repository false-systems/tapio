package unified

import (
	"time"
)

// HealthStatus represents health states
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// Severity defines event severity levels
type Severity string

const (
	SeverityCritical Severity = "critical" // Immediate action required
	SeverityHigh     Severity = "high"     // Serious issue, action needed soon
	SeverityError    Severity = "error"    // Error condition
	SeverityWarning  Severity = "warning"  // Warning condition
	SeverityMedium   Severity = "medium"   // Notable issue, should investigate
	SeverityLow      Severity = "low"      // Informational, no action needed
	SeverityInfo     Severity = "info"     // Informational only
	SeverityDebug    Severity = "debug"    // Debug information
)

// Category defines event categories
type Category string

const (
	CategoryNetwork     Category = "network"
	CategoryMemory      Category = "memory"
	CategoryCPU         Category = "cpu"
	CategoryDisk        Category = "disk"
	CategoryStorage     Category = "storage"
	CategoryProcess     Category = "process"
	CategoryKubernetes  Category = "kubernetes"
	CategorySecurity    Category = "security"
	CategoryPerf        Category = "performance"
	CategoryApp         Category = "application"
	CategorySystem      Category = "system"
	CategoryCNI         Category = "cni"
	CategoryMesh        Category = "mesh"
	CategoryReliability Category = "reliability"
)

// Risk levels for actionable items
type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// Collector type constants
const (
	CollectorTypeEBPF     = "ebpf"
	CollectorTypeK8s      = "kubernetes"
	CollectorTypeSystemd  = "systemd"
	CollectorTypeJournald = "journald"
	CollectorTypeCNI      = "cni"
	CollectorTypeMesh     = "mesh"
	CollectorTypeNetwork  = "network"
	CollectorTypeMemory   = "memory"
	CollectorTypeCPU      = "cpu"
	CollectorTypeDisk     = "disk"
	CollectorTypeProcess  = "process"
	CollectorTypeSecurity = "security"
)

// CollectorConfig provides unified configuration
type CollectorConfig struct {
	// Basic configuration
	Name            string   `json:"name"`
	Type            string   `json:"type"`
	Enabled         bool     `json:"enabled"`
	SamplingRate    float64  `json:"sampling_rate"` // 0.0-1.0
	EventBufferSize int      `json:"event_buffer_size"`
	MaxEventsPerSec int      `json:"max_events_per_sec"`
	MinSeverity     Severity `json:"min_severity"`

	// Resource limits
	MaxMemoryMB int `json:"max_memory_mb"`
	MaxCPUMilli int `json:"max_cpu_milli"`

	// Metadata
	Labels map[string]string `json:"labels,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`

	// Type-specific configuration
	Extra map[string]interface{} `json:"extra,omitempty"`
}

// DefaultCollectorConfig returns a sensible default configuration
func DefaultCollectorConfig(name, collectorType string) CollectorConfig {
	return CollectorConfig{
		Name:            name,
		Type:            collectorType,
		Enabled:         true,
		SamplingRate:    1.0,
		EventBufferSize: 10000,
		MaxEventsPerSec: 10000,
		MinSeverity:     SeverityInfo,
		MaxMemoryMB:     128,
		MaxCPUMilli:     100,
		Labels:          make(map[string]string),
		Tags:            make(map[string]string),
		Extra:           make(map[string]interface{}),
	}
}

// Health represents the unified health status
type Health struct {
	Status          HealthStatus           `json:"status"`
	Message         string                 `json:"message"`
	LastEventTime   time.Time              `json:"last_event_time"`
	EventsProcessed uint64                 `json:"events_processed"`
	EventsDropped   uint64                 `json:"events_dropped"`
	ErrorCount      uint64                 `json:"error_count"`
	Metrics         map[string]interface{} `json:"metrics,omitempty"`
}

// Stats provides detailed collector statistics
type Stats struct {
	EventsCollected uint64                 `json:"events_collected"`
	EventsDropped   uint64                 `json:"events_dropped"`
	EventsFiltered  uint64                 `json:"events_filtered"`
	ErrorCount      uint64                 `json:"error_count"`
	StartTime       time.Time              `json:"start_time"`
	LastEventTime   time.Time              `json:"last_event_time"`
	Custom          map[string]interface{} `json:"custom,omitempty"`
}
