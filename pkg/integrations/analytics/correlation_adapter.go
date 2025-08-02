package analytics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

// AnalyticsCorrelationAdapter adapts correlation.SimpleCorrelationSystem to CorrelationEngine interface
type AnalyticsCorrelationAdapter struct {
	correlationSystem *correlation.SimpleCorrelationSystem
	logger            *zap.Logger

	// State tracking
	latestFindings *correlation.Finding
	semanticGroups []*correlation.SemanticGroupSummary
	groupsByEvent  map[string]*correlation.SemanticGroupSummary
	mu             sync.RWMutex

	// Event tracking for findings
	eventBuffer     []*domain.UnifiedEvent
	eventBufferSize int

	// OTEL trace hierarchy tracking
	spanHierarchy map[string][]string             // parentSpanID -> []childSpanIDs
	spanToEvent   map[string]*domain.UnifiedEvent // spanID -> event

	// Semantic enrichment cache
	semanticCache map[string]*domain.SemanticContext // traceID -> semantic context

	// Insights channel consumer
	insightProcessor chan bool
	wg               sync.WaitGroup
}

// NewAnalyticsCorrelationAdapter creates an adapter for analytics engine integration
func NewAnalyticsCorrelationAdapter(correlationSystem *correlation.SimpleCorrelationSystem, logger *zap.Logger) *AnalyticsCorrelationAdapter {
	return &AnalyticsCorrelationAdapter{
		correlationSystem: correlationSystem,
		logger:            logger,
		groupsByEvent:     make(map[string]*correlation.SemanticGroupSummary),
		eventBuffer:       make([]*domain.UnifiedEvent, 0, 100),
		eventBufferSize:   100,
		spanHierarchy:     make(map[string][]string),
		spanToEvent:       make(map[string]*domain.UnifiedEvent),
		semanticCache:     make(map[string]*domain.SemanticContext),
		insightProcessor:  make(chan bool, 1),
	}
}

// Start initializes the correlation engine
func (a *AnalyticsCorrelationAdapter) Start() error {
	// Start the underlying correlation system
	if err := a.correlationSystem.Start(); err != nil {
		return fmt.Errorf("failed to start correlation system: %w", err)
	}

	// Start insight processor
	a.wg.Add(1)
	go a.processInsights()

	a.logger.Info("Analytics correlation adapter started")
	return nil
}

// Stop gracefully shuts down the correlation engine
func (a *AnalyticsCorrelationAdapter) Stop() error {
	// Stop insight processing
	close(a.insightProcessor)
	a.wg.Wait()

	// Stop the underlying correlation system
	if err := a.correlationSystem.Stop(); err != nil {
		return fmt.Errorf("failed to stop correlation system: %w", err)
	}

	a.logger.Info("Analytics correlation adapter stopped")
	return nil
}

// ProcessEvent processes a single event for correlation
func (a *AnalyticsCorrelationAdapter) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	// Add to event buffer for later correlation reference
	a.addToEventBuffer(event)

	// Process through correlation system
	if err := a.correlationSystem.ProcessEvent(ctx, event); err != nil {
		return fmt.Errorf("correlation processing failed: %w", err)
	}

	// Extract OTEL trace context for semantic grouping
	if event.HasTraceContext() {
		a.updateSemanticGroup(event)
	}

	return nil
}

// GetLatestFindings returns the most recent correlation findings
func (a *AnalyticsCorrelationAdapter) GetLatestFindings() *interfaces.Finding {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return ConvertToInterfacesFinding(a.latestFindings)
}

// GetSemanticGroups returns current semantic groups
func (a *AnalyticsCorrelationAdapter) GetSemanticGroups() []*interfaces.SemanticGroup {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Convert correlation.SemanticGroupSummary to interfaces.SemanticGroup
	groups := make([]*interfaces.SemanticGroup, len(a.semanticGroups))
	for i, sg := range a.semanticGroups {
		groups[i] = &interfaces.SemanticGroup{
			ID:     sg.ID,
			Intent: sg.Intent,
			Type:   sg.Type,
		}
	}
	return groups
}

