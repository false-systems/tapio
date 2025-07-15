package unified

import (
	"context"
	"time"
)

// Collector defines the unified interface for all data collection sources
type Collector interface {
	// Basic identification
	Name() string
	Type() string

	// Lifecycle management
	Start(ctx context.Context) error
	Stop() error
	IsEnabled() bool

	// Event streaming
	Events() <-chan *Event

	// Health and monitoring
	Health() *Health
	GetStats() *Stats

	// Configuration
	Configure(config CollectorConfig) error
}

// Manager coordinates multiple collectors
type Manager interface {
	// Collector management
	Register(collector Collector) error
	Unregister(name string) error
	GetCollector(name string) (Collector, bool)
	ListCollectors() []string

	// Lifecycle
	Start(ctx context.Context) error
	Stop() error

	// Event streaming
	Events() <-chan *Event

	// Health and monitoring
	Health() map[string]*Health
	GetStats() map[string]*Stats

	// Configuration
	Configure(config ManagerConfig) error
	AddFilter(filter EventFilter) error
	RemoveFilter(filterID string) error
}

// ManagerConfig provides manager-level configuration
type ManagerConfig struct {
	// Global settings
	MaxTotalMemoryMB  int `json:"max_total_memory_mb"`
	MaxTotalCPUMilli  int `json:"max_total_cpu_milli"`
	GlobalEventBuffer int `json:"global_event_buffer"`

	// Processing pipeline
	EnableFiltering   bool `json:"enable_filtering"`
	EnableTransforms  bool `json:"enable_transforms"`
	EnableCorrelation bool `json:"enable_correlation"`

	// Output configuration
	OutputBuffer  int           `json:"output_buffer"`
	BatchSize     int           `json:"batch_size"`
	FlushInterval time.Duration `json:"flush_interval"`

	// gRPC configuration
	GRPCEnabled  bool          `json:"grpc_enabled"`
	GRPCEndpoint string        `json:"grpc_endpoint"`
	GRPCTimeout  time.Duration `json:"grpc_timeout"`

	// Collector configurations
	Collectors []CollectorConfig `json:"collectors"`

	// Global filters
	Filters []EventFilter `json:"filters,omitempty"`
}

// Pipeline processes events through a series of stages
type Pipeline interface {
	// Add processing stages
	AddFilter(filter Filter) Pipeline
	AddTransformer(transformer Transformer) Pipeline
	AddHandler(handler Handler) Pipeline

	// Process an event through the pipeline
	Process(ctx context.Context, event *Event) error

	// Get pipeline statistics
	GetStats() PipelineStats
}

// Filter determines if an event should be processed
type Filter interface {
	Name() string
	Filter(event *Event) bool
	GetStats() FilterStats
}

// Transformer modifies events
type Transformer interface {
	Name() string
	Transform(event *Event) (*Event, error)
	GetStats() TransformerStats
}

// Handler processes events
type Handler interface {
	Name() string
	Handle(ctx context.Context, event *Event) error
	GetStats() HandlerStats
}

// PipelineStats provides pipeline performance metrics
type PipelineStats struct {
	EventsProcessed   uint64        `json:"events_processed"`
	EventsFiltered    uint64        `json:"events_filtered"`
	EventsTransformed uint64        `json:"events_transformed"`
	EventsHandled     uint64        `json:"events_handled"`
	ProcessingTime    time.Duration `json:"processing_time"`
	Errors            uint64        `json:"errors"`
}

// FilterStats provides filter performance metrics
type FilterStats struct {
	EventsEvaluated uint64 `json:"events_evaluated"`
	EventsPassed    uint64 `json:"events_passed"`
	EventsFiltered  uint64 `json:"events_filtered"`
}

// TransformerStats provides transformer performance metrics
type TransformerStats struct {
	EventsTransformed uint64        `json:"events_transformed"`
	TransformTime     time.Duration `json:"transform_time"`
	Errors            uint64        `json:"errors"`
}

// HandlerStats provides handler performance metrics
type HandlerStats struct {
	EventsHandled uint64        `json:"events_handled"`
	HandlingTime  time.Duration `json:"handling_time"`
	Errors        uint64        `json:"errors"`
}
