// Package ports defines the hexagonal architecture ports for OTEL integration
// showcasing clean architecture with proper separation of concerns and dependency inversion.
package ports

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/integrations/otel/domain"
)

// PRIMARY PORTS - Driving side (inbound)
// These ports are implemented by the application core and called by external actors

// TraceApplicationService defines the primary port for trace operations
// This is the main entry point for all trace-related use cases
type TraceApplicationService[T domain.TraceData] interface {
	// Core trace operations
	StartTrace(ctx context.Context, request StartTraceRequest[T]) (*TraceSession[T], error)
	EndTrace(ctx context.Context, session *TraceSession[T]) (domain.SpanSnapshot[T], error)

	// Span lifecycle management
	CreateSpan(ctx context.Context, request CreateSpanRequest[T]) (domain.Span[T], error)
	UpdateSpan(ctx context.Context, spanID domain.SpanID, updates SpanUpdates[T]) error
	FinishSpan(ctx context.Context, spanID domain.SpanID) (domain.SpanSnapshot[T], error)

	// Batch operations for performance
	CreateSpanBatch(ctx context.Context, requests []CreateSpanRequest[T]) ([]domain.Span[T], error)
	ProcessSpanBatch(ctx context.Context, spans []domain.SpanSnapshot[T]) (*BatchProcessingResult, error)

	// Query operations (CQRS read side)
	GetTrace(ctx context.Context, traceID domain.TraceID) (*domain.TraceAggregateView[T], error)
	QuerySpans(ctx context.Context, query SpanQuery) (*SpanQueryResult[T], error)
	GetTraceMetrics(ctx context.Context, filter MetricsFilter) (*domain.TraceMetrics, error)

	// Health and monitoring
	GetServiceHealth(ctx context.Context) (*domain.ServiceHealth, error)
	GetPerformanceMetrics(ctx context.Context) (*domain.PerformanceMetrics, error)
}

// TraceCommandService handles command operations (CQRS write side)
type TraceCommandService[T domain.TraceData] interface {
	// Command handling
	ExecuteCommand(ctx context.Context, cmd TraceCommand[T]) (*CommandResult[T], error)
	ExecuteCommandBatch(ctx context.Context, commands []TraceCommand[T]) (*BatchCommandResult[T], error)

	// Event handling
	HandleTraceEvent(ctx context.Context, event domain.TraceEvent) error
	HandleEventBatch(ctx context.Context, events []domain.TraceEvent) error

	// Saga management for distributed traces
	StartTraceSaga(ctx context.Context, sagaID string, request domain.SagaStartRequest) error
	UpdateTraceSaga(ctx context.Context, sagaID string, update domain.SagaUpdate[T]) error
	CompleteTraceSaga(ctx context.Context, sagaID string) (*domain.SagaCompletionResult[T], error)
}

// TraceQueryService handles query operations (CQRS read side)
type TraceQueryService[T domain.TraceData] interface {
	// Read operations
	FindTracesByFilter(ctx context.Context, filter TraceFilter) (*TraceSearchResult[T], error)
	GetTraceStatistics(ctx context.Context, timeRange domain.TimeRange) (*domain.TraceStatistics, error)
	GetSpanAnalytics(ctx context.Context, analyticsQuery domain.AnalyticsQuery) (*domain.SpanAnalytics, error)

	// Real-time queries
	StreamTraces(ctx context.Context, filter TraceFilter) (<-chan domain.TraceStreamEvent[T], error)
	StreamSpanUpdates(ctx context.Context, traceID domain.TraceID) (<-chan SpanUpdateEvent[T], error)

	// Materialized views
	GetTraceAggregateView(ctx context.Context, traceID domain.TraceID) (*domain.TraceAggregateView[T], error)
	RefreshMaterializedViews(ctx context.Context, viewTypes []ViewType) error
}

// SECONDARY PORTS - Driven side (outbound)
// These ports are called by the application core and implemented by external adapters

