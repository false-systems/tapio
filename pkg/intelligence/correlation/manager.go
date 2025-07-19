package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	// "github.com/yairfalse/tapio/pkg/patternrecognition"
)

// CollectionManager provides semantic correlation for collected events
type CollectionManager struct {
	// Semantic correlation engine
	semanticEngine *SemanticCorrelationEngine

	// Event processing
	eventBus    chan domain.Event
	insightChan chan domain.Insight

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	config Config
}

// Config for the collection manager
type Config struct {
	EventBufferSize          int
	PatternDetectionInterval time.Duration
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		EventBufferSize:          1000,
		PatternDetectionInterval: 5 * time.Second,
	}
}

// NewCollectionManager creates a collection manager with semantic correlation
func NewCollectionManager(config Config) *CollectionManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &CollectionManager{
		semanticEngine: NewSemanticCorrelationEngine(),
		eventBus:       make(chan domain.Event, config.EventBufferSize),
		insightChan:    make(chan domain.Insight, 100),
		ctx:            ctx,
		cancel:         cancel,
		config:         config,
	}
}

// Start begins semantic correlation processing
func (cm *CollectionManager) Start() error {
	// Start semantic correlation engine
	if err := cm.semanticEngine.Start(); err != nil {
		return fmt.Errorf("failed to start semantic engine: %w", err)
	}

	// Start event processing goroutine
	cm.wg.Add(1)
	go cm.processEvents()

	// Connect insight channels
	cm.wg.Add(1)
	go cm.forwardInsights()

	return nil
}

// ProcessEvents processes a batch of events through semantic correlation
func (cm *CollectionManager) ProcessEvents(events []domain.Event) []domain.Insight {
	// Send events to processing pipeline
	for _, event := range events {
		select {
		case cm.eventBus <- event:
			// Also send directly to semantic engine
			cm.semanticEngine.ProcessEvent(&event)
		case <-cm.ctx.Done():
			return nil
		}
	}

	// Collect any immediate insights
	var insights []domain.Insight
	timeout := time.After(100 * time.Millisecond)

	for {
		select {
		case insight := <-cm.insightChan:
			insights = append(insights, insight)
		case <-timeout:
			return insights
		case <-cm.ctx.Done():
			return insights
		}
	}
}

// GetInsights returns all available insights
func (cm *CollectionManager) GetInsights() []domain.Insight {
	var insights []domain.Insight

	// Non-blocking read of available insights
	for {
		select {
		case insight := <-cm.insightChan:
			insights = append(insights, insight)
		default:
			return insights
		}
	}
}

// processEvents runs continuous semantic correlation
func (cm *CollectionManager) processEvents() {
	defer cm.wg.Done()

	eventBuffer := make([]domain.Event, 0, 100)
	ticker := time.NewTicker(cm.config.PatternDetectionInterval)
	defer ticker.Stop()

	for {
		select {
		case event := <-cm.eventBus:
			eventBuffer = append(eventBuffer, event)

			// Process when buffer reaches threshold
			if len(eventBuffer) >= 10 {
				cm.analyzeSemantics(eventBuffer)
				eventBuffer = eventBuffer[:0] // Reset buffer
			}

		case <-ticker.C:
			// Periodic semantic analysis
			if len(eventBuffer) > 0 {
				cm.analyzeSemantics(eventBuffer)
				eventBuffer = eventBuffer[:0]
			}

		case <-cm.ctx.Done():
			return
		}
	}
}

// analyzeSemantics runs semantic correlation on events
func (cm *CollectionManager) analyzeSemantics(events []domain.Event) {
	// Use semantic grouper to analyze events
	groups := cm.semanticEngine.semanticGrouper.GroupEvents(events)

	for _, group := range groups {
		insight := domain.Insight{
			ID:          fmt.Sprintf("semantic-%s", group.ID),
			Type:        "semantic_correlation",
			Title:       fmt.Sprintf("Semantic Correlation: %s", group.Type),
			Description: fmt.Sprintf("%s (Confidence: %.2f)", group.Description, group.Confidence),
			Severity:    cm.calculateSeverityFromGroup(group),
			Source:      "semantic_correlation",
			Timestamp:   time.Now(),
			Metadata: map[string]interface{}{
				"group_type":       group.Type,
				"events_analyzed":  len(events),
				"events_in_group":  len(group.Events),
				"confidence_score": group.Confidence,
				"business_impact":  group.BusinessImpact,
				"trace_id":         group.TraceID,
			},
		}

		// Add root cause analysis if available
		if group.RootCauseAnalysis != nil {
			insight.Metadata["root_cause"] = group.RootCauseAnalysis.RootCauseEvent.ID
			insight.Metadata["causal_chain_length"] = len(group.RootCauseAnalysis.CausalChain)
		}

		// Add predictions if available
		if len(group.PredictedEvolution) > 0 {
			insight.Metadata["has_predictions"] = true
			insight.Metadata["predicted_events"] = len(group.PredictedEvolution)
		}

		select {
		case cm.insightChan <- insight:
		case <-cm.ctx.Done():
			return
		}
	}
}

// calculateSeverityFromGroup determines severity from event group
func (cm *CollectionManager) calculateSeverityFromGroup(group EventGroup) domain.Severity {
	// Use business impact and event severities
	if group.BusinessImpact > 0.8 {
		return domain.SeverityCritical
	} else if group.BusinessImpact > 0.6 {
		return domain.SeverityHigh
	} else if group.BusinessImpact > 0.4 {
		return domain.SeverityMedium
	}

	// Check max severity in events
	maxSeverity := domain.SeverityInfo
	for _, event := range group.Events {
		// Convert EventSeverity to SeverityLevel
		var severity domain.SeverityLevel
		switch event.Severity {
		case domain.EventSeverityCritical:
			severity = domain.SeverityCritical
		case domain.EventSeverityHigh, domain.EventSeverityError:
			severity = domain.SeverityHigh
		case domain.EventSeverityMedium, domain.EventSeverityWarning:
			severity = domain.SeverityMedium
		default:
			severity = domain.SeverityInfo
		}

		if severity > maxSeverity {
			maxSeverity = severity
		}
	}

	return maxSeverity
}

// forwardInsights forwards insights from semantic engine to manager channel
func (cm *CollectionManager) forwardInsights() {
	defer cm.wg.Done()

	for {
		select {
		case insight := <-cm.semanticEngine.Insights():
			// Convert and forward
			domainInsight := domain.Insight(insight)
			select {
			case cm.insightChan <- domainInsight:
			case <-cm.ctx.Done():
				return
			}
		case <-cm.ctx.Done():
			return
		}
	}
}

// Insights returns the channel of AI-generated insights
func (cm *CollectionManager) Insights() <-chan domain.Insight {
	return cm.insightChan
}

// Stop gracefully shuts down semantic correlation
func (cm *CollectionManager) Stop() error {
	// Stop semantic engine first
	if err := cm.semanticEngine.Stop(); err != nil {
		return fmt.Errorf("failed to stop semantic engine: %w", err)
	}

	cm.cancel()
	cm.wg.Wait()
	close(cm.eventBus)
	close(cm.insightChan)
	return nil
}

// Statistics returns processing statistics
func (cm *CollectionManager) Statistics() map[string]interface{} {
	stats := map[string]interface{}{
		"event_buffer_size":  len(cm.eventBus),
		"insight_queue_size": len(cm.insightChan),
	}

	// Add semantic engine stats
	if cm.semanticEngine != nil {
		stats["semantic_engine_stats"] = cm.semanticEngine.GetStats()
	}

	return stats
}
