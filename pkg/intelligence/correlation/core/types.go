package core
import (
	"time"
	"github.com/falseyair/tapio/pkg/domain"
)
// EngineConfig defines correlation engine configuration
type EngineConfig struct {
	// Basic settings
	Name                string        `json:"name"`
	Enabled             bool          `json:"enabled"`
	MaxConcurrentEvents int           `json:"max_concurrent_events"`
	// Buffer configuration
	EventBufferSize     int           `json:"event_buffer_size"`
	EventRetentionTime  time.Duration `json:"event_retention_time"`
	CleanupInterval     time.Duration `json:"cleanup_interval"`
	// Correlation settings
	DefaultTimeWindow   time.Duration `json:"default_time_window"`
	MinConfidenceScore  float64       `json:"min_confidence_score"`
	MaxCorrelationAge   time.Duration `json:"max_correlation_age"`
	// Performance tuning
	BatchSize           int           `json:"batch_size"`
	ProcessingTimeout   time.Duration `json:"processing_timeout"`
	PatternMatchTimeout time.Duration `json:"pattern_match_timeout"`
	// Algorithm configuration
	EnabledAlgorithms   []string      `json:"enabled_algorithms"`
	AlgorithmWeights    map[string]float64 `json:"algorithm_weights"`
	// Pattern configuration
	EnabledPatterns     []string      `json:"enabled_patterns"`
	PatternPriorities   map[string]int `json:"pattern_priorities"`
	// Output configuration
	OutputBufferSize    int           `json:"output_buffer_size"`
	OutputBatchSize     int           `json:"output_batch_size"`
	OutputFlushInterval time.Duration `json:"output_flush_interval"`
}
// EngineHealth represents correlation engine health status
type EngineHealth struct {
	Status              HealthStatus           `json:"status"`
	Message             string                 `json:"message"`
	LastEventTime       time.Time              `json:"last_event_time"`
	EventsProcessed     uint64                 `json:"events_processed"`
	CorrelationsFound   uint64                 `json:"correlations_found"`
	ErrorCount          uint64                 `json:"error_count"`
	BufferUtilization   float64                `json:"buffer_utilization"`
	ProcessingLatency   time.Duration          `json:"processing_latency"`
	ActivePatterns      int                    `json:"active_patterns"`
	Metrics             map[string]float64     `json:"metrics"`
}
// EngineStatistics represents runtime statistics
type EngineStatistics struct {
	StartTime           time.Time              `json:"start_time"`
	EventsProcessed     uint64                 `json:"events_processed"`
	CorrelationsFound   uint64                 `json:"correlations_found"`
	PatternsMatched     uint64                 `json:"patterns_matched"`
	ProcessingErrors    uint64                 `json:"processing_errors"`
	AverageLatency      time.Duration          `json:"average_latency"`
	EventsPerSecond     float64                `json:"events_per_second"`
	CorrelationsPerHour float64                `json:"correlations_per_hour"`
	PatternStatistics   map[string]PatternStats `json:"pattern_statistics"`
	AlgorithmMetrics    map[string]AlgorithmMetrics `json:"algorithm_metrics"`
	Custom              map[string]interface{} `json:"custom"`
}
// CorrelationCriteria defines criteria for querying correlations
type CorrelationCriteria struct {
	// Time filters
	StartTime           time.Time             `json:"start_time"`
	EndTime             time.Time             `json:"end_time"`
	// Content filters
	Sources             []domain.SourceType   `json:"sources"`
	EventTypes          []domain.EventType    `json:"event_types"`
	Severities          []domain.Severity     `json:"severities"`
	// Correlation filters
	PatternIDs          []string              `json:"pattern_ids"`
	Categories          []PatternCategory     `json:"categories"`
	MinConfidence       float64               `json:"min_confidence"`
	MaxResults          int                   `json:"max_results"`
	// Context filters
	Labels              map[string]string     `json:"labels"`
	Tags                []string              `json:"tags"`
	Hosts               []string              `json:"hosts"`
	Services            []string              `json:"services"`
}
// TemporalRelation represents a temporal relationship between events
type TemporalRelation struct {
	EventA              domain.EventID        `json:"event_a"`
	EventB              domain.EventID        `json:"event_b"`
	Relation            TemporalType          `json:"relation"`
	TimeDifference      time.Duration         `json:"time_difference"`
	Confidence          float64               `json:"confidence"`
}
// CausalChain represents a sequence of causally related events
type CausalChain struct {
	ID                  string                `json:"id"`
	Events              []domain.EventID      `json:"events"`
	Relations           []CausalRelation      `json:"relations"`
	StartTime           time.Time             `json:"start_time"`
	EndTime             time.Time             `json:"end_time"`
	Confidence          float64               `json:"confidence"`
	Category            ChainCategory         `json:"category"`
}
// CausalRelation represents a causal relationship between events
type CausalRelation struct {
	Cause               domain.EventID        `json:"cause"`
	Effect              domain.EventID        `json:"effect"`
	Strength            float64               `json:"strength"`
	Delay               time.Duration         `json:"delay"`
	Evidence            []Evidence            `json:"evidence"`
	Type                CausalType            `json:"type"`
}
// PredictedEffect represents a predicted effect of an event
type PredictedEffect struct {
	Event               domain.Event          `json:"event"`
	Probability         float64               `json:"probability"`
	EstimatedTime       time.Time             `json:"estimated_time"`
	Confidence          float64               `json:"confidence"`
	BasedOn             []domain.EventID      `json:"based_on"`
}
// ConfidenceFactor represents a factor contributing to confidence score
type ConfidenceFactor struct {
	Name                string                `json:"name"`
	Value               float64               `json:"value"`
	Weight              float64               `json:"weight"`
	Description         string                `json:"description"`
	Source              string                `json:"source"`
}
// FilterCriteria defines event filtering criteria
type FilterCriteria struct {
	Sources             []domain.SourceType   `json:"sources"`
	EventTypes          []domain.EventType    `json:"event_types"`
	Severities          []domain.Severity     `json:"severities"`
	TimeRange           TimeRange             `json:"time_range"`
	Labels              map[string]string     `json:"labels"`
	Tags                []string              `json:"tags"`
	MinConfidence       float64               `json:"min_confidence"`
}
// TimeRange represents a time range
type TimeRange struct {
	Start               time.Time             `json:"start"`
	End                 time.Time             `json:"end"`
}
// PatternStats represents statistics for a correlation pattern
type PatternStats struct {
	MatchCount          uint64                `json:"match_count"`
	AverageConfidence   float64               `json:"average_confidence"`
	LastMatchTime       time.Time             `json:"last_match_time"`
	ProcessingTime      time.Duration         `json:"processing_time"`
	SuccessRate         float64               `json:"success_rate"`
	ErrorCount          uint64                `json:"error_count"`
}
// AlgorithmMetrics represents metrics for a correlation algorithm
type AlgorithmMetrics struct {
	ExecutionCount      uint64                `json:"execution_count"`
	AverageExecutionTime time.Duration        `json:"average_execution_time"`
	SuccessRate         float64               `json:"success_rate"`
	ErrorCount          uint64                `json:"error_count"`
	CorrelationsFound   uint64                `json:"correlations_found"`
	AverageConfidence   float64               `json:"average_confidence"`
}
// Evidence represents evidence supporting a causal relationship
type Evidence struct {
	Type                EvidenceType          `json:"type"`
	Strength            float64               `json:"strength"`
	Description         string                `json:"description"`
	Source              string                `json:"source"`
	Metadata            map[string]interface{} `json:"metadata"`
}
// AlgorithmConfig defines configuration for correlation algorithms
type AlgorithmConfig struct {
	TimeWindow          time.Duration         `json:"time_window"`
	MinConfidence       float64               `json:"min_confidence"`
	MaxEvents           int                   `json:"max_events"`
	Parameters          map[string]interface{} `json:"parameters"`
	Weights             map[string]float64    `json:"weights"`
}
// Enums and constants
// HealthStatus represents the health state of the correlation engine
type HealthStatus string
const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)
// PatternCategory categorizes correlation patterns
type PatternCategory string
const (
	PatternCategoryMemory     PatternCategory = "memory"
	PatternCategoryNetwork    PatternCategory = "network"
	PatternCategoryCPU        PatternCategory = "cpu"
	PatternCategoryDisk       PatternCategory = "disk"
	PatternCategoryService    PatternCategory = "service"
	PatternCategorySecurity   PatternCategory = "security"
	PatternCategoryPerformance PatternCategory = "performance"
	PatternCategoryCascade    PatternCategory = "cascade"
	PatternCategoryPredictive PatternCategory = "predictive"
)
// PatternPriority defines pattern processing priority
type PatternPriority int
const (
	PatternPriorityLow    PatternPriority = 1
	PatternPriorityMedium PatternPriority = 2
	PatternPriorityHigh   PatternPriority = 3
	PatternPriorityCritical PatternPriority = 4
)
// AlgorithmType categorizes correlation algorithms
type AlgorithmType string
const (
	AlgorithmTypeTemporal    AlgorithmType = "temporal"
	AlgorithmTypeCausal      AlgorithmType = "causal"
	AlgorithmTypeStatistical AlgorithmType = "statistical"
	AlgorithmTypePattern     AlgorithmType = "pattern"
	AlgorithmTypeMachineLearning AlgorithmType = "ml"
)
// TemporalType defines types of temporal relationships
type TemporalType string
const (
	TemporalTypeBefore       TemporalType = "before"
	TemporalTypeAfter        TemporalType = "after"
	TemporalTypeConcurrent   TemporalType = "concurrent"
	TemporalTypeWithin       TemporalType = "within"
	TemporalTypeOverlapping  TemporalType = "overlapping"
)
// CausalType defines types of causal relationships
type CausalType string
const (
	CausalTypeDirect        CausalType = "direct"
	CausalTypeIndirect      CausalType = "indirect"
	CausalTypeContributing  CausalType = "contributing"
	CausalTypeNecessary     CausalType = "necessary"
	CausalTypeSufficient    CausalType = "sufficient"
)
// ChainCategory categorizes causal chains
type ChainCategory string
const (
	ChainCategoryFailure     ChainCategory = "failure"
	ChainCategoryPerformance ChainCategory = "performance"
	ChainCategoryResource    ChainCategory = "resource"
	ChainCategoryNetwork     ChainCategory = "network"
	ChainCategorySecurity    ChainCategory = "security"
)
// EvidenceType defines types of evidence for causal relationships
type EvidenceType string
const (
	EvidenceTypeTemporal     EvidenceType = "temporal"
	EvidenceTypeStatistical  EvidenceType = "statistical"
	EvidenceTypeDomain       EvidenceType = "domain"
	EvidenceTypeExperimental EvidenceType = "experimental"
	EvidenceTypeHeuristic    EvidenceType = "heuristic"
)
// Validation methods
// Validate validates the engine configuration
func (c EngineConfig) Validate() error {
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 10000
	}
	if c.EventRetentionTime <= 0 {
		c.EventRetentionTime = 24 * time.Hour
	}
	if c.DefaultTimeWindow <= 0 {
		c.DefaultTimeWindow = 5 * time.Minute
	}
	if c.MinConfidenceScore < 0 || c.MinConfidenceScore > 1 {
		c.MinConfidenceScore = 0.5
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.ProcessingTimeout <= 0 {
		c.ProcessingTimeout = 30 * time.Second
	}
	return nil
}
// Validate validates correlation criteria
func (c CorrelationCriteria) Validate() error {
	if c.MinConfidence < 0 || c.MinConfidence > 1 {
		c.MinConfidence = 0.0
	}
	if c.MaxResults <= 0 {
		c.MaxResults = 1000
	}
	if c.EndTime.Before(c.StartTime) {
		c.EndTime = c.StartTime.Add(24 * time.Hour)
	}
	return nil
}