// TraceRepositoryPort defines persistence operations
type TraceRepositoryPort[T domain.TraceData] interface {
	// Basic CRUD operations
	SaveSpan(ctx context.Context, span domain.SpanSnapshot[T]) error
	SaveSpanBatch(ctx context.Context, spans []domain.SpanSnapshot[T]) error
	GetSpan(ctx context.Context, traceID domain.TraceID, spanID domain.SpanID) (domain.SpanSnapshot[T], error)
	DeleteSpan(ctx context.Context, traceID domain.TraceID, spanID domain.SpanID) error

	// Advanced queries
	FindSpans(ctx context.Context, query SpanQuery) ([]domain.SpanSnapshot[T], error)
	FindTraces(ctx context.Context, filter TraceFilter) ([]domain.TraceInfo, error)
	CountSpans(ctx context.Context, filter SpanFilter) (int64, error)

	// Event sourcing support
	AppendTraceEvents(ctx context.Context, traceID domain.TraceID, events []domain.TraceEvent) error
	GetTraceEvents(ctx context.Context, traceID domain.TraceID, fromVersion int64) ([]domain.TraceEvent, error)

	// Streaming support
	StreamSpans(ctx context.Context, query SpanQuery) (<-chan domain.SpanSnapshot[T], error)

	// Maintenance operations
	CompactTrace(ctx context.Context, traceID domain.TraceID) error
	ArchiveOldTraces(ctx context.Context, cutoffTime time.Time) (*ArchiveResult, error)
}

// TraceEventStorePort defines event store operations for event sourcing
type TraceEventStorePort interface {
	// Event persistence
	AppendEvents(ctx context.Context, streamID string, expectedVersion int64, events []domain.TraceEvent) error
	ReadEvents(ctx context.Context, streamID string, fromVersion int64, maxCount int) ([]domain.TraceEvent, error)
	ReadEventsForward(ctx context.Context, streamID string, fromVersion int64) (<-chan domain.TraceEvent, error)

	// Stream management
	CreateStream(ctx context.Context, streamID string, metadata map[string]any) error
	GetStreamMetadata(ctx context.Context, streamID string) (*StreamMetadata, error)
	DeleteStream(ctx context.Context, streamID string) error

	// Projections
	RegisterProjection(projection EventProjection) error
	UpdateProjection(ctx context.Context, projectionName string, fromVersion int64) error
	GetProjectionState(ctx context.Context, projectionName string) (*ProjectionState, error)

	// Subscriptions
	SubscribeToStream(ctx context.Context, streamID string, fromVersion int64) (<-chan domain.TraceEvent, error)
	SubscribeToAll(ctx context.Context, fromPosition int64) (<-chan domain.TraceEvent, error)
}

// TraceCachePort defines caching operations for performance
type TraceCachePort[T domain.TraceData] interface {
	// Span caching
	GetSpan(ctx context.Context, key SpanCacheKey) (domain.SpanSnapshot[T], error)
	SetSpan(ctx context.Context, key SpanCacheKey, span domain.SpanSnapshot[T], ttl time.Duration) error
	DeleteSpan(ctx context.Context, key SpanCacheKey) error

	// Trace caching
	GetTrace(ctx context.Context, traceID domain.TraceID) (*domain.TraceAggregateView[T], error)
	SetTrace(ctx context.Context, traceID domain.TraceID, trace *domain.TraceAggregateView[T], ttl time.Duration) error
	InvalidateTrace(ctx context.Context, traceID domain.TraceID) error

	// Bulk operations
	GetSpanBatch(ctx context.Context, keys []SpanCacheKey) (map[SpanCacheKey]domain.SpanSnapshot[T], error)
	SetSpanBatch(ctx context.Context, items map[SpanCacheKey]CacheItem[T]) error
	InvalidateBatch(ctx context.Context, keys []SpanCacheKey) error

	// Cache management
	GetCacheStats(ctx context.Context) (*CacheStats, error)
	ClearCache(ctx context.Context, pattern string) error
	WarmupCache(ctx context.Context, requests []WarmupRequest) error
}

