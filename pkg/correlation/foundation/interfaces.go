package foundation

import (
	"context"
	"time"
)

// Foundation interfaces - The contract layer for all correlation functionality
// These interfaces define the core contracts that all correlation components must follow
// No implementation details, only pure interfaces

// ============================================================================
// CORE ENGINE INTERFACE
// ============================================================================

// Engine is the main correlation engine interface
// All correlation engines (basic, enhanced, perfect, pattern-integrated) implement this
type Engine interface {
	// Event processing
	ProcessEvents(ctx context.Context, events []Event) ([]Result, error)
	ProcessWindow(ctx context.Context, window TimeWindow, events []Event) ([]Result, error)

	// Rule management
	RegisterRule(rule Rule) error
	UnregisterRule(ruleID string) error
	EnableRule(ruleID string) error
	DisableRule(ruleID string) error
	GetRule(ruleID string) (Rule, bool)
	ListRules() []Rule

	// Configuration
	SetWindowSize(duration time.Duration)
	SetProcessingInterval(interval time.Duration)
	SetMaxConcurrentRules(limit int)

	// Lifecycle management
	Start(ctx context.Context) error
	Stop() error
	Health() error

	// Statistics and monitoring
	GetStats() Stats
}

// ============================================================================
// RULE INTERFACE
// ============================================================================

// Rule represents a correlation rule that can be executed
type Rule interface {
	// Metadata
	GetID() string
	GetName() string
	GetDescription() string
	GetCategory() Category
	GetVersion() string
	GetAuthor() string
	GetTags() []string

	// Configuration
	IsEnabled() bool
	GetMinConfidence() float64
	GetCooldown() time.Duration
	GetTTL() time.Duration

	// Requirements
	GetRequiredSources() []SourceType
	GetOptionalSources() []SourceType

	// Execution
	CheckRequirements(ctx context.Context, data *DataCollection) error
	Execute(ctx context.Context, ruleCtx *RuleContext) ([]Finding, error)

	// Performance tracking
	GetPerformance() RulePerformance
	UpdatePerformance(execution RuleExecution)
}

// ============================================================================
// DATA SOURCE INTERFACES
// ============================================================================

// EventStore provides access to historical events for correlation
type EventStore interface {
	// Event storage
	Store(ctx context.Context, events []Event) error
	StoreBatch(ctx context.Context, events []Event) error

	// Event querying
	GetEvents(ctx context.Context, filter Filter) ([]Event, error)
	GetEventsInWindow(ctx context.Context, window TimeWindow, filter Filter) ([]Event, error)

	// Metrics integration
	GetMetrics(ctx context.Context, name string, window TimeWindow) (MetricSeries, error)
	StoreMetrics(ctx context.Context, metrics []MetricSeries) error

	// Maintenance
	Cleanup(ctx context.Context, before time.Time) error
	GetStats(ctx context.Context) (EventStoreStats, error)

	// Health
	Health() error
}

// DataSource defines the interface for external data sources
type DataSource interface {
	// Identification
	Name() string
	Type() SourceType

	// Data retrieval
	GetData(ctx context.Context, dataType string, config map[string]interface{}) (interface{}, error)
	Subscribe(ctx context.Context, dataType string, handler DataHandler) error
	Unsubscribe(ctx context.Context, dataType string) error

	// Health and status
	Health() error
	GetStatus() DataSourceStatus
}

// DataHandler handles data from subscribed sources
type DataHandler func(data interface{}) error

// ============================================================================
// PATTERN DETECTION INTERFACES
// ============================================================================

// PatternDetector defines the interface for pattern detection components
type PatternDetector interface {
	// Identification
	Name() string
	PatternType() string
	Version() string

	// Configuration
	Configure(config interface{}) error
	GetConfig() interface{}

	// Pattern detection
	Detect(ctx context.Context, data *PatternDetectionInput) ([]PatternResult, error)

	// Validation and metrics
	Validate(ctx context.Context, results []PatternResult) (ValidationResult, error)
	GetMetrics() PatternDetectorMetrics

	// Lifecycle
	Start(ctx context.Context) error
	Stop() error
	Health() error
}

