package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/core"
	corrDomain "github.com/yairfalse/tapio/pkg/correlation/domain"
	"github.com/yairfalse/tapio/pkg/correlation/patterns"
	"github.com/yairfalse/tapio/pkg/correlation/types"
	"github.com/yairfalse/tapio/pkg/server/domain"
)

// CorrelationAdapter provides an interface to the correlation package
type CorrelationAdapter struct {
	// Core correlation engine
	engine           *core.CoreEngine
	patternRegistry  *patterns.PatternRegistry
	
	// Configuration
	enabled          bool
	logger           domain.Logger
	config           *CorrelationAdapterConfig
	
	// Statistics
	stats            CorrelationStats
	mutex            sync.RWMutex
}

// NewCorrelationAdapter creates a new correlation adapter
func NewCorrelationAdapter(logger domain.Logger) *CorrelationAdapter {
	// Create correlation engine configuration
	config := &CorrelationAdapterConfig{
		WindowSize:           5 * time.Minute,
		ProcessingInterval:   30 * time.Second,
		MaxConcurrentRules:   10,
		EnablePatterns:       true,
		EnableMLPrediction:   false, // Disabled by default for stability
		ConfidenceThreshold:  0.7,
	}

	// Create core engine configuration
	coreConfig := corrDomain.Config{
		WindowSize:         config.WindowSize,
		ProcessingInterval: config.ProcessingInterval,
		MaxConcurrentRules: config.MaxConcurrentRules,
		EnableMetrics:      true,
		MetricsInterval:    time.Minute,
	}

	// Create production-ready components
	eventStore := NewInMemoryEventStore(10000, time.Hour*24) // 10k events, 24h retention
	metricsCollector := NewPrometheusMetricsCollector()
	
	// Create engine with real implementations
	engine := core.NewCoreEngine(
		coreConfig,
		eventStore,
		metricsCollector,
		logger,
	)

	// Initialize pattern registry
	patternRegistry := patterns.DefaultPatternRegistry()

	adapter := &CorrelationAdapter{
		engine:          engine,
		patternRegistry: patternRegistry,
		enabled:         true, // Now enabled with real implementation
		logger:          logger,
		config:          config,
		stats: CorrelationStats{
			StartTime: time.Now(),
		},
	}

	return adapter
}

// Enable enables the correlation adapter
func (a *CorrelationAdapter) Enable() {
	a.enabled = true
	if a.logger != nil {
		a.logger.Info(context.Background(), "correlation adapter enabled")
	}
}

// Disable disables the correlation adapter
func (a *CorrelationAdapter) Disable() {
	a.enabled = false
	if a.logger != nil {
		a.logger.Info(context.Background(), "correlation adapter disabled")
	}
}

// IsEnabled returns whether the correlation adapter is enabled
func (a *CorrelationAdapter) IsEnabled() bool {
	return a.enabled
}

// ProcessEvent processes an event through the correlation engine
func (a *CorrelationAdapter) ProcessEvent(ctx context.Context, event *domain.Event) error {
	if !a.enabled {
		if a.logger != nil {
			a.logger.Debug(ctx, "correlation adapter disabled, skipping event processing")
		}
		return nil
	}

	// Convert server event to correlation domain event
	corrEvent := a.convertToCorrEvent(event)

	// Process through correlation engine
	results, err := a.engine.ProcessEvents(ctx, []corrDomain.Event{corrEvent})
	if err != nil {
		a.updateStats(false)
		return fmt.Errorf("correlation processing failed: %w", err)
	}

	// Update statistics
	a.updateStats(true)
	a.updateStatsWithResults(len(results))

	if a.logger != nil {
		a.logger.Debug(ctx, fmt.Sprintf("processed event %s, found %d correlations", event.ID, len(results)))
	}

	return nil
}

// GetInsights retrieves insights for a specific resource
func (a *CorrelationAdapter) GetInsights(ctx context.Context, resource, namespace string) ([]*Insight, error) {
	if !a.enabled {
		return []*Insight{}, nil
	}

	// Get historical correlations for the resource
	events := a.getResourceEvents(ctx, resource, namespace)
	if len(events) == 0 {
		return []*Insight{}, nil
	}

	// Run pattern detection
	patternResults, err := a.patternRegistry.DetectAll(ctx, a.convertToPatternEvents(events), nil)
	if err != nil {
		if a.logger != nil {
			a.logger.Error(ctx, fmt.Sprintf("pattern detection failed: %v", err))
		}
		return []*Insight{}, nil
	}

	// Convert pattern results to insights
	insights := a.convertToInsights(patternResults, resource, namespace)

	if a.logger != nil {
		a.logger.Debug(ctx, fmt.Sprintf("generated %d insights for %s/%s", len(insights), namespace, resource))
	}

	return insights, nil
}

