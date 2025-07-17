package correlation

import "time"

// Insight represents a correlated insight from multiple events
type Insight struct {
	ID              string
	Title           string
	Description     string
	Severity        string
	Category        string
	ResourceName    string
	Namespace       string
	Timestamp       time.Time
	Evidence        []*Evidence
	RootCause       *RootCause
	Prediction      *Prediction
	ActionableItems []*ActionableItem
}

// ActionableItem represents an action that can be taken based on an insight
type ActionableItem struct {
	ID          string
	Title       string
	Description string
	Command     string
	Risk        string
	Impact      string
}

// Evidence represents evidence supporting an insight
type Evidence struct {
	Type        string
	Description string
	Source      string
	Timestamp   time.Time
	Data        map[string]interface{}
}

// RootCause represents the root cause analysis of an issue
type RootCause struct {
	Type        string
	Description string
	Component   string
	Confidence  float64
}

// Prediction is defined in ai_models.go