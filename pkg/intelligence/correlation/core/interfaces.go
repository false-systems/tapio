package core
import (
	"context"
	"time"
	"github.com/falseyair/tapio/pkg/domain"
)
// CorrelationEngine defines the main interface for event correlation
type CorrelationEngine interface {
	// Lifecycle management
	Start(ctx context.Context) error
	Stop() error
	// Event processing
	ProcessEvent(ctx context.Context, event domain.Event) error
	ProcessEvents(ctx context.Context, events []domain.Event) ([]domain.Correlation, error)
	// Pattern management
	RegisterPattern(pattern CorrelationPattern) error
	UnregisterPattern(patternID string) error
	ListPatterns() []CorrelationPattern
	// Query and analysis
	GetCorrelations(ctx context.Context, criteria CorrelationCriteria) ([]domain.Correlation, error)
	AnalyzeTimeWindow(ctx context.Context, start, end time.Time) ([]domain.Correlation, error)
	// Health and monitoring
	Health() EngineHealth
	Statistics() EngineStatistics
	// Configuration
	Configure(config EngineConfig) error
}
// CorrelationPattern defines a pattern for detecting correlations
type CorrelationPattern interface {
	// Pattern identification
	ID() string
	Name() string
	Description() string
	Category() PatternCategory
	// Pattern matching
	Match(ctx context.Context, events []domain.Event) ([]domain.Correlation, error)
	CanMatch(event domain.Event) bool
	RequiredSources() []domain.SourceType
	// Pattern configuration
	TimeWindow() time.Duration
	MaxEvents() int
	MinConfidence() float64
	// Pattern metadata
	Tags() []string
	Priority() PatternPriority
	Enabled() bool
}
// CorrelationAlgorithm defines algorithms for correlating events
type CorrelationAlgorithm interface {
	// Algorithm identification
	Name() string
	Type() AlgorithmType
	// Correlation computation
	Correlate(ctx context.Context, events []domain.Event, config AlgorithmConfig) ([]domain.Correlation, error)
	ComputeConfidence(events []domain.Event, correlation domain.Correlation) float64
	// Algorithm capabilities
	SupportedSources() []domain.SourceType
	SupportedEventTypes() []domain.EventType
	RequiredParameters() []string
}
// EventBuffer manages events for correlation analysis
type EventBuffer interface {
	// Event management
	Add(event domain.Event) error
	Remove(eventID domain.EventID) error
	Clear() error
	// Event querying
	Get(eventID domain.EventID) (domain.Event, error)
	GetByTimeRange(start, end time.Time) ([]domain.Event, error)
	GetBySource(source domain.SourceType) ([]domain.Event, error)
	GetByType(eventType domain.EventType) ([]domain.Event, error)
	// Buffer management
	Size() int
	Capacity() int
	OldestEvent() (domain.Event, error)
	NewestEvent() (domain.Event, error)
	// Cleanup
	Expire(before time.Time) (int, error)
}
// PatternMatcher finds patterns in event sequences
type PatternMatcher interface {
	// Pattern matching
	FindPatterns(ctx context.Context, events []domain.Event, patterns []CorrelationPattern) ([]domain.Correlation, error)
	MatchPattern(ctx context.Context, events []domain.Event, pattern CorrelationPattern) ([]domain.Correlation, error)
	// Pattern optimization
	OptimizePatterns(patterns []CorrelationPattern) []CorrelationPattern
	FilterRelevantEvents(events []domain.Event, pattern CorrelationPattern) []domain.Event
}
// TemporalAnalyzer analyzes temporal relationships between events
type TemporalAnalyzer interface {
	// Temporal analysis
	AnalyzeSequence(events []domain.Event) ([]TemporalRelation, error)
	FindCausalChains(events []domain.Event) ([]CausalChain, error)
	ComputeTemporalDistance(event1, event2 domain.Event) time.Duration
	// Time window analysis
	GroupByTimeWindows(events []domain.Event, windowSize time.Duration) ([][]domain.Event, error)
	FindCoOccurringEvents(events []domain.Event, maxGap time.Duration) ([][]domain.Event, error)
}
// CausalAnalyzer detects causal relationships between events
type CausalAnalyzer interface {
	// Causal analysis
	DetectCausality(cause, effect domain.Event) (CausalRelation, error)
	FindCausalChains(events []domain.Event) ([]CausalChain, error)
	ComputeCausalStrength(cause, effect domain.Event) float64
	// Causal reasoning
	InferCauses(effect domain.Event, candidateCauses []domain.Event) ([]CausalRelation, error)
	PredictEffects(cause domain.Event, historicalData []domain.Event) ([]PredictedEffect, error)
}
// ConfidenceCalculator computes confidence scores for correlations
type ConfidenceCalculator interface {
	// Confidence computation
	ComputeConfidence(correlation domain.Correlation) float64
	ComputeEventConfidence(event domain.Event) float64
	ComputePatternConfidence(pattern CorrelationPattern, events []domain.Event) float64
	// Confidence factors
	GetConfidenceFactors(correlation domain.Correlation) []ConfidenceFactor
	WeightFactors(factors []ConfidenceFactor) float64
}
// EventProcessor processes individual events for correlation
type EventProcessor interface {
	// Event processing
	Process(ctx context.Context, event domain.Event) error
	Preprocess(event domain.Event) (domain.Event, error)
	Validate(event domain.Event) error
	// Event enrichment
	Enrich(ctx context.Context, event domain.Event) (domain.Event, error)
	AddContext(event domain.Event, context map[string]interface{}) domain.Event
	// Event filtering
	Filter(event domain.Event, criteria FilterCriteria) bool
	ShouldProcess(event domain.Event) bool
}