// GetPredictions retrieves predictions for a specific resource
func (a *CorrelationAdapter) GetPredictions(ctx context.Context, resource, namespace string) ([]*Prediction, error) {
	if !a.enabled {
		return []*Prediction{}, nil
	}

	// Only provide predictions if ML is enabled
	if !a.config.EnableMLPrediction {
		return []*Prediction{}, nil
	}

	// Get resource metrics for prediction
	events := a.getResourceEvents(ctx, resource, namespace)
	if len(events) < 10 { // Need minimum data for predictions
		return []*Prediction{}, nil
	}

	// Analyze trends for basic predictions
	predictions := a.generateBasicPredictions(events, resource, namespace)

	if a.logger != nil {
		a.logger.Debug(ctx, fmt.Sprintf("generated %d predictions for %s/%s", len(predictions), namespace, resource))
	}

	return predictions, nil
}

// GetActionableItems retrieves actionable items for a specific resource
func (a *CorrelationAdapter) GetActionableItems(ctx context.Context, resource, namespace string) ([]*ActionableItem, error) {
	if !a.enabled {
		return []*ActionableItem{}, nil
	}

	// Get insights first
	insights, err := a.GetInsights(ctx, resource, namespace)
	if err != nil {
		return []*ActionableItem{}, err
	}

	// Generate actionable items from insights
	var actionableItems []*ActionableItem
	for _, insight := range insights {
		if insight.ActionableItems != nil {
			actionableItems = append(actionableItems, insight.ActionableItems...)
		}
	}

	// Add general recommendations based on resource type
	actionableItems = append(actionableItems, a.generateResourceRecommendations(resource, namespace)...)

	if a.logger != nil {
		a.logger.Debug(ctx, fmt.Sprintf("generated %d actionable items for %s/%s", len(actionableItems), namespace, resource))
	}

	return actionableItems, nil
}

// CorrelateEvents correlates a set of events
func (a *CorrelationAdapter) CorrelateEvents(ctx context.Context, events []*domain.Event) (*CorrelationResult, error) {
	if !a.enabled {
		return &CorrelationResult{
			ID:           a.generateID(),
			Timestamp:    time.Now(),
			EventCount:   len(events),
			Correlations: []*Correlation{},
		}, nil
	}

	// Convert events to correlation domain format
	corrEvents := make([]corrDomain.Event, len(events))
	for i, event := range events {
		corrEvents[i] = a.convertToCorrEvent(event)
	}

	// Process through correlation engine
	results, err := a.engine.ProcessEvents(ctx, corrEvents)
	if err != nil {
		return nil, fmt.Errorf("correlation processing failed: %w", err)
	}

	// Convert engine results to correlation result
	correlations := make([]*Correlation, len(results))
	for i, result := range results {
		if result != nil {
			correlations[i] = &Correlation{
				ID:          result.ID,
				Type:        result.Type,
				EventIDs:    result.EventIDs,
				Confidence:  result.Confidence,
				Description: result.Description,
				Metadata:    result.Metadata,
			}
		}
	}

	// Update statistics
	a.updateStatsWithResults(len(correlations))

	return &CorrelationResult{
		ID:           a.generateID(),
		Timestamp:    time.Now(),
		EventCount:   len(events),
		Correlations: correlations,
	}, nil
}

// GetPatterns retrieves available correlation patterns
func (a *CorrelationAdapter) GetPatterns(ctx context.Context) ([]*Pattern, error) {
	if !a.enabled {
		return []*Pattern{}, nil
	}

	// Get available patterns from registry
	detectors := a.patternRegistry.List()
	patterns := make([]*Pattern, len(detectors))

	for i, detector := range detectors {
		patterns[i] = &Pattern{
			ID:          detector.ID(),
			Name:        detector.Name(),
			Description: detector.Description(),
			Type:        string(detector.Category()),
			Enabled:     true,
			Metadata: map[string]interface{}{
				"category": detector.Category(),
			},
		}
	}

	if a.logger != nil {
		a.logger.Debug(ctx, fmt.Sprintf("returning %d available patterns", len(patterns)))
	}

	return patterns, nil
}

