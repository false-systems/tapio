package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/types"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/events/opinionated"
)

// PerfectEngineWithStore wraps PerfectEngine to automatically store insights
type PerfectEngineWithStore struct {
	baseEngine   *PerfectEngine
	insightStore InsightStore
	mu           sync.RWMutex

	// Configuration
	storeAllInsights       bool
	minSeverityToStore     string
	maxInsightsPerResource int
}

// NewPerfectEngineWithStore creates a wrapper that stores insights
func NewPerfectEngineWithStore(baseEngine *PerfectEngine, store InsightStore) *PerfectEngineWithStore {
	return &PerfectEngineWithStore{
		baseEngine:             baseEngine,
		insightStore:           store,
		storeAllInsights:       true,
		minSeverityToStore:     "low",
		maxInsightsPerResource: 100,
	}
}

// GetBaseEngine returns the underlying perfect engine
func (e *PerfectEngineWithStore) GetBaseEngine() *PerfectEngine {
	return e.baseEngine
}

// ProcessOpinionatedEvent processes event and stores resulting insights
func (e *PerfectEngineWithStore) ProcessOpinionatedEvent(ctx context.Context, event *opinionated.OpinionatedEvent) error {
	// Process through base engine
	if err := e.baseEngine.ProcessOpinionatedEvent(ctx, event); err != nil {
		return err
	}

	// Extract and store insights generated during processing
	// In a real implementation, we'd modify PerfectEngine to return insights
	// For now, we'll simulate insight generation based on the event
	insights := e.extractInsightsFromEvent(event)

	// Store insights
	for _, insight := range insights {
		if e.shouldStoreInsight(insight) {
			if err := e.insightStore.Store(insight); err != nil {
				// Log error but don't fail processing
				fmt.Printf("Failed to store insight: %v\n", err)
			}
		}
	}

	return nil
}

// ProcessEvents implements the CorrelationEngine interface
func (e *PerfectEngineWithStore) ProcessEvents(ctx context.Context, events []*types.Event) error {
	// Convert to opinionated events and process
	for _, event := range events {
		// In real implementation, we'd convert Event to OpinionatedEvent
		// For now, create a minimal opinionated event
		opEvent := e.convertToOpinionatedEvent(event)
		if err := e.ProcessOpinionatedEvent(ctx, opEvent); err != nil {
			return err
		}
	}
	return nil
}

// Start starts the engine
func (e *PerfectEngineWithStore) Start(ctx context.Context) error {
	// Start cleanup goroutine for old insights
	go e.cleanupOldInsights(ctx)

	return e.baseEngine.Start(ctx)
}

// Stop stops the engine
func (e *PerfectEngineWithStore) Stop() error {
	return e.baseEngine.Stop()
}

// GetStats returns engine statistics
func (e *PerfectEngineWithStore) GetStats() *EngineStats {
	baseStats := e.baseEngine.GetStats()

	// Convert PerfectEngineStats to EngineStats
	return &EngineStats{
		EventsProcessed:    baseStats.EventsProcessed,
		CorrelationsFound:  baseStats.CorrelationsFound,
		InsightsGenerated:  baseStats.InsightsGenerated,
		ErrorCount:         0, // TODO: track errors
		LastProcessedTime:  time.Now(),
	}
}

