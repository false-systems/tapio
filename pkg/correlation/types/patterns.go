package types

import (
	"time"
)

// PatternResult represents the result of a pattern detection
type PatternResult struct {
	PatternID   string
	PatternName string
	Detected    bool
	Confidence  float64
	Severity    Severity
	Category    Category
	
	// Time information
	StartTime  time.Time
	EndTime    time.Time
	Duration   time.Duration
	DetectedAt time.Time
	
	// Detection details
	MatchedEvents    []Event
	AffectedEntity   Entity
	AffectedEntities []Entity
	Timeline         []TimelineEntry
	
	// Analysis
	RootCause   interface{} // Can be string or *CausalityNode
	CausalChain []interface{} // CausalityNode chain
	Impact      interface{} // Can be string or ImpactAssessment
	Prediction  string
	Predictions []interface{} // Prediction objects
	
	// Metrics
	Metrics interface{} // PatternMetrics
	
	// Recommendations
	Recommendations []Recommendation
	Remediation     []interface{} // RemediationAction objects
	AutoFixable     bool
	
	// Processing metadata
	ProcessingTime time.Duration
	DataQuality    float64
	ModelAccuracy  float64
	Score          float64
}

// PatternConfig represents configuration for pattern detection
type PatternConfig struct {
	// Time windows for pattern analysis
	EventWindow      time.Duration
	CorrelationDelay time.Duration
	LookbackWindow   time.Duration
	PredictionWindow time.Duration
	MinPatternDuration time.Duration
	
	// Thresholds
	MinConfidence    float64
	MinEventsCount   int
	MinDataPoints    int
	MaxFalsePositive float64
	Thresholds       map[string]float64 // Pattern-specific thresholds
	
	// Feature toggles
	EnablePrediction  bool
	EnablePredictions bool // Alias for compatibility
	EnableAutoFix     bool
	EnableRemediation bool
	EnableValidation  bool
	
	// Performance tuning
	MaxEventsPerWindow int
	BatchSize          int
	MaxDataAge         time.Duration
}

// ValidationRun represents a pattern validation execution
type ValidationRun struct {
	ID           string
	StartTime    time.Time
	EndTime      time.Time
	PatternsRun  []string
	EventsScanned int
	Results      []PatternResult
	Errors       []error
}

// TimelineEntry represents a point in the pattern timeline
type TimelineEntry struct {
	Timestamp   time.Time
	EventID     string
	Description string
	Severity    Severity
	Entity      Entity
}

// Recommendation represents an actionable recommendation
type Recommendation struct {
	ID          string
	Title       string
	Description string
	Priority    Priority
	Commands    []string
	Risk        RiskLevel
	AutoApply   bool
}

// Priority levels for recommendations
type Priority int

const (
	PriorityLow Priority = iota
	PriorityMedium
	PriorityHigh
	PriorityCritical
)

// RiskLevel for recommendations
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)