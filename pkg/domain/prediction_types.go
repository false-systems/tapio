package domain

import "time"

// Prediction represents a prediction about future system behavior
// Generated when a behavior pattern is matched
type Prediction struct {
	ID          string              `json:"id"`
	PatternID   string              `json:"pattern_id"`
	PatternName string              `json:"pattern_name"`
	EventID     string              `json:"event_id"`
	Type        PredictionType      `json:"type"`
	Confidence  float64             `json:"confidence"`
	TimeHorizon time.Duration       `json:"time_horizon"`
	Message     string              `json:"message"`
	Impact      string              `json:"impact"`
	Severity    string              `json:"severity"`
	Resources   []ResourceRef       `json:"resources"`
	Evidence    []Evidence          `json:"evidence"`
	Remediation *RemediationActions `json:"remediation,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	ExpiresAt   time.Time           `json:"expires_at"`
	Status      PredictionStatus    `json:"status"`
}

// PredictionType represents the type of prediction
type PredictionType string

const (
	PredictionTypeFailure         PredictionType = "failure"
	PredictionTypeDegradation     PredictionType = "degradation"
	PredictionTypeThresholdBreach PredictionType = "threshold_breach"
	PredictionTypeAnomaly         PredictionType = "anomaly"
)

// PredictionStatus represents the status of a prediction
type PredictionStatus string

const (
	PredictionStatusActive   PredictionStatus = "active"
	PredictionStatusResolved PredictionStatus = "resolved"
	PredictionStatusExpired  PredictionStatus = "expired"
	PredictionStatusIgnored  PredictionStatus = "ignored"
)

// RemediationActions represents actions to remediate a predicted issue
type RemediationActions struct {
	AutoRemediation   bool     `json:"auto_remediation"`
	ManualSteps       []string `json:"manual_steps"`
	PreventativeSteps []string `json:"preventative_steps"`
	Runbooks          []string `json:"runbooks,omitempty"`
	Scripts           []string `json:"scripts,omitempty"`
}

// ConditionMatch represents a matched condition
type ConditionMatch struct {
	ConditionID   string `json:"condition_id"`
	Matched       bool   `json:"matched"`
	ActualValue   string `json:"actual_value"`
	ExpectedValue string `json:"expected_value"`
	Message       string `json:"message"`
}
