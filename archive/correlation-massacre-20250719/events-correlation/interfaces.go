package events_correlation

import (
	"context"
	"time"
)

// Rule represents a correlation rule that can be executed
type Rule struct {
	// Rule metadata
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    Category `json:"category"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Tags        []string `json:"tags"`

	// Rule configuration
	Enabled       bool          `json:"enabled"`
	MinConfidence float64       `json:"min_confidence"`
	Cooldown      time.Duration `json:"cooldown"`
	TTL           time.Duration `json:"ttl"`

	// Source requirements
	RequiredSources []EventSource `json:"required_sources"`
	OptionalSources []EventSource `json:"optional_sources"`

	// The correlation logic - this is where the magic happens!
	Evaluate RuleFunction `json:"-"`

	// Optional: Rule-specific configuration
	Config map[string]interface{} `json:"config"`

	// Runtime metadata
	LastExecuted   time.Time       `json:"last_executed"`
	ExecutionCount uint64          `json:"execution_count"`
	LastResult     *Result         `json:"last_result,omitempty"`
	Performance    RulePerformance `json:"performance"`
}

// RuleFunction is the signature for correlation rule evaluation functions
type RuleFunction func(ctx *Context) *Result

// RulePerformance tracks performance metrics for a rule
type RulePerformance struct {
	AverageExecutionTime time.Duration `json:"average_execution_time"`
	MaxExecutionTime     time.Duration `json:"max_execution_time"`
	MinExecutionTime     time.Duration `json:"min_execution_time"`
	TotalExecutionTime   time.Duration `json:"total_execution_time"`
	SuccessRate          float64       `json:"success_rate"`
	MemoryUsage          uint64        `json:"memory_usage"`
}

// Engine is the main correlation engine that manages and executes rules
type Engine interface {
	// Rule management
	RegisterRule(rule *Rule) error
	UnregisterRule(ruleID string) error
	GetRule(ruleID string) (*Rule, bool)
	ListRules() []*Rule
	EnableRule(ruleID string) error
	DisableRule(ruleID string) error

	// Rule execution
	ProcessEvents(ctx context.Context, events []Event) ([]*Result, error)
	ProcessWindow(ctx context.Context, window TimeWindow, events []Event) ([]*Result, error)

	// Configuration
	SetWindowSize(duration time.Duration)
	SetProcessingInterval(interval time.Duration)
	SetMaxConcurrentRules(limit int)

	// Metrics and monitoring
	GetStats() Stats
	GetRuleStats(ruleID string) (RulePerformance, error)

	// Lifecycle management
	Start(ctx context.Context) error
	Stop() error
	Health() error
}

// EventStore provides access to historical events for correlation
type EventStore interface {
	// Query events
	GetEvents(ctx context.Context, filter Filter) ([]Event, error)
	GetEventsInWindow(ctx context.Context, window TimeWindow, filter Filter) ([]Event, error)

	// Store events
	StoreEvent(ctx context.Context, event Event) error
	StoreBatch(ctx context.Context, events []Event) error

	// Metrics integration
	GetMetrics(ctx context.Context, name string, window TimeWindow) (MetricSeries, error)

	// Maintenance
	Cleanup(ctx context.Context, before time.Time) error
	Stats(ctx context.Context) (EventStoreStats, error)
}

// EventStoreStats contains statistics about the event store
type EventStoreStats struct {
	TotalEvents     uint64                 `json:"total_events"`
	EventsPerSource map[EventSource]uint64 `json:"events_per_source"`
	StorageSize     uint64                 `json:"storage_size"`
	OldestEvent     time.Time              `json:"oldest_event"`
	NewestEvent     time.Time              `json:"newest_event"`
	RetentionPeriod time.Duration          `json:"retention_period"`
	QueryLatency    time.Duration          `json:"query_latency"`
}

// ResultHandler processes correlation results
type ResultHandler interface {
	HandleResult(ctx context.Context, result *Result) error
	HandleBatch(ctx context.Context, results []*Result) error
}

// AlertManager manages alerting based on correlation results
type AlertManager interface {
	// Send alerts
	SendAlert(ctx context.Context, result *Result) error

	// Alert management
	SuppressAlert(ruleID string, duration time.Duration) error
	UnsuppressAlert(ruleID string) error
	IsAlertSuppressed(ruleID string) bool

	// Alert history
	GetAlertHistory(ctx context.Context, ruleID string, window TimeWindow) ([]*Result, error)
}

// MetricsCollector collects metrics about the correlation engine
type MetricsCollector interface {
	// Rule metrics
	RecordRuleExecution(ruleID string, duration time.Duration, success bool)
	RecordRuleResult(ruleID string, result *Result)

	// Engine metrics
	RecordEventProcessed(source EventSource)
	RecordCorrelationFound(category Category, severity Severity)
	RecordProcessingLatency(duration time.Duration)

	// Resource metrics
	RecordMemoryUsage(bytes uint64)
	RecordCPUUsage(percent float64)
}

// HealthChecker provides health checking capabilities
type HealthChecker interface {
	// Component health
	CheckEventStore(ctx context.Context) error
	CheckRuleEngine(ctx context.Context) error
	CheckAlertManager(ctx context.Context) error

	// Overall health
	Health(ctx context.Context) error

	// Detailed status
	Status(ctx context.Context) HealthStatus
}

// HealthStatus represents the health status of the correlation engine
type HealthStatus struct {
	Healthy           bool                       `json:"healthy"`
	Timestamp         time.Time                  `json:"timestamp"`
	ComponentStatuses map[string]ComponentStatus `json:"component_statuses"`
	Errors            []string                   `json:"errors,omitempty"`
	Warnings          []string                   `json:"warnings,omitempty"`
}

// ComponentStatus represents the status of a specific component
type ComponentStatus struct {
	Name     string            `json:"name"`
	Healthy  bool              `json:"healthy"`
	Latency  time.Duration     `json:"latency"`
	Error    string            `json:"error,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

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
	RequireSources(sources ...EventSource) RuleBuilder
	OptionalSources(sources ...EventSource) RuleBuilder

	// Evaluation function
	Evaluate(fn RuleFunction) RuleBuilder

	// Build the rule
	Build() *Rule
}

