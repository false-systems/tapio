package domain

import "time"

// UserFeedback represents user validation of a prediction
type UserFeedback struct {
	ID           string `json:"id"`
	PredictionID string `json:"prediction_id"`
	PatternID    string `json:"pattern_id"`
	UserID       string `json:"user_id"`

	// Feedback data
	Accurate bool           `json:"accurate"` // Was the prediction correct?
	Rating   FeedbackRating `json:"rating"`   // User rating
	Comment  string         `json:"comment,omitempty"`

	// What actually happened
	ActualEvent  string        `json:"actual_event,omitempty"`
	ActualTiming time.Duration `json:"actual_timing,omitempty"` // How long it actually took

	// Metadata
	Timestamp time.Time `json:"timestamp"`
	Processed bool      `json:"processed"` // Has this been used to adjust confidence?
}

// FeedbackRating represents user rating of prediction quality
type FeedbackRating int

const (
	RatingThumbsDown FeedbackRating = -1
	RatingNeutral    FeedbackRating = 0
	RatingThumbsUp   FeedbackRating = 1
)

// FeedbackStats aggregates feedback for a pattern
type FeedbackStats struct {
	PatternID        string    `json:"pattern_id"`
	TotalFeedback    int       `json:"total_feedback"`
	AccuracyRate     float64   `json:"accuracy_rate"`     // Percentage accurate
	AverageRating    float64   `json:"average_rating"`    // Average of ratings
	ConfidenceAdjust float64   `json:"confidence_adjust"` // Multiplier for confidence
	LastUpdated      time.Time `json:"last_updated"`
}

// FeedbackSummary provides a summary of feedback for reporting
type FeedbackSummary struct {
	Period           string            `json:"period"` // "24h", "7d", "30d"
	TotalPredictions int               `json:"total_predictions"`
	TotalFeedback    int               `json:"total_feedback"`
	OverallAccuracy  float64           `json:"overall_accuracy"`
	PatternStats     []PatternFeedback `json:"pattern_stats"`
}

// PatternFeedback represents feedback for a specific pattern
type PatternFeedback struct {
	PatternName   string  `json:"pattern_name"`
	Predictions   int     `json:"predictions"`
	Feedback      int     `json:"feedback"`
	Accuracy      float64 `json:"accuracy"`
	AverageRating float64 `json:"average_rating"`
}