// extractInsightsFromEvent simulates insight extraction from processed event
func (e *PerfectEngineWithStore) extractInsightsFromEvent(event *opinionated.OpinionatedEvent) []*domain.Insight {
	insights := make([]*domain.Insight, 0)

	// Generate insights based on event data
	// This is a simplified implementation - real engine would generate from correlations

	// Check for anomalies
	if event.Anomaly != nil && event.Anomaly.AnomalyScore > 0.7 {
		insight := &domain.Insight{
			ID:           generateInsightID(),
			Title:        fmt.Sprintf("Anomaly detected in %s", event.Context.Pod),
			Description:  fmt.Sprintf("Anomaly detected with score %.2f", event.Anomaly.AnomalyScore),
			Severity:     severityFromScore(event.Anomaly.AnomalyScore),
			Category:     "anomaly",
			ResourceName: event.Context.Pod,
			Namespace:    event.Context.Namespace,
			Timestamp:    event.Timestamp,
			Evidence: []domain.Evidence{
				{
					EventID:     event.ID,
					Description: "Anomaly detection triggered",
					Timestamp:   event.Timestamp,
					Source:      event.Source.Collector,
				},
			},
		}

		// Add prediction if behavioral anomaly
		if event.Behavioral != nil && event.Behavioral.Confidence > 0.6 {
			insight.Prediction = &Prediction{
				Class:       "behavioral_degradation",
				Probability: event.Behavioral.Confidence,
				Confidence:  0.8,
				Explanation: "Behavioral pattern indicates potential degradation",
				Features:    make(map[string]float64),
			}
		}

		// Add actionable items
		insight.ActionableItems = e.generateActionableItems(event, insight)

		insights = append(insights, insight)
	}

	// Check for causality chains
	if event.Causality != nil && len(event.Causality.CausalChain) > 2 {
		insight := &domain.Insight{
			ID:           generateInsightID(),
			Title:        fmt.Sprintf("Causality chain detected for %s", event.Context.Pod),
			Description:  fmt.Sprintf("Multi-step causality chain with %d events", len(event.Causality.CausalChain)),
			Severity:     "medium",
			Category:     "causality",
			ResourceName: event.Context.Pod,
			Namespace:    event.Context.Namespace,
			Timestamp:    event.Timestamp,
			Evidence:     e.extractCausalityEvidence(event.Causality),
		}

		// Add root cause if identified
		if event.Causality.RootCause != "" {
			insight.RootCause = &RootCause{
				EventID:     event.Causality.RootCause,
				Description: "Identified root cause in causality chain",
				Confidence:  event.Causality.Confidence,
			}
		}

		insights = append(insights, insight)
	}

	// Check for performance issues - simplified for now
	if false { // TODO: Add performance context when available
		insight := &domain.Insight{
			ID:           generateInsightID(),
			Title:        fmt.Sprintf("Performance degradation in %s", event.Context.Pod),
			Description:  fmt.Sprintf("Performance metric degraded"),
			Severity:     "high",
			Category:     "performance",
			ResourceName: event.Context.Pod,
			Namespace:    event.Context.Namespace,
			Timestamp:    event.Timestamp,
			Evidence: []domain.Evidence{
				{
					EventID:     event.ID,
					Description: fmt.Sprintf("Performance metric degraded"),
					Timestamp:   event.Timestamp,
					Source:      event.Source.Collector,
				},
			},
		}

		// Add prediction for continued degradation
		insight.Prediction = &Prediction{
			Class:       "performance_failure",
			Probability: 0.7,
			Confidence:  0.75,
			Explanation: "Performance trending toward failure threshold",
			Features:    make(map[string]float64),
		}

		insights = append(insights, insight)
	}

	return insights
}

// generateActionableItems creates actionable recommendations
func (e *PerfectEngineWithStore) generateActionableItems(event *opinionated.OpinionatedEvent, insight *domain.Insight) []*domain.ActionItem {
	items := make([]*domain.ActionItem, 0)

	// Generate based on insight category
	switch insight.Category {
	case "anomaly":
		items = append(items, &ActionableItem{
			Description: "Increase memory limits",
			Command:     fmt.Sprintf("kubectl patch deployment %s -n %s -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"%s\",\"resources\":{\"limits\":{\"memory\":\"2Gi\"}}}]}}}}'", event.Context.Pod, event.Context.Namespace, event.Context.Pod),
			Impact:      "Prevents OOM kills and improves stability",
			Risk:        "low",
		})
	case "performance":
		items = append(items, &ActionableItem{
			Description: "Scale up deployment",
			Command:     fmt.Sprintf("kubectl scale deployment %s -n %s --replicas=3", event.Context.Pod, event.Context.Namespace),
			Impact:      "Distributes load and improves response times",
			Risk:        "low",
		})
		items = append(items, &ActionableItem{
			Description: "Enable HPA",
			Command:     fmt.Sprintf("kubectl autoscale deployment %s -n %s --cpu-percent=70 --min=2 --max=5", event.Context.Pod, event.Context.Namespace),
			Impact:      "Automatically scales based on load",
			Risk:        "medium",
		})
	}

	return items
}

