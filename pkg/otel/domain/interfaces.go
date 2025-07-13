// Package domain provides the core domain interfaces for OTEL integration
// showcasing hexagonal architecture, DDD patterns, and cutting-edge Go features.
package domain

import (
	"context"
	"fmt"
	"time"
	"unsafe"
)

// TraceData constraint defines valid trace data types using Go generics
type TraceData interface {
	~string | ~int | ~int8 | ~int16 | ~int32 | ~int64 |
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
	~float32 | ~float64 | ~bool |
	[]byte | map[string]any | []any
}

// SpanAttribute represents a type-safe span attribute with zero-allocation design
type SpanAttribute[T TraceData] struct {
	// Key stored as unsafe.Pointer for zero-allocation string access
	keyPtr unsafe.Pointer
	keyLen int

	// Value with type safety
	value T

	// Type information for efficient encoding
	valueType AttributeType

	// Memory arena reference for lifecycle management
	arena *ArenaRef
}

// Span represents a type-safe tracing span with generics
type Span[T TraceData] interface {
	// Core span operations
	SetAttribute(key string, value T) Span[T]
	SetAttributes(attrs map[string]T) Span[T]
	AddEvent(name string, attrs map[string]T) Span[T]
	RecordError(err error, attrs map[string]T) Span[T]
	
	// Status management
	SetStatus(code StatusCode, description string) Span[T]
	
	// Lifecycle
	End() SpanSnapshot[T]
	
	// Context integration
	Context() context.Context
	
	// Performance-critical methods (zero-allocation)
	SetAttributeUnsafe(keyPtr unsafe.Pointer, keyLen int, value T) Span[T]
	GetTraceID() TraceID
	GetSpanID() SpanID
	
	// Domain behavior
	IsRecording() bool
	IsRootSpan() bool
	GetParentSpanID() SpanID
	
	// Resource management
	GetArena() *ArenaRef
}

// Tracer creates and manages spans with type safety
type Tracer[T TraceData] interface {
	// Span creation with generics
	StartSpan(ctx context.Context, name string, opts ...SpanOption[T]) Span[T]
	StartSpanWithParent(ctx context.Context, parent Span[T], name string, opts ...SpanOption[T]) Span[T]
	
	// Zero-allocation span creation for hot paths
	StartSpanFromArena(arena *ArenaRef, ctx context.Context, name string) Span[T]
	
	// Batch operations for performance
	StartSpanBatch(ctx context.Context, requests []SpanRequest[T]) []Span[T]
	
	// Resource management
	GetTracerProvider() TracerProvider[T]
	GetInstrumentationScope() InstrumentationScope
}

// TracerProvider manages tracer instances and global configuration
type TracerProvider[T TraceData] interface {
	// Tracer management
	GetTracer(name string, opts ...TracerOption) Tracer[T]
	
	// Resource management
	RegisterSpanProcessor(processor SpanProcessor[T]) error
	RegisterSpanExporter(exporter SpanExporter[T]) error
	
	// Performance monitoring
	GetMetrics() TracerMetrics
	
	// Lifecycle
	Shutdown(ctx context.Context) error
	ForceFlush(ctx context.Context) error
}

// SpanProcessor defines the processing pipeline for spans
type SpanProcessor[T TraceData] interface {
	// Processing events
	OnStart(parent context.Context, span Span[T])
	OnEnd(span SpanSnapshot[T])
	
	// Batch processing for performance
	ProcessBatch(spans []SpanSnapshot[T]) error
	
	// Lifecycle
	Shutdown(ctx context.Context) error
	ForceFlush(ctx context.Context) error
	
	// Performance characteristics
	GetProcessingStats() ProcessingStats
}

// SpanExporter handles span export to external systems
type SpanExporter[T TraceData] interface {
	// Export operations
	ExportSpans(ctx context.Context, spans []SpanSnapshot[T]) error
	ExportSpansStream(ctx context.Context, spanCh <-chan SpanSnapshot[T]) error
	
	// Batch export optimization
	ExportSpansBatch(ctx context.Context, batches [][]SpanSnapshot[T]) error
	
	// Resource management
	Shutdown(ctx context.Context) error
	
	// Performance monitoring
	GetExportStats() ExportStats
}

