package types

import "time"

// CheckRequest represents a health check request
type CheckRequest struct {
	Resource  string // Optional resource to check (e.g., "my-app", "pod/my-pod")
	Namespace string // Kubernetes namespace
	All       bool   // Check all namespaces
	Verbose   bool   // Include detailed information
}

// CheckResult represents the result of a health check
type CheckResult struct {
	Summary    Summary     `json:"summary"`
	Problems   []Problem   `json:"problems"`
	QuickFixes []QuickFix  `json:"quick_fixes"`
	Timestamp  time.Time   `json:"timestamp"`
}

// Summary provides overall health statistics
type Summary struct {
	HealthyPods  int `json:"healthy_pods"`
	WarningPods  int `json:"warning_pods"`
	CriticalPods int `json:"critical_pods"`
	TotalPods    int `json:"total_pods"`
}

// Problem represents an identified issue
type Problem struct {
	Resource    ResourceRef `json:"resource"`
	Severity    Severity    `json:"severity"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Prediction  *Prediction `json:"prediction,omitempty"`
}

// ResourceRef identifies a Kubernetes resource
type ResourceRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Severity levels
type Severity string

const (
	SeverityHealthy  Severity = "healthy"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Prediction represents a future failure prediction
type Prediction struct {
	TimeToFailure time.Duration `json:"time_to_failure"`
	Confidence    float64       `json:"confidence"`
	Reason        string        `json:"reason"`
}

// QuickFix represents an actionable fix
type QuickFix struct {
	Command     string   `json:"command"`
	Description string   `json:"description"`
	Urgency     Severity `json:"urgency"`
	Safe        bool     `json:"safe"` // Can be auto-applied safely
}