// processInsights converts correlation insights to analytics findings
func (a *AnalyticsCorrelationAdapter) processInsights() {
	defer a.wg.Done()

	for {
		select {
		case insight, ok := <-a.correlationSystem.Insights():
			if !ok {
				return
			}
			a.convertInsightToFinding(insight)

		case <-a.insightProcessor:
			return
		}
	}
}

// convertInsightToFinding converts a correlation insight to an analytics finding
func (a *AnalyticsCorrelationAdapter) convertInsightToFinding(insight domain.Insight) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Extract correlation metadata
	correlationType := "unknown"
	confidence := 0.5

	if insight.Metadata != nil {
		if ct, ok := insight.Metadata["correlation_type"].(string); ok {
			correlationType = ct
		}
		if conf, ok := insight.Metadata["confidence"].(float64); ok {
			confidence = conf
		}
	}

	// Get related events from buffer - leverage multi-dimensional matching
	relatedEvents := a.getRelatedEventsForInsight(insight)

	// Create or update semantic group based on multi-dimensional context
	semanticGroup := a.getOrCreateSemanticGroupInternal(insight)

	// Create internal Finding with UnifiedEvents
	internalFinding := &correlation.Finding{
		ID:                   insight.ID,
		PatternType:          correlationType,
		Confidence:           confidence,
		Description:          a.enhanceDescription(insight, relatedEvents),
		RelatedUnifiedEvents: relatedEvents,
		SemanticGroup:        semanticGroup,
		Timestamp:            time.Now(),
	}

	// Store the internal finding
	a.latestFindings = internalFinding

	a.logger.Debug("Converted insight to finding",
		zap.String("insight_id", insight.ID),
		zap.String("pattern_type", correlationType),
		zap.Float64("confidence", confidence),
		zap.Int("related_events", len(relatedEvents)))
}

// enhanceDescription creates a richer description using multi-dimensional context
func (a *AnalyticsCorrelationAdapter) enhanceDescription(insight domain.Insight, events []*domain.UnifiedEvent) string {
	desc := insight.Description

	// Add impact context if available
	var maxImpact float64
	var affectedServices []string
	serviceMap := make(map[string]bool)

	for _, event := range events {
		if event.Impact != nil {
			if event.Impact.InfrastructureImpact > maxImpact {
				maxImpact = event.Impact.InfrastructureImpact
			}
			for _, svc := range event.Impact.AffectedServices {
				serviceMap[svc] = true
			}
		}
	}

	for svc := range serviceMap {
		affectedServices = append(affectedServices, svc)
	}

	if maxImpact > 0 {
		desc += fmt.Sprintf(" (Business Impact: %.0f%%, Affected Services: %v)",
			maxImpact*100, affectedServices)
	}

	// Add semantic narrative if available
	for _, event := range events {
		if event.Semantic != nil && event.Semantic.Narrative != "" {
			desc += "\nContext: " + event.Semantic.Narrative
			break // Just add the first good narrative
		}
	}

	return desc
}

// updateSemanticGroup updates semantic grouping based on OTEL trace context
func (a *AnalyticsCorrelationAdapter) updateSemanticGroup(event *domain.UnifiedEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	traceID := event.TraceContext.TraceID

	// Find or create semantic group for this trace
	var group *correlation.SemanticGroupSummary
	for _, sg := range a.semanticGroups {
		if sg.ID == traceID {
			group = sg
			break
		}
	}

	if group == nil {
		// Create new semantic group from trace
		group = &correlation.SemanticGroupSummary{
			ID:     traceID,
			Intent: a.inferSemanticIntent(event),
			Type:   a.inferSemanticType(event),
		}
		a.semanticGroups = append(a.semanticGroups, group)
	}

	// Map event to group
	a.groupsByEvent[event.ID] = group

	// If we have parent span, also check for hierarchical grouping
	if event.TraceContext.ParentSpanID != "" {
		// Store span hierarchy for causal chain analysis
		a.updateSpanHierarchy(event)
	}
}

