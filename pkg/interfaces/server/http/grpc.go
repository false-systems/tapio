package api

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TapioEngineService defines the gRPC service interface for the engine
type TapioEngineService interface {
	// Health and status
	HealthCheck(ctx context.Context, req *HealthCheckRequest) (*HealthCheckResponse, error)
	GetStatus(ctx context.Context, req *StatusRequest) (*StatusResponse, error)

	// Kubernetes analysis
	AnalyzeCluster(ctx context.Context, req *ClusterAnalysisRequest) (*ClusterAnalysisResponse, error)
	AnalyzeNamespace(ctx context.Context, req *NamespaceAnalysisRequest) (*NamespaceAnalysisResponse, error)
	AnalyzeResource(ctx context.Context, req *ResourceAnalysisRequest) (*ResourceAnalysisResponse, error)

	// Event processing
	ProcessEvents(ctx context.Context, req *EventBatchRequest) (*EventBatchResponse, error)
	StreamEvents(stream TapioEngine_StreamEventsServer) error

	// Pattern detection
	DetectPatterns(ctx context.Context, req *PatternDetectionRequest) (*PatternDetectionResponse, error)
	GetPatterns(ctx context.Context, req *GetPatternsRequest) (*GetPatternsResponse, error)

	// Metrics and monitoring
	GetMetrics(ctx context.Context, req *MetricsRequest) (*MetricsResponse, error)
}

// TapioCollectorService defines the gRPC service interface for collectors
type TapioCollectorService interface {
	// Collector registration and management
	RegisterCollector(ctx context.Context, req *CollectorRegistrationRequest) (*CollectorRegistrationResponse, error)
	HeartBeat(ctx context.Context, req *HeartBeatRequest) (*HeartBeatResponse, error)

	// Event streaming
	StreamEvents(stream TapioCollector_StreamEventsServer) error

	// Configuration management
	GetCollectorConfig(ctx context.Context, req *CollectorConfigRequest) (*CollectorConfigResponse, error)
	UpdateCollectorConfig(ctx context.Context, req *UpdateCollectorConfigRequest) (*UpdateCollectorConfigResponse, error)
}

// Request/Response message types

// Health Check
type HealthCheckRequest struct{}

type HealthCheckResponse struct {
	Status    string                 `json:"status"`
	Message   string                 `json:"message"`
	Timestamp *timestamppb.Timestamp `json:"timestamp"`
	Details   map[string]string      `json:"details"`
}

// Status
type StatusRequest struct{}

type StatusResponse struct {
	Version      string                 `json:"version"`
	Uptime       int64                  `json:"uptime_seconds"`
	Connections  int32                  `json:"active_connections"`
	EventsPerSec float64                `json:"events_per_second"`
	Timestamp    *timestamppb.Timestamp `json:"timestamp"`
}

// Cluster Analysis
type ClusterAnalysisRequest struct {
	IncludeNamespaces []string          `json:"include_namespaces"`
	ExcludeNamespaces []string          `json:"exclude_namespaces"`
	Options           map[string]string `json:"options"`
}

type ClusterAnalysisResponse struct {
	Status      string                 `json:"status"`
	Summary     string                 `json:"summary"`
	Issues      []*Issue               `json:"issues"`
	Suggestions []*Suggestion          `json:"suggestions"`
	Namespaces  []*NamespaceStatus     `json:"namespaces"`
	Metrics     *ClusterMetrics        `json:"metrics"`
	Timestamp   *timestamppb.Timestamp `json:"timestamp"`
}

// Namespace Analysis
type NamespaceAnalysisRequest struct {
	Namespace string            `json:"namespace"`
	Options   map[string]string `json:"options"`
}

type NamespaceAnalysisResponse struct {
	Namespace   string                 `json:"namespace"`
	Status      string                 `json:"status"`
	Summary     string                 `json:"summary"`
	Issues      []*Issue               `json:"issues"`
	Suggestions []*Suggestion          `json:"suggestions"`
	Resources   []*ResourceStatus      `json:"resources"`
	Timestamp   *timestamppb.Timestamp `json:"timestamp"`
}

// Resource Analysis
type ResourceAnalysisRequest struct {
	Resource  string            `json:"resource"`
	Namespace string            `json:"namespace"`
	Options   map[string]string `json:"options"`
}

type ResourceAnalysisResponse struct {
	Resource    string                 `json:"resource"`
	Namespace   string                 `json:"namespace"`
	Status      string                 `json:"status"`
	Summary     string                 `json:"summary"`
	Issues      []*Issue               `json:"issues"`
	Suggestions []*Suggestion          `json:"suggestions"`
	Details     map[string]interface{} `json:"details"`
	Timestamp   *timestamppb.Timestamp `json:"timestamp"`
}

// Event Processing
type EventBatchRequest struct {
	Events    []*Event               `json:"events"`
	BatchId   string                 `json:"batch_id"`
	Source    string                 `json:"source"`
	Timestamp *timestamppb.Timestamp `json:"timestamp"`
}

