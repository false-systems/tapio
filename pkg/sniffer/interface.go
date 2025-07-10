// Package sniffer provides a unified interface for data collection from various sources
package sniffer

import (
	"context"
	"time"
)

// Sniffer defines the standard interface for all data collection sources
type Sniffer interface {
	// Name returns the unique name of this sniffer
	Name() string

	// Events returns a channel that emits events from this sniffer
	Events() <-chan Event

	// Start begins data collection with the given configuration
	Start(ctx context.Context, config Config) error

	// Health returns the current health status of the sniffer
	Health() Health
}

// Event represents a single data point collected by a sniffer
type Event struct {
	// ID is a unique identifier for this event
	ID string `json:"id"`

	// Timestamp when the event occurred
	Timestamp time.Time `json:"timestamp"`

	// Source identifies which sniffer generated this event (e.g., "ebpf", "k8s-api")
	Source string `json:"source"`

	// Type categorizes the event (e.g., "memory_oom", "network_timeout", "pod_crash")
	Type string `json:"type"`

	// Severity indicates the importance of this event
	Severity Severity `json:"severity"`

	// Data contains source-specific event data
	Data map[string]interface{} `json:"data"`

	// Actionable contains kubectl commands or other fixes if available
	Actionable *ActionableItem `json:"actionable,omitempty"`

	// Context provides additional correlation information
	Context *EventContext `json:"context,omitempty"`
}

// EventContext provides correlation information for events
type EventContext struct {
	// Pod information if applicable
	Pod       string            `json:"pod,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Container string            `json:"container,omitempty"`
	Node      string            `json:"node,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`

	// Process information if applicable
	PID         uint32 `json:"pid,omitempty"`
	ProcessName string `json:"process_name,omitempty"`
	PPID        uint32 `json:"ppid,omitempty"`
	UID         uint32 `json:"uid,omitempty"`
	GID         uint32 `json:"gid,omitempty"`

	// Network information if applicable
	SrcIP   string `json:"src_ip,omitempty"`
	DstIP   string `json:"dst_ip,omitempty"`
	SrcPort uint16 `json:"src_port,omitempty"`
	DstPort uint16 `json:"dst_port,omitempty"`
	Proto   string `json:"proto,omitempty"`
}

// Severity levels for events
type Severity string

const (
	SeverityCritical Severity = "CRITICAL" // Immediate action required
	SeverityHigh     Severity = "HIGH"     // Serious issue, action needed soon
	SeverityMedium   Severity = "MEDIUM"   // Notable issue, should investigate
	SeverityLow      Severity = "LOW"      // Informational, no action needed
)

// ActionableItem provides specific actions to resolve an issue
type ActionableItem struct {
	// Title is a short description of the action
	Title string `json:"title"`

	// Description provides more detail about why this action is recommended
	Description string `json:"description"`

	// Commands are the actual kubectl or other commands to run
	Commands []string `json:"commands"`

	// Risk level of applying this fix
	Risk string `json:"risk"` // "low", "medium", "high"

	// EstimatedImpact describes what will happen if applied
	EstimatedImpact string `json:"estimated_impact"`
}

// Config provides configuration for a sniffer
type Config struct {
	// Common configuration
	Enabled         bool                   `json:"enabled"`
	SamplingRate    float64                `json:"sampling_rate"`    // 0.0-1.0, where 1.0 = 100%
	EventBufferSize int                    `json:"event_buffer_size"` // Size of the event channel buffer
	MaxEventsPerSec int                    `json:"max_events_per_sec"` // Rate limiting
	Extra           map[string]interface{} `json:"extra"`             // Sniffer-specific config

	// Resource limits
	MaxMemoryMB int `json:"max_memory_mb"`
	MaxCPUMilli int `json:"max_cpu_milli"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		SamplingRate:    1.0,
		EventBufferSize: 10000,
		MaxEventsPerSec: 100000,
		MaxMemoryMB:     128,
		MaxCPUMilli:     50,
		Extra:           make(map[string]interface{}),
	}
}

// Health represents the health status of a sniffer
type Health struct {
	// Status indicates if the sniffer is working properly
	Status HealthStatus `json:"status"`

	// Message provides additional context about the status
	Message string `json:"message"`

	// LastEventTime is when the last event was emitted
	LastEventTime time.Time `json:"last_event_time"`

	// EventsProcessed is the total number of events processed
	EventsProcessed uint64 `json:"events_processed"`

	// EventsDropped is the number of events dropped due to buffer overflow
	EventsDropped uint64 `json:"events_dropped"`

	// Metrics provides sniffer-specific health metrics
	Metrics map[string]interface{} `json:"metrics"`
}

// HealthStatus represents the health state
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// Manager coordinates multiple sniffers
type Manager interface {
	// Register adds a new sniffer to the manager
	Register(sniffer Sniffer) error

	// Start begins all registered sniffers
	Start(ctx context.Context) error

	// Events returns a merged stream of events from all sniffers
	Events() <-chan Event

	// Health returns the health status of all sniffers
	Health() map[string]Health

	// GetSniffer returns a specific sniffer by name
	GetSniffer(name string) (Sniffer, bool)
}