// SpanSnapshot represents an immutable view of a completed span
type SpanSnapshot[T TraceData] interface {
	// Identity
	GetTraceID() TraceID
	GetSpanID() SpanID
	GetParentSpanID() SpanID
	
	// Metadata
	GetName() string
	GetKind() SpanKind
	GetStatus() SpanStatus
	
	// Timing
	GetStartTime() time.Time
	GetEndTime() time.Time
	GetDuration() time.Duration
	
	// Attributes and events
	GetAttributes() []SpanAttribute[T]
	GetEvents() []SpanEvent[T]
	GetLinks() []SpanLink[T]
	
	// Resource context
	GetResource() Resource
	GetInstrumentationScope() InstrumentationScope
	
	// Serialization (zero-allocation for hot paths)
	MarshalBinary() ([]byte, error)
	WriteBinaryTo(buf []byte) (int, error)
	
	// Memory management
	GetArena() *ArenaRef
	Release() // Return to pool
}

// PORTS - Hexagonal Architecture Interfaces

// TraceRepository defines the persistence port
type TraceRepository[T TraceData] interface {
	// CQRS Write operations
	StoreSpan(ctx context.Context, span SpanSnapshot[T]) error
	StoreSpanBatch(ctx context.Context, spans []SpanSnapshot[T]) error
	
	// CQRS Read operations
	GetSpan(ctx context.Context, traceID TraceID, spanID SpanID) (SpanSnapshot[T], error)
	GetTrace(ctx context.Context, traceID TraceID) ([]SpanSnapshot[T], error)
	QuerySpans(ctx context.Context, query SpanQuery) ([]SpanSnapshot[T], error)
	
	// Event Sourcing operations
	GetTraceEvents(ctx context.Context, traceID TraceID) ([]TraceEvent, error)
	AppendTraceEvent(ctx context.Context, event TraceEvent) error
	
	// Performance operations
	GetSpanStream(ctx context.Context, query SpanQuery) (<-chan SpanSnapshot[T], error)
}

// MetricsPort defines the metrics collection port
type MetricsPort interface {
	// Performance metrics
	RecordSpanCreated(duration time.Duration, attributes map[string]any)
	RecordSpanProcessed(processorName string, batchSize int, duration time.Duration)
	RecordSpanExported(exporterName string, spanCount int, success bool)
	
	// Resource metrics
	RecordMemoryUsage(arenaCount int, totalBytes int64)
	RecordGCPressure(allocations int64, gcTime time.Duration)
	
	// Error metrics
	RecordError(operation string, errorType string, count int64)
}

// ConfigurationPort defines the configuration port
type ConfigurationPort interface {
	// Service configuration
	GetServiceName() string
	GetServiceVersion() string
	GetEnvironment() string
	
	// Performance configuration
	GetSamplingRate() float64
	GetBatchSize() int
	GetExportTimeout() time.Duration
	GetMaxArenaSize() int64
	
	// Feature flags
	IsZeroAllocationEnabled() bool
	IsCustomEncodingEnabled() bool
	IsSIMDProcessingEnabled() bool
	
	// Dynamic configuration
	Subscribe(callback func(key string, value any))
	GetValue(key string) (any, bool)
}

// ADAPTERS - Implementation contracts

// TraceCollectorAdapter handles trace collection from external sources
type TraceCollectorAdapter[T TraceData] interface {
	// Collection operations
	CollectTraces(ctx context.Context) (<-chan RawTrace, error)
	CollectTracesFromSource(ctx context.Context, source TraceSource) (<-chan RawTrace, error)
	
	// Transformation
	TransformTrace(raw RawTrace) ([]SpanSnapshot[T], error)
	TransformTraceBatch(raw []RawTrace) ([]SpanSnapshot[T], error)
	
	// Configuration
	Configure(config CollectorConfig) error
	GetSupportedFormats() []TraceFormat
}