type EventBatchResponse struct {
	BatchId        string `json:"batch_id"`
	ProcessedCount int32  `json:"processed_count"`
	FailedCount    int32  `json:"failed_count"`
	Success        bool   `json:"success"`
	Message        string `json:"message"`
}

// Pattern Detection
type PatternDetectionRequest struct {
	Events    []*Event   `json:"events"`
	Patterns  []string   `json:"patterns"`
	TimeRange *TimeRange `json:"time_range"`
}

type PatternDetectionResponse struct {
	Results   []*PatternResult       `json:"results"`
	Summary   string                 `json:"summary"`
	Timestamp *timestamppb.Timestamp `json:"timestamp"`
}

type GetPatternsRequest struct {
	Category string `json:"category"`
	Enabled  bool   `json:"enabled"`
}

type GetPatternsResponse struct {
	Patterns []*PatternInfo `json:"patterns"`
}

// Metrics
type MetricsRequest struct {
	MetricNames []string   `json:"metric_names"`
	TimeRange   *TimeRange `json:"time_range"`
}

type MetricsResponse struct {
	Metrics   []*MetricData          `json:"metrics"`
	Timestamp *timestamppb.Timestamp `json:"timestamp"`
}

// Collector Registration
type CollectorRegistrationRequest struct {
	CollectorId   string            `json:"collector_id"`
	CollectorType string            `json:"collector_type"`
	Version       string            `json:"version"`
	NodeInfo      *NodeInfo         `json:"node_info"`
	Capabilities  []string          `json:"capabilities"`
	Metadata      map[string]string `json:"metadata"`
}

type CollectorRegistrationResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	AssignedId string `json:"assigned_id"`
	ConfigHash string `json:"config_hash"`
}

// Heartbeat
type HeartBeatRequest struct {
	CollectorId string                 `json:"collector_id"`
	Status      string                 `json:"status"`
	Metrics     *CollectorMetrics      `json:"metrics"`
	Timestamp   *timestamppb.Timestamp `json:"timestamp"`
}

type HeartBeatResponse struct {
	Success       bool   `json:"success"`
	ConfigChanged bool   `json:"config_changed"`
	ConfigHash    string `json:"config_hash"`
	Message       string `json:"message"`
}

// Collector Configuration
type CollectorConfigRequest struct {
	CollectorId string `json:"collector_id"`
	ConfigHash  string `json:"config_hash"`
}

type CollectorConfigResponse struct {
	Config     map[string]interface{} `json:"config"`
	ConfigHash string                 `json:"config_hash"`
	Success    bool                   `json:"success"`
	Message    string                 `json:"message"`
}

type UpdateCollectorConfigRequest struct {
	CollectorId string                 `json:"collector_id"`
	Config      map[string]interface{} `json:"config"`
}

type UpdateCollectorConfigResponse struct {
	Success    bool   `json:"success"`
	ConfigHash string `json:"config_hash"`
	Message    string `json:"message"`
}

