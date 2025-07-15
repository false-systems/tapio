// Package metrics provides enterprise-grade Prometheus integration with advanced Go patterns
// featuring factory pattern, observer pattern, and memory-efficient metric streaming.
package metrics

import (
	"context"
	"io"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// MetricClient defines the interface for different Prometheus client types using strategy pattern
type MetricClient[T MetricType] interface {
	// Register registers metrics with the Prometheus registry
	Register(ctx context.Context, metrics []T) error

	// Push pushes metrics to a gateway or collector
	Push(ctx context.Context, metrics []T) error

	// Stream provides real-time metric streaming
	Stream(ctx context.Context, opts StreamOptions) (<-chan MetricEvent[T], error)

	// Collect collects metrics for scraping
	Collect(ctx context.Context) ([]T, error)

	// Health returns client health status
	Health() ClientHealth

	// Close gracefully shuts down the client
	Close(ctx context.Context) error
}

// MetricFactory creates different types of Prometheus clients using factory pattern
type MetricFactory interface {
	// CreatePushClient creates a push-based client for gateways
	CreatePushClient(config PushClientConfig) (MetricClient[PushMetric], error)

	// CreatePullClient creates a pull-based client for scraping
	CreatePullClient(config PullClientConfig) (MetricClient[PullMetric], error)

	// CreateStreamClient creates a streaming client for real-time metrics
	CreateStreamClient(config StreamClientConfig) (MetricClient[StreamMetric], error)

	// CreateCollectorClient creates a custom collector client
	CreateCollectorClient(config CollectorConfig) (MetricClient[CustomMetric], error)

	// GetRegisteredClients returns all registered clients
	GetRegisteredClients() []RegisteredClient

	// Shutdown gracefully shuts down all clients
	Shutdown(ctx context.Context) error
}

// MetricObserver defines the observer pattern for real-time metric updates
type MetricObserver[T MetricType] interface {
	// OnMetricCreated is called when a new metric is created
	OnMetricCreated(ctx context.Context, metric T) error

	// OnMetricUpdated is called when a metric value changes
	OnMetricUpdated(ctx context.Context, metric T, oldValue, newValue interface{}) error

	// OnMetricDeleted is called when a metric is removed
	OnMetricDeleted(ctx context.Context, metric T) error

	// OnError is called when an error occurs during metric processing
	OnError(ctx context.Context, err error, metric T) error

	// GetID returns unique observer identifier
	GetID() string

	// GetPriority returns observer priority for ordering
	GetPriority() ObserverPriority
}

// MetricPublisher manages observers and publishes metric events
type MetricPublisher[T MetricType] interface {
	// Subscribe adds an observer
	Subscribe(observer MetricObserver[T]) error

	// Unsubscribe removes an observer
	Unsubscribe(observerID string) error

	// Publish publishes a metric event to all observers
	Publish(ctx context.Context, event MetricEvent[T]) error

	// PublishBatch publishes multiple events efficiently
	PublishBatch(ctx context.Context, events []MetricEvent[T]) error

	// GetObservers returns all registered observers
	GetObservers() []MetricObserver[T]

	// SetEventBuffer configures event buffering for performance
	SetEventBuffer(size int, flushInterval time.Duration) error
}

// MetricCollector defines advanced metric collection with rate limiting
type MetricCollector[T MetricType] interface {
	// Collect collects metrics with rate limiting and backpressure
	Collect(ctx context.Context, opts CollectionOptions) (<-chan CollectionResult[T], error)

	// CollectBatch collects metrics in batches for efficiency
	CollectBatch(ctx context.Context, batchSize int, opts CollectionOptions) (<-chan BatchResult[T], error)

	// SetRateLimit configures collection rate limiting
	SetRateLimit(requestsPerSecond float64, burstSize int) error

	// SetBackpressure configures backpressure handling
	SetBackpressure(strategy BackpressureStrategy, options BackpressureOptions) error

	// GetStats returns collection performance statistics
	GetStats() CollectorStats

	// Reset resets collector state and statistics
	Reset() error
}

// MetricStreamer provides memory-efficient metric streaming
type MetricStreamer[T MetricType] interface {
	// StartStream starts streaming metrics with context propagation
	StartStream(ctx context.Context, opts StreamOptions) (<-chan StreamResult[T], error)

	// StopStream stops an active stream
	StopStream(streamID string) error

	// GetActiveStreams returns information about active streams
	GetActiveStreams() []StreamInfo

	// SetBuffering configures stream buffering strategy
	SetBuffering(strategy BufferingStrategy, options BufferingOptions) error

	// SetCompression configures stream compression
	SetCompression(enabled bool, algorithm CompressionAlgorithm) error
}

// MetricRegistry provides type-safe metric registration with Go generics
type MetricRegistry[T MetricType] interface {
	// Register registers a metric with type safety
	Register(metric T) error

	// RegisterWithLabels registers a metric with labels
	RegisterWithLabels(metric T, labels Labels) error

	// Unregister removes a metric
	Unregister(metricName string) error

	// Get retrieves a registered metric
	Get(metricName string) (T, bool)

	// List returns all registered metrics
	List() []T

	// Count returns the number of registered metrics
	Count() int

	// Validate validates metric definitions
	Validate(metric T) error
}

// MetricExporter handles metric export to various formats
type MetricExporter interface {
	// Export exports metrics to the specified writer
	Export(ctx context.Context, writer io.Writer, metrics []MetricType, format ExportFormat) error

	// ExportBatch exports metrics in batches for large datasets
	ExportBatch(ctx context.Context, writer io.Writer, batches <-chan []MetricType, format ExportFormat) error

	// GetSupportedFormats returns supported export formats
	GetSupportedFormats() []ExportFormat

	// SetCompressionLevel configures export compression
	SetCompressionLevel(level CompressionLevel) error
}

// MetricValidator validates metric definitions and values
type MetricValidator[T MetricType] interface {
	// ValidateDefinition validates metric definition
	ValidateDefinition(metric T) ValidationResult

	// ValidateValue validates metric value
	ValidateValue(metric T, value interface{}) ValidationResult

	// ValidateBatch validates multiple metrics efficiently
	ValidateBatch(metrics []T) []ValidationResult

	// GetValidationRules returns active validation rules
	GetValidationRules() []ValidationRule

	// AddValidationRule adds a custom validation rule
	AddValidationRule(rule ValidationRule) error
}

// MetricType represents different types of metrics using Go generics
type MetricType interface {
	// GetName returns metric name
	GetName() string

	// GetType returns metric type (counter, gauge, histogram, summary)
	GetType() string

	// GetLabels returns metric labels
	GetLabels() Labels

	// GetValue returns current metric value
	GetValue() interface{}

	// GetTimestamp returns metric timestamp
	GetTimestamp() time.Time

	// GetMetadata returns metric metadata
	GetMetadata() map[string]interface{}

	// Validate validates the metric
	Validate() error
}

// Specific metric type implementations
type (
	// PushMetric for push-based clients
	PushMetric interface {
		MetricType
		GetGatewayURL() string
		GetJobName() string
		GetInstance() string
	}

	// PullMetric for pull-based clients
	PullMetric interface {
		MetricType
		prometheus.Metric
		GetScrapeInterval() time.Duration
		GetScrapeTimeout() time.Duration
	}

	// StreamMetric for streaming clients
	StreamMetric interface {
		MetricType
		GetStreamID() string
		GetBufferSize() int
		IsRealTime() bool
	}

	// CustomMetric for custom collectors
	CustomMetric interface {
		MetricType
		GetCollector() prometheus.Collector
		GetCollectionInterval() time.Duration
	}
)

// Configuration types
type (
	// PushClientConfig configures push clients
	PushClientConfig struct {
		GatewayURL     string
		JobName        string
		Instance       string
		Timeout        time.Duration
		RetryAttempts  int
		RetryBackoff   time.Duration
		BasicAuth      *BasicAuth
		TLSConfig      *TLSConfig
		Headers        map[string]string
		Compression    CompressionConfig
		RateLimiting   RateLimitConfig
		CircuitBreaker CircuitBreakerConfig
	}

	// PullClientConfig configures pull clients
	PullClientConfig struct {
		ListenAddress  string
		ListenPort     int
		MetricsPath    string
		ScrapeInterval time.Duration
		ScrapeTimeout  time.Duration
		TLSConfig      *TLSConfig
		Authentication AuthConfig
		Registry       *prometheus.Registry
		Gatherer       prometheus.Gatherer
		MaxConnections int
	}

	// StreamClientConfig configures streaming clients
	StreamClientConfig struct {
		StreamEndpoint   string
		BufferSize       int
		FlushInterval    time.Duration
		Compression      CompressionConfig
		Batching         BatchingConfig
		BackpressureMode BackpressureMode
		Encryption       EncryptionConfig
		HealthCheck      HealthCheckConfig
	}

	// CollectorConfig configures custom collectors
	CollectorConfig struct {
		CollectorName      string
		CollectionFunc     func(context.Context) ([]CustomMetric, error)
		CollectionInterval time.Duration
		ErrorStrategy      ErrorStrategy
		MemoryLimit        int64
		TimeoutConfig      TimeoutConfig
	}
)

// Event and result types
type (
	// MetricEvent represents a metric event for observers
	MetricEvent[T MetricType] struct {
		Type      EventType
		Metric    T
		OldValue  interface{}
		NewValue  interface{}
		Timestamp time.Time
		Source    string
		Context   map[string]interface{}
	}

	// CollectionResult represents collection results
	CollectionResult[T MetricType] struct {
		Metrics   []T
		Error     error
		Duration  time.Duration
		Timestamp time.Time
		Source    string
		Metadata  map[string]interface{}
	}

	// BatchResult represents batch collection results
	BatchResult[T MetricType] struct {
		Batch     []T
		BatchSize int
		Error     error
		Duration  time.Duration
		Timestamp time.Time
		Sequence  int64
	}

	// StreamResult represents streaming results
	StreamResult[T MetricType] struct {
		Metrics     []T
		StreamID    string
		Error       error
		Timestamp   time.Time
		Sequence    int64
		EndOfStream bool
	}

	// StreamInfo provides information about active streams
	StreamInfo struct {
		StreamID    string
		StartTime   time.Time
		MetricCount int64
		BytesSent   int64
		ErrorCount  int64
		Status      StreamStatus
		Options     StreamOptions
	}

	// RegisteredClient information
	RegisteredClient struct {
		ID      string
		Type    ClientType
		Config  interface{}
		Health  ClientHealth
		Stats   ClientStats
		Created time.Time
	}

	// ValidationResult represents validation outcome
	ValidationResult struct {
		Valid    bool
		Errors   []ValidationError
		Warnings []ValidationWarning
		Score    float64
	}
)

// Supporting types and enums
type (
	EventType            string
	ObserverPriority     string
	BackpressureStrategy string
	BackpressureMode     string
	BufferingStrategy    string
	CompressionAlgorithm string
	CompressionLevel     string
	ExportFormat         string
	ClientType           string
	StreamStatus         string
	ErrorStrategy        string
	Labels               map[string]string
)

// Options and configuration structures
type (
	StreamOptions struct {
		BufferSize      int
		FlushInterval   time.Duration
		EnableBatching  bool
		BatchSize       int
		Compression     bool
		IncludeMetadata bool
		FilterFunc      func(MetricType) bool
		TransformFunc   func(MetricType) MetricType
		ErrorHandler    func(error) bool
	}

	CollectionOptions struct {
		Timeout         time.Duration
		MaxMetrics      int
		EnableFiltering bool
		FilterFunc      func(MetricType) bool
		SortBy          string
		SortOrder       string
		IncludeMetadata bool
	}

	BackpressureOptions struct {
		MaxBufferSize   int
		DropStrategy    string
		AlertThreshold  float64
		RecoveryTimeout time.Duration
		MetricsCallback func(BackpressureStats)
	}

	BufferingOptions struct {
		BufferSize      int
		FlushInterval   time.Duration
		FlushThreshold  int
		MemoryLimit     int64
		DiskSpillover   bool
		CompressionMode string
	}

	// Authentication and security
	BasicAuth struct {
		Username string
		Password string
	}

	TLSConfig struct {
		CertFile           string
		KeyFile            string
		CAFile             string
		InsecureSkipVerify bool
		ServerName         string
	}

	AuthConfig struct {
		Type     string
		Config   map[string]string
		Enabled  bool
		Required bool
	}

	EncryptionConfig struct {
		Enabled   bool
		Algorithm string
		KeyFile   string
		CertFile  string
	}

	// Performance and reliability
	RateLimitConfig struct {
		RequestsPerSecond float64
		BurstSize         int
		Algorithm         string
		Enabled           bool
	}

	CircuitBreakerConfig struct {
		FailureThreshold int
		RecoveryTimeout  time.Duration
		TestInterval     time.Duration
		Enabled          bool
	}

	CompressionConfig struct {
		Enabled   bool
		Algorithm string
		Level     int
		MinSize   int64
	}

	BatchingConfig struct {
		Enabled       bool
		MaxBatchSize  int
		FlushInterval time.Duration
		MemoryLimit   int64
	}

	HealthCheckConfig struct {
		Enabled          bool
		Interval         time.Duration
		Timeout          time.Duration
		FailureThreshold int
		Endpoint         string
	}

	TimeoutConfig struct {
		ConnectionTimeout time.Duration
		RequestTimeout    time.Duration
		ShutdownTimeout   time.Duration
	}

	// Statistics and monitoring
	ClientHealth struct {
		Status      string
		LastCheck   time.Time
		ErrorCount  int64
		Uptime      time.Duration
		Version     string
		Connections int
	}

	ClientStats struct {
		RequestCount     int64
		ErrorCount       int64
		BytesTransferred int64
		AverageLatency   time.Duration
		LastRequest      time.Time
		Uptime           time.Duration
	}

	CollectorStats struct {
		CollectionCount  int64
		MetricsCollected int64
		ErrorCount       int64
		RateLimit        float64
		BackpressureHits int64
		AverageLatency   time.Duration
		LastCollection   time.Time
	}

	BackpressureStats struct {
		BufferUtilization float64
		DroppedEvents     int64
		DelayedEvents     int64
		RecoveryTime      time.Duration
		Status            string
	}

	ValidationError struct {
		Field   string
		Message string
		Code    string
		Value   interface{}
	}

	ValidationWarning struct {
		Field   string
		Message string
		Code    string
		Value   interface{}
	}

	ValidationRule struct {
		Name        string
		Description string
		Validator   func(MetricType) ValidationResult
		Priority    int
		Enabled     bool
	}
)

// Constants for event types
const (
	EventTypeCreated EventType = "created"
	EventTypeUpdated EventType = "updated"
	EventTypeDeleted EventType = "deleted"
	EventTypeError   EventType = "error"
)

// Constants for observer priorities
const (
	ObserverPriorityHigh   ObserverPriority = "high"
	ObserverPriorityMedium ObserverPriority = "medium"
	ObserverPriorityLow    ObserverPriority = "low"
)

// Constants for backpressure strategies
const (
	BackpressureStrategyDrop     BackpressureStrategy = "drop"
	BackpressureStrategyBuffer   BackpressureStrategy = "buffer"
	BackpressureStrategyBlock    BackpressureStrategy = "block"
	BackpressureStrategyAdaptive BackpressureStrategy = "adaptive"
)

// Constants for export formats
const (
	ExportFormatPrometheus  ExportFormat = "prometheus"
	ExportFormatJSON        ExportFormat = "json"
	ExportFormatCSV         ExportFormat = "csv"
	ExportFormatOpenMetrics ExportFormat = "openmetrics"
)

// Constants for client types
const (
	ClientTypePush      ClientType = "push"
	ClientTypePull      ClientType = "pull"
	ClientTypeStream    ClientType = "stream"
	ClientTypeCollector ClientType = "collector"
)

// Constants for stream status
const (
	StreamStatusActive  StreamStatus = "active"
	StreamStatusPaused  StreamStatus = "paused"
	StreamStatusStopped StreamStatus = "stopped"
	StreamStatusError   StreamStatus = "error"
)
