package patternrecognition

import (
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// PatternCategory defines categories of patterns
type PatternCategory string

const (
	PatternCategoryResource    PatternCategory = "resource"
	PatternCategoryNetwork     PatternCategory = "network"
	PatternCategoryPerformance PatternCategory = "performance"
	PatternCategoryStability   PatternCategory = "stability"
	PatternCategorySecurity    PatternCategory = "security"
	PatternCategoryCustom      PatternCategory = "custom"
)

// PatternPriority defines pattern priority levels
type PatternPriority string

const (
	PatternPriorityCritical PatternPriority = "critical"
	PatternPriorityHigh     PatternPriority = "high"
	PatternPriorityMedium   PatternPriority = "medium"
	PatternPriorityLow      PatternPriority = "low"
)

// Config represents pattern recognition engine configuration
type Config struct {
	// Engine settings
	Name                string `json:"name"`
	Enabled             bool   `json:"enabled"`
	MaxConcurrentEvents int    `json:"max_concurrent_events"`

	// Pattern settings
	EnabledPatterns    []string      `json:"enabled_patterns"`
	DefaultTimeWindow  time.Duration `json:"default_time_window"`
	MinConfidenceScore float64       `json:"min_confidence_score"`

	// Performance settings
	BatchSize           int           `json:"batch_size"`
	ProcessingTimeout   time.Duration `json:"processing_timeout"`
	PatternMatchTimeout time.Duration `json:"pattern_match_timeout"`

	// Memory management
	EventBufferSize     int           `json:"event_buffer_size"`
	MaxEventsPerPattern int           `json:"max_events_per_pattern"`
	CleanupInterval     time.Duration `json:"cleanup_interval"`
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Name:                "tapio-pattern-recognition",
		Enabled:             true,
		MaxConcurrentEvents: 1000,
		EnabledPatterns:     []string{"memory_leak", "cascade_failure", "network_failure", "oom_prediction"},
		DefaultTimeWindow:   30 * time.Minute,
		MinConfidenceScore:  0.7,
		BatchSize:           100,
		ProcessingTimeout:   5 * time.Second,
		PatternMatchTimeout: 1 * time.Second,
		EventBufferSize:     10000,
		MaxEventsPerPattern: 100,
		CleanupInterval:     5 * time.Minute,
	}
}

// EventGroup represents a group of related events
type EventGroup struct {
	Key    string         `json:"key"`
	Events []domain.Event `json:"events"`
	Start  time.Time      `json:"start"`
	End    time.Time      `json:"end"`
}

// TemporalWindow represents a time window for pattern analysis
type TemporalWindow struct {
	Start    time.Time     `json:"start"`
	End      time.Time     `json:"end"`
	Duration time.Duration `json:"duration"`
}

// ConfidenceFactors represents factors contributing to pattern confidence
type ConfidenceFactors struct {
	TemporalCorrelation  float64 `json:"temporal_correlation"`
	EventCompleteness    float64 `json:"event_completeness"`
	SeverityAlignment    float64 `json:"severity_alignment"`
	SourceDiversity      float64 `json:"source_diversity"`
	PatternSpecificScore float64 `json:"pattern_specific_score"`
}

// PatternContext provides context for pattern matching
type PatternContext struct {
	TimeWindow   TemporalWindow         `json:"time_window"`
	EventSources map[string]int         `json:"event_sources"`
	EventTypes   map[string]int         `json:"event_types"`
	HostGroups   map[string]EventGroup  `json:"host_groups"`
	Metadata     map[string]interface{} `json:"metadata"`
}