// TraceExporterAdapter handles trace export to external systems
type TraceExporterAdapter[T TraceData] interface {
	// Export operations
	ExportToJaeger(ctx context.Context, spans []SpanSnapshot[T]) error
	ExportToZipkin(ctx context.Context, spans []SpanSnapshot[T]) error
	ExportToOTLP(ctx context.Context, spans []SpanSnapshot[T]) error
	
	// Custom export
	ExportWithFormat(ctx context.Context, spans []SpanSnapshot[T], format ExportFormat) error
	ExportStream(ctx context.Context, spanCh <-chan SpanSnapshot[T], format ExportFormat) error
	
	// Performance optimization
	ExportBinary(ctx context.Context, data []byte) error
	ExportCompressed(ctx context.Context, data []byte, compression CompressionType) error
}

// DOMAIN SERVICES

// TraceCorrelationService handles trace correlation and analysis
type TraceCorrelationService[T TraceData] interface {
	// Correlation operations
	CorrelateTraces(ctx context.Context, traces []SpanSnapshot[T]) ([]TraceCorrelation, error)
	FindRelatedTraces(ctx context.Context, traceID TraceID) ([]TraceID, error)
	
	// Analysis operations
	AnalyzeTrace(ctx context.Context, traceID TraceID) (TraceAnalysis, error)
	DetectAnomalies(ctx context.Context, traces []SpanSnapshot[T]) ([]TraceAnomaly, error)
	
	// Performance analysis
	CalculateSpanMetrics(ctx context.Context, spans []SpanSnapshot[T]) (SpanMetrics, error)
	GenerateTraceInsights(ctx context.Context, timeRange TimeRange) (TraceInsights, error)
}

// TraceSamplingService handles intelligent sampling decisions
type TraceSamplingService[T TraceData] interface {
	// Sampling decisions
	ShouldSample(ctx context.Context, traceID TraceID, spanName string, attrs map[string]T) SamplingDecision
	ShouldSampleRoot(ctx context.Context, traceID TraceID, spanName string) SamplingDecision
	
	// Dynamic sampling
	UpdateSamplingRate(service string, operation string, rate float64)
	GetSamplingStats() SamplingStats
	
	// Advanced sampling strategies
	SampleByAttributes(attrs map[string]T, rules []SamplingRule) SamplingDecision
	SampleByPerformance(latency time.Duration, errorRate float64) SamplingDecision
}

// DOMAIN EVENTS

// TraceEventHandler processes trace events
type TraceEventHandler interface {
	// Event processing
	HandleEvent(ctx context.Context, event TraceEvent) error
	HandleEventBatch(ctx context.Context, events []TraceEvent) error
	
	// Event filtering
	CanHandle(eventType TraceEventType) bool
	GetHandlerPriority() int
}

// Supporting types and constraints

