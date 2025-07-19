package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SemanticCorrelationEngine provides AI-powered semantic correlation with OTEL integration
type SemanticCorrelationEngine struct {
	// Collectors
	collectors map[string]Collector

	// Event processing
	eventChan   chan Event
	insightChan chan Insight

	// Semantic processing (our improved correlation)
	semanticGrouper *SimpleSemanticGrouper

	// Revolutionary OTEL semantic tracer
	semanticTracer *SemanticOTELTracer

	// Event buffer for temporal correlation
	eventBuffer      []domain.Event
	eventBufferMutex sync.RWMutex
	bufferSize       int
	bufferTimeout    time.Duration

	// Human-readable output formatter
	humanFormatter *HumanReadableFormatter

	// OTEL tracing
	tracer trace.Tracer

	// State
	ctx     context.Context
	cancel  context.CancelFunc
	running bool
	mu      sync.RWMutex

	// Stats
	stats           map[string]interface{}
	statsUpdateTime time.Time
}

// NewSemanticCorrelationEngine creates our improved correlation engine
func NewSemanticCorrelationEngine() *SemanticCorrelationEngine {
	return &SemanticCorrelationEngine{
		collectors:      make(map[string]Collector),
		eventChan:       make(chan Event, 1000),
		insightChan:     make(chan Insight, 100),
		semanticGrouper: NewSimpleSemanticGrouper(),
		semanticTracer:  NewSemanticOTELTracer(),
		eventBuffer:     make([]domain.Event, 0, 1000),
		bufferSize:      1000,
		bufferTimeout:   30 * time.Second,
		humanFormatter:  NewHumanReadableFormatter(StyleSimple, AudienceDeveloper),
		tracer:          otel.Tracer("tapio.correlation.engine"),
		stats:           make(map[string]interface{}),
		statsUpdateTime: time.Now(),
	}
}

// GetSemanticTracer returns the semantic OTEL tracer
func (sce *SemanticCorrelationEngine) GetSemanticTracer() *SemanticOTELTracer {
	return sce.semanticTracer
}

// RegisterCollector registers a collector with the engine
func (sce *SemanticCorrelationEngine) RegisterCollector(c Collector) error {
	sce.mu.Lock()
	defer sce.mu.Unlock()

	name := c.Name()
	sce.collectors[name] = c
	return nil
}

// Start begins the semantic correlation engine
func (sce *SemanticCorrelationEngine) Start() error {
	sce.mu.Lock()
	defer sce.mu.Unlock()

	if sce.running {
		return nil
	}

	sce.ctx, sce.cancel = context.WithCancel(context.Background())
	sce.running = true

	// Start processing events with our semantic correlation
	go sce.processEvents()

	// Start periodic semantic analysis
	go sce.periodicSemanticAnalysis()

	return nil
}

// Stop gracefully shuts down the engine
func (sce *SemanticCorrelationEngine) Stop() error {
	sce.mu.Lock()
	defer sce.mu.Unlock()

	if !sce.running {
		return nil
	}

	sce.cancel()
	sce.running = false

	// Close channels
	close(sce.eventChan)
	close(sce.insightChan)

	return nil
}

// ProcessEvent processes a single event through the correlation engine
func (sce *SemanticCorrelationEngine) ProcessEvent(event *domain.Event) {
	select {
	case sce.eventChan <- Event(*event):
		sce.updateStats("events_received")
	default:
		sce.updateStats("events_dropped")
	}
}

// GetLatestFindings returns the latest correlation findings
func (sce *SemanticCorrelationEngine) GetLatestFindings() *Finding {
	sce.mu.RLock()
	defer sce.mu.RUnlock()

	// Get semantic groups from tracer
	groups := sce.semanticTracer.GetSemanticGroups()
	if len(groups) == 0 {
		return nil
	}

	// Return the most recent semantic group as a finding
	var latest *SemanticTraceGroup
	var latestTime time.Time

	for _, group := range groups {
		if len(group.CausalChain) > 0 {
			lastEvent := group.CausalChain[len(group.CausalChain)-1]
			if lastEvent.Timestamp.After(latestTime) {
				latest = group
				latestTime = lastEvent.Timestamp
			}
		}
	}

	if latest == nil {
		return nil
	}

	return &Finding{
		ID:            latest.ID,
		PatternType:   latest.SemanticType,
		Confidence:    latest.ConfidenceScore,
		RelatedEvents: latest.CausalChain,
		SemanticGroup: &SemanticGroupSummary{
			ID:         latest.ID,
			Intent:     latest.Intent,
			Type:       latest.SemanticType,
			Impact:     latest.ImpactAssessment,
			Prediction: latest.PredictedOutcome,
		},
		Timestamp:   latestTime,
		Description: fmt.Sprintf("Semantic correlation: %s", latest.Intent),
	}
}

