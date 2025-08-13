package domain

import "time"

// BehaviorPattern represents a K8s behavior pattern that can be matched against events
// This is a core domain type with ZERO dependencies
type BehaviorPattern struct {
	ID            string        `json:"id" yaml:"id"`
	Name          string        `json:"name" yaml:"name"`
	Category      string        `json:"category" yaml:"category"`
	Severity      string        `json:"severity" yaml:"severity"`
	Description   string        `json:"description" yaml:"description"`
	Enabled       bool          `json:"enabled" yaml:"enabled"`
	MinConfidence float64       `json:"min_confidence" yaml:"min_confidence"`
	TimeWindow    time.Duration `json:"time_window" yaml:"time_window"`

	// Pattern matching conditions
	Conditions []Condition `json:"conditions" yaml:"conditions"`

	// Relationships between conditions
	Relationships []Relationship `json:"relationships,omitempty" yaml:"relationships,omitempty"`

	// Predictions this pattern generates when matched
	PredictionTemplate PredictionTemplate `json:"prediction_template" yaml:"prediction_template"`

	// Remediation actions for this pattern
	Remediation *RemediationActions `json:"remediation,omitempty" yaml:"remediation,omitempty"`

	// Metadata
	Metadata BehaviorMetadata `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	// Confidence scoring (runtime)
	BaseConfidence     float64 `json:"base_confidence,omitempty"`
	AdjustedConfidence float64 `json:"adjusted_confidence,omitempty"` // Adjusted based on feedback

	// Tracking (runtime)
	LastMatched time.Time `json:"last_matched,omitempty"`
	MatchCount  int64     `json:"match_count,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}
