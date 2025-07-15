package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// REST API definitions for HTTP endpoints

// RESTHandler defines the HTTP handler interface
type RESTHandler interface {
	// Health endpoints
	HealthCheck(w http.ResponseWriter, r *http.Request)
	GetStatus(w http.ResponseWriter, r *http.Request)
	
	// Kubernetes analysis endpoints
	AnalyzeCluster(w http.ResponseWriter, r *http.Request)
	AnalyzeNamespace(w http.ResponseWriter, r *http.Request)
	AnalyzeResource(w http.ResponseWriter, r *http.Request)
	
	// Pattern detection endpoints
	DetectPatterns(w http.ResponseWriter, r *http.Request)
	GetPatterns(w http.ResponseWriter, r *http.Request)
	
	// Metrics endpoints
	GetMetrics(w http.ResponseWriter, r *http.Request)
	
	// Event endpoints
	ProcessEvents(w http.ResponseWriter, r *http.Request)
	
	// Collector management endpoints
	RegisterCollector(w http.ResponseWriter, r *http.Request)
	GetCollectorConfig(w http.ResponseWriter, r *http.Request)
	UpdateCollectorConfig(w http.ResponseWriter, r *http.Request)
}

// REST request/response types that mirror gRPC but are HTTP-friendly

// Common REST response wrapper
type RESTResponse struct {
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// Health endpoints
type RESTHealthResponse struct {
	Status    string            `json:"status"`
	Message   string            `json:"message"`
	Timestamp time.Time         `json:"timestamp"`
	Details   map[string]string `json:"details"`
}

type RESTStatusResponse struct {
	Version      string    `json:"version"`
	Uptime       int64     `json:"uptime_seconds"`
	Connections  int32     `json:"active_connections"`
	EventsPerSec float64   `json:"events_per_second"`
	Timestamp    time.Time `json:"timestamp"`
}

// Kubernetes analysis endpoints
type RESTClusterAnalysisRequest struct {
	IncludeNamespaces []string          `json:"include_namespaces"`
	ExcludeNamespaces []string          `json:"exclude_namespaces"`
	Options           map[string]string `json:"options"`
}

type RESTClusterAnalysisResponse struct {
	Status      string                   `json:"status"`
	Summary     string                   `json:"summary"`
	Issues      []*RESTIssue             `json:"issues"`
	Suggestions []*RESTSuggestion        `json:"suggestions"`
	Namespaces  []*RESTNamespaceStatus   `json:"namespaces"`
	Metrics     *RESTClusterMetrics      `json:"metrics"`
	Timestamp   time.Time                `json:"timestamp"`
}

type RESTNamespaceAnalysisRequest struct {
	Namespace string            `json:"namespace"`
	Options   map[string]string `json:"options"`
}

type RESTNamespaceAnalysisResponse struct {
	Namespace   string                  `json:"namespace"`
	Status      string                  `json:"status"`
	Summary     string                  `json:"summary"`
	Issues      []*RESTIssue            `json:"issues"`
	Suggestions []*RESTSuggestion       `json:"suggestions"`
	Resources   []*RESTResourceStatus   `json:"resources"`
	Timestamp   time.Time               `json:"timestamp"`
}

type RESTResourceAnalysisRequest struct {
	Resource  string            `json:"resource"`
	Namespace string            `json:"namespace"`
	Options   map[string]string `json:"options"`
}

type RESTResourceAnalysisResponse struct {
	Resource    string                 `json:"resource"`
	Namespace   string                 `json:"namespace"`
	Status      string                 `json:"status"`
	Summary     string                 `json:"summary"`
	Issues      []*RESTIssue           `json:"issues"`
	Suggestions []*RESTSuggestion      `json:"suggestions"`
	Details     map[string]interface{} `json:"details"`
	Timestamp   time.Time              `json:"timestamp"`
}

// Pattern detection endpoints
type RESTPatternDetectionRequest struct {
	Events    []*RESTEvent    `json:"events"`
	Patterns  []string        `json:"patterns"`
	TimeRange *RESTTimeRange  `json:"time_range"`
}

type RESTPatternDetectionResponse struct {
	Results   []*RESTPatternResult `json:"results"`
	Summary   string               `json:"summary"`
	Timestamp time.Time            `json:"timestamp"`
}

type RESTGetPatternsRequest struct {
	Category string `json:"category"`
	Enabled  bool   `json:"enabled"`
}

type RESTGetPatternsResponse struct {
	Patterns []*RESTPatternInfo `json:"patterns"`
}

// Metrics endpoints
type RESTMetricsRequest struct {
	MetricNames []string       `json:"metric_names"`
	TimeRange   *RESTTimeRange `json:"time_range"`
}

type RESTMetricsResponse struct {
	Metrics   []*RESTMetricData `json:"metrics"`
	Timestamp time.Time         `json:"timestamp"`
}

// Event endpoints
type RESTEventBatchRequest struct {
	Events    []*RESTEvent `json:"events"`
	BatchId   string       `json:"batch_id"`
	Source    string       `json:"source"`
	Timestamp time.Time    `json:"timestamp"`
}

type RESTEventBatchResponse struct {
	BatchId        string `json:"batch_id"`
	ProcessedCount int32  `json:"processed_count"`
	FailedCount    int32  `json:"failed_count"`
	Success        bool   `json:"success"`
	Message        string `json:"message"`
}

// Collector management endpoints
type RESTCollectorRegistrationRequest struct {
	CollectorId   string            `json:"collector_id"`
	CollectorType string            `json:"collector_type"`
	Version       string            `json:"version"`
	NodeInfo      *RESTNodeInfo     `json:"node_info"`
	Capabilities  []string          `json:"capabilities"`
	Metadata      map[string]string `json:"metadata"`
}

type RESTCollectorRegistrationResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	AssignedId string `json:"assigned_id"`
	ConfigHash string `json:"config_hash"`
}