// Events returns the event channel
func (sce *SemanticCorrelationEngine) Events() <-chan Event {
	return sce.eventChan
}

// Insights returns the insights channel
func (sce *SemanticCorrelationEngine) Insights() <-chan Insight {
	return sce.insightChan
}

// processEvents is the main event processing loop
func (sce *SemanticCorrelationEngine) processEvents() {
	ctx, span := sce.tracer.Start(sce.ctx, "correlation.process_events")
	defer span.End()

	for {
		select {
		case event := <-sce.eventChan:
			// Convert to domain event
			domainEvent := sce.convertToDomainEvent(event)
			if domainEvent == nil {
				continue
			}

			// Process with OTEL semantic tracer
			if err := sce.semanticTracer.ProcessEventWithSemanticTrace(ctx, domainEvent); err != nil {
				span.RecordError(err)
				sce.updateStats("semantic_trace_errors")
			}

			// Add to buffer for temporal analysis
			sce.addToBuffer(*domainEvent)

			// Generate insights from semantic groups
			sce.generateSemanticInsights()

			sce.updateStats("events_processed")

		case <-sce.ctx.Done():
			return
		}
	}
}

// periodicSemanticAnalysis runs periodic analysis on buffered events
func (sce *SemanticCorrelationEngine) periodicSemanticAnalysis() {
	ticker := time.NewTicker(sce.bufferTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sce.analyzeSemanticPatterns()
			sce.cleanupOldGroups()

		case <-sce.ctx.Done():
			return
		}
	}
}

// analyzeSemanticPatterns performs deep semantic analysis on buffered events
func (sce *SemanticCorrelationEngine) analyzeSemanticPatterns() {
	_, span := sce.tracer.Start(sce.ctx, "correlation.analyze_patterns",
		trace.WithAttributes(
			attribute.Int("buffer_size", len(sce.eventBuffer)),
		),
	)
	defer span.End()

	sce.eventBufferMutex.RLock()
	events := make([]domain.Event, len(sce.eventBuffer))
	copy(events, sce.eventBuffer)
	sce.eventBufferMutex.RUnlock()

	// Run semantic grouping
	groups := sce.semanticGrouper.GroupEvents(events)

	// Generate insights from semantic groups
	for _, group := range groups {
		insight := sce.createInsightFromSemanticGroup(group)

		select {
		case sce.insightChan <- insight:
			sce.updateStats("semantic_insights_generated")
		default:
			sce.updateStats("semantic_insights_dropped")
		}
	}

	span.SetAttributes(
		attribute.Int("groups_found", len(groups)),
		attribute.Int64("insights_generated", sce.getStatValue("semantic_insights_generated")),
	)
}

// generateSemanticInsights creates insights from current semantic trace groups
func (sce *SemanticCorrelationEngine) generateSemanticInsights() {
	groups := sce.semanticTracer.GetSemanticGroups()

	for _, group := range groups {
		// Only generate insights for groups with significant events
		if len(group.CausalChain) < 2 {
			continue
		}

		// Check if we've already generated an insight for this group
		insightID := fmt.Sprintf("semantic-%s", group.ID)
		if sce.hasGeneratedInsight(insightID) {
			continue
		}

		insight := sce.createInsightFromTraceGroup(group)

		select {
		case sce.insightChan <- insight:
			sce.markInsightGenerated(insightID)
			sce.updateStats("trace_insights_generated")
		default:
			sce.updateStats("trace_insights_dropped")
		}
	}
}