type (
	// Identity types with strong typing
	TraceID     [16]byte
	SpanID      [8]byte
	
	// Configuration types
	AttributeType    uint8
	StatusCode       uint8
	SpanKind         uint8
	ExportFormat     uint8
	CompressionType  uint8
	TraceFormat      uint8
	
	// Performance types
	ArenaRef struct {
		ptr  unsafe.Pointer
		size int64
		used int64
	}
	
	// Domain value objects
	SpanStatus struct {
		Code        StatusCode
		Description string
	}
	
	SpanEvent[T TraceData] struct {
		Name       string
		Timestamp  time.Time
		Attributes []SpanAttribute[T]
	}
	
	SpanLink[T TraceData] struct {
		TraceID    TraceID
		SpanID     SpanID
		Attributes []SpanAttribute[T]
	}
	
	Resource struct {
		Attributes map[string]any
		SchemaURL  string
	}
	
	InstrumentationScope struct {
		Name      string
		Version   string
		SchemaURL string
	}
	
	// Request/Response types
	SpanRequest[T TraceData] struct {
		Name       string
		Kind       SpanKind
		Attributes map[string]T
		Parent     context.Context
	}
	
	SpanOption[T TraceData] func(*SpanConfig[T])
	TracerOption              func(*TracerConfig)
	
	SpanConfig[T TraceData] struct {
		Kind       SpanKind
		Attributes map[string]T
		Links      []SpanLink[T]
		StartTime  time.Time
	}
	
	TracerConfig struct {
		Version   string
		SchemaURL string
		Scope     InstrumentationScope
	}
	
	// Query types for CQRS
	SpanQuery struct {
		TraceID     *TraceID
		ServiceName *string
		Operation   *string
		TimeRange   *TimeRange
		Attributes  map[string]any
		Limit       int
		Offset      int
	}
	
	TimeRange struct {
		Start time.Time
		End   time.Time
	}
	
	// Statistics and metrics
	TracerMetrics struct {
		SpansCreated    int64
		SpansEnded      int64
		SpansExported   int64
		SpansDropped    int64
		AverageLatency  time.Duration
		MemoryUsage     int64
		GCPressure      float64
	}
	
	ProcessingStats struct {
		SpansProcessed   int64
		BatchesProcessed int64
		ProcessingTime   time.Duration
		QueueDepth       int
		ErrorCount       int64
	}
	
	ExportStats struct {
		ExportsAttempted int64
		ExportsSucceeded int64
		ExportsFailed    int64
		BytesExported    int64
		AverageLatency   time.Duration
	}
	
	SamplingStats struct {
		SamplesEvaluated int64
		SamplesAccepted  int64
		SamplesRejected  int64
		SamplingRate     float64
	}
	
	// Analysis types
	TraceCorrelation struct {
		PrimaryTraceID   TraceID
		RelatedTraceIDs  []TraceID
		CorrelationType  string
		Confidence       float64
	}
	
	TraceAnalysis struct {
		TraceID         TraceID
		TotalDuration   time.Duration
		CriticalPath    []SpanID
		BottleneckSpans []SpanID
		ErrorSpans      []SpanID
		Performance     PerformanceAnalysis
	}
	
	TraceAnomaly struct {
		TraceID     TraceID
		SpanID      SpanID
		AnomalyType string
		Severity    float64
		Description string
		Timestamp   time.Time
	}
	
	SpanMetrics struct {
		Count           int64
		AverageDuration time.Duration
		P50Duration     time.Duration
		P95Duration     time.Duration
		P99Duration     time.Duration
		ErrorRate       float64
	}
	
	TraceInsights struct {
		TimeRange       TimeRange
		TotalTraces     int64
		TotalSpans      int64
		TopOperations   []OperationStats
		ErrorAnalysis   ErrorAnalysis
		PerformanceAnalysis PerformanceAnalysis
	}
	
	OperationStats struct {
		Name          string
		Count         int64
		AverageLatency time.Duration
		ErrorRate     float64
	}
	
	ErrorAnalysis struct {
		TotalErrors      int64
		ErrorsByService  map[string]int64
		ErrorsByType     map[string]int64
		TopErrorMessages []string
	}
	
	PerformanceAnalysis struct {
		SlowestTraces    []TraceID
		FastestTraces    []TraceID
		AverageLatency   time.Duration
		LatencyDistribution map[string]int64
	}
	
	// Sampling types
	SamplingDecision struct {
		Sample     bool
		Rate       float64
		Attributes map[string]any
		Reason     string
	}
	
	// Additional types for ports compatibility
	TraceAggregateView[T TraceData] struct {
		TraceID   TraceID
		Spans     []SpanSnapshot[T]
		Duration  time.Duration
		Status    string
	}
	
	TraceMetrics struct {
		TotalSpans    int64
		TotalTraces   int64
		AverageLatency time.Duration
		ErrorRate     float64
	}
	
	ServiceHealth struct {
		ServiceName   string
		HealthStatus  string
		LastChecked   time.Time
		Issues        []string
	}
	
	PerformanceMetrics struct {
		Throughput    int64
		Latency       time.Duration
		ErrorRate     float64
		MemoryUsage   int64
	}
	
	SagaStartRequest struct {
		SagaID        string
		SagaType      string
		InitialData   map[string]any
	}
	
	ServiceHealthStatus struct {
		ServiceID     string
		Status        string
		LastUpdate    time.Time
		Metrics       PerformanceMetrics
	}
	
	HealthUpdate struct {
		ServiceID     string
		Status        string
		Timestamp     time.Time
		Details       map[string]any
	}
	
	SortCriteria struct {
		Field     string
		Direction string
	}
	
	// Additional missing types for ports compatibility
	SagaUpdate[T TraceData] struct {
		SagaID      string
		UpdateType  string
		Data        map[string]T
		Timestamp   time.Time
	}
	
	SagaCompletionResult[T TraceData] struct {
		SagaID      string
		Success     bool
		Results     map[string]T
		Duration    time.Duration
		CompletedAt time.Time
	}
	
	TraceStatistics struct {
		TotalTraces      int64
		TotalSpans       int64
		AverageLatency   time.Duration
		ErrorRate        float64
		ServicesCount    int64
	}
	
	AnalyticsQuery struct {
		ServiceNames  []string
		Operations    []string
		TimeRange     TimeRange
		Filters       map[string]any
		GroupBy       []string
		Metrics       []string
	}
	
	SpanAnalytics struct {
		TotalSpans      int64
		AverageLatency  time.Duration
		P50Latency      time.Duration
		P95Latency      time.Duration
		P99Latency      time.Duration
		ErrorRate       float64
		ByService       map[string]SpanServiceMetrics
	}
	
	SpanServiceMetrics struct {
		ServiceName     string
		SpanCount       int64
		AverageLatency  time.Duration
		ErrorCount      int64
	}
	
	TraceStreamEvent[T TraceData] struct {
		EventType   string
		TraceID     TraceID
		SpanID      *SpanID
		Data        T
		Timestamp   time.Time
	}
	
	ProcessingError struct {
		ErrorType   string
		Message     string
		SpanID      *SpanID
		TraceID     *TraceID
		Timestamp   time.Time
		RetryCount  int
	}
	
	TraceInfo struct {
		TraceID       TraceID
		ServiceName   string
		RootOperation string
		Duration      time.Duration
		SpanCount     int64
		ErrorCount    int64
		StartTime     time.Time
		EndTime       time.Time
	}
	
	SamplingRule struct {
		Service    string
		Operation  string
		Attributes map[string]any
		Rate       float64
		Priority   int
	}
	
	// External data types
	RawTrace struct {
		Data   []byte
		Format TraceFormat
		Source string
		Metadata map[string]any
	}
	
	TraceSource struct {
		Name     string
		Type     string
		Endpoint string
		Config   map[string]any
	}
	
	CollectorConfig struct {
		Sources    []TraceSource
		BatchSize  int
		Timeout    time.Duration
		BufferSize int
	}
)

