package patternrecognition

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// PatternRecognitionEngine provides intelligent pattern detection across events
type PatternRecognitionEngine interface {
	// DetectPatterns analyzes events and returns detected patterns with correlations
	DetectPatterns(ctx context.Context, events []domain.Event) ([]PatternMatch, error)

	// RegisterPattern adds a new pattern to the recognition engine
	RegisterPattern(pattern Pattern) error

	// UnregisterPattern removes a pattern from the engine
	UnregisterPattern(patternID string) error

	// GetSupportedPatterns returns all registered patterns
	GetSupportedPatterns() []PatternInfo

	// GetPatternStats returns statistics about pattern matching
	GetPatternStats() PatternStats

	// Configure updates the engine configuration
	Configure(config *Config) error
}

// Pattern represents a detectable event pattern
type Pattern interface {
	// ID returns unique pattern identifier
	ID() string

	// Name returns human-readable pattern name
	Name() string

	// Description returns pattern description
	Description() string

	// Match analyzes events and returns correlations if pattern detected
	Match(ctx context.Context, events []domain.Event) ([]domain.Correlation, error)

	// CanMatch quickly checks if an event could be part of this pattern
	CanMatch(event domain.Event) bool

	// GetMetadata returns pattern metadata
	GetMetadata() PatternMetadata

	// Category returns the pattern category
	Category() PatternCategory

	// TimeWindow returns the time window for this pattern
	TimeWindow() time.Duration

	// MinConfidence returns minimum confidence threshold
	MinConfidence() float64

	// Priority returns pattern priority
	Priority() PatternPriority

	// RequiredSources returns required event sources
	RequiredSources() []domain.SourceType

	// Enabled returns whether pattern is enabled
	Enabled() bool
}

// PatternMatch represents a detected pattern with its correlation
type PatternMatch struct {
	Pattern     PatternInfo             `json:"pattern"`
	Correlation domain.Correlation      `json:"correlation"`
	Confidence  float64                 `json:"confidence"`
	Events      []domain.EventReference `json:"events"`
	Detected    time.Time               `json:"detected"`
}

// PatternInfo contains pattern information
type PatternInfo struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Category    PatternCategory `json:"category"`
	Priority    PatternPriority `json:"priority"`
	Tags        []string        `json:"tags"`
	Enabled     bool            `json:"enabled"`
}

// PatternMetadata contains pattern metadata
type PatternMetadata struct {
	Version     string          `json:"version"`
	Author      string          `json:"author"`
	Tags        []string        `json:"tags"`
	Performance PerformanceInfo `json:"performance"`
	LastUpdate  time.Time       `json:"last_update"`
}

// PerformanceInfo contains pattern performance characteristics
type PerformanceInfo struct {
	AverageMatchTime   time.Duration `json:"average_match_time"`
	MaxEventsProcessed int           `json:"max_events_processed"`
	MemoryUsage        int64         `json:"memory_usage"`
}

// PatternStats contains pattern matching statistics
type PatternStats struct {
	TotalMatches      map[string]int64         `json:"total_matches"`
	MatchRate         map[string]float64       `json:"match_rate"`
	AverageConfidence map[string]float64       `json:"average_confidence"`
	LastMatchTime     map[string]time.Time     `json:"last_match_time"`
	ProcessingTime    map[string]time.Duration `json:"processing_time"`
}