// createInsightFromTraceGroup creates an insight from a semantic trace group
func (sce *SemanticCorrelationEngine) createInsightFromTraceGroup(group *SemanticTraceGroup) Insight {
	// Determine severity based on impact assessment
	severity := SeverityMedium
	if group.ImpactAssessment != nil {
		if group.ImpactAssessment.BusinessImpact > 0.8 {
			severity = SeverityCritical
		} else if group.ImpactAssessment.BusinessImpact > 0.6 {
			severity = SeverityHigh
		}
	}

	// Extract event IDs
	relatedEvents := make([]string, 0, len(group.CausalChain))
	for _, event := range group.CausalChain {
		relatedEvents = append(relatedEvents, string(event.ID))
	}

	// Extract affected resources
	resources := make([]AffectedResource, 0)
	resourceMap := make(map[string]bool)

	for _, event := range group.CausalChain {
		// Add namespace/pod resources
		if event.Context.Namespace != "" && event.Context.Labels != nil {
			if pod, exists := event.Context.Labels["pod"]; exists {
				key := fmt.Sprintf("%s/%s", event.Context.Namespace, pod)
				if !resourceMap[key] {
					resources = append(resources, AffectedResource{
						Type: "pod",
						Name: key,
					})
					resourceMap[key] = true
				}
			}
		}

		// Add node resources
		if event.Context.Host != "" {
			if !resourceMap[event.Context.Host] {
				resources = append(resources, AffectedResource{
					Type: "node",
					Name: event.Context.Host,
				})
				resourceMap[event.Context.Host] = true
			}
		}
	}

	// Create prediction from group's predicted outcome
	var prediction *Prediction
	if group.PredictedOutcome != nil {
		actions := make([]ActionItem, 0, len(group.PredictedOutcome.PreventionActions))
		for i, action := range group.PredictedOutcome.PreventionActions {
			actions = append(actions, ActionItem{
				ID:          fmt.Sprintf("action-%d", i+1),
				Type:        "prevention",
				Description: action,
				Priority:    "high",
				Command:     action,
			})
		}

		prediction = &Prediction{
			Scenario:    group.PredictedOutcome.Scenario,
			Probability: group.PredictedOutcome.Probability,
			TimeWindow:  group.PredictedOutcome.TimeToOutcome,
			Actions:     actions,
		}
	}

	// Create actionable items from impact assessment
	actions := make([]ActionableItem, 0)
	if group.ImpactAssessment != nil {
		for _, action := range group.ImpactAssessment.RecommendedActions {
			actions = append(actions, ActionableItem{
				Title:       fmt.Sprintf("Action: %s", group.Intent),
				Description: "Recommended action based on semantic correlation",
				Commands:    []string{action},
				Risk:        "medium",
				EstimatedImpact: fmt.Sprintf("Business impact: %.2f, Cascade risk: %.2f",
					group.ImpactAssessment.BusinessImpact,
					group.ImpactAssessment.CascadeRisk),
			})
		}
	}

	correlationInsight := CorrelationInsight{
		Insight: domain.Insight{
			ID:       fmt.Sprintf("semantic-%s-%d", group.ID, time.Now().UnixNano()),
			Type:     fmt.Sprintf("semantic:%s", group.SemanticType),
			Severity: severity,
			Title:    fmt.Sprintf("Semantic Correlation: %s", group.Intent),
			Description: fmt.Sprintf("Detected %s pattern with %d related events. Confidence: %.2f",
				group.SemanticType, len(group.CausalChain), group.ConfidenceScore),
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"semantic_group_id": group.ID,
				"trace_id":          group.TraceID,
				"correlation_type":  "semantic",
			},
		},
		RelatedEvents: relatedEvents,
		Resources:     resources,
		Actions:       actions,
		Prediction:    prediction,
	}

	// Convert to domain.Insight for compatibility
	return correlationInsight.ToInsight()
}

// createInsightFromSemanticGroup creates an insight from a semantic group
func (sce *SemanticCorrelationEngine) createInsightFromSemanticGroup(group EventGroup) Insight {
	// Extract event IDs
	relatedEvents := make([]string, 0, len(group.Events))
	for _, event := range group.Events {
		relatedEvents = append(relatedEvents, string(event.ID))
	}

	// Determine severity based on events
	severity := SeverityLow
	for _, event := range group.Events {
		switch event.Severity {
		case domain.EventSeverityCritical:
			severity = SeverityCritical
		case domain.EventSeverityHigh:
			if severity < SeverityHigh {
				severity = SeverityHigh
			}
		case domain.EventSeverityMedium:
			if severity < SeverityMedium {
				severity = SeverityMedium
			}
		}
	}

	return domain.Insight{
		ID:       fmt.Sprintf("group-%s-%d", group.ID, time.Now().UnixNano()),
		Type:     fmt.Sprintf("semantic_group:%s", group.Type),
		Severity: severity,
		Title:    fmt.Sprintf("Semantic Group: %s", group.Description),
		Description: fmt.Sprintf("Identified semantic group with %d events over %s",
			len(group.Events), group.TimeSpan),
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"group_id":       group.ID,
			"group_type":     group.Type,
			"confidence":     group.Confidence,
			"related_events": relatedEvents,
		},
	}
}