// TracePublisherPort defines event publishing for integration
type TracePublisherPort interface {
	// Event publishing
	PublishTraceEvent(ctx context.Context, event TraceEventMessage) error
	PublishTraceEventBatch(ctx context.Context, events []TraceEventMessage) error

	// Stream publishing
	PublishToStream(ctx context.Context, stream string, event TraceEventMessage) error
	PublishToTopic(ctx context.Context, topic string, event TraceEventMessage) error

	// Notification publishing
	PublishNotification(ctx context.Context, notification TraceNotification) error
	PublishAlert(ctx context.Context, alert TraceAlert) error

	// Configuration
	RegisterEventHandler(eventType string, handler EventHandler) error
	UnregisterEventHandler(eventType string) error
	GetPublisherHealth(ctx context.Context) (*PublisherHealth, error)
}

// MetricsCollectorPort defines metrics collection operations
type MetricsCollectorPort interface {
	// Performance metrics
	RecordSpanCreated(ctx context.Context, duration time.Duration, labels map[string]string)
	RecordSpanProcessed(ctx context.Context, processingTime time.Duration, success bool)
	RecordTraceCompleted(ctx context.Context, traceInfo TraceCompletionInfo)

	// Resource metrics
	RecordMemoryUsage(ctx context.Context, usage MemoryUsage)
	RecordCacheMetrics(ctx context.Context, metrics CacheMetrics)
	RecordRepositoryMetrics(ctx context.Context, operation string, duration time.Duration, success bool)

	// Business metrics
	RecordUserAction(ctx context.Context, action UserAction)
	RecordServiceCall(ctx context.Context, call ServiceCall)
	RecordErrorOccurred(ctx context.Context, error ErrorInfo)

	// Aggregated metrics
	GetMetricsSummary(ctx context.Context, timeRange domain.TimeRange) (*MetricsSummary, error)
	ExportMetrics(ctx context.Context, format MetricsFormat) ([]byte, error)
}

// ConfigurationPort defines configuration management
type ConfigurationPort interface {
	// Configuration retrieval
	GetConfiguration(ctx context.Context, key string) (ConfigValue, error)
	GetConfigurationBatch(ctx context.Context, keys []string) (map[string]ConfigValue, error)

	// Dynamic configuration
	WatchConfiguration(ctx context.Context, key string) (<-chan ConfigChange, error)
	UpdateConfiguration(ctx context.Context, key string, value ConfigValue) error

	// Feature flags
	IsFeatureEnabled(ctx context.Context, feature string) (bool, error)
	GetFeatureFlags(ctx context.Context) (map[string]bool, error)

	// Environment specific
	GetEnvironmentConfig(ctx context.Context) (*EnvironmentConfig, error)
	ValidateConfiguration(ctx context.Context, config map[string]ConfigValue) (*ValidationResult, error)
}

// LoggingPort defines structured logging operations
type LoggingPort interface {
	// Structured logging
	LogTrace(ctx context.Context, event TraceLogEvent)
	LogSpan(ctx context.Context, event SpanLogEvent)
	LogError(ctx context.Context, error ErrorLogEvent)

	// Contextual logging
	WithContext(ctx context.Context) LoggingPort
	WithFields(fields map[string]any) LoggingPort
	WithTraceID(traceID domain.TraceID) LoggingPort

	// Log streaming
	StreamLogs(ctx context.Context, filter LogFilter) (<-chan LogEntry, error)

	// Log analysis
	AnalyzeLogs(ctx context.Context, query LogAnalysisQuery) (*LogAnalysisResult, error)
}

