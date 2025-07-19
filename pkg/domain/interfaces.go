package domain

import (
	"context"
	"time"
)

// =============================================================================
// EVENT COLLECTION INTERFACES
// =============================================================================

// EventSource provides events from various sources
type EventSource interface {
	// GetSourceType returns the type of this event source
	GetSourceType() SourceType

	// Subscribe creates a subscription for real-time events
	Subscribe(ctx context.Context, opts SubscriptionOptions) (EventStream, error)

	// Query retrieves historical events matching criteria
	Query(ctx context.Context, criteria QueryCriteria) ([]Event, error)

	// Health returns the health status of this source
	Health(ctx context.Context) SourceHealth
}

// EventStream represents a stream of events
type EventStream interface {
	// Events returns the channel of events
	Events() <-chan Event

	// Errors returns the channel of errors
	Errors() <-chan error

	// Close closes the stream
	Close() error
}

// SubscriptionOptions configures event subscriptions
type SubscriptionOptions struct {
	// Filters
	EventTypes []EventType   `json:"event_types,omitempty"`
	Severities []Severity    `json:"severities,omitempty"`
	Resources  []ResourceRef `json:"resources,omitempty"`

	// Stream control
	BufferSize int           `json:"buffer_size,omitempty"`
	BatchSize  int           `json:"batch_size,omitempty"`
	Timeout    time.Duration `json:"timeout,omitempty"`
}

// QueryCriteria represents criteria for querying events
type QueryCriteria struct {
	// Time constraints
	TimeWindow TimeWindow `json:"time_window"`

	// Content filters
	EventTypes []EventType   `json:"event_types,omitempty"`
	Sources    []SourceType  `json:"sources,omitempty"`
	Severities []Severity    `json:"severities,omitempty"`
	Resources  []ResourceRef `json:"resources,omitempty"`

	// Text search
	SearchQuery string `json:"search_query,omitempty"`

	// Pagination
	Limit      int    `json:"limit,omitempty"`
	Offset     int    `json:"offset,omitempty"`
	OrderBy    string `json:"order_by,omitempty"`
	Descending bool   `json:"descending,omitempty"`
}

// SourceHealth represents the health of an event source
type SourceHealth struct {
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message,omitempty"`
	LastSeen  time.Time    `json:"last_seen"`
	EventRate float64      `json:"event_rate"` // events per second
	Errors    int          `json:"errors"`
	Warnings  int          `json:"warnings"`
}

// HealthStatus represents health states
type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthDegraded  HealthStatus = "degraded"
	HealthUnhealthy HealthStatus = "unhealthy"
	HealthUnknown   HealthStatus = "unknown"
)

// =============================================================================
// EVENT STORAGE INTERFACES
// =============================================================================

// EventStore persists and retrieves events
type EventStore interface {
	// Store persists events (batch operation)
	Store(ctx context.Context, events []Event) error

	// Get retrieves an event by ID
	Get(ctx context.Context, id EventID) (*Event, error)

	// Query retrieves events matching criteria
	Query(ctx context.Context, criteria QueryCriteria) ([]Event, error)

	// Delete removes events (by criteria or specific IDs)
	Delete(ctx context.Context, criteria DeleteCriteria) error

	// Count returns the number of events matching criteria
	Count(ctx context.Context, criteria QueryCriteria) (int64, error)

	// Archive moves old events to archive storage
	Archive(ctx context.Context, olderThan time.Time) error
}

// DeleteCriteria specifies what to delete
type DeleteCriteria struct {
	EventIDs   []EventID    `json:"event_ids,omitempty"`
	OlderThan  *time.Time   `json:"older_than,omitempty"`
	EventTypes []EventType  `json:"event_types,omitempty"`
	Sources    []SourceType `json:"sources,omitempty"`
}

// =============================================================================
// CORRELATION ENGINE INTERFACES
// =============================================================================

// RuleEngine processes events through rules to generate insights
type RuleEngine interface {
	// ProcessEvent processes a single event through all rules
	ProcessEvent(ctx context.Context, event Event) ([]Insight, error)

	// ProcessEvents processes multiple events
	ProcessEvents(ctx context.Context, events []Event) ([]Insight, error)

	// RegisterRule registers a new rule
	RegisterRule(rule Rule) error

	// GetRules returns all registered rules
	GetRules() []Rule
}

// Rule defines a correlation rule
type Rule interface {
	// GetID returns the rule ID
	GetID() string

	// GetName returns the rule name
	GetName() string

	// Evaluate evaluates the rule against events
	Evaluate(ctx context.Context, events []Event) ([]Insight, error)
}

