package domain

import "time"

// BehaviorPrediction represents a prediction about future system behavior
// Generated when a behavior pattern is matched
type BehaviorPrediction struct {
	ID          string    `json:"id"`
	PatternID   string    `json:"pattern_id"`
	PatternName string    `json:"pattern_name"`
	GeneratedAt time.Time `json:"generated_at"`

	// Prediction details
	Confidence         float64       `json:"confidence"`          // 0.0 to 1.0
	TimeHorizon        time.Duration `json:"time_horizon"`        // When this might happen
	PotentialImpacts   []string      `json:"potential_impacts"`   // What might be affected
	RecommendedActions []string      `json:"recommended_actions"` // What to do about it

	// Context
	AffectedResources []string          `json:"affected_resources"` // Resource IDs
	Evidence          []Evidence        `json:"evidence"`           // Supporting evidence
	Metadata          map[string]string `json:"metadata"`           // Additional context
}

// PredictionResult represents the result of behavior processing
type PredictionResult struct {
	Prediction    *Prediction            `json:"prediction,omitempty"`
	Context       map[string]interface{} `json:"context,omitempty"`
	RelatedEvents []string               `json:"related_events,omitempty"`
}