// Common data types
type Event struct {
	Id        string                 `json:"id"`
	Type      string                 `json:"type"`
	Source    string                 `json:"source"`
	Timestamp *timestamppb.Timestamp `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Labels    map[string]string      `json:"labels"`
	Severity  string                 `json:"severity"`
}

type Issue struct {
	Id          string                 `json:"id"`
	Type        string                 `json:"type"`
	Resource    string                 `json:"resource"`
	Severity    string                 `json:"severity"`
	Message     string                 `json:"message"`
	Details     string                 `json:"details"`
	Remediation string                 `json:"remediation"`
	Timestamp   *timestamppb.Timestamp `json:"timestamp"`
}

type Suggestion struct {
	Id          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Command     string   `json:"command"`
	Steps       []string `json:"steps"`
	Priority    string   `json:"priority"`
}

type NamespaceStatus struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	PodCount   int32  `json:"pod_count"`
	IssueCount int32  `json:"issue_count"`
}

type ResourceStatus struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Ready      bool   `json:"ready"`
	IssueCount int32  `json:"issue_count"`
}

type ClusterMetrics struct {
	NodeCount      int32   `json:"node_count"`
	PodCount       int32   `json:"pod_count"`
	NamespaceCount int32   `json:"namespace_count"`
	ResourceUsage  float64 `json:"resource_usage_percent"`
	HealthScore    float64 `json:"health_score"`
}

type PatternResult struct {
	PatternId   string                 `json:"pattern_id"`
	PatternName string                 `json:"pattern_name"`
	Detected    bool                   `json:"detected"`
	Confidence  float64                `json:"confidence"`
	Severity    string                 `json:"severity"`
	Message     string                 `json:"message"`
	Evidence    []*Event               `json:"evidence"`
	Timestamp   *timestamppb.Timestamp `json:"timestamp"`
}

type PatternInfo struct {
	Id          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	EventTypes  []string `json:"event_types"`
	Enabled     bool     `json:"enabled"`
}

type MetricData struct {
	Name      string                 `json:"name"`
	Type      string                 `json:"type"`
	Value     float64                `json:"value"`
	Labels    map[string]string      `json:"labels"`
	Timestamp *timestamppb.Timestamp `json:"timestamp"`
}

type TimeRange struct {
	Start *timestamppb.Timestamp `json:"start"`
	End   *timestamppb.Timestamp `json:"end"`
}

type NodeInfo struct {
	NodeId       string            `json:"node_id"`
	Hostname     string            `json:"hostname"`
	Os           string            `json:"os"`
	Architecture string            `json:"architecture"`
	Region       string            `json:"region"`
	Labels       map[string]string `json:"labels"`
}

type CollectorMetrics struct {
	EventsPerSecond   float64 `json:"events_per_second"`
	ProcessingLatency float64 `json:"processing_latency_ms"`
	MemoryUsage       float64 `json:"memory_usage_mb"`
	CpuUsage          float64 `json:"cpu_usage_percent"`
	ErrorRate         float64 `json:"error_rate"`
}

// gRPC service interfaces for streaming

type TapioEngine_StreamEventsServer interface {
	Send(*EventBatchResponse) error
	Recv() (*EventBatchRequest, error)
	grpc.ServerStream
}

type TapioCollector_StreamEventsServer interface {
	Send(*EventBatchResponse) error
	Recv() (*EventBatchRequest, error)
	grpc.ServerStream
}

// Client interfaces for easier testing and mocking

type TapioEngineClient interface {
	HealthCheck(ctx context.Context, req *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error)
	GetStatus(ctx context.Context, req *StatusRequest, opts ...grpc.CallOption) (*StatusResponse, error)
	AnalyzeCluster(ctx context.Context, req *ClusterAnalysisRequest, opts ...grpc.CallOption) (*ClusterAnalysisResponse, error)
	AnalyzeNamespace(ctx context.Context, req *NamespaceAnalysisRequest, opts ...grpc.CallOption) (*NamespaceAnalysisResponse, error)
	AnalyzeResource(ctx context.Context, req *ResourceAnalysisRequest, opts ...grpc.CallOption) (*ResourceAnalysisResponse, error)
	ProcessEvents(ctx context.Context, req *EventBatchRequest, opts ...grpc.CallOption) (*EventBatchResponse, error)
	DetectPatterns(ctx context.Context, req *PatternDetectionRequest, opts ...grpc.CallOption) (*PatternDetectionResponse, error)
	GetPatterns(ctx context.Context, req *GetPatternsRequest, opts ...grpc.CallOption) (*GetPatternsResponse, error)
	GetMetrics(ctx context.Context, req *MetricsRequest, opts ...grpc.CallOption) (*MetricsResponse, error)
}

type TapioCollectorClient interface {
	RegisterCollector(ctx context.Context, req *CollectorRegistrationRequest, opts ...grpc.CallOption) (*CollectorRegistrationResponse, error)
	HeartBeat(ctx context.Context, req *HeartBeatRequest, opts ...grpc.CallOption) (*HeartBeatResponse, error)
	GetCollectorConfig(ctx context.Context, req *CollectorConfigRequest, opts ...grpc.CallOption) (*CollectorConfigResponse, error)
	UpdateCollectorConfig(ctx context.Context, req *UpdateCollectorConfigRequest, opts ...grpc.CallOption) (*UpdateCollectorConfigResponse, error)
}

// Utility functions for common operations

func NewHealthCheckRequest() *HealthCheckRequest {
	return &HealthCheckRequest{}
}

func NewStatusRequest() *StatusRequest {
	return &StatusRequest{}
}

func NewTimestamp(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t)
}

func NewTimeRange(start, end time.Time) *TimeRange {
	return &TimeRange{
		Start: NewTimestamp(start),
		End:   NewTimestamp(end),
	}
}

func NewEvent(id, eventType, source string, data map[string]interface{}, labels map[string]string) *Event {
	return &Event{
		Id:        id,
		Type:      eventType,
		Source:    source,
		Timestamp: NewTimestamp(time.Now()),
		Data:      data,
		Labels:    labels,
		Severity:  "info",
	}
}

func NewIssue(id, issueType, resource, severity, message, details, remediation string) *Issue {
	return &Issue{
		Id:          id,
		Type:        issueType,
		Resource:    resource,
		Severity:    severity,
		Message:     message,
		Details:     details,
		Remediation: remediation,
		Timestamp:   NewTimestamp(time.Now()),
	}
}

func NewSuggestion(id, title, description, command string, steps []string, priority string) *Suggestion {
	return &Suggestion{
		Id:          id,
		Title:       title,
		Description: description,
		Command:     command,
		Steps:       steps,
		Priority:    priority,
	}
}