// Constants for type safety

const (
	// Attribute types
	AttributeTypeString AttributeType = iota
	AttributeTypeInt
	AttributeTypeFloat
	AttributeTypeBool
	AttributeTypeBytes
	AttributeTypeArray
	AttributeTypeMap
)

const (
	// Status codes
	StatusCodeUnset StatusCode = iota
	StatusCodeOK
	StatusCodeError
)

const (
	// Span kinds
	SpanKindInternal SpanKind = iota
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)


const (
	// Export formats
	ExportFormatOTLP ExportFormat = iota
	ExportFormatJaeger
	ExportFormatZipkin
	ExportFormatCustomBinary
)

const (
	// Compression types
	CompressionTypeNone CompressionType = iota
	CompressionTypeGzip
	CompressionTypeZstd
	CompressionTypeLZ4
)

const (
	// Trace formats
	TraceFormatOTLP TraceFormat = iota
	TraceFormatJaeger
	TraceFormatZipkin
	TraceFormatB3
	TraceFormatCustom
)

// Arena size constants for memory management
const (
	DefaultArenaSize = 64 * 1024      // 64KB
	MaxArenaSize     = 1024 * 1024    // 1MB
	MinArenaSize     = 4 * 1024       // 4KB
)

// String methods for ID types
func (t TraceID) String() string {
	return fmt.Sprintf("%x", t[:])
}

func (s SpanID) String() string {
	return fmt.Sprintf("%x", s[:])
}