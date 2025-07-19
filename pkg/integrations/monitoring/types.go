package monitoring

import "time"

// AlertType represents different types of monitoring alerts
type AlertType string

const (
	// Resource-based alert types
	AlertTypeMemoryHigh     AlertType = "memory_high"
	AlertTypeMemoryCritical AlertType = "memory_critical"
	AlertTypeCPUHigh        AlertType = "cpu_high"
	AlertTypeCPUCritical    AlertType = "cpu_critical"
	AlertTypeRecovered      AlertType = "recovered"

	// Performance-based alert types
	AlertTypeThreshold    AlertType = "threshold"
	AlertTypeAnomaly      AlertType = "anomaly"
	AlertTypeTrend        AlertType = "trend"
	AlertTypeResourceLeak AlertType = "resource_leak"
)

// AlertSeverity represents the severity level of an alert
type AlertSeverity string

const (
	SeverityInfo     AlertSeverity = "info"
	SeverityWarning  AlertSeverity = "warning"
	SeverityError    AlertSeverity = "error"
	SeverityCritical AlertSeverity = "critical"
)

// AlertStatus represents the current status of an alert
type AlertStatus string

const (
	StatusActive     AlertStatus = "active"
	StatusResolved   AlertStatus = "resolved"
	StatusSuppressed AlertStatus = "suppressed"
)

// AlertLevel defines alert severity levels (legacy compatibility)
type AlertLevel int

const (
	AlertLevelInfo AlertLevel = iota
	AlertLevelWarning
	AlertLevelCritical
)

func (al AlertLevel) String() string {
	switch al {
	case AlertLevelInfo:
		return "info"
	case AlertLevelWarning:
		return "warning"
	case AlertLevelCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Alert represents a monitoring alert
type Alert struct {
	ID          string                 `json:"id"`
	Type        AlertType              `json:"type"`
	Severity    AlertSeverity          `json:"severity"`
	Status      AlertStatus            `json:"status"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Source      string                 `json:"source"`
	Timestamp   time.Time              `json:"timestamp"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`

	// Performance-specific fields
	Level      AlertLevel             `json:"level,omitempty"`
	Metric     string                 `json:"metric,omitempty"`
	Value      float64                `json:"value,omitempty"`
	Threshold  float64                `json:"threshold,omitempty"`
	Message    string                 `json:"message,omitempty"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Resolved   bool                   `json:"resolved"`
	ResolvedAt *time.Time             `json:"resolved_at,omitempty"`
}
