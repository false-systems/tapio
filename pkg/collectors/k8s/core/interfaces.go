package core

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Collector defines the interface for Kubernetes event collection
type Collector interface {
	// Lifecycle management
	Start(ctx context.Context) error
	Stop() error

	// Event streaming - Returns UnifiedEvent for direct analytics integration
	Events() <-chan domain.UnifiedEvent

	// Health and monitoring
	Health() Health
	Statistics() Statistics

	// Configuration
	Configure(config Config) error
}

// Config defines Kubernetes collector configuration
type Config struct {
	// Basic settings
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	EventBufferSize int    `json:"event_buffer_size"`

	// Kubernetes configuration
	KubeConfig string `json:"kubeconfig,omitempty"` // Path to kubeconfig file
	InCluster  bool   `json:"in_cluster"`           // Use in-cluster config
	Namespace  string `json:"namespace,omitempty"`  // Namespace to watch (empty = all)

	// Resource filters
	WatchPods        bool `json:"watch_pods"`
	WatchNodes       bool `json:"watch_nodes"`
	WatchServices    bool `json:"watch_services"`
	WatchDeployments bool `json:"watch_deployments"`
	WatchEvents      bool `json:"watch_events"`
	WatchConfigMaps  bool `json:"watch_configmaps"`
	WatchSecrets     bool `json:"watch_secrets"`

	// Performance tuning
	ResyncPeriod   time.Duration `json:"resync_period"`
	EventRateLimit int           `json:"event_rate_limit"`
	LabelSelector  string        `json:"label_selector,omitempty"`
	FieldSelector  string        `json:"field_selector,omitempty"`
}

// Health represents collector health status
type Health struct {
	Status          HealthStatus       `json:"status"`
	Message         string             `json:"message"`
	LastEventTime   time.Time          `json:"last_event_time"`
	EventsProcessed uint64             `json:"events_processed"`
	EventsDropped   uint64             `json:"events_dropped"`
	ErrorCount      uint64             `json:"error_count"`
	Metrics         map[string]float64 `json:"metrics"`
	Connected       bool               `json:"connected"`
	ClusterInfo     ClusterInfo        `json:"cluster_info"`
}

// HealthStatus represents the health state
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// Statistics represents runtime statistics
type Statistics struct {
	StartTime        time.Time              `json:"start_time"`
	EventsCollected  uint64                 `json:"events_collected"`
	EventsDropped    uint64                 `json:"events_dropped"`
	ResourcesWatched map[string]int         `json:"resources_watched"`
	WatchersActive   int                    `json:"watchers_active"`
	APICallsTotal    uint64                 `json:"api_calls_total"`
	APIErrors        uint64                 `json:"api_errors"`
	ReconnectCount   uint64                 `json:"reconnect_count"`
	Custom           map[string]interface{} `json:"custom"`
}

// ClusterInfo contains information about the connected cluster
type ClusterInfo struct {
	Name         string    `json:"name"`
	Version      string    `json:"version"`
	Platform     string    `json:"platform"`
	ConnectedAt  time.Time `json:"connected_at"`
	APIServerURL string    `json:"api_server_url"`
}

// ResourceWatcher watches specific Kubernetes resources
type ResourceWatcher interface {
	// Start watching resources
	Start(ctx context.Context) error

	// Stop watching
	Stop() error

	// Events channel
	Events() <-chan RawEvent

	// Resource type being watched
	ResourceType() string
}

// EventProcessor processes raw K8s events into UnifiedEvents
// This eliminates conversion overhead and enables rich semantic correlation
type EventProcessor interface {
	ProcessEvent(ctx context.Context, raw RawEvent) (*domain.UnifiedEvent, error)
}

// RawEvent represents a raw Kubernetes event
type RawEvent struct {
	Type         EventType              `json:"type"`
	Object       interface{}            `json:"object"`
	OldObject    interface{}            `json:"old_object,omitempty"`
	ResourceKind string                 `json:"resource_kind"`
	Namespace    string                 `json:"namespace"`
	Name         string                 `json:"name"`
	Timestamp    time.Time              `json:"timestamp"`
	Raw          map[string]interface{} `json:"raw,omitempty"`
}

// EventType represents the type of Kubernetes event
type EventType string

const (
	EventTypeAdded    EventType = "ADDED"
	EventTypeModified EventType = "MODIFIED"
	EventTypeDeleted  EventType = "DELETED"
	EventTypeError    EventType = "ERROR"
)

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 1000
	}
	if c.ResyncPeriod <= 0 {
		c.ResyncPeriod = 30 * time.Minute
	}

	// At least one resource type should be watched
	if !c.WatchPods && !c.WatchNodes && !c.WatchServices &&
		!c.WatchDeployments && !c.WatchEvents && !c.WatchConfigMaps && !c.WatchSecrets {
		// Default to watching pods and events
		c.WatchPods = true
		c.WatchEvents = true
	}

	return nil
}
