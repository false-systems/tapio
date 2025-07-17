package correlation

import "time"

// CorrelationRulePerformance tracks performance metrics for a correlation rule
type CorrelationRulePerformance struct {
	RuleID          string
	TotalExecutions uint64
	TotalMatches    uint64
	TotalErrors     uint64
	AverageLatency  time.Duration
	MaxLatency      time.Duration
	MinLatency      time.Duration
	LastExecuted    time.Time
	LastError       error
	MatchRate       float64
	ErrorRate       float64
}

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState string

const (
	CircuitBreakerClosed   CircuitBreakerState = "closed"
	CircuitBreakerOpen     CircuitBreakerState = "open"
	CircuitBreakerHalfOpen CircuitBreakerState = "half_open"
)

// EngineMetrics provides detailed metrics about the correlation engine
type EngineMetrics struct {
	// Event processing metrics
	EventsReceived  uint64
	EventsProcessed uint64
	EventsDropped   uint64
	EventsFiltered  uint64

	// Correlation metrics
	CorrelationsFound   uint64
	CorrelationsActive  uint64
	CorrelationsExpired uint64

	// Rule metrics
	RulesActive   uint64
	RulesExecuted uint64
	RulesMatched  uint64
	RulesFailed   uint64

	// Performance metrics
	AverageProcessingTime time.Duration
	MaxProcessingTime     time.Duration
	MinProcessingTime     time.Duration

	// Resource metrics
	MemoryUsage    uint64
	GoroutineCount int

	// Timing
	StartTime     time.Time
	LastEventTime time.Time
	Uptime        time.Duration
}

// SemanticRulesConfig configuration for semantic rules
type SemanticRulesConfig struct {
	// Enable/disable features
	EnableMLInference       bool
	EnableSemanticMatching  bool
	EnableOntologyReasoning bool

	// Performance settings
	MaxConcurrentRules int
	RuleTimeout        time.Duration
	CacheSize          int
	CacheTTL           time.Duration

	// ML settings
	ModelPath          string
	ModelVersion       string
	InferenceBatchSize int

	// Semantic settings
	EmbeddingDimension  int
	SimilarityThreshold float64

	// Ontology settings
	OntologyPath      string
	MaxInferenceDepth int
}