// CorrelationEngine analyzes events to find relationships
type CorrelationEngine interface {
	// ProcessEvents processes a batch of events for correlation
	ProcessEvents(ctx context.Context, events []Event) ([]Correlation, error)

	// ProcessEvent processes a single event
	ProcessEvent(ctx context.Context, event Event) ([]Correlation, error)

	// GetCorrelations retrieves correlations within a time window
	GetCorrelations(ctx context.Context, criteria CorrelationCriteria) ([]Correlation, error)

	// RegisterPattern registers a new correlation pattern
	RegisterPattern(ctx context.Context, pattern CorrelationPattern) error

	// GetMetrics returns correlation engine metrics
	GetMetrics(ctx context.Context) CorrelationMetrics
}

// CorrelationCriteria for querying correlations
type CorrelationCriteria struct {
	TimeWindow    TimeWindow        `json:"time_window"`
	Types         []CorrelationType `json:"types,omitempty"`
	MinConfidence float64           `json:"min_confidence,omitempty"`
	EventIDs      []EventID         `json:"event_ids,omitempty"`
	Limit         int               `json:"limit,omitempty"`
	Offset        int               `json:"offset,omitempty"`
}

// CorrelationPattern defines how to detect correlations
type CorrelationPattern interface {
	// GetName returns the pattern name
	GetName() string

	// GetType returns the correlation type this pattern detects
	GetType() CorrelationType

	// Evaluate checks if the pattern matches given events
	Evaluate(ctx context.Context, events []Event) ([]Correlation, error)

	// GetRequiredEventTypes returns what event types this pattern needs
	GetRequiredEventTypes() []EventType

	// GetTimeWindow returns the time window this pattern operates on
	GetTimeWindow() time.Duration
}

// CorrelationMetrics provides engine performance metrics
type CorrelationMetrics struct {
	EventsProcessed   int64         `json:"events_processed"`
	CorrelationsFound int64         `json:"correlations_found"`
	PatternsEvaluated int64         `json:"patterns_evaluated"`
	ProcessingTime    time.Duration `json:"processing_time"`
	QueueDepth        int           `json:"queue_depth"`
	ErrorRate         float64       `json:"error_rate"`
}

// =============================================================================
// FINDING GENERATION INTERFACES
// =============================================================================

// FindingEngine generates findings from correlations
type FindingEngine interface {
	// GenerateFindings creates findings from correlations
	GenerateFindings(ctx context.Context, correlations []Correlation) ([]Finding, error)

	// GetFindings retrieves findings matching criteria
	GetFindings(ctx context.Context, criteria FindingCriteria) ([]Finding, error)

	// UpdateFinding updates an existing finding
	UpdateFinding(ctx context.Context, finding Finding) error

	// ResolveFinding marks a finding as resolved
	ResolveFinding(ctx context.Context, id FindingID, resolution Resolution) error
}

// FindingCriteria for querying findings
type FindingCriteria struct {
	TimeWindow    TimeWindow    `json:"time_window"`
	Types         []FindingType `json:"types,omitempty"`
	Severities    []Severity    `json:"severities,omitempty"`
	MinConfidence float64       `json:"min_confidence,omitempty"`
	Resolved      *bool         `json:"resolved,omitempty"`
	Limit         int           `json:"limit,omitempty"`
	Offset        int           `json:"offset,omitempty"`
}