// ExternalServicePort defines integration with external services
type ExternalServicePort interface {
	// Service discovery
	DiscoverServices(ctx context.Context, criteria DiscoveryCriteria) ([]ServiceInfo, error)
	RegisterService(ctx context.Context, service ServiceRegistration) error
	DeregisterService(ctx context.Context, serviceID string) error

	// Health checks
	CheckServiceHealth(ctx context.Context, serviceID string) (*domain.ServiceHealthStatus, error)
	MonitorServiceHealth(ctx context.Context, serviceID string) (<-chan domain.HealthUpdate, error)

	// Load balancing
	SelectService(ctx context.Context, serviceName string, strategy LoadBalancingStrategy) (*ServiceEndpoint, error)
	ReportServiceMetrics(ctx context.Context, serviceID string, metrics ServiceMetrics) error

	// Circuit breaker
	CallWithCircuitBreaker(ctx context.Context, serviceID string, call func() error) error
	GetCircuitBreakerState(ctx context.Context, serviceID string) (*CircuitBreakerState, error)
}

// Supporting types for hexagonal architecture

// Primary port request/response types
type StartTraceRequest[T domain.TraceData] struct {
	TraceName        string
	ServiceName      string
	SpanKind         domain.SpanKind
	Attributes       map[string]T
	ParentContext    context.Context
	SamplingDecision *domain.SamplingDecision
	Deadline         *time.Time
}

type CreateSpanRequest[T domain.TraceData] struct {
	TraceID      domain.TraceID
	SpanName     string
	ParentSpanID *domain.SpanID
	SpanKind     domain.SpanKind
	Attributes   map[string]T
	StartTime    *time.Time
	Links        []domain.SpanLink[T]
}

type SpanUpdates[T domain.TraceData] struct {
	Attributes map[string]T
	Events     []domain.SpanEvent[T]
	Status     *domain.SpanStatus
	EndTime    *time.Time
}

type TraceSession[T domain.TraceData] struct {
	TraceID      domain.TraceID
	RootSpan     domain.Span[T]
	Context      context.Context
	StartTime    time.Time
	Metadata     map[string]any
	SamplingRate float64
}

// Command types for CQRS
type TraceCommand[T domain.TraceData] interface {
	GetCommandType() CommandType
	GetTraceID() domain.TraceID
	GetCommandID() string
	GetTimestamp() time.Time
	Validate() error
}

type CreateSpanCommand[T domain.TraceData] struct {
	CommandID string
	TraceID   domain.TraceID
	Request   CreateSpanRequest[T]
	Timestamp time.Time
}

type UpdateSpanCommand[T domain.TraceData] struct {
	CommandID string
	TraceID   domain.TraceID
	SpanID    domain.SpanID
	Updates   SpanUpdates[T]
	Timestamp time.Time
}

type FinishSpanCommand[T domain.TraceData] struct {
	CommandID string
	TraceID   domain.TraceID
	SpanID    domain.SpanID
	EndTime   time.Time
	Timestamp time.Time
}

// Query types
type SpanQuery struct {
	TraceIDs     []domain.TraceID
	ServiceNames []string
	Operations   []string
	TimeRange    *domain.TimeRange
	Attributes   map[string]any
	MinDuration  *time.Duration
	MaxDuration  *time.Duration
	HasErrors    *bool
	Limit        int
	Offset       int
	SortBy       []domain.SortCriteria
}

type TraceFilter struct {
	ServiceNames []string
	Operations   []string
	TimeRange    domain.TimeRange
	MinDuration  time.Duration
	MaxDuration  time.Duration
	HasErrors    bool
	Tags         map[string]string
}

type MetricsFilter struct {
	ServiceNames []string
	TimeRange    domain.TimeRange
	MetricTypes  []MetricType
	Granularity  time.Duration
}

// Result types
type BatchProcessingResult struct {
	ProcessedCount int
	FailedCount    int
	Errors         []domain.ProcessingError
	Duration       time.Duration
	ThroughputOps  float64
}

type CommandResult[T domain.TraceData] struct {
	CommandID     string
	Success       bool
	Result        any
	Error         error
	Timestamp     time.Time
	ExecutionTime time.Duration
}