// extractCausalityEvidence extracts evidence from causality chain
func (e *PerfectEngineWithStore) extractCausalityEvidence(causality *opinionated.CausalityContext) []domain.Evidence {
	evidence := make([]domain.Evidence, 0, len(causality.CausalChain))

	for _, event := range causality.CausalChain {
		evidence = append(evidence, domain.Evidence{
			EventID:     event.EventID,
			Description: event.Description,
			Timestamp:   event.Timestamp,
			Source:      "causality_analysis",
		})
	}

	return evidence
}

// shouldStoreInsight determines if an insight should be stored
func (e *PerfectEngineWithStore) shouldStoreInsight(insight *domain.Insight) bool {
	if !e.storeAllInsights {
		// Check severity threshold
		if !e.meetsMinSeverity(insight.Severity) {
			return false
		}
	}

	// Check resource limits
	existing := e.insightStore.GetInsights(insight.ResourceName, insight.Namespace)
	if len(existing) >= e.maxInsightsPerResource {
		// Remove oldest insights to make room
		e.removeOldestInsights(existing, len(existing)-e.maxInsightsPerResource+1)
	}

	return true
}

// meetsMinSeverity checks if severity meets minimum threshold
func (e *PerfectEngineWithStore) meetsMinSeverity(severity string) bool {
	severityLevels := map[string]int{
		"critical": 4,
		"high":     3,
		"medium":   2,
		"low":      1,
	}

	minLevel := severityLevels[e.minSeverityToStore]
	actualLevel := severityLevels[severity]

	return actualLevel >= minLevel
}

// removeOldestInsights removes the oldest insights
func (e *PerfectEngineWithStore) removeOldestInsights(insights []*domain.Insight, count int) {
	// Sort by timestamp and remove oldest
	// In real implementation, this would be handled by the store
	for i := 0; i < count && i < len(insights); i++ {
		e.insightStore.Delete(insights[i].ID)
	}
}

// cleanupOldInsights periodically removes old insights
func (e *PerfectEngineWithStore) cleanupOldInsights(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Remove insights older than retention period
			cutoff := time.Now().Add(-24 * time.Hour)
			e.insightStore.DeleteOlderThan(cutoff)
		}
	}
}

// convertToOpinionatedEvent converts basic Event to OpinionatedEvent
func (e *PerfectEngineWithStore) convertToOpinionatedEvent(event *types.Event) *opinionated.OpinionatedEvent {
	// This is a simplified conversion
	// Real implementation would map all fields properly
	return &opinionated.OpinionatedEvent{
		ID:         event.ID,
		Timestamp:  event.Timestamp,
		Category:   opinionated.CategorySystemHealth,
		Severity:   opinionated.SeverityMedium,
		Confidence: 0.8,

		Source: opinionated.EventSource{
			Collector: event.Source,
			Component: "correlation",
			Node:      event.Entity.Node,
		},

		Context: opinionated.OpinionatedContext{
			Namespace: event.Entity.Namespace,
			Pod:       event.Entity.Pod,
			Container: event.Entity.Container,
		},

		// Other contexts would be populated based on event data
	}
}

// Helper functions

func generateInsightID() string {
	return fmt.Sprintf("insight-%d", time.Now().UnixNano())
}

func severityFromScore(score float32) string {
	switch {
	case score > 0.9:
		return "critical"
	case score > 0.7:
		return "high"
	case score > 0.5:
		return "medium"
	default:
		return "low"
	}
}