// RuleTemplate defines a template for creating similar rules
type RuleTemplate struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Category    Category               `json:"category"`
	Parameters  []TemplateParameter    `json:"parameters"`
	Template    string                 `json:"template"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// TemplateParameter defines a parameter for a rule template
type TemplateParameter struct {
	Name         string      `json:"name"`
	Type         string      `json:"type"`
	Description  string      `json:"description"`
	DefaultValue interface{} `json:"default_value"`
	Required     bool        `json:"required"`
	Validation   string      `json:"validation,omitempty"`
}

// RuleValidator validates rules before registration
type RuleValidator interface {
	ValidateRule(rule *Rule) error
	ValidateRuleFunction(fn RuleFunction) error
	ValidateConfiguration(config map[string]interface{}) error
}

// RuleRegistry manages rule registration and discovery
type RuleRegistry interface {
	// Rule registration
	Register(rule *Rule) error
	Unregister(ruleID string) error

	// Rule discovery
	List() []*Rule
	Find(filter RuleFilter) []*Rule
	Get(ruleID string) (*Rule, bool)

	// Rule templates
	RegisterTemplate(template *RuleTemplate) error
	CreateFromTemplate(templateName string, params map[string]interface{}) (*Rule, error)

	// Bulk operations
	RegisterBatch(rules []*Rule) error
	Export() ([]*Rule, error)
	Import(rules []*Rule) error
}

// RuleFilter defines criteria for filtering rules
type RuleFilter struct {
	Category      Category      `json:"category,omitempty"`
	Tags          []string      `json:"tags,omitempty"`
	Author        string        `json:"author,omitempty"`
	Enabled       *bool         `json:"enabled,omitempty"`
	MinConfidence *float64      `json:"min_confidence,omitempty"`
	Sources       []EventSource `json:"sources,omitempty"`
}

// Matches checks if a rule matches the filter criteria
func (rf RuleFilter) Matches(rule *Rule) bool {
	if rf.Category != "" && rule.Category != rf.Category {
		return false
	}

	if rf.Author != "" && rule.Author != rf.Author {
		return false
	}

	if rf.Enabled != nil && rule.Enabled != *rf.Enabled {
		return false
	}

	if rf.MinConfidence != nil && rule.MinConfidence < *rf.MinConfidence {
		return false
	}

	// Check if rule has any of the required tags
	if len(rf.Tags) > 0 {
		found := false
		for _, filterTag := range rf.Tags {
			for _, ruleTag := range rule.Tags {
				if ruleTag == filterTag {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check if rule requires any of the specified sources
	if len(rf.Sources) > 0 {
		found := false
		for _, filterSource := range rf.Sources {
			for _, ruleSource := range rule.RequiredSources {
				if ruleSource == filterSource {
					found = true
					break
				}
			}
			if found {
				break
			}
			for _, ruleSource := range rule.OptionalSources {
				if ruleSource == filterSource {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