// PatternRegistry manages pattern detectors
type PatternRegistry interface {
	// Registration
	Register(detector PatternDetector) error
	Unregister(name string) error

	// Discovery
	Get(name string) (PatternDetector, bool)
	List() []PatternDetector
	ListByType(patternType string) []PatternDetector

	// Bulk operations
	DetectAll(ctx context.Context, data *PatternDetectionInput) ([]PatternResult, error)
	ValidateAll(ctx context.Context, results []PatternResult) (map[string]ValidationResult, error)
}

// ============================================================================
// AUTOFIX INTERFACES
// ============================================================================

// AutoFixEngine defines the interface for automated remediation
type AutoFixEngine interface {
	// Action registration
	RegisterAction(action AutoFixAction) error
	UnregisterAction(actionID string) error
	GetAction(actionID string) (AutoFixAction, bool)
	ListActions() []AutoFixAction

	// Execution
	ExecuteAutoFix(ctx context.Context, request AutoFixRequest) (AutoFixResult, error)
	CanAutoFix(ctx context.Context, finding Finding) bool
	GetRecommendedActions(ctx context.Context, finding Finding) ([]AutoFixAction, error)

	// Monitoring
	GetExecutionHistory(ctx context.Context, filter AutoFixFilter) ([]AutoFixExecution, error)
	GetStats() AutoFixStats

	// Configuration
	SetSafetyLevel(level SafetyLevel)
	EnableDryRunMode(enabled bool)
	SetApprovalRequired(required bool)
}

// ============================================================================
// RESULT PROCESSING INTERFACES
// ============================================================================

// ResultHandler processes correlation results
type ResultHandler interface {
	HandleResult(ctx context.Context, result Result) error
	HandleBatch(ctx context.Context, results []Result) error
	GetStats() ResultHandlerStats
}

// AlertManager manages alerting based on correlation results
type AlertManager interface {
	// Alerting
	SendAlert(ctx context.Context, result Result) error

	// Alert management
	SuppressAlert(ruleID string, duration time.Duration) error
	UnsuppressAlert(ruleID string) error
	IsAlertSuppressed(ruleID string) bool

	// Alert history
	GetAlertHistory(ctx context.Context, ruleID string, window TimeWindow) ([]Result, error)

	// Configuration
	ConfigureChannel(channel AlertChannel) error
	ListChannels() []AlertChannel
}

// ============================================================================
// MONITORING AND OBSERVABILITY INTERFACES
// ============================================================================

// MetricsCollector collects metrics about correlation engine performance
type MetricsCollector interface {
	// Rule metrics
	RecordRuleExecution(ruleID string, duration time.Duration, success bool)
	RecordRuleResult(ruleID string, result Result)

	// Engine metrics
	RecordEventProcessed(source SourceType)
	RecordCorrelationFound(category Category, severity Severity)
	RecordProcessingLatency(duration time.Duration)

	// Resource metrics
	RecordMemoryUsage(bytes uint64)
	RecordCPUUsage(percent float64)

	// Pattern metrics
	RecordPatternDetection(patternType string, confidence float64)
	RecordPatternValidation(patternType string, accuracy float64)

	// Export
	Export(ctx context.Context, format ExportFormat) ([]byte, error)
}

// HealthChecker provides comprehensive health checking
type HealthChecker interface {
	// Component health
	CheckEventStore(ctx context.Context) error
	CheckRuleEngine(ctx context.Context) error
	CheckPatternDetectors(ctx context.Context) error
	CheckAutoFixEngine(ctx context.Context) error
	CheckAlertManager(ctx context.Context) error

	// Overall health
	Health(ctx context.Context) error
	Status(ctx context.Context) HealthStatus

	// Deep health checks
	DeepHealthCheck(ctx context.Context) (HealthReport, error)
}

// ============================================================================
// BUILDER AND FACTORY INTERFACES
// ============================================================================

// RuleBuilder provides a fluent interface for building rules
type RuleBuilder interface {
	// Basic properties
	ID(id string) RuleBuilder
	Name(name string) RuleBuilder
	Description(desc string) RuleBuilder
	Category(cat Category) RuleBuilder
	Version(version string) RuleBuilder
	Author(author string) RuleBuilder
	Tags(tags ...string) RuleBuilder

	// Configuration
	MinConfidence(conf float64) RuleBuilder
	Cooldown(duration time.Duration) RuleBuilder
	TTL(duration time.Duration) RuleBuilder

	// Sources
	RequireSources(sources ...SourceType) RuleBuilder
	OptionalSources(sources ...SourceType) RuleBuilder

	// Evaluation function
	Evaluate(fn RuleFunction) RuleBuilder

	// Build the rule
	Build() Rule
	Validate() error
}