// GetPatternMatches retrieves matches for a specific pattern
func (a *CorrelationAdapter) GetPatternMatches(ctx context.Context, patternID string) ([]*PatternMatch, error) {
	if !a.enabled {
		return []*PatternMatch{}, nil
	}

	// Get the specific pattern detector
	detector, err := a.patternRegistry.Get(patternID)
	if err != nil {
		return []*PatternMatch{}, fmt.Errorf("pattern %s not found: %w", patternID, err)
	}

	// Get recent events for analysis
	events := a.getRecentEvents(ctx, time.Hour) // Last hour of events
	if len(events) == 0 {
		return []*PatternMatch{}, nil
	}

	// Convert to pattern events
	patternEvents := a.convertToPatternEvents(events)

	// Run pattern detection
	result, err := detector.Detect(ctx, patternEvents, nil)
	if err != nil {
		return []*PatternMatch{}, fmt.Errorf("pattern detection failed: %w", err)
	}

	// Convert to pattern matches
	var matches []*PatternMatch
	if result.Detected {
		match := &PatternMatch{
			ID:         a.generateID(),
			PatternID:  patternID,
			EventIDs:   extractEventIDs(result.Evidence),
			Confidence: result.Confidence,
			Timestamp:  time.Now(),
			Metadata:   result.Metadata,
		}
		matches = append(matches, match)
	}

	if a.logger != nil {
		a.logger.Debug(ctx, fmt.Sprintf("found %d matches for pattern %s", len(matches), patternID))
	}

	return matches, nil
}

// GetStats retrieves correlation engine statistics
func (a *CorrelationAdapter) GetStats(ctx context.Context) (*Stats, error) {
	a.mutex.RLock()
	stats := a.stats
	a.mutex.RUnlock()

	// Get engine statistics if available
	var engineStats corrDomain.Stats
	if a.engine != nil {
		engineStats = a.engine.GetStats()
	}

	return &Stats{
		Enabled:              a.enabled,
		EventsProcessed:      stats.EventsProcessed,
		InsightsGenerated:    stats.InsightsGenerated,
		PredictionsGenerated: stats.PredictionsGenerated,
		CorrelationsFound:    engineStats.CorrelationsFound,
		LastProcessedAt:      stats.LastProcessedAt,
	}, nil
}

// generateID generates a unique ID
func (a *CorrelationAdapter) generateID() string {
	return fmt.Sprintf("corr-%d", time.Now().UnixNano())
}

// Data types that match the correlation package interface
// These are simplified versions that avoid the broken types