// getOrCreateSemanticGroupInternal gets or creates a semantic group summary for an insight
func (a *AnalyticsCorrelationAdapter) getOrCreateSemanticGroupInternal(insight domain.Insight) *correlation.SemanticGroupSummary {
	// Try to find existing group based on insight metadata
	groupID := fmt.Sprintf("sg-%s-%d", insight.Type, time.Now().Unix())

	for _, group := range a.semanticGroups {
		if group.Type == insight.Type {
			// Return as correlation.SemanticGroupSummary
			return &correlation.SemanticGroupSummary{
				ID:     group.ID,
				Intent: group.Intent,
				Type:   group.Type,
			}
		}
	}

	// Create new group
	group := &correlation.SemanticGroupSummary{
		ID:     groupID,
		Intent: a.inferIntentFromInsight(insight),
		Type:   insight.Type,
	}

	a.semanticGroups = append(a.semanticGroups, group)

	// Return the group
	return group
}

// updateSpanHierarchy tracks span parent-child relationships
func (a *AnalyticsCorrelationAdapter) updateSpanHierarchy(event *domain.UnifiedEvent) {
	if event.TraceContext == nil {
		return
	}

	spanID := event.TraceContext.SpanID
	parentSpanID := event.TraceContext.ParentSpanID

	// Track span to event mapping
	a.spanToEvent[spanID] = event

	// Track parent-child relationship
	if parentSpanID != "" {
		children := a.spanHierarchy[parentSpanID]
		children = append(children, spanID)
		a.spanHierarchy[parentSpanID] = children
	}

	// Cache enhanced semantic context for the trace
	if event.Semantic != nil && event.TraceContext.TraceID != "" {
		existingSemantic := a.semanticCache[event.TraceContext.TraceID]
		if existingSemantic == nil || event.Semantic.Confidence > existingSemantic.Confidence {
			a.semanticCache[event.TraceContext.TraceID] = event.Semantic
		}
	}
}

// inferSemanticIntent infers semantic intent from event
func (a *AnalyticsCorrelationAdapter) inferSemanticIntent(event *domain.UnifiedEvent) string {
	// First check if event has explicit semantic intent
	if event.Semantic != nil && event.Semantic.Intent != "" {
		return event.Semantic.Intent
	}

	// Check cached semantic context for the trace
	if event.HasTraceContext() {
		if cached := a.semanticCache[event.TraceContext.TraceID]; cached != nil && cached.Intent != "" {
			return cached.Intent
		}
	}

	// Fall back to type-based inference
	switch event.Type {
	case domain.EventTypeKubernetes:
		if event.Kubernetes != nil {
			switch event.Kubernetes.EventType {
			case "pod_oom_killed":
				return "resource_exhaustion"
			case "pod_crash_loop":
				return "application_failure"
			case "deployment_scaled":
				return "scaling_operation"
			default:
				return "infrastructure_change"
			}
		}
	case domain.EventTypeLog:
		if event.Application != nil && event.Application.Level == "error" {
			return "error_condition"
		}
		return "application_event"
	case domain.EventTypeNetwork:
		return "network_activity"
	case domain.EventTypeSystem:
		return "system_event"
	}
	return "unknown_intent"
}

// inferSemanticType infers semantic type from event
func (a *AnalyticsCorrelationAdapter) inferSemanticType(event *domain.UnifiedEvent) string {
	if event.Kubernetes != nil {
		return fmt.Sprintf("k8s_%s", event.Kubernetes.ObjectKind)
	}
	return string(event.Type)
}

