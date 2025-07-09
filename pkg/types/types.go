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
	Summary    Summary    `json:"summary"`
	Problems   []Problem  `json:"problems"`
	QuickFixes []QuickFix `json:"quick_fixes"`
	Timestamp  time.Time  `json:"timestamp"`
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

// ExplainRequest represents a request for explanation
type ExplainRequest struct {
	Resource  *ResourceRef `json:"resource"`
	Namespace string       `json:"namespace"`
	Verbose   bool         `json:"verbose"`
}

// Explanation contains the detailed analysis and explanation
type Explanation struct {
	Resource   *ResourceRef       `json:"resource"`
	Summary    string             `json:"summary"`
	Problems   []Problem          `json:"problems"`
	Analysis   *Analysis          `json:"analysis"`
	RootCauses []RootCause        `json:"root_causes"`
	Solutions  []Solution         `json:"solutions"`
	Prediction *PredictionSummary `json:"prediction,omitempty"`
	Learning   *Learning          `json:"learning,omitempty"`
	Timestamp  time.Time          `json:"timestamp"`
}

// Analysis contains the technical details
type Analysis struct {
	KubernetesView *KubernetesView `json:"kubernetes_view"`
	RealityCheck   *RealityCheck   `json:"reality_check"`
}

// KubernetesView shows what Kubernetes API reports
type KubernetesView struct {
	Status     string            `json:"status"`
	Phase      string            `json:"phase"`
	Conditions []string          `json:"conditions"`
	Resources  map[string]string `json:"resources"`
	Events     []string          `json:"recent_events"`
}

// RealityCheck shows actual system state
type RealityCheck struct {
	ActualMemory   string   `json:"actual_memory,omitempty"`
	RestartPattern string   `json:"restart_pattern,omitempty"`
	ErrorPatterns  []string `json:"error_patterns,omitempty"`
	NetworkIssues  []string `json:"network_issues,omitempty"`
}

// RootCause represents an identified root cause
type RootCause struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Evidence    []string `json:"evidence"`
	Confidence  float64  `json:"confidence"`
}

// Solution represents a recommended fix
type Solution struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"`
	Urgency     Severity `json:"urgency"`
	Difficulty  string   `json:"difficulty"` // "easy", "medium", "hard"
	Risk        string   `json:"risk"`       // "low", "medium", "high"
}

// Learning contains educational information
type Learning struct {
	ConceptExplanation string   `json:"concept_explanation"`
	WhyItMatters       string   `json:"why_it_matters"`
	CommonMistakes     []string `json:"common_mistakes"`
	BestPractices      []string `json:"best_practices"`
}

// PredictionSummary contains prediction data
type PredictionSummary struct {
	Type        string        `json:"type"`
	TimeToEvent time.Duration `json:"time_to_event"`
	Confidence  float64       `json:"confidence"`
	Impact      []string      `json:"impact"`
}
