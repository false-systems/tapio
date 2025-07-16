// Package correlation implements event correlation and pattern detection
package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Engine is the main correlation engine implementation
type Engine struct {
	eventSources []domain.EventSource
	eventStore   domain.EventStore
	ruleEngine   domain.RuleEngine
	metrics      domain.MetricsCollector
	logger       domain.Logger
	
	// Processing state
	mu            sync.RWMutex
	correlations  map[string]*domain.Correlation
	insights      map[string]*domain.Insight
	patterns      map[string]*domain.Pattern
	
	// Configuration
	config Config
}

// Config holds configuration for the correlation engine
type Config struct {
	// Processing configuration
	CorrelationWindow   time.Duration `json:"correlation_window"`
	PatternWindow       time.Duration `json:"pattern_window"`
	InsightThreshold    float64       `json:"insight_threshold"`
	
	// Performance configuration
	MaxConcurrentEvents int           `json:"max_concurrent_events"`
	EventBufferSize     int           `json:"event_buffer_size"`
	FlushInterval       time.Duration `json:"flush_interval"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() Config {
	return Config{
		CorrelationWindow:   5 * time.Minute,
		PatternWindow:       1 * time.Hour,
		InsightThreshold:    0.7,
		MaxConcurrentEvents: 100,
		EventBufferSize:     1000,
		FlushInterval:       30 * time.Second,
	}
}

// New creates a new correlation engine
func New(config Config, logger domain.Logger) *Engine {
	if logger == nil {
		logger = &noOpLogger{}
	}
	
	return &Engine{
		config:       config,
		logger:       logger,
		correlations: make(map[string]*domain.Correlation),
		insights:     make(map[string]*domain.Insight),
		patterns:     make(map[string]*domain.Pattern),
	}
}

// WithEventSource adds an event source to the engine
func (e *Engine) WithEventSource(source domain.EventSource) *Engine {
	e.eventSources = append(e.eventSources, source)
	return e
}

// WithEventStore sets the event store
func (e *Engine) WithEventStore(store domain.EventStore) *Engine {
	e.eventStore = store
	return e
}

// WithRuleEngine sets the rule engine
func (e *Engine) WithRuleEngine(engine domain.RuleEngine) *Engine {
	e.ruleEngine = engine
	return e
}

// WithMetricsCollector sets the metrics collector
func (e *Engine) WithMetricsCollector(collector domain.MetricsCollector) *Engine {
	e.metrics = collector
	return e
}

// Start begins processing events
func (e *Engine) Start(ctx context.Context) error {
	if len(e.eventSources) == 0 {
		return fmt.Errorf("no event sources configured")
	}
	
	// Start event processing for each source
	for _, source := range e.eventSources {
		events, err := source.Subscribe(ctx)
		if err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", source.GetSourceType(), err)
		}
		
		go e.processEventStream(ctx, source.GetSourceType(), events)
	}
	
	// Start periodic flush
	go e.periodicFlush(ctx)
	
	e.logger.Info("correlation engine started", 
		"sources", len(e.eventSources),
		"config", e.config)
	
	return nil
}

// ProcessEvent implements domain.CorrelationEngine
func (e *Engine) ProcessEvent(ctx context.Context, event domain.Event) error {
	// Record metric
	if e.metrics != nil {
		e.metrics.RecordEvent(event.Type)
	}
	
	// Store event if we have a store
	if e.eventStore != nil {
		if err := e.eventStore.Store(ctx, event); err != nil {
			e.logger.Error("failed to store event", "error", err, "event_id", event.ID)
			if e.metrics != nil {
				e.metrics.RecordError("event_store_error")
			}
		}
	}
	
	// Evaluate rules if we have a rule engine
	if e.ruleEngine != nil {
		matches, err := e.ruleEngine.EvaluateEvent(ctx, event)
		if err != nil {
			e.logger.Error("failed to evaluate rules", "error", err, "event_id", event.ID)
			if e.metrics != nil {
				e.metrics.RecordError("rule_evaluation_error")
			}
		} else {
			for _, match := range matches {
				e.processRuleMatch(ctx, event, match)
			}
		}
	}
	
	// Perform correlation
	correlations := e.correlateEvent(ctx, event)
	for _, correlation := range correlations {
		e.storeCorrelation(correlation)
		if e.metrics != nil {
			e.metrics.RecordCorrelation(correlation.Type)
		}
	}
	
	// Generate insights
	insights := e.generateInsights(ctx, event, correlations)
	for _, insight := range insights {
		e.storeInsight(insight)
		if e.metrics != nil {
			e.metrics.RecordInsight(insight.Category)
		}
	}
	
	// Detect patterns
	e.detectPatterns(ctx, event)
	
	return nil
}

// GetCorrelations implements domain.CorrelationEngine
func (e *Engine) GetCorrelations(ctx context.Context, window domain.TimeWindow) ([]domain.Correlation, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	var result []domain.Correlation
	for _, correlation := range e.correlations {
		if correlation.Timestamp.After(window.Start) && correlation.Timestamp.Before(window.End) {
			result = append(result, *correlation)
		}
	}
	
	return result, nil
}

// GetInsights implements domain.CorrelationEngine
func (e *Engine) GetInsights(ctx context.Context, window domain.TimeWindow) ([]domain.Insight, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	var result []domain.Insight
	for _, insight := range e.insights {
		if insight.Timestamp.After(window.Start) && insight.Timestamp.Before(window.End) {
			result = append(result, *insight)
		}
	}
	
	return result, nil
}

// processEventStream processes events from a source
func (e *Engine) processEventStream(ctx context.Context, sourceType string, events <-chan domain.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				e.logger.Info("event stream closed", "source", sourceType)
				return
			}
			
			if err := e.ProcessEvent(ctx, event); err != nil {
				e.logger.Error("failed to process event", 
					"error", err, 
					"source", sourceType,
					"event_id", event.ID)
			}
		}
	}
}

// correlateEvent performs correlation on an event
func (e *Engine) correlateEvent(ctx context.Context, event domain.Event) []domain.Correlation {
	// This is a simplified implementation
	// Real implementation would use sophisticated correlation algorithms
	
	var correlations []domain.Correlation
	
	// Time-based correlation
	window := domain.TimeWindow{
		Start: event.Timestamp.Add(-e.config.CorrelationWindow),
		End:   event.Timestamp,
	}
	
	if e.eventStore != nil {
		relatedEvents, err := e.eventStore.Query(ctx, domain.QueryCriteria{
			TimeWindow: window,
			Types:      []string{event.Type},
		})
		
		if err == nil && len(relatedEvents) > 1 {
			eventIDs := make([]string, 0, len(relatedEvents))
			for _, evt := range relatedEvents {
				eventIDs = append(eventIDs, evt.ID)
			}
			
			correlation := domain.Correlation{
				ID:          fmt.Sprintf("corr_%s_%d", event.ID, time.Now().UnixNano()),
				Type:        "temporal",
				Events:      eventIDs,
				Confidence:  0.8,
				Description: fmt.Sprintf("Temporal correlation of %s events", event.Type),
				Timestamp:   time.Now(),
				TTL:         1 * time.Hour,
			}
			
			correlations = append(correlations, correlation)
		}
	}
	
	return correlations
}

// generateInsights generates insights from correlations
func (e *Engine) generateInsights(ctx context.Context, event domain.Event, correlations []domain.Correlation) []domain.Insight {
	var insights []domain.Insight
	
	// Generate insights based on correlations
	for _, correlation := range correlations {
		if correlation.Confidence >= e.config.InsightThreshold {
			insight := domain.Insight{
				ID:           fmt.Sprintf("insight_%s_%d", correlation.ID, time.Now().UnixNano()),
				Title:        fmt.Sprintf("Pattern detected: %s", correlation.Type),
				Description:  correlation.Description,
				Severity:     event.Severity.String(),
				Category:     correlation.Type,
				Timestamp:    time.Now(),
			}
			
			insights = append(insights, insight)
		}
	}
	
	return insights
}

// detectPatterns detects patterns in events
func (e *Engine) detectPatterns(ctx context.Context, event domain.Event) {
	// Simplified pattern detection
	// Real implementation would use pattern detection algorithms
}

// processRuleMatch processes a rule match
func (e *Engine) processRuleMatch(ctx context.Context, event domain.Event, match domain.RuleMatch) {
	// Process actions for the matched rule
	e.logger.Info("rule matched", 
		"rule_id", match.RuleID,
		"event_id", match.EventID,
		"confidence", match.Confidence)
}

// storeCorrelation stores a correlation
func (e *Engine) storeCorrelation(correlation domain.Correlation) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.correlations[correlation.ID] = &correlation
}

// storeInsight stores an insight
func (e *Engine) storeInsight(insight domain.Insight) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.insights[insight.ID] = &insight
}

// periodicFlush periodically flushes old data
func (e *Engine) periodicFlush(ctx context.Context) {
	ticker := time.NewTicker(e.config.FlushInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.flushOldData()
		}
	}
}

// flushOldData removes old correlations and insights
func (e *Engine) flushOldData() {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	now := time.Now()
	
	// Flush old correlations
	for id, correlation := range e.correlations {
		if correlation.TTL > 0 && now.Sub(correlation.Timestamp) > correlation.TTL {
			delete(e.correlations, id)
		}
	}
	
	// Flush old insights (keep for 24 hours)
	for id, insight := range e.insights {
		if now.Sub(insight.Timestamp) > 24*time.Hour {
			delete(e.insights, id)
		}
	}
}

// noOpLogger is a no-op logger implementation
type noOpLogger struct{}

func (n *noOpLogger) Debug(msg string, fields ...interface{}) {}
func (n *noOpLogger) Info(msg string, fields ...interface{}) {}
func (n *noOpLogger) Warn(msg string, fields ...interface{}) {}
func (n *noOpLogger) Error(msg string, fields ...interface{}) {}
func (n *noOpLogger) WithField(key string, value interface{}) domain.Logger { return n }
func (n *noOpLogger) WithFields(fields map[string]interface{}) domain.Logger { return n }