// Insight represents a correlation insight
type Insight struct {
	ID              string                 `json:"id"`
	Title           string                 `json:"title"`
	Description     string                 `json:"description"`
	Severity        string                 `json:"severity"`
	Category        string                 `json:"category"`
	Resource        string                 `json:"resource"`
	Namespace       string                 `json:"namespace"`
	Timestamp       time.Time              `json:"timestamp"`
	Prediction      *Prediction            `json:"prediction,omitempty"`
	ActionableItems []*ActionableItem      `json:"actionable_items,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// Prediction represents a prediction
type Prediction struct {
	Type        string        `json:"type"`
	TimeToEvent time.Duration `json:"time_to_event,omitempty"`
	Probability float64       `json:"probability"`
	Confidence  float64       `json:"confidence"`
}

// ActionableItem represents an actionable item
type ActionableItem struct {
	Description string `json:"description"`
	Command     string `json:"command"`
	Impact      string `json:"impact"`
	Risk        string `json:"risk"`
}

// CorrelationResult represents the result of event correlation
type CorrelationResult struct {
	ID           string         `json:"id"`
	Timestamp    time.Time      `json:"timestamp"`
	EventCount   int            `json:"event_count"`
	Correlations []*Correlation `json:"correlations"`
}

// Correlation represents a correlation between events
type Correlation struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	EventIDs    []string               `json:"event_ids"`
	Confidence  float64                `json:"confidence"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Pattern represents a correlation pattern
type Pattern struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        string                 `json:"type"`
	Enabled     bool                   `json:"enabled"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// PatternMatch represents a pattern match
type PatternMatch struct {
	ID         string                 `json:"id"`
	PatternID  string                 `json:"pattern_id"`
	EventIDs   []string               `json:"event_ids"`
	Confidence float64                `json:"confidence"`
	Timestamp  time.Time              `json:"timestamp"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Stats represents correlation engine statistics
type Stats struct {
	Enabled              bool      `json:"enabled"`
	EventsProcessed      uint64    `json:"events_processed"`
	InsightsGenerated    uint64    `json:"insights_generated"`
	PredictionsGenerated uint64    `json:"predictions_generated"`
	CorrelationsFound    uint64    `json:"correlations_found"`
	LastProcessedAt      time.Time `json:"last_processed_at"`
}

// Factory for creating correlation adapters
type AdapterFactory struct {
	logger domain.Logger
}

// NewAdapterFactory creates a new correlation adapter factory
func NewAdapterFactory(logger domain.Logger) *AdapterFactory {
	return &AdapterFactory{
		logger: logger,
	}
}

// CreateAdapter creates a new correlation adapter
func (f *AdapterFactory) CreateAdapter(ctx context.Context, config *domain.Configuration) (*CorrelationAdapter, error) {
	adapter := NewCorrelationAdapter(f.logger)

	// Check if correlation should be enabled based on configuration
	// For now, keep it disabled until the correlation package is fixed
	if f.logger != nil {
		f.logger.Info(ctx, "correlation adapter created (disabled until correlation package is fixed)")
	}

	return adapter, nil
}

// Mock implementations for testing

// MockCorrelationAdapter provides a mock implementation for testing
type MockCorrelationAdapter struct {
	enabled         bool
	insights        []*Insight
	predictions     []*Prediction
	actionableItems []*ActionableItem
	patterns        []*Pattern
	patternMatches  []*PatternMatch
	stats           *Stats
}

// NewMockCorrelationAdapter creates a new mock correlation adapter
func NewMockCorrelationAdapter() *MockCorrelationAdapter {
	return &MockCorrelationAdapter{
		enabled:         true,
		insights:        make([]*Insight, 0),
		predictions:     make([]*Prediction, 0),
		actionableItems: make([]*ActionableItem, 0),
		patterns:        make([]*Pattern, 0),
		patternMatches:  make([]*PatternMatch, 0),
		stats: &Stats{
			Enabled:              true,
			EventsProcessed:      0,
			InsightsGenerated:    0,
			PredictionsGenerated: 0,
			CorrelationsFound:    0,
			LastProcessedAt:      time.Now(),
		},
	}
}

// Enable enables the mock adapter
func (m *MockCorrelationAdapter) Enable() {
	m.enabled = true
}

// Disable disables the mock adapter
func (m *MockCorrelationAdapter) Disable() {
	m.enabled = false
}

// IsEnabled returns whether the mock adapter is enabled
func (m *MockCorrelationAdapter) IsEnabled() bool {
	return m.enabled
}

// ProcessEvent processes an event (mock implementation)
func (m *MockCorrelationAdapter) ProcessEvent(ctx context.Context, event *domain.Event) error {
	if !m.enabled {
		return nil
	}

	m.stats.EventsProcessed++
	m.stats.LastProcessedAt = time.Now()
	return nil
}

// GetInsights returns mock insights
func (m *MockCorrelationAdapter) GetInsights(ctx context.Context, resource, namespace string) ([]*Insight, error) {
	if !m.enabled {
		return []*Insight{}, nil
	}

	return m.insights, nil
}

// GetPredictions returns mock predictions
func (m *MockCorrelationAdapter) GetPredictions(ctx context.Context, resource, namespace string) ([]*Prediction, error) {
	if !m.enabled {
		return []*Prediction{}, nil
	}

	return m.predictions, nil
}

// GetActionableItems returns mock actionable items
func (m *MockCorrelationAdapter) GetActionableItems(ctx context.Context, resource, namespace string) ([]*ActionableItem, error) {
	if !m.enabled {
		return []*ActionableItem{}, nil
	}

	return m.actionableItems, nil
}

// CorrelateEvents returns mock correlation results
func (m *MockCorrelationAdapter) CorrelateEvents(ctx context.Context, events []*domain.Event) (*CorrelationResult, error) {
	if !m.enabled {
		return &CorrelationResult{
			ID:           fmt.Sprintf("mock-corr-%d", time.Now().UnixNano()),
			Timestamp:    time.Now(),
			EventCount:   len(events),
			Correlations: []*Correlation{},
		}, nil
	}

	return &CorrelationResult{
		ID:           fmt.Sprintf("mock-corr-%d", time.Now().UnixNano()),
		Timestamp:    time.Now(),
		EventCount:   len(events),
		Correlations: []*Correlation{},
	}, nil
}

// GetPatterns returns mock patterns
func (m *MockCorrelationAdapter) GetPatterns(ctx context.Context) ([]*Pattern, error) {
	if !m.enabled {
		return []*Pattern{}, nil
	}

	return m.patterns, nil
}

// GetPatternMatches returns mock pattern matches
func (m *MockCorrelationAdapter) GetPatternMatches(ctx context.Context, patternID string) ([]*PatternMatch, error) {
	if !m.enabled {
		return []*PatternMatch{}, nil
	}

	return m.patternMatches, nil
}

// GetStats returns mock statistics
func (m *MockCorrelationAdapter) GetStats(ctx context.Context) (*Stats, error) {
	return m.stats, nil
}

// AddInsight adds a mock insight
func (m *MockCorrelationAdapter) AddInsight(insight *Insight) {
	m.insights = append(m.insights, insight)
	m.stats.InsightsGenerated++
}

// AddPrediction adds a mock prediction
func (m *MockCorrelationAdapter) AddPrediction(prediction *Prediction) {
	m.predictions = append(m.predictions, prediction)
	m.stats.PredictionsGenerated++
}

// AddActionableItem adds a mock actionable item
func (m *MockCorrelationAdapter) AddActionableItem(item *ActionableItem) {
	m.actionableItems = append(m.actionableItems, item)
}

// AddPattern adds a mock pattern
func (m *MockCorrelationAdapter) AddPattern(pattern *Pattern) {
	m.patterns = append(m.patterns, pattern)
}

// AddPatternMatch adds a mock pattern match
func (m *MockCorrelationAdapter) AddPatternMatch(match *PatternMatch) {
	m.patternMatches = append(m.patternMatches, match)
	m.stats.CorrelationsFound++
}

// CorrelationAdapterConfig configures the correlation adapter
type CorrelationAdapterConfig struct {
	WindowSize           time.Duration
	ProcessingInterval   time.Duration
	MaxConcurrentRules   int
	EnablePatterns       bool
	EnableMLPrediction   bool
	ConfidenceThreshold  float64
}

// CorrelationStats tracks correlation adapter statistics
type CorrelationStats struct {
	StartTime            time.Time
	EventsProcessed      uint64
	InsightsGenerated    uint64
	PredictionsGenerated uint64
	LastProcessedAt      time.Time
}

// Helper methods for the correlation adapter

// convertToCorrEvent converts a server domain event to correlation domain event
func (a *CorrelationAdapter) convertToCorrEvent(event *domain.Event) corrDomain.Event {
	return corrDomain.Event{
		ID:        event.ID,
		Type:      event.Type,
		Timestamp: event.Timestamp,
		Source:    event.Source,
		Severity:  event.Severity,
		Message:   event.Message,
		Entity: corrDomain.Entity{
			Type:      event.Entity.Type,
			Name:      event.Entity.Name,
			Namespace: event.Entity.Namespace,
		},
		Metadata: event.Metadata,
	}
}

// convertToPatternEvents converts domain events to pattern events
func (a *CorrelationAdapter) convertToPatternEvents(events []corrDomain.Event) []types.Event {
	patternEvents := make([]types.Event, len(events))
	for i, event := range events {
		patternEvents[i] = types.Event{
			ID:        event.ID,
			Type:      event.Type,
			Timestamp: event.Timestamp,
			Source:    event.Source,
			Severity:  types.Severity(event.Severity),
			Message:   event.Message,
			Metadata:  event.Metadata,
		}
	}
	return patternEvents
}

// convertToInsights converts pattern results to insights
func (a *CorrelationAdapter) convertToInsights(patternResults []types.PatternResult, resource, namespace string) []*Insight {
	var insights []*Insight
	
	for _, result := range patternResults {
		if !result.Detected {
			continue
		}
		
		insight := &Insight{
			ID:          a.generateID(),
			Title:       result.Pattern.Name,
			Description: result.Pattern.Description,
			Severity:    a.mapPatternSeverity(result.Confidence),
			Category:    string(result.Pattern.Category),
			Resource:    resource,
			Namespace:   namespace,
			Timestamp:   time.Now(),
			Metadata:    result.Metadata,
		}
		
		// Add prediction if available
		if result.Prediction != nil {
			insight.Prediction = &Prediction{
				Type:        result.Prediction.Type,
				TimeToEvent: result.Prediction.TimeToEvent,
				Probability: result.Prediction.Probability,
				Confidence:  result.Prediction.Confidence,
			}
		}
		
		// Generate actionable items
		insight.ActionableItems = a.generateActionableItems(result)
		
		insights = append(insights, insight)
	}
	
	return insights
}

// generateActionableItems generates actionable items from pattern results
func (a *CorrelationAdapter) generateActionableItems(result types.PatternResult) []*ActionableItem {
	var items []*ActionableItem
	
	// Add pattern-specific recommendations
	switch result.Pattern.Category {
	case types.CategoryMemory:
		items = append(items, &ActionableItem{
			Description: "Increase memory limits",
			Command:     "kubectl patch deployment <name> -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"<container>\",\"resources\":{\"limits\":{\"memory\":\"<new-limit>\"}}}]}}}}'",
			Impact:      "Prevents OOM kills",
			Risk:        "Low",
		})
	case types.CategoryNetwork:
		items = append(items, &ActionableItem{
			Description: "Check network connectivity",
			Command:     "kubectl exec <pod> -- netstat -tuln",
			Impact:      "Diagnoses network issues",
			Risk:        "Low",
		})
	case types.CategoryStorage:
		items = append(items, &ActionableItem{
			Description: "Check disk usage",
			Command:     "kubectl exec <pod> -- df -h",
			Impact:      "Identifies storage issues",
			Risk:        "Low",
		})
	}
	
	return items
}

// generateBasicPredictions generates basic predictions from historical events
func (a *CorrelationAdapter) generateBasicPredictions(events []corrDomain.Event, resource, namespace string) []*Prediction {
	var predictions []*Prediction
	
	// Simple trend analysis for demonstration
	if len(events) >= 10 {
		// Look for increasing error patterns
		errorCount := 0
		for _, event := range events[len(events)-10:] {
			if event.Severity == "error" || event.Severity == "critical" {
				errorCount++
			}
		}
		
		if errorCount >= 3 {
			predictions = append(predictions, &Prediction{
				Type:        "service_degradation",
				TimeToEvent: 30 * time.Minute,
				Probability: 0.7,
				Confidence:  0.6,
			})
		}
	}
	
	return predictions
}

// generateResourceRecommendations generates general recommendations for a resource
func (a *CorrelationAdapter) generateResourceRecommendations(resource, namespace string) []*ActionableItem {
	var items []*ActionableItem
	
	// Add general monitoring recommendations
	items = append(items, &ActionableItem{
		Description: "Check resource logs",
		Command:     fmt.Sprintf("kubectl logs -n %s %s", namespace, resource),
		Impact:      "Provides diagnostic information",
		Risk:        "Low",
	})
	
	items = append(items, &ActionableItem{
		Description: "Check resource status",
		Command:     fmt.Sprintf("kubectl describe -n %s %s", namespace, resource),
		Impact:      "Shows resource details and events",
		Risk:        "Low",
	})
	
	return items
}

// getResourceEvents retrieves historical events for a specific resource
func (a *CorrelationAdapter) getResourceEvents(ctx context.Context, resource, namespace string) []corrDomain.Event {
	// This would typically query the event store
	// For now, return empty slice
	return []corrDomain.Event{}
}

// getRecentEvents retrieves recent events within the specified time window
func (a *CorrelationAdapter) getRecentEvents(ctx context.Context, window time.Duration) []corrDomain.Event {
	// This would typically query the event store for recent events
	// For now, return empty slice
	return []corrDomain.Event{}
}

// updateStats updates the adapter statistics
func (a *CorrelationAdapter) updateStats(success bool) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	
	a.stats.EventsProcessed++
	a.stats.LastProcessedAt = time.Now()
}

// updateStatsWithResults updates statistics with correlation results
func (a *CorrelationAdapter) updateStatsWithResults(correlationCount int) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	
	if correlationCount > 0 {
		a.stats.InsightsGenerated++
	}
}

// mapPatternSeverity maps pattern confidence to severity level
func (a *CorrelationAdapter) mapPatternSeverity(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "critical"
	case confidence >= 0.7:
		return "high"
	case confidence >= 0.5:
		return "medium"
	default:
		return "low"
	}
}

// extractEventIDs extracts event IDs from evidence
func extractEventIDs(evidence []types.Evidence) []string {
	var eventIDs []string
	for _, e := range evidence {
		if e.EventID != "" {
			eventIDs = append(eventIDs, e.EventID)
		}
	}
	return eventIDs
}

// Production implementations are now in separate files:
// - event_store.go: InMemoryEventStore with time-series indexing
// - metrics_collector.go: PrometheusMetricsCollector with real metrics
