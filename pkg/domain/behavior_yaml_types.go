package domain

import "time"

// Condition represents a condition in a behavior pattern (from YAML)
type Condition struct {
	EventType   string        `json:"event_type" yaml:"event_type"`
	Match       MatchCriteria `json:"match" yaml:"match"`
	Required    bool          `json:"required,omitempty" yaml:"required,omitempty"`
	Aggregation *Aggregation  `json:"aggregation,omitempty" yaml:"aggregation,omitempty"`
}

// MatchCriteria defines how to match an event
type MatchCriteria struct {
	Type      string  `json:"type" yaml:"type"` // exact, regex, contains, threshold, exists
	Field     string  `json:"field,omitempty" yaml:"field,omitempty"`
	Value     string  `json:"value,omitempty" yaml:"value,omitempty"`
	Threshold float64 `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	Operator  string  `json:"operator,omitempty" yaml:"operator,omitempty"` // >=, >, <=, <, ==
}

// Aggregation defines how to aggregate events
type Aggregation struct {
	Type      string        `json:"type" yaml:"type"` // count, sum, avg, max, min
	Window    time.Duration `json:"window" yaml:"window"`
	GroupBy   string        `json:"group_by,omitempty" yaml:"group_by,omitempty"`
	Threshold float64       `json:"threshold,omitempty" yaml:"threshold,omitempty"`
}

// Relationship defines relationships between conditions
type Relationship struct {
	Type       string        `json:"type" yaml:"type"`                                 // temporal, causal, spatial, progression
	Constraint string        `json:"constraint,omitempty" yaml:"constraint,omitempty"` // e.g., "BEFORE", "OVERLAPS"
	Window     time.Duration `json:"window,omitempty" yaml:"window,omitempty"`
	From       string        `json:"from,omitempty" yaml:"from,omitempty"`
	To         string        `json:"to,omitempty" yaml:"to,omitempty"`
	Confidence float64       `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	Stages     []string      `json:"stages,omitempty" yaml:"stages,omitempty"` // For progression type
}

// PredictionTemplate defines what predictions to generate
type PredictionTemplate struct {
	Type               PredictionType `json:"type" yaml:"type"`
	Message            string         `json:"message" yaml:"message"`
	Impact             string         `json:"impact" yaml:"impact"`
	Severity           string         `json:"severity" yaml:"severity"`
	TimeHorizon        string         `json:"time_horizon" yaml:"time_horizon"`
	PotentialImpacts   []string       `json:"potential_impacts" yaml:"potential_impacts"`
	RecommendedActions []string       `json:"recommended_actions" yaml:"recommended_actions"`
}

// BehaviorMetadata contains additional behavior pattern information
type BehaviorMetadata struct {
	Tags       []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	References []string `json:"references,omitempty" yaml:"references,omitempty"`
}
