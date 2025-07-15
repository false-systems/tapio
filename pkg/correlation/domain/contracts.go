package domain

import (
	"context"
	"time"
)

// EventSource provides events for correlation
type EventSource interface {
	// GetEvents retrieves events matching the filter
	GetEvents(ctx context.Context, filter Filter) ([]Event, error)
	
	// Stream provides a continuous stream of events
	Stream(ctx context.Context, filter Filter) (<-chan Event, error)
	
	// GetSourceType returns the source type identifier
	GetSourceType() string
	
	// IsAvailable checks if the source is available
	IsAvailable() bool
	
	// Close closes the event source
	Close() error
}

// Rule defines a correlation rule
type Rule interface {
	// ID returns the unique rule identifier
	ID() string
	
	// Name returns the human-readable rule name
	Name() string
	
	// Evaluate evaluates the rule against events in the context
	Evaluate(ctx *Context) *Result
	
	// GetMinConfidence returns the minimum confidence threshold
	GetMinConfidence() float64
	
	// GetCooldown returns the cooldown period between rule executions
	GetCooldown() time.Duration
	
	// IsEnabled checks if the rule is enabled
	IsEnabled() bool
	
	// GetCategory returns the rule category
	GetCategory() Category
}

// Context provides the execution environment for correlation rules
type Context struct {
	// Time window for this correlation cycle
	Window TimeWindow
	
	// Events in the current window
	Events []Event
	
	// Correlation metadata
	CorrelationID string
	RuleID        string
	Metadata      map[string]string
}

// GetEvents returns events matching the filter
func (c *Context) GetEvents(filter Filter) []Event {
	var result []Event
	for _, event := range c.Events {
		if filter.Matches(event) {
			result = append(result, event)
		}
	}
	
	// Apply limit if specified
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	
	return result
}

// CountEvents returns the number of events matching the filter
func (c *Context) CountEvents(filter Filter) int {
	return len(c.GetEvents(filter))
}

// HasEvents checks if any events match the filter
func (c *Context) HasEvents(filter Filter) bool {
	return c.CountEvents(filter) > 0
}

// GetEventsBySource returns events from a specific source
func (c *Context) GetEventsBySource(source string) []Event {
	return c.GetEvents(Filter{Source: source})
}

// GetEventsByType returns events of a specific type
func (c *Context) GetEventsByType(eventType string) []Event {
	return c.GetEvents(Filter{Type: eventType})
}

// GetEventsForEntity returns events for a specific entity
func (c *Context) GetEventsForEntity(entityType, entityName string) []Event {
	return c.GetEvents(Filter{
		EntityType: entityType,
		EntityName: entityName,
	})
}

// Engine defines the core correlation engine interface
type Engine interface {
	// ProcessEvents processes a batch of events
	ProcessEvents(ctx context.Context, events []Event) ([]*Result, error)
	
	// RegisterRule registers a correlation rule
	RegisterRule(rule Rule) error
	
	// UnregisterRule removes a correlation rule
	UnregisterRule(ruleID string) error
	
	// GetRules returns all registered rules
	GetRules() []Rule
	
	// Start starts the correlation engine
	Start(ctx context.Context) error
	
	// Stop stops the correlation engine
	Stop() error
	
	// GetStats returns engine statistics
	GetStats() Stats
	
	// Health checks the engine health
	Health() error
}

// EventStore provides event storage and retrieval
type EventStore interface {
	// Store stores events
	Store(ctx context.Context, events []Event) error
	
	// Get retrieves events by IDs
	Get(ctx context.Context, ids []string) ([]Event, error)
	
	// Query queries events with filters
	Query(ctx context.Context, filter Filter) ([]Event, error)
	
	// Delete removes events
	Delete(ctx context.Context, ids []string) error
	
	// Cleanup removes old events
	Cleanup(ctx context.Context, before time.Time) error
}

// ResultHandler handles correlation results
type ResultHandler interface {
	// HandleResult processes a correlation result
	HandleResult(ctx context.Context, result *Result) error
	
	// GetHandlerType returns the handler type
	GetHandlerType() string
}

// MetricsCollector collects correlation metrics
type MetricsCollector interface {
	// RecordEventProcessed records an event processing metric
	RecordEventProcessed(source string, eventType string, duration time.Duration)
	
	// RecordCorrelationFound records a correlation found metric
	RecordCorrelationFound(ruleID string, confidence float64)
	
	// RecordRuleExecution records a rule execution metric
	RecordRuleExecution(ruleID string, duration time.Duration, success bool)
	
	// RecordEngineStats records engine statistics
	RecordEngineStats(stats Stats)
}

// Logger provides logging interface
type Logger interface {
	// Debug logs debug messages
	Debug(msg string, fields ...interface{})
	
	// Info logs info messages
	Info(msg string, fields ...interface{})
	
	// Warn logs warning messages
	Warn(msg string, fields ...interface{})
	
	// Error logs error messages
	Error(msg string, fields ...interface{})
	
	// With returns a logger with additional fields
	With(fields ...interface{}) Logger
}

// Configuration for the correlation engine
type Config struct {
	// Processing configuration
	WindowSize         time.Duration `json:"window_size"`
	ProcessingInterval time.Duration `json:"processing_interval"`
	MaxConcurrentRules int           `json:"max_concurrent_rules"`
	
	// Event processing
	MaxEventsPerWindow int           `json:"max_events_per_window"`
	EventRetention     time.Duration `json:"event_retention"`
	
	// Result handling
	MaxResultsPerRule int           `json:"max_results_per_rule"`
	ResultRetention   time.Duration `json:"result_retention"`
	
	// Performance
	EnableMetrics   bool          `json:"enable_metrics"`
	MetricsInterval time.Duration `json:"metrics_interval"`
	
	// Logging
	LogLevel string `json:"log_level"`
}