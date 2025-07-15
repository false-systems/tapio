// Package relay provides intelligent event aggregation and routing
// Following Tapio's zero-config philosophy with high performance
package relay

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/api"
	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/types"
	"go.opentelemetry.io/otel/trace"
)

// RelayService is the core interface for Tapio Relay
// It acts as an intelligent aggregation layer between collectors and consumers
type RelayService interface {
	// Start begins relay operations
	Start(ctx context.Context) error
	
	// Stop gracefully shuts down the relay
	Stop() error
	
	// GetStats returns relay performance statistics
	GetStats() RelayStats
}

// EventProcessor handles incoming events from collectors
type EventProcessor interface {
	// ProcessEvent handles a single event
	ProcessEvent(ctx context.Context, event *api.Event) error
	
	// ProcessBatch handles a batch of events efficiently
	ProcessBatch(ctx context.Context, events []*api.Event) error
	
	// Flush forces processing of any buffered events
	Flush(ctx context.Context) error
}

// ExportPipeline defines how data is exported from the relay
type ExportPipeline interface {
	// Export sends data to external systems (OTEL, Prometheus, etc)
	Export(ctx context.Context, data ExportData) error
	
	// Configure updates export settings dynamically
	Configure(config ExportConfig) error
	
	// HealthCheck verifies export targets are reachable
	HealthCheck(ctx context.Context) error
}

// ExportData represents data ready for export
type ExportData interface {
	// GetType returns the data type (events, metrics, traces)
	GetType() string
	
	// AsOTELSpans converts to OTEL spans for tracing
	AsOTELSpans() []trace.Span
	
	// AsMetrics converts to Prometheus metrics
	AsMetrics() []Metric
	
	// AsEvents returns raw events
	AsEvents() []*api.Event
}

// AggregationStrategy defines how events are aggregated
type AggregationStrategy interface {
	// ShouldAggregate determines if events should be aggregated
	ShouldAggregate(events []*api.Event) bool
	
	// Aggregate combines multiple events into aggregated form
	Aggregate(events []*api.Event) (*AggregatedEvent, error)
	
	// GetWindow returns the aggregation time window
	GetWindow() time.Duration
}

// RoutingPolicy determines where events should be sent
type RoutingPolicy interface {
	// Route determines destinations for an event
	Route(event *api.Event) []Destination
	
	// UpdatePolicy updates routing rules dynamically
	UpdatePolicy(rules []RoutingRule) error
}

// Destination represents where events can be sent
type Destination struct {
	Type     DestinationType // engine, otel, metrics, etc
	Endpoint string          // gRPC/HTTP endpoint
	Priority int             // For failover ordering
}

// DestinationType defines supported destinations
type DestinationType string

const (
	DestinationEngine     DestinationType = "engine"
	DestinationOTEL       DestinationType = "otel"
	DestinationPrometheus DestinationType = "prometheus"
	DestinationWebhook    DestinationType = "webhook"
)

// RelayStats provides relay performance metrics
type RelayStats struct {
	// Input metrics
	EventsReceived   int64
	BatchesReceived  int64
	BytesReceived    int64
	
	// Processing metrics
	EventsProcessed  int64
	EventsAggregated int64
	EventsDropped    int64
	
	// Export metrics
	ExportsSuccess   int64
	ExportsFailed    int64
	ExportLatencyP99 time.Duration
	
	// System metrics
	BufferUtilization float64
	CPUUsage          float64
	MemoryUsage       int64
	
	// Timing
	UptimeSeconds     int64
	LastEventTime     time.Time
}

// AggregatedEvent represents multiple events combined
type AggregatedEvent struct {
	ID            string
	Type          string
	Count         int
	FirstSeen     time.Time
	LastSeen      time.Time
	Sources       []string
	Pattern       string
	Significance  float64
	CorrelationID string
	Events        []*api.Event // Original events if needed
}

// RoutingRule defines how to route events
type RoutingRule struct {
	Name        string
	Priority    int
	Condition   string // CEL expression
	Destination Destination
	Transform   string // Optional transformation
}

// ExportConfig configures export pipelines
type ExportConfig struct {
	// OTEL configuration
	OTELEnabled  bool
	OTELEndpoint string
	OTELHeaders  map[string]string
	
	// Prometheus configuration
	PrometheusEnabled  bool
	PrometheusEndpoint string
	
	// Feature flags
	IncludeRawEvents     bool
	IncludeAggregations  bool
	IncludeCorrelations  bool
	CompressionEnabled   bool
}

// Metric represents a metric for export
type Metric struct {
	Name      string
	Type      string
	Value     float64
	Labels    map[string]string
	Timestamp time.Time
}

// BufferManager handles event buffering and backpressure
type BufferManager interface {
	// Add attempts to add an event to the buffer
	Add(event *api.Event) error
	
	// AddBatch attempts to add multiple events
	AddBatch(events []*api.Event) error
	
	// Drain retrieves events for processing
	Drain(maxCount int) []*api.Event
	
	// Size returns current buffer size
	Size() int
	
	// IsFull checks if buffer is at capacity
	IsFull() bool
}

// ResilienceManager handles failures and recovery
type ResilienceManager interface {
	// WrapCall wraps a function call with resilience patterns
	WrapCall(fn func() error) error
	
	// RecordSuccess records successful operation
	RecordSuccess(operation string)
	
	// RecordFailure records failed operation
	RecordFailure(operation string, err error)
	
	// IsHealthy checks if operation should proceed
	IsHealthy(operation string) bool
}