// EngineFactory creates correlation engines with specific configurations
type EngineFactory interface {
	// Engine creation
	CreateBasicEngine(config BasicEngineConfig) (Engine, error)
	CreateEnhancedEngine(config EnhancedEngineConfig) (Engine, error)
	CreatePerfectEngine(config PerfectEngineConfig) (Engine, error)
	CreatePatternIntegratedEngine(config PatternIntegratedEngineConfig) (Engine, error)

	// Configuration validation
	ValidateConfig(config interface{}) error
	GetDefaultConfig(engineType EngineType) interface{}
}

// ============================================================================
// CONFIGURATION INTERFACES
// ============================================================================

// ConfigurationManager manages correlation engine configuration
type ConfigurationManager interface {
	// Configuration loading
	LoadConfig(source string) (Configuration, error)
	SaveConfig(config Configuration, destination string) error

	// Configuration validation
	ValidateConfiguration(config Configuration) error
	GetSchema() ConfigurationSchema

	// Dynamic configuration
	UpdateConfiguration(updates map[string]interface{}) error
	GetConfiguration() Configuration
	WatchConfiguration(ctx context.Context, handler ConfigChangeHandler) error
}

// ConfigChangeHandler handles configuration changes
type ConfigChangeHandler func(oldConfig, newConfig Configuration) error

// ============================================================================
// VALIDATION INTERFACES
// ============================================================================

// RuleValidator validates rules before registration
type RuleValidator interface {
	ValidateRule(rule Rule) error
	ValidateRuleFunction(fn RuleFunction) error
	ValidateConfiguration(config map[string]interface{}) error
	GetValidationRules() []ValidationRule
}

// PatternValidator validates pattern detection results
type PatternValidator interface {
	ValidatePattern(pattern PatternResult) error
	ValidatePatterns(patterns []PatternResult) (ValidationSummary, error)
	GetValidationMetrics() ValidationMetrics
}

// ============================================================================
// FUNCTION TYPES
// ============================================================================

// RuleFunction defines the signature for correlation rule evaluation functions
type RuleFunction func(ctx context.Context, ruleCtx *RuleContext) ([]Finding, error)

// ============================================================================
// UTILITY INTERFACES
// ============================================================================

// Serializer handles serialization of correlation data
type Serializer interface {
	Serialize(data interface{}) ([]byte, error)
	Deserialize(data []byte, target interface{}) error
	GetFormat() SerializationFormat
}

// Cache provides caching capabilities for correlation components
type Cache interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{}, ttl time.Duration) error
	Delete(key string) error
	Clear() error
	GetStats() CacheStats
}

// ============================================================================
// ENUMS AND CONSTANTS
// ============================================================================

// EngineType represents different types of correlation engines
type EngineType string

const (
	EngineTypeBasic             EngineType = "basic"
	EngineTypeEnhanced          EngineType = "enhanced"
	EngineTypePerfect           EngineType = "perfect"
	EngineTypePatternIntegrated EngineType = "pattern-integrated"
)

// ExportFormat represents different export formats for metrics
type ExportFormat string

const (
	ExportFormatPrometheus ExportFormat = "prometheus"
	ExportFormatOTEL       ExportFormat = "otel"
	ExportFormatJSON       ExportFormat = "json"
	ExportFormatCSV        ExportFormat = "csv"
)

// SerializationFormat represents different serialization formats
type SerializationFormat string

const (
	SerializationFormatJSON     SerializationFormat = "json"
	SerializationFormatProtobuf SerializationFormat = "protobuf"
	SerializationFormatMsgPack  SerializationFormat = "msgpack"
)

// SafetyLevel represents the safety level for autofix operations
type SafetyLevel string

const (
	SafetyLevelSafe      SafetyLevel = "safe"
	SafetyLevelModerate  SafetyLevel = "moderate"
	SafetyLevelRisky     SafetyLevel = "risky"
	SafetyLevelDangerous SafetyLevel = "dangerous"
)
