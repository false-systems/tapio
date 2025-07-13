// Package collectors provides a pluggable collector architecture for lightweight data collection
package collectors

import (
	"context"
	"time"
)

// Collector defines the standard interface for all data collection sources
type Collector interface {
	// Name returns the unique name of this collector
	Name() string

	// Type returns the collector type (e.g., "ebpf", "k8s", "systemd")
	Type() string

	// Start begins data collection with the given configuration
	Start(ctx context.Context) error

	// Stop gracefully stops the collector
	Stop() error

	// Events returns a channel that emits events from this collector
	Events() <-chan *Event

	// Health returns the current health status of the collector
	Health() *Health

	// GetStats returns collector-specific statistics
	GetStats() *Stats

	// Configure updates the collector configuration
	Configure(config CollectorConfig) error

	// IsEnabled returns whether the collector is currently enabled
	IsEnabled() bool
}

// Event represents a unified event from any collector source
type Event struct {
	// Core identification
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	
	// Source information
	Source EventSource `json:"source"`
	
	// Event classification
	Type     EventType `json:"type"`     // Event type
	Category Category  `json:"category"`  // High-level category
	Severity Severity  `json:"severity"`  // Event severity level
	
	// Event data
	Data       map[string]interface{} `json:"data"`        // Raw event data
	Attributes map[string]interface{} `json:"attributes"`  // Additional attributes
	Labels     map[string]string      `json:"labels"`      // Kubernetes/system labels
	
	// Context for correlation
	Context *EventContext `json:"context,omitempty"`
	
	// Metadata for processing
	Metadata EventMetadata `json:"metadata"`
	
	// Optional actionable recommendations
	Actionable *ActionableItem `json:"actionable,omitempty"`
}

// EventSource identifies the source of an event
type EventSource struct {
	Collector string `json:"collector"` // Collector name (e.g., "ebpf")
	Component string `json:"component"` // Component within collector (e.g., "memory")
	Node      string `json:"node"`      // Node where event originated
}

// EventType represents the type of event
type EventType string

const (
	EventTypeMetric  EventType = "metric"
	EventTypeLog     EventType = "log"
	EventTypeTrace   EventType = "trace"
	EventTypeAlert   EventType = "alert"
	EventTypeAnomaly EventType = "anomaly"
)

// EventMetadata provides processing metadata
type EventMetadata struct {
	Importance  float32  `json:"importance"`  // 0.0-1.0 importance score
	Reliability float32  `json:"reliability"` // 0.0-1.0 reliability score
	Correlation []string `json:"correlation"` // Correlation IDs
}

