package engine

import (
	"context"
	"time"
)

// Engine defines the core correlation engine interface
type Engine interface {
	// Start initializes and starts the engine
	Start(ctx context.Context) error

	// Stop gracefully shuts down the engine
	Stop(ctx context.Context) error

	// HealthCheck returns engine health status
	HealthCheck(ctx context.Context) (*HealthStatus, error)

	// ProcessEvents processes incoming events for correlation
	ProcessEvents(ctx context.Context, events []Event) (*CorrelationResult, error)

	// GetPatterns returns available pattern detectors
	GetPatterns() []PatternInfo

	// GetMetrics returns engine performance metrics
	GetMetrics() *EngineMetrics
}

// KubernetesAnalyzer handles kubernetes-specific analysis
type KubernetesAnalyzer interface {
	// AnalyzeCluster performs cluster-wide health analysis
	AnalyzeCluster(ctx context.Context, req *ClusterAnalysisRequest) (*ClusterAnalysisResponse, error)

	// AnalyzeNamespace performs namespace-specific analysis
	AnalyzeNamespace(ctx context.Context, req *NamespaceAnalysisRequest) (*NamespaceAnalysisResponse, error)

	// AnalyzeResource performs resource-specific analysis
	AnalyzeResource(ctx context.Context, req *ResourceAnalysisRequest) (*ResourceAnalysisResponse, error)
}

// EventCollector handles event collection and streaming
type EventCollector interface {
	// Subscribe subscribes to event stream
	Subscribe(ctx context.Context, filter EventFilter) (<-chan Event, error)

	// GetEventHistory returns historical events
	GetEventHistory(ctx context.Context, filter EventFilter) ([]Event, error)
}

// PatternDetector handles pattern detection
type PatternDetector interface {
	// DetectPatterns detects patterns in event stream
	DetectPatterns(ctx context.Context, events []Event) ([]PatternResult, error)

	// GetSupportedPatterns returns supported pattern types
	GetSupportedPatterns() []PatternInfo
}

// HealthStatus represents engine health
type HealthStatus struct {
	Status    string            `json:"status"`
	Message   string            `json:"message"`
	Timestamp time.Time         `json:"timestamp"`
	Details   map[string]string `json:"details"`
}

// Event represents a system event
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Source    string                 `json:"source"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Labels    map[string]string      `json:"labels"`
}

// EventFilter defines event filtering criteria
type EventFilter struct {
	Sources   []string          `json:"sources"`
	Types     []string          `json:"types"`
	StartTime time.Time         `json:"start_time"`
	EndTime   time.Time         `json:"end_time"`
	Labels    map[string]string `json:"labels"`
	Limit     int               `json:"limit"`
}

// CorrelationResult represents correlation analysis results
type CorrelationResult struct {
	CorrelationID string          `json:"correlation_id"`
	Patterns      []PatternResult `json:"patterns"`
	Issues        []Issue         `json:"issues"`
	Suggestions   []Suggestion    `json:"suggestions"`
	Confidence    float64         `json:"confidence"`
	Timestamp     time.Time       `json:"timestamp"`
}

// PatternResult represents a detected pattern
type PatternResult struct {
	PatternID   string    `json:"pattern_id"`
	PatternName string    `json:"pattern_name"`
	Detected    bool      `json:"detected"`
	Confidence  float64   `json:"confidence"`
	Severity    string    `json:"severity"`
	Message     string    `json:"message"`
	Evidence    []Event   `json:"evidence"`
	Timestamp   time.Time `json:"timestamp"`
}

// PatternInfo describes a pattern detector
type PatternInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	EventTypes  []string `json:"event_types"`
	Enabled     bool     `json:"enabled"`
}

// Issue represents a detected problem
type Issue struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Resource    string    `json:"resource"`
	Severity    string    `json:"severity"`
	Message     string    `json:"message"`
	Details     string    `json:"details"`
	Remediation string    `json:"remediation"`
	Timestamp   time.Time `json:"timestamp"`
}

// Suggestion represents a recommended action
type Suggestion struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Command     string   `json:"command"`
	Steps       []string `json:"steps"`
	Priority    string   `json:"priority"`
}

// EngineMetrics represents engine performance metrics
type EngineMetrics struct {
	EventsProcessed   int64     `json:"events_processed"`
	PatternsDetected  int64     `json:"patterns_detected"`
	IssuesFound       int64     `json:"issues_found"`
	ProcessingLatency float64   `json:"processing_latency_ms"`
	LastProcessed     time.Time `json:"last_processed"`
	Uptime            float64   `json:"uptime_seconds"`
}

// ClusterAnalysisRequest represents a cluster analysis request
type ClusterAnalysisRequest struct {
	IncludeNamespaces []string          `json:"include_namespaces"`
	ExcludeNamespaces []string          `json:"exclude_namespaces"`
	Options           map[string]string `json:"options"`
}

// ClusterAnalysisResponse represents cluster analysis results
type ClusterAnalysisResponse struct {
	Status      string            `json:"status"`
	Summary     string            `json:"summary"`
	Issues      []Issue           `json:"issues"`
	Suggestions []Suggestion      `json:"suggestions"`
	Namespaces  []NamespaceStatus `json:"namespaces"`
	Metrics     *ClusterMetrics   `json:"metrics"`
	Timestamp   time.Time         `json:"timestamp"`
}

// NamespaceAnalysisRequest represents namespace analysis request
type NamespaceAnalysisRequest struct {
	Namespace string            `json:"namespace"`
	Options   map[string]string `json:"options"`
}

// NamespaceAnalysisResponse represents namespace analysis results
type NamespaceAnalysisResponse struct {
	Namespace   string           `json:"namespace"`
	Status      string           `json:"status"`
	Summary     string           `json:"summary"`
	Issues      []Issue          `json:"issues"`
	Suggestions []Suggestion     `json:"suggestions"`
	Resources   []ResourceStatus `json:"resources"`
	Timestamp   time.Time        `json:"timestamp"`
}

// ResourceAnalysisRequest represents resource analysis request
type ResourceAnalysisRequest struct {
	Resource  string            `json:"resource"`
	Namespace string            `json:"namespace"`
	Options   map[string]string `json:"options"`
}

// ResourceAnalysisResponse represents resource analysis results
type ResourceAnalysisResponse struct {
	Resource    string       `json:"resource"`
	Namespace   string       `json:"namespace"`
	Status      string       `json:"status"`
	Summary     string       `json:"summary"`
	Issues      []Issue      `json:"issues"`
	Suggestions []Suggestion `json:"suggestions"`
	Details     interface{}  `json:"details"`
	Timestamp   time.Time    `json:"timestamp"`
}

// NamespaceStatus represents namespace health status
type NamespaceStatus struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	PodCount   int    `json:"pod_count"`
	IssueCount int    `json:"issue_count"`
}

// ResourceStatus represents resource health status
type ResourceStatus struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Ready      bool   `json:"ready"`
	IssueCount int    `json:"issue_count"`
}

// ClusterMetrics represents cluster-level metrics
type ClusterMetrics struct {
	NodeCount      int     `json:"node_count"`
	PodCount       int     `json:"pod_count"`
	NamespaceCount int     `json:"namespace_count"`
	ResourceUsage  float64 `json:"resource_usage_percent"`
	HealthScore    float64 `json:"health_score"`
}