// inferIntentFromInsight infers intent from insight
func (a *AnalyticsCorrelationAdapter) inferIntentFromInsight(insight domain.Insight) string {
	switch insight.Type {
	case "k8s_correlation":
		return "kubernetes_relationship"
	case "temporal_correlation":
		return "time_based_pattern"
	case "sequence_correlation":
		return "sequential_pattern"
	default:
		return "correlation_discovery"
	}
}

// addToEventBuffer adds event to circular buffer
func (a *AnalyticsCorrelationAdapter) addToEventBuffer(event *domain.UnifiedEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.eventBuffer = append(a.eventBuffer, event)
	if len(a.eventBuffer) > a.eventBufferSize {
		// Remove oldest events
		a.eventBuffer = a.eventBuffer[len(a.eventBuffer)-a.eventBufferSize:]
	}
}

// getRelatedEventsForInsight finds related events for an insight
func (a *AnalyticsCorrelationAdapter) getRelatedEventsForInsight(insight domain.Insight) []*domain.UnifiedEvent {
	// For now, return recent events that match the insight type
	// This could be enhanced with more sophisticated matching
	var related []*domain.UnifiedEvent

	for _, event := range a.eventBuffer {
		if a.isEventRelatedToInsight(event, insight) {
			related = append(related, event)
			if len(related) >= 10 { // Limit to 10 related events
				break
			}
		}
	}

	return related
}

// isEventRelatedToInsight checks if an event is related to an insight using multi-dimensional matching
func (a *AnalyticsCorrelationAdapter) isEventRelatedToInsight(event *domain.UnifiedEvent, insight domain.Insight) bool {
	// Multi-dimensional matching strategy
	score := 0.0

	// 1. Type dimension matching
	switch insight.Type {
	case "k8s_correlation":
		if event.Type == domain.EventTypeKubernetes {
			score += 0.3
		}
	case "temporal_correlation", "sequence_correlation":
		score += 0.1 // These can involve any event type
	}

	// 2. Trace dimension matching
	if event.HasTraceContext() {
		// Check if event is in same trace as any event mentioned in insight
		if insight.Metadata != nil {
			if traceID, ok := insight.Metadata["trace_id"].(string); ok && event.TraceContext.TraceID == traceID {
				score += 0.4 // Strong correlation if same trace
			}
		}
	}

	// 3. Semantic dimension matching
	if event.Semantic != nil {
		// Check semantic intent alignment
		if insight.Metadata != nil {
			if intent, ok := insight.Metadata["semantic_intent"].(string); ok && event.Semantic.Intent == intent {
				score += 0.2
			}
			// Check semantic tags overlap
			if tags, ok := insight.Metadata["semantic_tags"].([]string); ok {
				for _, tag := range tags {
					for _, eventTag := range event.Semantic.Tags {
						if tag == eventTag {
							score += 0.1
							break
						}
					}
				}
			}
		}
	}

	// 4. Entity dimension matching
	if event.Entity != nil {
		if insight.Metadata != nil {
			// Check if same entity
			if entityName, ok := insight.Metadata["entity_name"].(string); ok && event.Entity.Name == entityName {
				score += 0.3
			}
			// Check if same namespace
			if namespace, ok := insight.Metadata["namespace"].(string); ok && event.Entity.Namespace == namespace {
				score += 0.1
			}
		}
	}

	// 5. Impact dimension matching
	if event.Impact != nil && event.Impact.Severity == string(insight.Severity) {
		score += 0.1
	}

	// 6. Time dimension matching
	// Events within 5 minutes are potentially related
	timeDiff := time.Since(event.Timestamp).Abs()
	if timeDiff < 5*time.Minute {
		score += 0.2
	} else if timeDiff < 30*time.Minute {
		score += 0.1
	}

	// Consider related if score > 0.3 (at least some dimensional overlap)
	return score > 0.3
}