// Resolution represents how a finding was resolved
type Resolution struct {
	Timestamp  time.Time         `json:"timestamp"`
	ResolvedBy string            `json:"resolved_by"`
	Action     string            `json:"action"`
	Notes      string            `json:"notes,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// =============================================================================
// PREDICTION INTERFACES
// =============================================================================

// PredictionEngine provides predictive analytics
type PredictionEngine interface {
	// Predict generates predictions based on current state
	Predict(ctx context.Context, request PredictionRequest) ([]Prediction, error)

	// GetPredictions retrieves historical predictions
	GetPredictions(ctx context.Context, criteria PredictionCriteria) ([]Prediction, error)

	// UpdatePredictionFeedback provides feedback on prediction accuracy
	UpdatePredictionFeedback(ctx context.Context, id PredictionID, feedback PredictionFeedback) error
}

// PredictionRequest specifies what to predict
type PredictionRequest struct {
	Type        PredictionType    `json:"type"`
	TimeHorizon time.Duration     `json:"time_horizon"`
	Target      *ResourceRef      `json:"target,omitempty"`
	Context     []Event           `json:"context,omitempty"`
	Parameters  map[string]string `json:"parameters,omitempty"`
}

// PredictionType defines types of predictions
type PredictionType string

const (
	PredictionOOM      PredictionType = "oom"
	PredictionFailure  PredictionType = "failure"
	PredictionCapacity PredictionType = "capacity"
	PredictionAnomaly  PredictionType = "anomaly"
)

// Prediction represents a prediction result
type Prediction struct {
	ID          PredictionID   `json:"id"`
	Type        PredictionType `json:"type"`
	Confidence  float64        `json:"confidence"`
	Probability float64        `json:"probability"`
	TimeHorizon time.Duration  `json:"time_horizon"`
	Target      *ResourceRef   `json:"target,omitempty"`
	Description string         `json:"description"`
	Evidence    []Evidence     `json:"evidence"`
	Timestamp   time.Time      `json:"timestamp"`
	ExpiresAt   time.Time      `json:"expires_at"`
}

// PredictionID uniquely identifies a prediction
type PredictionID string

// PredictionCriteria for querying predictions
type PredictionCriteria struct {
	TimeWindow TimeWindow       `json:"time_window"`
	Types      []PredictionType `json:"types,omitempty"`
	Targets    []ResourceRef    `json:"targets,omitempty"`
	Active     *bool            `json:"active,omitempty"`
	Limit      int              `json:"limit,omitempty"`
	Offset     int              `json:"offset,omitempty"`
}

// PredictionFeedback provides accuracy feedback
type PredictionFeedback struct {
	Accurate   bool       `json:"accurate"`
	ActualTime *time.Time `json:"actual_time,omitempty"`
	Notes      string     `json:"notes,omitempty"`
	UpdatedBy  string     `json:"updated_by"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// =============================================================================
// OBSERVABILITY INTERFACES
// =============================================================================

// MetricsCollector collects and reports system metrics
type MetricsCollector interface {
	// Event metrics
	RecordEvent(eventType EventType, source SourceType)
	RecordEventProcessingTime(duration time.Duration)

	// Correlation metrics
	RecordCorrelation(correlationType CorrelationType, confidence float64)
	RecordCorrelationLatency(duration time.Duration)

	// Finding metrics
	RecordFinding(findingType FindingType, severity Severity)
	RecordFindingResolution(findingType FindingType, resolution string)

	// Error metrics
	RecordError(component string, operation string, error error)

	// Performance metrics
	RecordQueueDepth(component string, depth int)
	RecordProcessingRate(component string, rate float64)

	// Get current metrics
	GetMetrics() SystemMetrics
}

// SystemMetrics provides comprehensive system metrics
type SystemMetrics struct {
	Events      EventMetrics       `json:"events"`
	Correlation CorrelationMetrics `json:"correlation"`
	Findings    FindingMetrics     `json:"findings"`
	Performance PerformanceMetrics `json:"performance"`
	Errors      ErrorMetrics       `json:"errors"`
	Timestamp   time.Time          `json:"timestamp"`
}

// EventMetrics tracks event-related metrics
type EventMetrics struct {
	Total          int64                `json:"total"`
	Rate           float64              `json:"rate"` // events/second
	ByType         map[EventType]int64  `json:"by_type"`
	BySource       map[SourceType]int64 `json:"by_source"`
	BySeverity     map[Severity]int64   `json:"by_severity"`
	ProcessingTime time.Duration        `json:"processing_time"`
}

// FindingMetrics tracks finding-related metrics
type FindingMetrics struct {
	Total      int64                 `json:"total"`
	Active     int64                 `json:"active"`
	Resolved   int64                 `json:"resolved"`
	ByType     map[FindingType]int64 `json:"by_type"`
	BySeverity map[Severity]int64    `json:"by_severity"`
}

// PerformanceMetrics tracks system performance
type PerformanceMetrics struct {
	CPU         float64            `json:"cpu_usage"`
	Memory      int64              `json:"memory_usage"`
	QueueDepths map[string]int     `json:"queue_depths"`
	Throughput  map[string]float64 `json:"throughput"`
}

// ErrorMetrics tracks error rates
type ErrorMetrics struct {
	Total       int64            `json:"total"`
	Rate        float64          `json:"rate"`
	ByComponent map[string]int64 `json:"by_component"`
	ByType      map[string]int64 `json:"by_type"`
}

// Logger provides structured logging with context
type Logger interface {
	// Standard log levels
	Debug(ctx context.Context, msg string, fields ...LogField)
	Info(ctx context.Context, msg string, fields ...LogField)
	Warn(ctx context.Context, msg string, fields ...LogField)
	Error(ctx context.Context, msg string, fields ...LogField)

	// Create contextual loggers
	WithField(key string, value interface{}) Logger
	WithFields(fields map[string]interface{}) Logger
	WithError(err error) Logger
	WithEvent(event Event) Logger
	WithCorrelation(correlation Correlation) Logger
	WithFinding(finding Finding) Logger
}

// LogField represents a log field
type LogField struct {
	Key   string
	Value interface{}
}

// Helper functions for creating log fields
func F(key string, value interface{}) LogField {
	return LogField{Key: key, Value: value}
}