type BatchCommandResult[T domain.TraceData] struct {
	Results      []CommandResult[T]
	SuccessCount int
	FailureCount int
	TotalTime    time.Duration
}

type SpanQueryResult[T domain.TraceData] struct {
	Spans      []domain.SpanSnapshot[T]
	TotalCount int64
	HasMore    bool
	NextCursor string
	QueryTime  time.Duration
}

type TraceSearchResult[T domain.TraceData] struct {
	Traces     []domain.TraceInfo
	TotalCount int64
	HasMore    bool
	Facets     map[string][]FacetValue
	QueryTime  time.Duration
}

// Event sourcing types
type StreamMetadata struct {
	StreamID     string
	Version      int64
	Created      time.Time
	LastModified time.Time
	Metadata     map[string]any
}

type EventProjection interface {
	GetName() string
	GetVersion() string
	Handle(ctx context.Context, event domain.TraceEvent) error
	GetState() ([]byte, error)
	LoadState(data []byte) error
}

type ProjectionState struct {
	Name         string
	Version      string
	LastPosition int64
	State        []byte
	UpdatedAt    time.Time
}

// Cache types
type SpanCacheKey struct {
	TraceID domain.TraceID
	SpanID  domain.SpanID
}

type CacheItem[T domain.TraceData] struct {
	Key   SpanCacheKey
	Value domain.SpanSnapshot[T]
	TTL   time.Duration
}

type CacheStats struct {
	HitCount        int64
	MissCount       int64
	HitRatio        float64
	Size            int64
	MaxSize         int64
	EvictionCount   int64
	AverageLoadTime time.Duration
}

type WarmupRequest struct {
	Type     WarmupType
	Keys     []string
	Priority int
}

// Publisher types
type TraceEventMessage struct {
	EventID   string
	EventType string
	TraceID   domain.TraceID
	SpanID    *domain.SpanID
	Payload   []byte
	Headers   map[string]string
	Timestamp time.Time
	Source    string
}

type TraceNotification struct {
	Type       NotificationType
	Severity   NotificationSeverity
	Title      string
	Message    string
	TraceID    domain.TraceID
	Recipients []string
	Metadata   map[string]any
	Timestamp  time.Time
}

type TraceAlert struct {
	AlertID     string
	RuleName    string
	Severity    AlertSeverity
	Description string
	TraceID     domain.TraceID
	SpanID      *domain.SpanID
	Conditions  map[string]any
	Timestamp   time.Time
	ExpiresAt   *time.Time
}

// Configuration types
type ConfigValue struct {
	Value     any
	Type      ConfigType
	Source    string
	Version   string
	UpdatedAt time.Time
}

type ConfigChange struct {
	Key       string
	OldValue  ConfigValue
	NewValue  ConfigValue
	Timestamp time.Time
	Source    string
}

type EnvironmentConfig struct {
	Environment    string
	ServiceName    string
	ServiceVersion string
	Region         string
	Cluster        string
	Features       map[string]bool
	Limits         map[string]int64
}

type ValidationResult struct {
	Valid    bool
	Errors   []ValidationError
	Warnings []ValidationWarning
}

// Enums and constants
type CommandType string
type MetricType string
type ViewType string
type WarmupType string
type NotificationType string
type NotificationSeverity string
type AlertSeverity string
type ConfigType string
type LoadBalancingStrategy string

const (
	CommandTypeCreateSpan CommandType = "create_span"
	CommandTypeUpdateSpan CommandType = "update_span"
	CommandTypeFinishSpan CommandType = "finish_span"
)

const (
	MetricTypeLatency     MetricType = "latency"
	MetricTypeThroughput  MetricType = "throughput"
	MetricTypeErrorRate   MetricType = "error_rate"
	MetricTypeResourceUse MetricType = "resource_usage"
)

// Additional supporting types would be defined here...
// (This is a representative sample showing the hexagonal architecture pattern)