// convertToLegacyEvents converts UnifiedEvents to legacy Event format
// This is a temporary adapter until the analytics engine is updated
func (a *AnalyticsCorrelationAdapter) convertToLegacyEvents(events []*domain.UnifiedEvent) []*domain.Event {
	legacyEvents := make([]*domain.Event, 0, len(events))

	for _, ue := range events {
		le := &domain.Event{
			ID:         domain.EventID(ue.ID),
			Type:       ue.Type,
			Timestamp:  ue.Timestamp,
			Source:     domain.SourceType(ue.Source),
			Message:    ue.Message,
			Tags:       ue.Tags,
			Category:   ue.Category,
			Severity:   ue.Severity,
			Confidence: ue.Confidence,
		}

		// Convert data to generic map
		le.Data = make(map[string]interface{})

		// Add kubernetes data if present
		if ue.Kubernetes != nil {
			le.Data["kubernetes"] = ue.Kubernetes
		}

		// Add application data if present
		if ue.Application != nil {
			le.Data["application"] = ue.Application
		}

		// Copy attributes
		if ue.Attributes != nil {
			le.Attributes = ue.Attributes
		}

		legacyEvents = append(legacyEvents, le)
	}

	return legacyEvents
}

// GetStats returns adapter statistics
func (a *AnalyticsCorrelationAdapter) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	stats := a.correlationSystem.GetStats()

	// Add adapter-specific stats
	stats["semantic_groups_count"] = len(a.semanticGroups)
	stats["event_buffer_size"] = len(a.eventBuffer)
	stats["latest_finding_id"] = ""
	if a.latestFindings != nil {
		stats["latest_finding_id"] = a.latestFindings.ID
	}

	return stats
}

// ConvertToInterfacesFinding converts internal Finding to interfaces.Finding
func ConvertToInterfacesFinding(f *correlation.Finding) *interfaces.Finding {
	if f == nil {
		return nil
	}

	// Convert correlation.SemanticGroupSummary to interfaces.SemanticGroup
	var semanticGroup *interfaces.SemanticGroup
	if f.SemanticGroup != nil {
		semanticGroup = &interfaces.SemanticGroup{
			ID:     f.SemanticGroup.ID,
			Intent: f.SemanticGroup.Intent,
			Type:   f.SemanticGroup.Type,
		}
	}

	// For backward compatibility, use RelatedEvents if available
	// Otherwise, convert UnifiedEvents to Events
	relatedEvents := f.RelatedEvents
	if len(relatedEvents) == 0 && len(f.RelatedUnifiedEvents) > 0 {
		relatedEvents = make([]*domain.Event, 0, len(f.RelatedUnifiedEvents))
		for _, ue := range f.RelatedUnifiedEvents {
			// Convert UnifiedEvent to Event for interface compatibility
			event := &domain.Event{
				ID:        domain.EventID(ue.ID),
				Type:      ue.Type,
				Timestamp: ue.Timestamp,
				Source:    domain.SourceType(ue.Source),
			}
			relatedEvents = append(relatedEvents, event)
		}
	}

	return &interfaces.Finding{
		ID:            f.ID,
		Confidence:    f.Confidence,
		PatternType:   f.PatternType,
		Description:   f.Description,
		RelatedEvents: relatedEvents,
		SemanticGroup: semanticGroup,
	}
}

// ConvertFromInterfacesFinding converts interfaces.Finding to internal Finding
func ConvertFromInterfacesFinding(f *interfaces.Finding) *correlation.Finding {
	if f == nil {
		return nil
	}

	// Convert interfaces.SemanticGroup to correlation.SemanticGroupSummary
	var semanticGroup *correlation.SemanticGroupSummary
	if f.SemanticGroup != nil {
		semanticGroup = &correlation.SemanticGroupSummary{
			ID:     f.SemanticGroup.ID,
			Intent: f.SemanticGroup.Intent,
			Type:   f.SemanticGroup.Type,
		}
	}

	return &correlation.Finding{
		ID:            f.ID,
		PatternType:   f.PatternType,
		Confidence:    f.Confidence,
		Description:   f.Description,
		RelatedEvents: f.RelatedEvents,
		SemanticGroup: semanticGroup,
	}
}