// EventContext provides correlation information for events
type EventContext struct {
	// Kubernetes context
	Namespace string            `json:"namespace,omitempty"`
	Pod       string            `json:"pod,omitempty"`
	Container string            `json:"container,omitempty"`
	Node      string            `json:"node,omitempty"`
	Service   string            `json:"service,omitempty"`
	Labels    map[string]string `json:"k8s_labels,omitempty"`
	
	// Process context
	PID         uint32 `json:"pid,omitempty"`
	ProcessName string `json:"process_name,omitempty"`
	PPID        uint32 `json:"ppid,omitempty"`
	UID         uint32 `json:"uid,omitempty"`
	GID         uint32 `json:"gid,omitempty"`
	
	// Network context
	SrcIP    string `json:"src_ip,omitempty"`
	DstIP    string `json:"dst_ip,omitempty"`
	SrcPort  uint16 `json:"src_port,omitempty"`
	DstPort  uint16 `json:"dst_port,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	
	// Additional context
	WorkloadType string            `json:"workload_type,omitempty"` // "deployment", "daemonset", etc.
	Environment  string            `json:"environment,omitempty"`   // "prod", "staging", etc.
	Team         string            `json:"team,omitempty"`          // Owner team
	Custom       map[string]string `json:"custom,omitempty"`        // Custom context fields
}

// Category represents the high-level event category
type Category string

const (
	CategoryNetwork     Category = "network"
	CategoryMemory      Category = "memory"
	CategoryCPU         Category = "cpu"
	CategoryDisk        Category = "disk"
	CategoryProcess     Category = "process"
	CategoryKubernetes  Category = "kubernetes"
	CategorySecurity    Category = "security"
	CategoryPerformance Category = "performance"
	CategoryApplication Category = "application"
	CategorySystem      Category = "system"
)

// Severity levels for events
type Severity string

const (
	SeverityCritical Severity = "critical" // Immediate action required
	SeverityHigh     Severity = "high"     // Serious issue, action needed soon
	SeverityMedium   Severity = "medium"   // Notable issue, should investigate
	SeverityLow      Severity = "low"      // Informational, no action needed
	SeverityDebug    Severity = "debug"    // Debug information
)

// ActionableItem provides specific actions to resolve an issue
type ActionableItem struct {
	Title           string   `json:"title"`             // Short description of the action
	Description     string   `json:"description"`       // Detailed explanation
	Commands        []string `json:"commands"`          // kubectl or other commands to run
	Risk            string   `json:"risk"`              // "low", "medium", "high"
	EstimatedImpact string   `json:"estimated_impact"`  // Expected outcome
	AutoApplicable  bool     `json:"auto_applicable"`   // Can be applied automatically
	Category        string   `json:"category"`          // Type of fix ("resource", "config", etc.)
}

// Health represents the health status of a collector
type Health struct {
	Status           HealthStatus           `json:"status"`
	Message          string                 `json:"message"`
	LastEventTime    time.Time              `json:"last_event_time"`
	EventsProcessed  uint64                 `json:"events_processed"`
	EventsDropped    uint64                 `json:"events_dropped"`
	ErrorCount       uint64                 `json:"error_count"`
	LastError        error                  `json:"-"`
	LastErrorTime    time.Time              `json:"last_error_time,omitempty"`
	Metrics          map[string]interface{} `json:"metrics"`
}

// HealthStatus represents the health state
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusStopped   HealthStatus = "stopped"
)

// Stats provides detailed collector statistics
type Stats struct {
	// Basic counters
	EventsTotal       uint64        `json:"events_total"`
	EventsDropped     uint64        `json:"events_dropped"`
	EventsPerSecond   float64       `json:"events_per_second"`
	ErrorCount        uint64        `json:"error_count"`
	LastEventTime     time.Time     `json:"last_event_time"`
	
	// Collector-specific metrics
	CollectorSpecific map[string]interface{} `json:"collector_specific"`
}

// CollectorConfig provides configuration for a specific collector
type CollectorConfig struct {
	// Basic configuration
	Name             string                 `json:"name"`
	Type             string                 `json:"type"`
	Enabled          bool                   `json:"enabled"`
	
	// Data collection settings
	SamplingRate     float64                `json:"sampling_rate"`      // 0.0-1.0, where 1.0 = 100%
	EventBufferSize  int                    `json:"event_buffer_size"`  // Size of the event channel buffer
	MaxEventsPerSec  int                    `json:"max_events_per_sec"` // Rate limiting
	
	// Resource limits
	MaxMemoryMB      int                    `json:"max_memory_mb"`
	MaxCPUMilli      int                    `json:"max_cpu_milli"`
	
	// Quality and filtering
	MinSeverity      Severity               `json:"min_severity"`       // Only emit events >= this severity
	IncludeCategories []Category            `json:"include_categories"` // Only include these categories
	ExcludeCategories []Category            `json:"exclude_categories"` // Exclude these categories
	
	// Collector-specific configuration
	Extra map[string]interface{} `json:"extra"`
	
	// Environment context
	Environment      string                 `json:"environment"`        // "prod", "staging", etc.
	Team             string                 `json:"team"`               // Owner team
	Labels           map[string]string      `json:"labels"`             // Additional labels
}

// DefaultCollectorConfig returns a default configuration
func DefaultCollectorConfig(name, collectorType string) CollectorConfig {
	return CollectorConfig{
		Name:              name,
		Type:              collectorType,
		Enabled:           true,
		SamplingRate:      1.0,
		EventBufferSize:   10000,
		MaxEventsPerSec:   10000,
		MaxMemoryMB:       100,  // DaemonSet resource limit
		MaxCPUMilli:       10,   // 1% CPU limit
		MinSeverity:       SeverityLow,
		IncludeCategories: nil,  // Include all by default
		ExcludeCategories: nil,  // Exclude none by default
		Extra: make(map[string]interface{}),
		Labels:            make(map[string]string),
	}
}

// Factory defines the interface for creating collectors
type Factory interface {
	// CreateCollector creates a new collector instance
	CreateCollector(config CollectorConfig) (Collector, error)
	
	// ValidateConfig validates a configuration for this collector type
	ValidateConfig(config CollectorConfig) error
	
	// GetRequirements returns the requirements for running this collector
	GetRequirements() CollectorRequirements
}

// CollectorRequirements describes what a collector needs to run
type CollectorRequirements struct {
	Capabilities []string             `json:"capabilities"` // Linux capabilities required
	KernelVersion string              `json:"kernel_version"` // Minimum kernel version
	Features     []string             `json:"features"` // Required kernel features
	Resources    ResourceRequirements `json:"resources"` // Resource requirements
}

// ResourceRequirements describes resource needs
type ResourceRequirements struct {
	MinMemoryMB int `json:"min_memory_mb"`
	MinCPUMilli int `json:"min_cpu_milli"`
}

// Manager coordinates multiple collectors with lifecycle management
type Manager interface {
	// Register adds a new collector to the manager
	Register(collector Collector) error
	
	// Unregister removes a collector from the manager
	Unregister(name string) error
	
	// Start begins all registered collectors
	Start(ctx context.Context) error
	
	// Stop gracefully stops all collectors
	Stop() error
	
	// Events returns a merged stream of events from all collectors
	Events() <-chan *Event
	
	// Health returns the health status of all collectors
	Health() map[string]*Health
	
	// GetStats returns statistics for all collectors
	GetStats() map[string]*Stats
	
	// GetCollector returns a specific collector by name
	GetCollector(name string) (Collector, bool)
	
	// ListCollectors returns all registered collector names
	ListCollectors() []string
	
	// Configure updates the configuration for a specific collector
	Configure(name string, config CollectorConfig) error
	
	// Reload reloads configuration for all collectors
	Reload() error
}

// EventHandler defines the interface for processing events
type EventHandler interface {
	// HandleEvent processes a single event
	HandleEvent(ctx context.Context, event *Event) error
	
	// HandleBatch processes a batch of events for efficiency
	HandleBatch(ctx context.Context, events []*Event) error
}

// Filter defines the interface for filtering events
type Filter interface {
	// ShouldInclude returns true if the event should be included
	ShouldInclude(event *Event) bool
	
	// Configure updates the filter configuration
	Configure(config map[string]interface{}) error
}

// Transformer defines the interface for transforming events
type Transformer interface {
	// Transform modifies an event
	Transform(event *Event) (*Event, error)
	
	// Configure updates the transformer configuration  
	Configure(config map[string]interface{}) error
}

// Pipeline defines a processing pipeline for events
type Pipeline interface {
	// Process runs an event through the pipeline
	Process(ctx context.Context, event *Event) (*Event, error)
	
	// AddFilter adds a filter to the pipeline
	AddFilter(filter Filter)
	
	// AddTransformer adds a transformer to the pipeline
	AddTransformer(transformer Transformer)
}