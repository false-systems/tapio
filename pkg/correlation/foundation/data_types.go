package foundation

import (
	"time"
)

// Foundation data types - Supporting structures for the core interfaces
// These are concrete types that implement the data contracts defined by the interfaces

// ============================================================================
// RESULT TYPES
// ============================================================================

// Result represents the output of a correlation rule
type Result struct {
	ID          string                 `json:"id"`
	RuleID      string                 `json:"rule_id"`
	RuleName    string                 `json:"rule_name"`
	Type        string                 `json:"type"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Severity    Severity               `json:"severity"`
	Category    Category               `json:"category"`
	Confidence  float64                `json:"confidence"`
	Events      []string               `json:"events"`      // Event IDs
	Entities    []Entity               `json:"entities"`    // Affected entities
	Evidence    []Evidence             `json:"evidence"`    // Supporting evidence
	Impact      string                 `json:"impact,omitempty"`
	TTL         time.Duration          `json:"ttl,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// Finding represents a single finding from rule execution
type Finding struct {
	ID          string                 `json:"id"`
	RuleID      string                 `json:"rule_id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Severity    Severity               `json:"severity"`
	Confidence  float64                `json:"confidence"`
	Resource    ResourceInfo           `json:"resource,omitempty"`
	Evidence    []Evidence             `json:"evidence"`
	Prediction  *Prediction            `json:"prediction,omitempty"`
	Tags        []string               `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// ============================================================================
// PATTERN DETECTION TYPES
// ============================================================================

// PatternResult represents the result of pattern detection
type PatternResult struct {
	PatternID        string                 `json:"pattern_id"`
	PatternName      string                 `json:"pattern_name"`
	PatternType      string                 `json:"pattern_type"`
	Version          string                 `json:"version"`
	Detected         bool                   `json:"detected"`
	Confidence       float64                `json:"confidence"`
	DetectionTime    time.Time              `json:"detection_time"`
	AffectedEntities []Entity               `json:"affected_entities"`
	Severity         Severity               `json:"severity"`
	Description      string                 `json:"description"`
	Evidence         []Evidence             `json:"evidence"`
	Predictions      []Prediction           `json:"predictions"`
	RootCause        *RootCause             `json:"root_cause,omitempty"`
	Remediation      []RemediationAction    `json:"remediation,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// PatternDetectionInput contains input data for pattern detection
type PatternDetectionInput struct {
	Events      []Event                `json:"events"`
	Metrics     map[string]MetricSeries `json:"metrics"`
	Window      TimeWindow             `json:"window"`
	Context     map[string]interface{} `json:"context"`
	Constraints map[string]interface{} `json:"constraints"`
}

// RootCause represents the identified root cause of a pattern
type RootCause struct {
	EventType    string                 `json:"event_type"`
	Description  string                 `json:"description"`
	Probability  float64                `json:"probability"`
	Evidence     []Evidence             `json:"evidence"`
	Contributing []string               `json:"contributing_factors"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// RemediationAction represents an automated remediation action
type RemediationAction struct {
	ID               string            `json:"id"`
	Type             string            `json:"type"`
	Priority         int               `json:"priority"`
	Description      string            `json:"description"`
	Command          string            `json:"command"`
	Parameters       map[string]string `json:"parameters"`
	ExpectedEffect   string            `json:"expected_effect"`
	RiskLevel        SafetyLevel       `json:"risk_level"`
	EstimatedDuration time.Duration    `json:"estimated_duration"`
	RequiresApproval bool              `json:"requires_approval"`
	RollbackSupported bool             `json:"rollback_supported"`
	DryRunSupported  bool              `json:"dry_run_supported"`
}

// ============================================================================
// VALIDATION TYPES
// ============================================================================

// ValidationResult represents the result of pattern validation
type ValidationResult struct {
	PatternID         string    `json:"pattern_id"`
	IsValid           bool      `json:"is_valid"`
	Accuracy          float64   `json:"accuracy"`
	FalsePositiveRate float64   `json:"false_positive_rate"`
	FalseNegativeRate float64   `json:"false_negative_rate"`
	Errors            []string  `json:"errors,omitempty"`
	Warnings          []string  `json:"warnings,omitempty"`
	ValidatedAt       time.Time `json:"validated_at"`
}

// ValidationSummary summarizes validation results across multiple patterns
type ValidationSummary struct {
	TotalPatterns     int                          `json:"total_patterns"`
	ValidPatterns     int                          `json:"valid_patterns"`
	InvalidPatterns   int                          `json:"invalid_patterns"`
	OverallAccuracy   float64                      `json:"overall_accuracy"`
	Results           map[string]ValidationResult  `json:"results"`
	GeneratedAt       time.Time                    `json:"generated_at"`
}

// ValidationRule defines a validation rule for configuration or patterns
type ValidationRule struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Validator   func(interface{}) error `json:"-"`
	Severity    Severity               `json:"severity"`
}

// ============================================================================
// AUTOFIX TYPES
// ============================================================================

// AutoFixAction represents an automated fix action
type AutoFixAction struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	PatternTypes      []string          `json:"pattern_types"`
	SafetyLevel       SafetyLevel       `json:"safety_level"`
	RequiresApproval  bool              `json:"requires_approval"`
	RollbackSupported bool              `json:"rollback_supported"`
	DryRunSupported   bool              `json:"dry_run_supported"`
	EstimatedDuration time.Duration     `json:"estimated_duration"`
	Parameters        []ActionParameter `json:"parameters"`
	Prerequisites     []string          `json:"prerequisites"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// ActionParameter defines a parameter for an autofix action
type ActionParameter struct {
	Name         string      `json:"name"`
	Type         string      `json:"type"`
	Description  string      `json:"description"`
	Required     bool        `json:"required"`
	DefaultValue interface{} `json:"default_value,omitempty"`
	Validation   string      `json:"validation,omitempty"`
}

// AutoFixRequest represents a request to execute an auto-fix action
type AutoFixRequest struct {
	ID               string            `json:"id"`
	ActionID         string            `json:"action_id"`
	PatternResult    *PatternResult    `json:"pattern_result"`
	Parameters       map[string]string `json:"parameters"`
	RequestedBy      string            `json:"requested_by"`
	Priority         string            `json:"priority"`
	DryRun           bool              `json:"dry_run"`
	RequiresApproval bool              `json:"requires_approval"`
	Timeout          time.Duration     `json:"timeout"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// AutoFixResult represents the result of an auto-fix execution
type AutoFixResult struct {
	RequestID       string                 `json:"request_id"`
	ActionID        string                 `json:"action_id"`
	Status          AutoFixStatus          `json:"status"`
	Success         bool                   `json:"success"`
	Message         string                 `json:"message"`
	Output          string                 `json:"output,omitempty"`
	Error           string                 `json:"error,omitempty"`
	StartTime       time.Time              `json:"start_time"`
	EndTime         time.Time              `json:"end_time"`
	Duration        time.Duration          `json:"duration"`
	SafetyScore     float64                `json:"safety_score"`
	RollbackInfo    *RollbackInfo          `json:"rollback_info,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// AutoFixStatus represents the status of an autofix execution
type AutoFixStatus string

const (
	AutoFixStatusPending   AutoFixStatus = "pending"
	AutoFixStatusRunning   AutoFixStatus = "running"
	AutoFixStatusCompleted AutoFixStatus = "completed"
	AutoFixStatusFailed    AutoFixStatus = "failed"
	AutoFixStatusRolledBack AutoFixStatus = "rolled_back"
	AutoFixStatusCancelled AutoFixStatus = "cancelled"
)

// RollbackInfo contains information needed for rollback operations
type RollbackInfo struct {
	RollbackID       string                 `json:"rollback_id"`
	RollbackCommands []string               `json:"rollback_commands"`
	OriginalState    map[string]interface{} `json:"original_state"`
	CreatedAt        time.Time              `json:"created_at"`
	ExpiresAt        time.Time              `json:"expires_at"`
}

// AutoFixExecution represents a complete autofix execution record
type AutoFixExecution struct {
	ID        string        `json:"id"`
	Request   AutoFixRequest `json:"request"`
	Result    AutoFixResult  `json:"result"`
	Events    []AutoFixEvent `json:"events"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// AutoFixEvent represents an event during autofix execution
type AutoFixEvent struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// AutoFixFilter defines filtering criteria for autofix executions
type AutoFixFilter struct {
	ActionID    string        `json:"action_id,omitempty"`
	Status      AutoFixStatus `json:"status,omitempty"`
	RequestedBy string        `json:"requested_by,omitempty"`
	Since       time.Time     `json:"since,omitempty"`
	Until       time.Time     `json:"until,omitempty"`
	Limit       int           `json:"limit,omitempty"`
}

// ============================================================================
// CONTEXT AND EXECUTION TYPES
// ============================================================================

// DataCollection contains the data available for rule execution
type DataCollection struct {
	Events  []Event                `json:"events"`
	Metrics map[string]MetricSeries `json:"metrics"`
	Window  TimeWindow             `json:"window"`
	Sources []SourceType           `json:"sources"`
	Context map[string]interface{} `json:"context,omitempty"`
}

// RuleContext provides execution context for correlation rules
type RuleContext struct {
	RuleID         string                  `json:"rule_id"`
	CorrelationID  string                  `json:"correlation_id"`
	Window         TimeWindow              `json:"window"`
	Events         []Event                 `json:"events"`
	Metrics        map[string]MetricSeries `json:"metrics"`
	EventsBySource map[SourceType][]Event  `json:"events_by_source"`
	EventsByType   map[string][]Event      `json:"events_by_type"`
	EventsByEntity map[string][]Event      `json:"events_by_entity"`
	Metadata       map[string]string       `json:"metadata"`
	StartTime      time.Time               `json:"start_time"`
}

// RuleExecution represents the execution of a correlation rule
type RuleExecution struct {
	RuleID      string        `json:"rule_id"`
	StartTime   time.Time     `json:"start_time"`
	EndTime     time.Time     `json:"end_time"`
	Duration    time.Duration `json:"duration"`
	Success     bool          `json:"success"`
	ResultCount int           `json:"result_count"`
	Error       string        `json:"error,omitempty"`
}

// RulePerformance tracks performance metrics for correlation rules
type RulePerformance struct {
	AverageExecutionTime time.Duration `json:"average_execution_time"`
	MaxExecutionTime     time.Duration `json:"max_execution_time"`
	MinExecutionTime     time.Duration `json:"min_execution_time"`
	TotalExecutionTime   time.Duration `json:"total_execution_time"`
	ExecutionCount       int64         `json:"execution_count"`
	SuccessCount         int64         `json:"success_count"`
	FailureCount         int64         `json:"failure_count"`
	SuccessRate          float64       `json:"success_rate"`
	MemoryUsage          uint64        `json:"memory_usage"`
	LastExecuted         time.Time     `json:"last_executed"`
}

// ============================================================================
// STATISTICS AND MONITORING TYPES
// ============================================================================

// Stats contains runtime statistics for correlation engines
type Stats struct {
	// Basic counters
	RulesRegistered     int                       `json:"rules_registered"`
	EventsProcessed     uint64                    `json:"events_processed"`
	CorrelationsFound   uint64                    `json:"correlations_found"`
	
	// Performance metrics
	ProcessingLatency   time.Duration             `json:"processing_latency"`
	RuleExecutionTime   map[string]time.Duration  `json:"rule_execution_time"`
	LastProcessedAt     time.Time                 `json:"last_processed_at"`
	
	// Resource usage
	MemoryUsage         uint64                    `json:"memory_usage"`
	CPUUsage            float64                   `json:"cpu_usage"`
	
	// Engine status
	Running             bool                      `json:"running"`
	StartTime           time.Time                 `json:"start_time"`
	Uptime              time.Duration             `json:"uptime"`
}

// EventStoreStats contains statistics about the event store
type EventStoreStats struct {
	TotalEvents     uint64                  `json:"total_events"`
	EventsPerSource map[SourceType]uint64   `json:"events_per_source"`
	StorageSize     uint64                  `json:"storage_size"`
	OldestEvent     time.Time               `json:"oldest_event"`
	NewestEvent     time.Time               `json:"newest_event"`
	RetentionPeriod time.Duration           `json:"retention_period"`
	QueryLatency    time.Duration           `json:"query_latency"`
}

// PatternDetectorMetrics contains metrics for pattern detectors
type PatternDetectorMetrics struct {
	PatternsDetected   uint64        `json:"patterns_detected"`
	AverageConfidence  float64       `json:"average_confidence"`
	AverageLatency     time.Duration `json:"average_latency"`
	ValidationAccuracy float64       `json:"validation_accuracy"`
	LastRunAt          time.Time     `json:"last_run_at"`
}

// ValidationMetrics contains validation performance metrics
type ValidationMetrics struct {
	TotalValidations  uint64    `json:"total_validations"`
	AverageAccuracy   float64   `json:"average_accuracy"`
	FalsePositiveRate float64   `json:"false_positive_rate"`
	FalseNegativeRate float64   `json:"false_negative_rate"`
	LastValidatedAt   time.Time `json:"last_validated_at"`
}

// AutoFixStats contains autofix engine statistics
type AutoFixStats struct {
	TotalExecutions     uint64                    `json:"total_executions"`
	SuccessfulExecutions uint64                   `json:"successful_executions"`
	FailedExecutions    uint64                    `json:"failed_executions"`
	AverageExecutionTime time.Duration           `json:"average_execution_time"`
	ExecutionsByAction  map[string]uint64        `json:"executions_by_action"`
	SafetyScoreAverage  float64                  `json:"safety_score_average"`
	LastExecutionAt     time.Time                `json:"last_execution_at"`
}

// ResultHandlerStats contains result handler statistics
type ResultHandlerStats struct {
	ResultsProcessed    uint64        `json:"results_processed"`
	AverageProcessingTime time.Duration `json:"average_processing_time"`
	ErrorCount          uint64        `json:"error_count"`
	LastProcessedAt     time.Time     `json:"last_processed_at"`
}

// CacheStats contains cache performance statistics
type CacheStats struct {
	Size         int           `json:"size"`
	Hits         uint64        `json:"hits"`
	Misses       uint64        `json:"misses"`
	HitRate      float64       `json:"hit_rate"`
	Evictions    uint64        `json:"evictions"`
	AverageAge   time.Duration `json:"average_age"`
	LastAccessed time.Time     `json:"last_accessed"`
}

// ============================================================================
// HEALTH AND STATUS TYPES
// ============================================================================

// HealthStatus represents the health status of correlation components
type HealthStatus struct {
	Healthy           bool                       `json:"healthy"`
	Timestamp         time.Time                  `json:"timestamp"`
	ComponentStatuses map[string]ComponentStatus `json:"component_statuses"`
	Errors            []string                   `json:"errors,omitempty"`
	Warnings          []string                   `json:"warnings,omitempty"`
	OverallScore      float64                    `json:"overall_score"`
}

// ComponentStatus represents the status of a specific component
type ComponentStatus struct {
	Name        string            `json:"name"`
	Healthy     bool              `json:"healthy"`
	Latency     time.Duration     `json:"latency"`
	Error       string            `json:"error,omitempty"`
	LastChecked time.Time         `json:"last_checked"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// HealthReport provides detailed health information
type HealthReport struct {
	Status          HealthStatus              `json:"status"`
	Dependencies    map[string]ComponentStatus `json:"dependencies"`
	Performance     Stats                     `json:"performance"`
	ResourceUsage   ResourceUsage             `json:"resource_usage"`
	RecommendedActions []string               `json:"recommended_actions,omitempty"`
	GeneratedAt     time.Time                 `json:"generated_at"`
}

// ResourceUsage contains resource utilization information
type ResourceUsage struct {
	MemoryUsed   uint64  `json:"memory_used"`
	MemoryLimit  uint64  `json:"memory_limit,omitempty"`
	CPUUsage     float64 `json:"cpu_usage"`
	DiskUsage    uint64  `json:"disk_usage,omitempty"`
	NetworkIn    uint64  `json:"network_in,omitempty"`
	NetworkOut   uint64  `json:"network_out,omitempty"`
}

// DataSourceStatus represents the status of a data source
type DataSourceStatus struct {
	Connected       bool          `json:"connected"`
	LastHeartbeat   time.Time     `json:"last_heartbeat"`
	ResponseTime    time.Duration `json:"response_time"`
	EventsReceived  uint64        `json:"events_received"`
	ErrorCount      uint64        `json:"error_count"`
	LastError       string        `json:"last_error,omitempty"`
}

// ============================================================================
// CONFIGURATION TYPES
// ============================================================================

// Configuration represents the complete correlation engine configuration
type Configuration struct {
	Engine       EngineConfig              `json:"engine"`
	Rules        RulesConfig               `json:"rules"`
	Patterns     PatternsConfig            `json:"patterns"`
	AutoFix      AutoFixConfig             `json:"autofix"`
	EventStore   EventStoreConfig          `json:"event_store"`
	DataSources  map[string]DataSourceConfig `json:"data_sources"`
	Monitoring   MonitoringConfig          `json:"monitoring"`
	Alerting     AlertingConfig            `json:"alerting"`
}

// EngineConfig contains engine-specific configuration
type EngineConfig struct {
	Type                  EngineType    `json:"type"`
	WindowSize            time.Duration `json:"window_size"`
	ProcessingInterval    time.Duration `json:"processing_interval"`
	MaxConcurrentRules    int           `json:"max_concurrent_rules"`
	EnableMetrics         bool          `json:"enable_metrics"`
	EnableCircuitBreaker  bool          `json:"enable_circuit_breaker"`
	CircuitBreakerConfig  map[string]interface{} `json:"circuit_breaker_config,omitempty"`
}

// BasicEngineConfig contains configuration for basic correlation engine
type BasicEngineConfig struct {
	EngineConfig
	BufferSize int `json:"buffer_size"`
}

// EnhancedEngineConfig contains configuration for enhanced correlation engine
type EnhancedEngineConfig struct {
	EngineConfig
	BufferSize           int           `json:"buffer_size"`
	FailureThreshold     int           `json:"failure_threshold"`
	RecoveryTimeout      time.Duration `json:"recovery_timeout"`
	MaxTimelineEvents    int           `json:"max_timeline_events"`
}

// PerfectEngineConfig contains configuration for perfect correlation engine
type PerfectEngineConfig struct {
	EngineConfig
	PatternCacheSize     int           `json:"pattern_cache_size"`
	EntityCacheSize      int           `json:"entity_cache_size"`
	SemanticAnalysis     bool          `json:"semantic_analysis"`
	BehavioralAnalysis   bool          `json:"behavioral_analysis"`
	TemporalAnalysis     bool          `json:"temporal_analysis"`
}

// PatternIntegratedEngineConfig contains configuration for pattern-integrated engine
type PatternIntegratedEngineConfig struct {
	PerfectEngineConfig
	EnablePatternDetection   bool          `json:"enable_pattern_detection"`
	PatternDetectionInterval time.Duration `json:"pattern_detection_interval"`
	PatternBufferSize        int           `json:"pattern_buffer_size"`
	PatternConfidenceWeight  float64       `json:"pattern_confidence_weight"`
	EnableValidation         bool          `json:"enable_validation"`
	ValidationInterval       time.Duration `json:"validation_interval"`
}

// RulesConfig contains rules configuration
type RulesConfig struct {
	AutoLoad        bool              `json:"auto_load"`
	RulesDirectory  string            `json:"rules_directory"`
	EnabledRules    []string          `json:"enabled_rules,omitempty"`
	DisabledRules   []string          `json:"disabled_rules,omitempty"`
	DefaultCooldown time.Duration     `json:"default_cooldown"`
	DefaultTTL      time.Duration     `json:"default_ttl"`
	RuleTimeout     time.Duration     `json:"rule_timeout"`
}

// PatternsConfig contains pattern detection configuration
type PatternsConfig struct {
	EnablePatternDetection bool              `json:"enable_pattern_detection"`
	PatternsDirectory      string            `json:"patterns_directory"`
	EnabledPatterns        []string          `json:"enabled_patterns,omitempty"`
	PatternTimeout         time.Duration     `json:"pattern_timeout"`
	ValidationEnabled      bool              `json:"validation_enabled"`
	MinConfidence          float64           `json:"min_confidence"`
}

// AutoFixConfig contains autofix engine configuration
type AutoFixConfig struct {
	EnableAutoFix         bool          `json:"enable_autofix"`
	SafetyLevel           SafetyLevel   `json:"safety_level"`
	RequireApproval       bool          `json:"require_approval"`
	DryRunMode            bool          `json:"dry_run_mode"`
	ExecutionTimeout      time.Duration `json:"execution_timeout"`
	MaxConcurrentActions  int           `json:"max_concurrent_actions"`
	MinPatternConfidence  float64       `json:"min_pattern_confidence"`
}

// EventStoreConfig contains event store configuration
type EventStoreConfig struct {
	Type            string        `json:"type"`
	ConnectionURL   string        `json:"connection_url"`
	RetentionPeriod time.Duration `json:"retention_period"`
	BatchSize       int           `json:"batch_size"`
	FlushInterval   time.Duration `json:"flush_interval"`
	Credentials     map[string]string `json:"credentials,omitempty"`
}

// DataSourceConfig contains data source configuration
type DataSourceConfig struct {
	Type        string                 `json:"type"`
	URL         string                 `json:"url,omitempty"`
	Credentials map[string]string      `json:"credentials,omitempty"`
	Config      map[string]interface{} `json:"config,omitempty"`
	Enabled     bool                   `json:"enabled"`
}

// MonitoringConfig contains monitoring configuration
type MonitoringConfig struct {
	EnableMetrics        bool          `json:"enable_metrics"`
	MetricsPort          int           `json:"metrics_port"`
	MetricsPath          string        `json:"metrics_path"`
	HealthCheckInterval  time.Duration `json:"health_check_interval"`
	EnableTracing        bool          `json:"enable_tracing"`
	TracingEndpoint      string        `json:"tracing_endpoint,omitempty"`
}

// AlertingConfig contains alerting configuration
type AlertingConfig struct {
	EnableAlerting bool                      `json:"enable_alerting"`
	Channels       []AlertChannelConfig      `json:"channels"`
	Rules          []AlertRuleConfig         `json:"rules"`
	Suppression    AlertSuppressionConfig    `json:"suppression"`
}

// AlertChannelConfig contains alert channel configuration
type AlertChannelConfig struct {
	Name     string                 `json:"name"`
	Type     string                 `json:"type"`
	Config   map[string]interface{} `json:"config"`
	Enabled  bool                   `json:"enabled"`
}

// AlertRuleConfig contains alert rule configuration
type AlertRuleConfig struct {
	Name        string   `json:"name"`
	Conditions  []string `json:"conditions"`
	Channels    []string `json:"channels"`
	Severity    Severity `json:"severity"`
	Enabled     bool     `json:"enabled"`
}

// AlertSuppressionConfig contains alert suppression configuration
type AlertSuppressionConfig struct {
	DefaultDuration time.Duration         `json:"default_duration"`
	MaxDuration     time.Duration         `json:"max_duration"`
	Rules           []SuppressionRule     `json:"rules"`
}

// SuppressionRule defines when alerts should be suppressed
type SuppressionRule struct {
	Name       string        `json:"name"`
	Pattern    string        `json:"pattern"`
	Duration   time.Duration `json:"duration"`
	Conditions []string      `json:"conditions"`
}

// ConfigurationSchema defines the structure of valid configuration
type ConfigurationSchema struct {
	Version    string                    `json:"version"`
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required"`
}

// PropertySchema defines the schema for a configuration property
type PropertySchema struct {
	Type        string                    `json:"type"`
	Description string                    `json:"description"`
	Default     interface{}               `json:"default,omitempty"`
	Enum        []interface{}             `json:"enum,omitempty"`
	Properties  map[string]PropertySchema `json:"properties,omitempty"`
}

// ============================================================================
// ALERT TYPES
// ============================================================================

// AlertChannel represents an alert delivery channel
type AlertChannel struct {
	Name    string                 `json:"name"`
	Type    string                 `json:"type"`
	Config  map[string]interface{} `json:"config"`
	Enabled bool                   `json:"enabled"`
}

// Alert represents an alert message
type Alert struct {
	ID          string                 `json:"id"`
	RuleID      string                 `json:"rule_id"`
	Title       string                 `json:"title"`
	Message     string                 `json:"message"`
	Severity    Severity               `json:"severity"`
	Result      Result                 `json:"result"`
	Channels    []string               `json:"channels"`
	SentAt      time.Time              `json:"sent_at"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}