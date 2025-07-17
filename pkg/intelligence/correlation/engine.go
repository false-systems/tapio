package correlation
import (
	"context"
	"fmt"
	"sync"
	"time"
	"github.com/falseyair/tapio/pkg/domain"
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
func (e *PerfectEngineWithStore) ProcessOpinionatedEvent(ctx context.Context, event *domain.Event) error {
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
func (e *PerfectEngineWithStore) ProcessEvents(ctx context.Context, events []*Event) error {
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
func (e *PerfectEngineWithStore) extractInsightsFromEvent(event *domain.Event) []*domain.Insight {
	insights := make([]*domain.Insight, 0)
	// Generate insights based on event data
	// This is a simplified implementation - real engine would generate from correlations
	// Check for anomalies
	anomalyScore := 0.0
	if event.HasAnomaly() != nil {
		if score, ok := event.HasAnomaly()["AnomalyScore"].(float64); ok {
			anomalyScore = score
		}
	}
	if anomalyScore > 0.7 {
		pod := ""
		namespace := ""
		if true {
			if p, ok := event.Context.Pod(string); ok {
				pod = p
			}
			if n, ok := event.Context.Namespace(string); ok {
				namespace = n
			}
		}
		insight := &domain.Insight{
			ID:           generateInsightID(),
			Type:         "anomaly",
			Title:        fmt.Sprintf("Anomaly detected in %s", pod),
			Description:  fmt.Sprintf("Anomaly detected with score %.2f", anomalyScore),
			Severity:     domain.Severity(severityFromScore(float32(anomalyScore))),
			Data: map[string]interface{}{
				"category":     "anomaly",
				"resourceName": pod,
				"namespace":    namespace,
				"anomalyScore": anomalyScore,
			},
			Timestamp:    event.Timestamp,
		}
		// Add prediction data if behavioral anomaly
		if event.Behavioral != nil {
			if confidence, ok := event.Behavioral["Confidence"].(float64); ok && confidence > 0.6 {
				insight.Data["prediction"] = map[string]interface{}{
					"id":          fmt.Sprintf("pred_%s", event.ID),
					"type":        "behavioral_degradation",
					"event":       "Service degradation",
					"probability": confidence,
					"confidence":  0.8,
					"description": "Behavioral pattern indicates potential degradation",
					"timeWindow": map[string]interface{}{
						"start":    event.Timestamp,
						"end":      event.Timestamp.Add(time.Hour),
						"duration": time.Hour.String(),
					},
				}
			}
		}
		// Add actionable items to data
		insight.Data["actionableItems"] = e.generateActionableItems(event, insight)
		insights = append(insights, insight)
	}
	// Check for causality chains
	if event.Causality != nil && len(event.Causality.CausalChain) > 2 {
		pod := ""
		namespace := ""
		if true {
			if p, ok := event.Context.Pod(string); ok {
				pod = p
			}
			if n, ok := event.Context.Namespace(string); ok {
				namespace = n
			}
		}
		insight := &domain.Insight{
			ID:           generateInsightID(),
			Type:         "causality",
			Title:        fmt.Sprintf("Causality chain detected for %s", pod),
			Description:  fmt.Sprintf("Multi-step causality chain with %d events", len(event.Causality.CausalChain)),
			Severity:     domain.SeverityMedium,
			Data: map[string]interface{}{
				"category":        "causality",
				"resourceName":    pod,
				"namespace":       namespace,
				"causalityEvidence": e.extractCausalityEvidence(event.Causality),
			},
			Timestamp:    event.Timestamp,
		}
		// Add root cause if identified
		if event.Causality.RootCause != "" {
			insight.Data["rootCause"] = map[string]interface{}{
				"id":          event.Causality.RootCause,
				"description": "Identified root cause in causality chain",
				"confidence":  event.Causality.Confidence,
			}
		}
		insights = append(insights, insight)
	}
	// Check for performance issues - simplified for now
	/*
	// TODO: Add performance context when available
	if false {
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
					ID:          event.ID,
					Type:        "performance",
					Description: fmt.Sprintf("Performance metric degraded"),
					Timestamp:   event.Timestamp,
					Source:      event.Source.Collector,
					Confidence:  0.8,
				},
			},
		}
		// Add prediction for continued degradation
		insight.Prediction = &domain.Prediction{
			ID:          fmt.Sprintf("pred_perf_%s", event.ID),
			Type:        "performance_failure",
			Event:       "Service performance failure",
			Probability: 0.7,
			Confidence:  0.75,
			Description: "Performance trending toward failure threshold",
			TimeWindow: domain.TimeWindow{
				Start:    event.Timestamp,
				End:      event.Timestamp.Add(30 * time.Minute),
				Duration: 30 * time.Minute,
			},
			Evidence: []domain.Evidence{},
		}
		insights = append(insights, insight)
	}
	*/
	return insights
}
// generateActionableItems creates actionable recommendations
func (e *PerfectEngineWithStore) generateActionableItems(event *domain.Event, insight *domain.Insight) []domain.ActionItem {
	items := make([]domain.ActionItem, 0)
	// Get category from insight data
	category := ""
	if insight.Data != nil {
		if cat, ok := insight.Data["category"].(string); ok {
			category = cat
		}
	}
	// Get pod and namespace from event context
	pod := ""
	namespace := ""
	if true {
		if p, ok := event.Context.Pod(string); ok {
			pod = p
		}
		if n, ok := event.Context.Namespace(string); ok {
			namespace = n
		}
	}
	// Generate based on insight category
	switch category {
	case "anomaly":
		items = append(items, domain.ActionItem{
			ID:          fmt.Sprintf("action_mem_%s", event.ID),
			Type:        "resource_adjustment",
			Description: "Increase memory limits",
			Command:     fmt.Sprintf("kubectl patch deployment %s -n %s -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"%s\",\"resources\":{\"limits\":{\"memory\":\"2Gi\"}}}]}}}}'", pod, namespace, pod),
			Priority:    "high",
			Metadata: map[string]interface{}{
				"impact": "Prevents OOM kills and improves stability",
				"risk":   "low",
			},
		})
	case "performance":
		items = append(items, domain.ActionItem{
			ID:          fmt.Sprintf("action_scale_%s", event.ID),
			Type:        "scaling",
			Description: "Scale up deployment",
			Command:     fmt.Sprintf("kubectl scale deployment %s -n %s --replicas=3", pod, namespace),
			Priority:    "medium",
			Metadata: map[string]interface{}{
				"impact": "Distributes load and improves response times",
				"risk":   "low",
			},
		})
		items = append(items, domain.ActionItem{
			ID:          fmt.Sprintf("action_hpa_%s", event.ID),
			Description: "Enable HPA",
			Command:     fmt.Sprintf("kubectl autoscale deployment %s -n %s --cpu-percent=70 --min=2 --max=5", event.Context.Pod, event.Context.Namespace),
			Impact:      "Automatically scales based on load",
			Risk:        "medium",
		})
	}
	return items
}
// extractCausalityEvidence extracts evidence from causality chain
func (e *PerfectEngineWithStore) extractCausalityEvidence(causality *domain.CausalityContext) []domain.Evidence {
	evidence := make([]domain.Evidence, 0, len(causality.CausalChain))
	for _, event := range causality.CausalChain {
		evidence = append(evidence, domain.Evidence{
			ID:          event.EventID,
			Type:        "causality",
			Description: event.Description,
			Timestamp:   event.Timestamp,
			Source:      "causality_analysis",
			Content:     event,
			Confidence:  0.8,
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
func (e *PerfectEngineWithStore) convertToOpinionatedEvent(event *Event) *domain.Event {
	// This is a simplified conversion
	// Real implementation would map all fields properly
	return &domain.Event{
		ID:         event.ID,
		Timestamp:  event.Timestamp,
		Category:   domain.CategorySystemHealth,
		Severity:   domain.SeverityMedium,
		Confidence: 0.8,
		Source: domain.EventSource{
			Collector: event.Source,
			Component: "correlation",
			Node:      event.Entity.Node,
		},
		Context: domain.OpinionatedContext{
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
