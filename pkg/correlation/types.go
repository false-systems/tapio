package correlation

import (
	"time"
)

// Event represents a system event that can be correlated
type Event struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Source      SourceType             `json:"source"`
	Timestamp   time.Time              `json:"timestamp"`
	Description string                 `json:"description"`
	Confidence  float64                `json:"confidence"`
	PID         uint32                 `json:"pid,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Additional severity constants for compatibility with test files
const (
	SeverityHigh Severity = SeverityCritical // Alias for backward compatibility
)