// convertToDomainEvent converts collector Event to domain.Event
func (sce *SemanticCorrelationEngine) convertToDomainEvent(event Event) *domain.Event {
	// Type assertion to domain.Event
	if domainEvent, ok := interface{}(event).(domain.Event); ok {
		return &domainEvent
	}

	// If not directly convertible, create new domain event
	return &domain.Event{
		ID:         domain.EventID(fmt.Sprintf("evt-%d", time.Now().UnixNano())),
		Type:       domain.EventType("unknown"),
		Timestamp:  time.Now(),
		Source:     domain.SourceType("collector"),
		Severity:   domain.EventSeverityInfo,
		Confidence: 0.5,
		Context:    domain.EventContext{},
	}
}

// addToBuffer adds an event to the temporal buffer
func (sce *SemanticCorrelationEngine) addToBuffer(event domain.Event) {
	sce.eventBufferMutex.Lock()
	defer sce.eventBufferMutex.Unlock()

	sce.eventBuffer = append(sce.eventBuffer, event)

	// Maintain buffer size
	if len(sce.eventBuffer) > sce.bufferSize {
		sce.eventBuffer = sce.eventBuffer[len(sce.eventBuffer)-sce.bufferSize:]
	}
}

// cleanupOldGroups removes old semantic groups
func (sce *SemanticCorrelationEngine) cleanupOldGroups() {
	sce.semanticTracer.CleanupOldGroups(30 * time.Minute)
	sce.updateStats("cleanup_runs")
}

// updateStats updates internal statistics
func (sce *SemanticCorrelationEngine) updateStats(key string) {
	sce.mu.Lock()
	defer sce.mu.Unlock()

	if val, exists := sce.stats[key]; exists {
		if count, ok := val.(int64); ok {
			sce.stats[key] = count + 1
		}
	} else {
		sce.stats[key] = int64(1)
	}

	sce.stats["last_update"] = time.Now()
}

// getStatValue returns a stat value
func (sce *SemanticCorrelationEngine) getStatValue(key string) int64 {
	sce.mu.RLock()
	defer sce.mu.RUnlock()

	if val, exists := sce.stats[key]; exists {
		if count, ok := val.(int64); ok {
			return count
		}
	}
	return 0
}

// hasGeneratedInsight checks if we've already generated an insight
func (sce *SemanticCorrelationEngine) hasGeneratedInsight(insightID string) bool {
	sce.mu.RLock()
	defer sce.mu.RUnlock()

	key := fmt.Sprintf("insight_generated_%s", insightID)
	_, exists := sce.stats[key]
	return exists
}

// markInsightGenerated marks an insight as generated
func (sce *SemanticCorrelationEngine) markInsightGenerated(insightID string) {
	sce.mu.Lock()
	defer sce.mu.Unlock()

	key := fmt.Sprintf("insight_generated_%s", insightID)
	sce.stats[key] = time.Now()
}

// GetHumanExplanation returns a human-readable explanation for an insight
func (sce *SemanticCorrelationEngine) GetHumanExplanation(insight Insight) *HumanReadableExplanation {
	return sce.humanFormatter.FormatInsight(insight)
}

// SetHumanOutputStyle changes the human output formatting style
func (sce *SemanticCorrelationEngine) SetHumanOutputStyle(style ExplanationStyle, audience Audience) {
	sce.mu.Lock()
	defer sce.mu.Unlock()
	sce.humanFormatter = NewHumanReadableFormatter(style, audience)
}

// GetStats returns current engine statistics
func (sce *SemanticCorrelationEngine) GetStats() map[string]interface{} {
	sce.mu.RLock()
	defer sce.mu.RUnlock()

	// Create a copy of stats
	statsCopy := make(map[string]interface{})
	for k, v := range sce.stats {
		statsCopy[k] = v
	}

	// Add current state
	statsCopy["running"] = sce.running
	statsCopy["buffer_size"] = len(sce.eventBuffer)
	statsCopy["semantic_groups"] = len(sce.semanticTracer.GetSemanticGroups())
	statsCopy["collectors_registered"] = len(sce.collectors)

	return statsCopy
}
