package correlation

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/adapters/sources"
	"github.com/yairfalse/tapio/pkg/correlation/core"
	"github.com/yairfalse/tapio/pkg/correlation/domain"
)

// Correlation is the main correlation service that orchestrates all components
type Correlation struct {
	engine       domain.Engine
	eventSources []domain.EventSource
	config       domain.Config
	logger       domain.Logger
}

// New creates a new correlation service with dependency injection
func New(config domain.Config, logger domain.Logger) *Correlation {
	if logger == nil {
		logger = &noOpLogger{}
	}
	
	// Create core engine with injected dependencies
	engine := core.NewCoreEngine(
		config,
		nil, // eventStore - can be injected later
		nil, // metricsCollector - can be injected later
		logger,
	)
	
	// Create default event sources based on platform
	eventSources := []domain.EventSource{
		sources.NewKubernetesStubSource(), // Always available
		sources.NewEBPFStubSource(),       // Platform-specific
		sources.NewSystemdStubSource(),    // Platform-specific
		sources.NewJournaldStubSource(),   // Platform-specific
	}
	
	return &Correlation{
		engine:       engine,
		eventSources: eventSources,
		config:       config,
		logger:       logger,
	}
}

// DefaultConfig returns default configuration
func DefaultConfig() domain.Config {
	return domain.Config{
		WindowSize:         5 * time.Minute,
		ProcessingInterval: 30 * time.Second,
		MaxConcurrentRules: 10,
		MaxEventsPerWindow: 1000,
		EventRetention:     24 * time.Hour,
		MaxResultsPerRule:  100,
		ResultRetention:    7 * 24 * time.Hour,
		EnableMetrics:      true,
		MetricsInterval:    1 * time.Minute,
		LogLevel:           "info",
	}
}

// WithEventStore sets the event store
func (c *Correlation) WithEventStore(store domain.EventStore) *Correlation {
	// If the engine supports setting event store, do it here
	// For now, this is a placeholder for future implementation
	return c
}

// WithMetricsCollector sets the metrics collector
func (c *Correlation) WithMetricsCollector(collector domain.MetricsCollector) *Correlation {
	// If the engine supports setting metrics collector, do it here
	// For now, this is a placeholder for future implementation
	return c
}

// AddEventSource adds an event source
func (c *Correlation) AddEventSource(source domain.EventSource) {
	c.eventSources = append(c.eventSources, source)
}

// AddResultHandler adds a result handler
func (c *Correlation) AddResultHandler(handler domain.ResultHandler) {
	if coreEngine, ok := c.engine.(*core.CoreEngine); ok {
		coreEngine.AddResultHandler(handler)
	}
}

// RegisterRule registers a correlation rule
func (c *Correlation) RegisterRule(rule domain.Rule) error {
	return c.engine.RegisterRule(rule)
}

// RegisterDefaultRules registers a set of default correlation rules
func (c *Correlation) RegisterDefaultRules() error {
	// High frequency events rule
	rule1 := core.NewHighFrequencyRule(
		"high_frequency_events",
		"High Frequency Events",
		10,
		time.Minute,
	)
	if err := c.RegisterRule(rule1); err != nil {
		return fmt.Errorf("failed to register high frequency rule: %w", err)
	}
	
	// Error spike rule
	rule2 := core.NewErrorSpikeRule(
		"error_spike",
		"Error Spike Detection",
	)
	if err := c.RegisterRule(rule2); err != nil {
		return fmt.Errorf("failed to register error spike rule: %w", err)
	}
	
	// Resource exhaustion rule
	rule3 := core.NewResourceExhaustionRule(
		"resource_exhaustion",
		"Resource Exhaustion Detection",
	)
	if err := c.RegisterRule(rule3); err != nil {
		return fmt.Errorf("failed to register resource exhaustion rule: %w", err)
	}
	
	// Sequential events rule
	rule4 := core.NewSequentialEventsRule(
		"pod_restart_sequence",
		"Pod Restart Sequence",
		[]string{"pod_killed", "pod_started"},
		5*time.Minute,
	)
	if err := c.RegisterRule(rule4); err != nil {
		return fmt.Errorf("failed to register sequential events rule: %w", err)
	}
	
	// Cascading failure rule
	rule5 := core.NewCascadingFailureRule(
		"cascading_failure",
		"Cascading Failure Detection",
	)
	if err := c.RegisterRule(rule5); err != nil {
		return fmt.Errorf("failed to register cascading failure rule: %w", err)
	}
	
	c.logger.Info("registered default correlation rules", "count", 5)
	
	return nil
}

// Start starts the correlation service
func (c *Correlation) Start(ctx context.Context) error {
	c.logger.Info("starting correlation service")
	
	// Start the correlation engine
	if err := c.engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start correlation engine: %w", err)
	}
	
	// Log available event sources
	availableSources := 0
	for _, source := range c.eventSources {
		if source.IsAvailable() {
			availableSources++
			c.logger.Info("event source available", "source", source.GetSourceType())
		} else {
			c.logger.Debug("event source not available", "source", source.GetSourceType())
		}
	}
	
	c.logger.Info("correlation service started", "available_sources", availableSources)
	
	return nil
}

// Stop stops the correlation service
func (c *Correlation) Stop() error {
	c.logger.Info("stopping correlation service")
	
	// Stop the correlation engine
	if err := c.engine.Stop(); err != nil {
		return fmt.Errorf("failed to stop correlation engine: %w", err)
	}
	
	// Close event sources
	for _, source := range c.eventSources {
		if err := source.Close(); err != nil {
			c.logger.Error("failed to close event source", "source", source.GetSourceType(), "error", err)
		}
	}
	
	c.logger.Info("correlation service stopped")
	
	return nil
}

// ProcessEvents processes events through the correlation engine
func (c *Correlation) ProcessEvents(ctx context.Context, events []domain.Event) ([]*domain.Result, error) {
	return c.engine.ProcessEvents(ctx, events)
}

// GetStats returns correlation statistics
func (c *Correlation) GetStats() domain.Stats {
	return c.engine.GetStats()
}

// GetAvailableEventSources returns available event sources
func (c *Correlation) GetAvailableEventSources() []domain.EventSource {
	var available []domain.EventSource
	for _, source := range c.eventSources {
		if source.IsAvailable() {
			available = append(available, source)
		}
	}
	return available
}

// GetRules returns registered rules
func (c *Correlation) GetRules() []domain.Rule {
	return c.engine.GetRules()
}

// Health checks the correlation service health
func (c *Correlation) Health() error {
	return c.engine.Health()
}

// noOpLogger is a no-op logger implementation
type noOpLogger struct{}

func (l *noOpLogger) Debug(msg string, fields ...interface{}) {}
func (l *noOpLogger) Info(msg string, fields ...interface{})  {}
func (l *noOpLogger) Warn(msg string, fields ...interface{})  {}
func (l *noOpLogger) Error(msg string, fields ...interface{}) {}
func (l *noOpLogger) With(fields ...interface{}) domain.Logger { return l }