type RESTCollectorConfigRequest struct {
	CollectorId string `json:"collector_id"`
	ConfigHash  string `json:"config_hash"`
}

type RESTCollectorConfigResponse struct {
	Config     map[string]interface{} `json:"config"`
	ConfigHash string                 `json:"config_hash"`
	Success    bool                   `json:"success"`
	Message    string                 `json:"message"`
}

type RESTUpdateCollectorConfigRequest struct {
	CollectorId string                 `json:"collector_id"`
	Config      map[string]interface{} `json:"config"`
}

type RESTUpdateCollectorConfigResponse struct {
	Success    bool   `json:"success"`
	ConfigHash string `json:"config_hash"`
	Message    string `json:"message"`
}

// Common REST data types
type RESTEvent struct {
	Id        string                 `json:"id"`
	Type      string                 `json:"type"`
	Source    string                 `json:"source"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Labels    map[string]string      `json:"labels"`
	Severity  string                 `json:"severity"`
}

type RESTIssue struct {
	Id          string    `json:"id"`
	Type        string    `json:"type"`
	Resource    string    `json:"resource"`
	Severity    string    `json:"severity"`
	Message     string    `json:"message"`
	Details     string    `json:"details"`
	Remediation string    `json:"remediation"`
	Timestamp   time.Time `json:"timestamp"`
}

type RESTSuggestion struct {
	Id          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Command     string   `json:"command"`
	Steps       []string `json:"steps"`
	Priority    string   `json:"priority"`
}

type RESTNamespaceStatus struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	PodCount   int32  `json:"pod_count"`
	IssueCount int32  `json:"issue_count"`
}

type RESTResourceStatus struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Ready      bool   `json:"ready"`
	IssueCount int32  `json:"issue_count"`
}

type RESTClusterMetrics struct {
	NodeCount      int32   `json:"node_count"`
	PodCount       int32   `json:"pod_count"`
	NamespaceCount int32   `json:"namespace_count"`
	ResourceUsage  float64 `json:"resource_usage_percent"`
	HealthScore    float64 `json:"health_score"`
}

type RESTPatternResult struct {
	PatternId   string      `json:"pattern_id"`
	PatternName string      `json:"pattern_name"`
	Detected    bool        `json:"detected"`
	Confidence  float64     `json:"confidence"`
	Severity    string      `json:"severity"`
	Message     string      `json:"message"`
	Evidence    []*RESTEvent `json:"evidence"`
	Timestamp   time.Time   `json:"timestamp"`
}

type RESTPatternInfo struct {
	Id          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	EventTypes  []string `json:"event_types"`
	Enabled     bool     `json:"enabled"`
}

type RESTMetricData struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels"`
	Timestamp time.Time         `json:"timestamp"`
}

type RESTTimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type RESTNodeInfo struct {
	NodeId       string            `json:"node_id"`
	Hostname     string            `json:"hostname"`
	Os           string            `json:"os"`
	Architecture string            `json:"architecture"`
	Region       string            `json:"region"`
	Labels       map[string]string `json:"labels"`
}

// Utility functions for REST API

func WriteJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	response := RESTResponse{
		Success:   statusCode < 400,
		Data:      data,
		Timestamp: time.Now(),
	}
	
	json.NewEncoder(w).Encode(response)
}

func WriteJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	response := RESTResponse{
		Success:   false,
		Error:     message,
		Timestamp: time.Now(),
	}
	
	json.NewEncoder(w).Encode(response)
}

func ParseJSONRequest(r *http.Request, dest interface{}) error {
	return json.NewDecoder(r.Body).Decode(dest)
}

// REST endpoint paths
const (
	// Health endpoints
	EndpointHealthCheck = "/health"
	EndpointStatus      = "/status"
	
	// Kubernetes analysis endpoints
	EndpointAnalyzeCluster   = "/analyze/cluster"
	EndpointAnalyzeNamespace = "/analyze/namespace"
	EndpointAnalyzeResource  = "/analyze/resource"
	
	// Pattern detection endpoints
	EndpointDetectPatterns = "/patterns/detect"
	EndpointGetPatterns    = "/patterns"
	
	// Metrics endpoints
	EndpointGetMetrics = "/metrics"
	
	// Event endpoints
	EndpointProcessEvents = "/events"
	
	// Collector management endpoints
	EndpointRegisterCollector      = "/collectors/register"
	EndpointGetCollectorConfig     = "/collectors/config"
	EndpointUpdateCollectorConfig  = "/collectors/config"
)

// HTTP methods
const (
	MethodGET    = "GET"
	MethodPOST   = "POST"
	MethodPUT    = "PUT"
	MethodDELETE = "DELETE"
	MethodPATCH  = "PATCH"
)

// Common HTTP headers
const (
	HeaderContentType   = "Content-Type"
	HeaderAccept        = "Accept"
	HeaderAuthorization = "Authorization"
	HeaderUserAgent     = "User-Agent"
)

// Content types
const (
	ContentTypeJSON = "application/json"
	ContentTypeText = "text/plain"
	ContentTypeHTML = "text/html"
)