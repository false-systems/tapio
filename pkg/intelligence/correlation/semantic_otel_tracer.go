package correlation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SemanticOTELTracer adds revolutionary multi-dimensional correlation to OTEL traces
// This is the crown jewel - grouping traces by MEANING, not just time
type SemanticOTELTracer struct {
	tracer           trace.Tracer
	semanticGroups   map[string]*SemanticTraceGroup
	adaptiveWindows  map[string]time.Duration
	causalityTracker *SimpleCausalityTracker
	spatialRadius    int
}

// NewSemanticOTELTracer creates the revolutionary tracer
func NewSemanticOTELTracer() *SemanticOTELTracer {
	return &SemanticOTELTracer{
		tracer:         otel.Tracer("tapio-semantic-correlation"),
		semanticGroups: make(map[string]*SemanticTraceGroup),
		adaptiveWindows: map[string]time.Duration{
			"memory_leak":     30 * time.Second, // Memory issues develop quickly
			"network_failure": 10 * time.Second, // Network cascades fast
			"disk_pressure":   2 * time.Minute,  // Disk issues develop slowly
			"cpu_throttling":  5 * time.Second,  // CPU throttling is immediate
			"pod_evicted":     15 * time.Second, // Pod evictions cluster
			"service_restart": 20 * time.Second, // Service restarts correlate
		},
		causalityTracker: &SimpleCausalityTracker{
			causalLinks:       make(map[string][]CausalLink),
			timeWindow:        5 * time.Minute,
			strengthThreshold: 0.7,
		},
		spatialRadius: 2, // Events within 2 K8s hops
	}
}

// ProcessEventWithSemanticTrace creates multi-dimensional correlation traces
func (st *SemanticOTELTracer) ProcessEventWithSemanticTrace(ctx context.Context, event *domain.Event) error {
	// Step 1: Classify semantic intent
	intent := st.classifySemanticIntent(event)

	// Step 2: Find or create semantic group
	group := st.findOrCreateSemanticGroup(ctx, event, intent)

	// Step 3: Create the revolutionary correlation trace
	return st.createSemanticCorrelationTrace(ctx, event, group)
}

// classifySemanticIntent determines what the event MEANS
func (st *SemanticOTELTracer) classifySemanticIntent(event *domain.Event) string {
	// Extract intent from event type and context
	switch event.Type {
	case "memory_oom", "memory_pressure":
		return "memory_exhaustion_investigation"
	case "pod_evicted":
		return "resource_pressure_cascade"
	case "service_restart", "service_failure":
		return "service_reliability_incident"
	case "network_timeout", "network_failure":
		return "connectivity_degradation"
	case "cpu_throttling":
		return "performance_bottleneck"
	default:
		// Analyze severity and impact
		if event.Severity == "critical" {
			return "critical_incident_response"
		}
		return "operational_monitoring"
	}
}

// findOrCreateSemanticGroup finds existing group or creates new one
func (st *SemanticOTELTracer) findOrCreateSemanticGroup(ctx context.Context, event *domain.Event, intent string) *SemanticTraceGroup {
	// Check causal relationships
	if group := st.findCausallyRelatedGroup(event); group != nil {
		st.addEventToGroup(event, group)
		return group
	}

	// Check spatial relationships (same namespace/node)
	if group := st.findSpatiallyRelatedGroup(event); group != nil {
		st.addEventToGroup(event, group)
		return group
	}

	// Check temporal relationships with adaptive windows
	if group := st.findTemporallyRelatedGroup(event); group != nil {
		st.addEventToGroup(event, group)
		return group
	}

	// Create new semantic group
	return st.createNewSemanticGroup(ctx, event, intent)
}

// findCausallyRelatedGroup finds group based on causality
func (st *SemanticOTELTracer) findCausallyRelatedGroup(event *domain.Event) *SemanticTraceGroup {
	// Look for causal links
	eventID := string(event.ID)

	// Check if this event is caused by events in existing groups
	for _, group := range st.semanticGroups {
		for _, groupEvent := range group.CausalChain {
			if st.areEventsCausallyLinked(string(groupEvent.ID), eventID) {
				return group
			}
		}
	}

	return nil
}

// findSpatiallyRelatedGroup finds group based on K8s topology
func (st *SemanticOTELTracer) findSpatiallyRelatedGroup(event *domain.Event) *SemanticTraceGroup {
	for _, group := range st.semanticGroups {
		if st.areEventsSpatiallyRelated(event, group.RootCause) {
			return group
		}
	}
	return nil
}

// findTemporallyRelatedGroup uses adaptive time windows
func (st *SemanticOTELTracer) findTemporallyRelatedGroup(event *domain.Event) *SemanticTraceGroup {
	window := st.getAdaptiveTimeWindow(event)

	for _, group := range st.semanticGroups {
		// Check if event is within adaptive window of group
		for _, groupEvent := range group.CausalChain {
			if event.Timestamp.Sub(groupEvent.Timestamp).Abs() <= window {
				return group
			}
		}
	}

	return nil
}

// createNewSemanticGroup creates a new trace group
func (st *SemanticOTELTracer) createNewSemanticGroup(ctx context.Context, event *domain.Event, intent string) *SemanticTraceGroup {
	groupID := fmt.Sprintf("semantic_%s_%d", intent, time.Now().UnixNano())

	// Create OTEL trace for this semantic group
	ctx, span := st.tracer.Start(ctx, fmt.Sprintf("semantic.%s", intent))

	group := &SemanticTraceGroup{
		ID:              groupID,
		Intent:          intent,
		SemanticType:    st.deriveSemanticType(event),
		RootCause:       event,
		CausalChain:     []*domain.Event{event},
		ConfidenceScore: st.calculateConfidence(event),
		TraceID:         span.SpanContext().TraceID().String(),
		SpanContext:     span.SpanContext(),
	}

	// Assess impact and predict outcome
	group.ImpactAssessment = st.assessGroupImpact(group)
	group.PredictedOutcome = st.predictGroupOutcome(group)

	// Cache the group
	st.semanticGroups[groupID] = group

	return group
}

// createSemanticCorrelationTrace creates the revolutionary OTEL trace
func (st *SemanticOTELTracer) createSemanticCorrelationTrace(ctx context.Context, event *domain.Event, group *SemanticTraceGroup) error {
	// Use group's span context for correlation
	ctx = trace.ContextWithSpanContext(ctx, group.SpanContext)

	// Create span with multi-dimensional attributes
	ctx, span := st.tracer.Start(ctx, fmt.Sprintf("event.%s", event.Type),
		trace.WithAttributes(
			// Core event attributes
			attribute.String("event.id", string(event.ID)),
			attribute.String("event.type", string(event.Type)),
			attribute.String("event.severity", string(event.Severity)),
			attribute.Float64("event.confidence", event.Confidence),

			// Revolutionary semantic grouping
			attribute.String("semantic.group_id", group.ID),
			attribute.String("semantic.intent", group.Intent),
			attribute.String("semantic.type", group.SemanticType),
			attribute.Float64("semantic.group_confidence", group.ConfidenceScore),
			attribute.Int("semantic.causal_chain_length", len(group.CausalChain)),

			// Multi-dimensional correlation
			attribute.Bool("correlation.is_root_cause", event.ID == group.RootCause.ID),
			attribute.Int("correlation.related_events", len(group.CausalChain)),
			attribute.String("correlation.dimension", st.getCorrelationDimension(event, group)),

			// Business impact in traces!
			attribute.Float64("impact.business", float64(group.ImpactAssessment.BusinessImpact)),
			attribute.String("impact.severity", group.ImpactAssessment.TechnicalSeverity),
			attribute.Float64("impact.cascade_risk", float64(group.ImpactAssessment.CascadeRisk)),

			// Predictions in traces!
			attribute.String("prediction.scenario", group.PredictedOutcome.Scenario),
			attribute.Float64("prediction.probability", group.PredictedOutcome.Probability),
			attribute.Int64("prediction.time_to_outcome_seconds", int64(group.PredictedOutcome.TimeToOutcome.Seconds())),
		),
	)
	defer span.End()

	// Add context-specific attributes
	st.addContextAttributes(span, event)

	// Add causal chain as span events
	for i, causalEvent := range group.CausalChain {
		span.AddEvent(fmt.Sprintf("causal_event_%d", i),
			trace.WithAttributes(
				attribute.String("event.id", string(causalEvent.ID)),
				attribute.String("event.type", string(causalEvent.Type)),
				attribute.Float64("event.confidence", causalEvent.Confidence),
			),
			trace.WithTimestamp(causalEvent.Timestamp),
		)
	}

	// Add recommended actions as span events
	for i, action := range group.ImpactAssessment.RecommendedActions {
		span.AddEvent(fmt.Sprintf("action_%d", i),
			trace.WithAttributes(
				attribute.String("action.command", action),
				attribute.String("action.type", "recommendation"),
			),
		)
	}

	return nil
}

// Helper methods

func (st *SemanticOTELTracer) addEventToGroup(event *domain.Event, group *SemanticTraceGroup) {
	group.CausalChain = append(group.CausalChain, event)

	// Update impact assessment with new event
	group.ImpactAssessment = st.assessGroupImpact(group)
	group.PredictedOutcome = st.predictGroupOutcome(group)
}

func (st *SemanticOTELTracer) areEventsCausallyLinked(sourceID, targetID string) bool {
	links, exists := st.causalityTracker.causalLinks[sourceID]
	if !exists {
		return false
	}

	for _, link := range links {
		if link.TargetEventID == targetID && link.Strength >= st.causalityTracker.strengthThreshold {
			return true
		}
	}

	return false
}

func (st *SemanticOTELTracer) areEventsSpatiallyRelated(event1, event2 *domain.Event) bool {
	// Same namespace = spatially related
	if event1.Context.Namespace != "" && event1.Context.Namespace == event2.Context.Namespace {
		return true
	}

	// Same node = spatially related
	if event1.Context.Host != "" && event1.Context.Host == event2.Context.Host {
		return true
	}

	// Same pod = definitely related
	if event1.Context.Labels != nil && event2.Context.Labels != nil {
		pod1, _ := event1.Context.Labels["pod"]
		pod2, _ := event2.Context.Labels["pod"]
		if pod1 != "" && pod1 == pod2 {
			return true
		}
	}

	return false
}

func (st *SemanticOTELTracer) getAdaptiveTimeWindow(event *domain.Event) time.Duration {
	// Get adaptive window based on event type
	if window, exists := st.adaptiveWindows[string(event.Type)]; exists {
		return window
	}

	// Adjust based on severity
	baseWindow := 30 * time.Second
	switch event.Severity {
	case "critical":
		return baseWindow / 2 // Tight grouping for critical
	case "high":
		return baseWindow
	case "medium":
		return baseWindow * 2
	default:
		return baseWindow * 3
	}
}

func (st *SemanticOTELTracer) deriveSemanticType(event *domain.Event) string {
	// Derive semantic type from event
	eventType := string(event.Type)

	// Add context for richer semantic types
	if strings.Contains(eventType, "memory") {
		return "memory_pressure_cascade"
	}
	if strings.Contains(eventType, "network") {
		return "network_connectivity_issue"
	}
	if strings.Contains(eventType, "pod") {
		return "pod_lifecycle_event"
	}
	if strings.Contains(eventType, "service") {
		return "service_reliability_event"
	}

	return eventType
}

func (st *SemanticOTELTracer) calculateConfidence(event *domain.Event) float64 {
	// Start with event confidence
	confidence := event.Confidence

	// Boost confidence based on available context
	if event.Context.Namespace != "" {
		confidence += 0.1
	}
	if event.Context.Host != "" {
		confidence += 0.1
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func (st *SemanticOTELTracer) assessGroupImpact(group *SemanticTraceGroup) *ImpactAssessment {
	if group == nil {
		return &ImpactAssessment{
			AffectedResources:  []string{},
			RecommendedActions: []string{},
			TimeToResolution:   30 * time.Minute,
		}
	}

	assessment := &ImpactAssessment{
		AffectedResources:  []string{},
		RecommendedActions: []string{},
	}

	// Analyze impact across all events
	maxBusinessImpact := float32(0)
	severityScore := 0

	resourceMap := make(map[string]bool)

	for _, event := range group.CausalChain {
		// Track affected resources
		if event.Context.Namespace != "" && event.Context.Labels != nil {
			if pod, exists := event.Context.Labels["pod"]; exists {
				resource := fmt.Sprintf("%s/%s", event.Context.Namespace, pod)
				resourceMap[resource] = true
			}
		}

		// Calculate severity
		switch event.Severity {
		case "critical":
			severityScore = max(severityScore, 4)
			maxBusinessImpact = max(maxBusinessImpact, 0.9)
		case "high":
			severityScore = max(severityScore, 3)
			maxBusinessImpact = max(maxBusinessImpact, 0.7)
		case "medium":
			severityScore = max(severityScore, 2)
			maxBusinessImpact = max(maxBusinessImpact, 0.5)
		case "low":
			severityScore = max(severityScore, 1)
			maxBusinessImpact = max(maxBusinessImpact, 0.3)
		}
	}

	assessment.BusinessImpact = maxBusinessImpact

	// Convert severity score
	switch severityScore {
	case 4:
		assessment.TechnicalSeverity = "critical"
	case 3:
		assessment.TechnicalSeverity = "high"
	case 2:
		assessment.TechnicalSeverity = "medium"
	default:
		assessment.TechnicalSeverity = "low"
	}

	// Calculate cascade risk (more sensitive)
	assessment.CascadeRisk = float32(len(group.CausalChain)) / 5.0 // More sensitive to cascade size
	if assessment.CascadeRisk > 1.0 {
		assessment.CascadeRisk = 1.0
	}

	// Convert resources
	for resource := range resourceMap {
		assessment.AffectedResources = append(assessment.AffectedResources, resource)
	}

	// Generate actions based on semantic type
	assessment.RecommendedActions = st.generateRecommendedActions(group)

	// Estimate resolution time
	assessment.TimeToResolution = st.estimateResolutionTime(group)

	return assessment
}

func (st *SemanticOTELTracer) predictGroupOutcome(group *SemanticTraceGroup) *PredictedOutcome {
	outcome := &PredictedOutcome{
		PreventionActions: []string{},
	}

	// Predict based on semantic type
	switch group.SemanticType {
	case "memory_pressure_cascade":
		outcome.Scenario = "oom_kill_cascade"
		outcome.Probability = 0.8
		outcome.TimeToOutcome = 5 * time.Minute
		outcome.PreventionActions = []string{
			"kubectl scale deployment --replicas=+2",
			"kubectl set resources deployment --limits=memory=2Gi",
		}

	case "network_connectivity_issue":
		outcome.Scenario = "service_isolation"
		outcome.Probability = 0.7
		outcome.TimeToOutcome = 2 * time.Minute
		outcome.PreventionActions = []string{
			"kubectl get networkpolicies",
			"kubectl rollout restart deployment",
		}

	case "pod_lifecycle_event":
		outcome.Scenario = "pod_crash_loop"
		outcome.Probability = 0.6
		outcome.TimeToOutcome = 3 * time.Minute
		outcome.PreventionActions = []string{
			"kubectl describe pod",
			"kubectl logs --previous",
		}

	default:
		outcome.Scenario = "service_degradation"
		outcome.Probability = 0.5
		outcome.TimeToOutcome = 10 * time.Minute
		outcome.PreventionActions = []string{
			"kubectl get events --sort-by='.lastTimestamp'",
			"kubectl top pods --sort-by=cpu",
		}
	}

	outcome.ConfidenceLevel = group.ConfidenceScore * outcome.Probability

	return outcome
}

func (st *SemanticOTELTracer) generateRecommendedActions(group *SemanticTraceGroup) []string {
	actions := []string{}

	// Get namespace from root cause
	namespace := group.RootCause.Context.Namespace
	if namespace == "" {
		namespace = "default"
	}

	switch group.SemanticType {
	case "memory_pressure_cascade":
		actions = append(actions,
			fmt.Sprintf("kubectl top pods -n %s | sort -k3 -h", namespace),
			"kubectl describe nodes | grep -A 5 'Allocated resources'",
		)

	case "service_reliability_event":
		actions = append(actions,
			fmt.Sprintf("kubectl get pods -n %s -o wide", namespace),
			fmt.Sprintf("kubectl get events -n %s --sort-by='.lastTimestamp'", namespace),
		)

	default:
		actions = append(actions,
			"kubectl get events --all-namespaces --sort-by='.lastTimestamp'",
		)
	}

	return actions
}

func (st *SemanticOTELTracer) estimateResolutionTime(group *SemanticTraceGroup) time.Duration {
	// Base estimation on severity and type
	baseTime := 10 * time.Minute

	if group == nil || group.ImpactAssessment == nil {
		return baseTime
	}

	switch group.ImpactAssessment.TechnicalSeverity {
	case "critical":
		baseTime = 5 * time.Minute
	case "high":
		baseTime = 15 * time.Minute
	case "medium":
		baseTime = 30 * time.Minute
	default:
		baseTime = 60 * time.Minute
	}

	// Adjust based on cascade risk
	if group.ImpactAssessment.CascadeRisk > 0.7 {
		baseTime = baseTime / 2 // Urgent
	}

	return baseTime
}

func (st *SemanticOTELTracer) getCorrelationDimension(event *domain.Event, group *SemanticTraceGroup) string {
	// Determine which dimension caused the correlation

	// Check temporal
	window := st.getAdaptiveTimeWindow(event)
	for _, groupEvent := range group.CausalChain {
		if event.Timestamp.Sub(groupEvent.Timestamp).Abs() <= window {
			return "temporal"
		}
	}

	// Check spatial
	if st.areEventsSpatiallyRelated(event, group.RootCause) {
		return "spatial"
	}

	// Check causal
	for _, groupEvent := range group.CausalChain {
		if st.areEventsCausallyLinked(string(groupEvent.ID), string(event.ID)) {
			return "causal"
		}
	}

	return "semantic"
}

func (st *SemanticOTELTracer) addContextAttributes(span trace.Span, event *domain.Event) {
	// Add Kubernetes context
	if event.Context.Namespace != "" {
		span.SetAttributes(attribute.String("k8s.namespace", event.Context.Namespace))
	}
	if event.Context.Host != "" {
		span.SetAttributes(attribute.String("k8s.node", event.Context.Host))
	}

	// Add pod info from labels
	if event.Context.Labels != nil {
		if pod, exists := event.Context.Labels["pod"]; exists {
			span.SetAttributes(attribute.String("k8s.pod", pod))
		}
		if deployment, exists := event.Context.Labels["deployment"]; exists {
			span.SetAttributes(attribute.String("k8s.deployment", deployment))
		}
	}

	// Add event-specific context based on payload type
	switch payload := event.Payload.(type) {
	case domain.MemoryEventPayload:
		span.SetAttributes(
			attribute.Float64("memory.usage_percent", payload.Usage),
			attribute.Int64("memory.available_bytes", int64(payload.Available)),
			attribute.Int64("memory.total_bytes", int64(payload.Total)),
		)
	case domain.KubernetesEventPayload:
		span.SetAttributes(
			attribute.String("k8s.resource.kind", payload.Resource.Kind),
			attribute.String("k8s.resource.name", payload.Resource.Name),
			attribute.String("k8s.event_type", payload.EventType),
			attribute.String("k8s.reason", payload.Reason),
		)
	case domain.ServiceEventPayload:
		span.SetAttributes(
			attribute.String("service.name", payload.ServiceName),
			attribute.String("service.event_type", payload.EventType),
		)
	}
}

// max returns the maximum of two values
func max[T int | float32](a, b T) T {
	if a > b {
		return a
	}
	return b
}

// AddCausalLink registers a causal relationship between events
func (st *SemanticOTELTracer) AddCausalLink(sourceID, targetID string, strength float64, linkType string) {
	link := CausalLink{
		SourceEventID: sourceID,
		TargetEventID: targetID,
		Strength:      strength,
		Type:          linkType,
	}

	st.causalityTracker.causalLinks[sourceID] = append(
		st.causalityTracker.causalLinks[sourceID],
		link,
	)
}

// GetSemanticGroups returns current semantic groups for analysis
func (st *SemanticOTELTracer) GetSemanticGroups() map[string]*SemanticTraceGroup {
	return st.semanticGroups
}

// CleanupOldGroups removes groups older than retention period
func (st *SemanticOTELTracer) CleanupOldGroups(retention time.Duration) {
	cutoff := time.Now().Add(-retention)

	for id, group := range st.semanticGroups {
		if len(group.CausalChain) > 0 {
			lastEvent := group.CausalChain[len(group.CausalChain)-1]
			if lastEvent.Timestamp.Before(cutoff) {
				delete(st.semanticGroups, id)
			}
		}
	}
}

// classifyUnifiedSemanticIntent determines what the UnifiedEvent MEANS
func (st *SemanticOTELTracer) classifyUnifiedSemanticIntent(event *domain.UnifiedEvent) string {
	// First check if semantic intent is already defined
	if event.Semantic != nil && event.Semantic.Intent != "" {
		return event.Semantic.Intent
	}

	// Extract intent from event type and layer-specific data
	intent := string(event.Type)

	// Analyze kernel events
	if event.IsKernelEvent() && event.Kernel != nil {
		if event.Kernel.Syscall == "oom_kill" {
			intent = "memory_pressure_cascade"
		} else if event.Kernel.ReturnCode < 0 {
			intent = "kernel_failure"
		}
	}

	// Analyze network events
	if event.IsNetworkEvent() && event.Network != nil {
		if event.Network.StatusCode >= 500 {
			intent = "service_failure"
		} else if event.Network.StatusCode >= 400 {
			intent = "client_error"
		} else if event.Network.Latency > 5000000000 { // 5 seconds
			intent = "network_slowness"
		}
	}

	// Analyze application events
	if event.IsApplicationEvent() && event.Application != nil {
		switch event.Application.Level {
		case "error", "critical":
			intent = "application_error"
		case "warn":
			intent = "application_warning"
		}
		if event.Application.ErrorType != "" {
			intent = fmt.Sprintf("%s_%s", intent, event.Application.ErrorType)
		}
	}

	// Analyze Kubernetes events
	if event.IsKubernetesEvent() && event.Kubernetes != nil {
		if event.Kubernetes.EventType == "Warning" {
			switch event.Kubernetes.Reason {
			case "BackOff", "CrashLoopBackOff":
				intent = "pod_crash_loop"
			case "OOMKilled":
				intent = "memory_pressure_cascade"
			case "FailedScheduling":
				intent = "resource_shortage"
			default:
				intent = "kubernetes_warning"
			}
		}
	}

	return intent
}

// findOrCreateUnifiedSemanticGroup finds existing group or creates new one for UnifiedEvent
func (st *SemanticOTELTracer) findOrCreateUnifiedSemanticGroup(ctx context.Context, event *domain.UnifiedEvent, intent string) *SemanticTraceGroup {
	// Check existing groups
	for _, group := range st.semanticGroups {
		if st.shouldAddUnifiedEventToGroup(event, group, intent) {
			// Add to unified causal chain
			if group.UnifiedCausalChain == nil {
				group.UnifiedCausalChain = make([]*domain.UnifiedEvent, 0)
			}
			group.UnifiedCausalChain = append(group.UnifiedCausalChain, event)

			// Also convert and add to legacy causal chain for backward compatibility
			if domainEvent := st.convertUnifiedToDomainEvent(event); domainEvent != nil {
				group.CausalChain = append(group.CausalChain, domainEvent)
			}

			// Update confidence with unified event data
			if event.Semantic != nil && event.Semantic.Confidence > 0 {
				group.ConfidenceScore = (group.ConfidenceScore + event.Semantic.Confidence) / 2
			}

			return group
		}
	}

	// Create new group
	groupID := fmt.Sprintf("group-%s-%d", intent, time.Now().UnixNano())

	// Convert unified event to domain event for root cause (temporary)
	rootCause := st.convertUnifiedToDomainEvent(event)

	group := &SemanticTraceGroup{
		ID:                 groupID,
		Intent:             intent,
		SemanticType:       st.classifySemanticType(intent),
		RootCause:          rootCause,
		CausalChain:        []*domain.Event{rootCause},
		UnifiedCausalChain: []*domain.UnifiedEvent{event},
		ConfidenceScore:    0.7,
		TraceID:            "",
		SpanContext:        trace.SpanContext{},
	}

	// Extract trace ID from event
	if event.TraceContext != nil {
		group.TraceID = event.TraceContext.TraceID
	}

	st.semanticGroups[groupID] = group
	return group
}

// shouldAddUnifiedEventToGroup determines if event belongs to existing group
func (st *SemanticOTELTracer) shouldAddUnifiedEventToGroup(event *domain.UnifiedEvent, group *SemanticTraceGroup, intent string) bool {
	// Check semantic match
	if group.Intent != intent && !st.areIntentsRelated(group.Intent, intent) {
		return false
	}

	// Check trace context match
	if event.TraceContext != nil && group.TraceID != "" {
		if event.TraceContext.TraceID == group.TraceID {
			return true // Same trace, definitely related
		}
	}

	// Check temporal proximity with latest event
	if len(group.UnifiedCausalChain) > 0 {
		lastEvent := group.UnifiedCausalChain[len(group.UnifiedCausalChain)-1]
		timeDiff := event.Timestamp.Sub(lastEvent.Timestamp).Abs()
		if timeDiff <= st.getAdaptiveTimeWindowForUnified(event) {
			return true
		}
	}

	// Check entity relationship
	if event.Entity != nil && group.RootCause != nil {
		// Check if same entity
		if event.GetEntityID() != "" {
			for _, chainEvent := range group.UnifiedCausalChain {
				if chainEvent.GetEntityID() == event.GetEntityID() {
					return true
				}
			}
		}

		// Check namespace match
		if event.Entity.Namespace != "" && group.RootCause.Context.Namespace == event.Entity.Namespace {
			return true
		}
	}

	// Check correlation context
	if event.Correlation != nil {
		if event.Correlation.GroupID == group.ID {
			return true
		}
		// Check if part of causal chain
		for _, relatedID := range event.Correlation.RelatedEvents {
			for _, chainEvent := range group.UnifiedCausalChain {
				if chainEvent.ID == relatedID {
					return true
				}
			}
		}
	}

	return false
}

// createUnifiedSemanticCorrelationTrace creates OTEL trace for UnifiedEvent correlation
func (st *SemanticOTELTracer) createUnifiedSemanticCorrelationTrace(ctx context.Context, event *domain.UnifiedEvent, group *SemanticTraceGroup) error {
	// Determine correlation dimension
	dimension := st.getUnifiedCorrelationDimension(event, group)

	_, span := st.tracer.Start(ctx, fmt.Sprintf("correlation.%s.%s", dimension, group.SemanticType),
		trace.WithAttributes(
			attribute.String("correlation.dimension", dimension),
			attribute.String("semantic.group.id", group.ID),
			attribute.String("semantic.intent", group.Intent),
			attribute.String("semantic.type", group.SemanticType),
			attribute.Float64("semantic.confidence", group.ConfidenceScore),
			attribute.Int("causal.chain.length", len(group.UnifiedCausalChain)),
			attribute.String("event.id", event.ID),
			attribute.String("event.type", string(event.Type)),
			attribute.String("event.severity", event.GetSeverity()),
		),
	)
	defer span.End()

	// Add event trace context if available
	if event.TraceContext != nil {
		span.SetAttributes(
			attribute.String("trace.id", event.TraceContext.TraceID),
			attribute.String("span.id", event.TraceContext.SpanID),
		)
		if event.TraceContext.ParentSpanID != "" {
			span.SetAttributes(attribute.String("parent.span.id", event.TraceContext.ParentSpanID))
		}
	}

	// Add unified event context
	st.addUnifiedContextAttributes(span, event)

	// Update group assessments
	group.ImpactAssessment = st.assessUnifiedGroupImpact(group)
	group.PredictedOutcome = st.predictGroupOutcome(group)

	// Set span status based on severity
	if event.GetSeverity() == "critical" || event.GetSeverity() == "error" {
		span.RecordError(fmt.Errorf("critical event in semantic group: %s", group.Intent))
	}

	return nil
}

// addUnifiedContextAttributes adds UnifiedEvent attributes to span
func (st *SemanticOTELTracer) addUnifiedContextAttributes(span trace.Span, event *domain.UnifiedEvent) {
	// Add entity context
	if event.Entity != nil {
		span.SetAttributes(
			attribute.String("entity.type", event.Entity.Type),
			attribute.String("entity.name", event.Entity.Name),
		)
		if event.Entity.Namespace != "" {
			span.SetAttributes(attribute.String("entity.namespace", event.Entity.Namespace))
		}
		if event.Entity.UID != "" {
			span.SetAttributes(attribute.String("entity.uid", event.Entity.UID))
		}
	}

	// Add semantic context
	if event.Semantic != nil {
		span.SetAttributes(
			attribute.String("semantic.intent", event.Semantic.Intent),
			attribute.String("semantic.category", event.Semantic.Category),
			attribute.Float64("semantic.confidence", event.Semantic.Confidence),
		)
		if len(event.Semantic.Tags) > 0 {
			span.SetAttributes(attribute.StringSlice("semantic.tags", event.Semantic.Tags))
		}
	}

	// Add layer-specific attributes
	if event.Kernel != nil {
		span.SetAttributes(
			attribute.String("kernel.syscall", event.Kernel.Syscall),
			attribute.Int64("kernel.pid", int64(event.Kernel.PID)),
			attribute.Int64("kernel.return_code", int64(event.Kernel.ReturnCode)),
		)
	}

	if event.Network != nil {
		span.SetAttributes(
			attribute.String("network.protocol", event.Network.Protocol),
			attribute.String("network.source", fmt.Sprintf("%s:%d", event.Network.SourceIP, event.Network.SourcePort)),
			attribute.String("network.destination", fmt.Sprintf("%s:%d", event.Network.DestIP, event.Network.DestPort)),
			attribute.Int("network.status_code", event.Network.StatusCode),
			attribute.Int64("network.latency_ns", event.Network.Latency),
		)
	}

	if event.Application != nil {
		span.SetAttributes(
			attribute.String("app.level", event.Application.Level),
			attribute.String("app.logger", event.Application.Logger),
		)
		if event.Application.ErrorType != "" {
			span.SetAttributes(attribute.String("app.error_type", event.Application.ErrorType))
		}
		if event.Application.RequestID != "" {
			span.SetAttributes(attribute.String("app.request_id", event.Application.RequestID))
		}
	}

	if event.Kubernetes != nil {
		span.SetAttributes(
			attribute.String("k8s.event_type", event.Kubernetes.EventType),
			attribute.String("k8s.reason", event.Kubernetes.Reason),
			attribute.String("k8s.object", event.Kubernetes.Object),
			attribute.String("k8s.object_kind", event.Kubernetes.ObjectKind),
		)
	}

	// Add impact context
	if event.Impact != nil {
		span.SetAttributes(
			attribute.String("impact.severity", event.Impact.Severity),
			attribute.Float64("impact.business", event.Impact.BusinessImpact),
			attribute.Bool("impact.customer_facing", event.Impact.CustomerFacing),
			attribute.Bool("impact.revenue_impacting", event.Impact.RevenueImpacting),
			attribute.Bool("impact.slo_impact", event.Impact.SLOImpact),
		)
	}
}

// getAdaptiveTimeWindowForUnified gets adaptive time window for UnifiedEvent
func (st *SemanticOTELTracer) getAdaptiveTimeWindowForUnified(event *domain.UnifiedEvent) time.Duration {
	baseWindow := 30 * time.Second

	// Adjust based on event severity
	switch event.GetSeverity() {
	case "critical":
		baseWindow = 1 * time.Minute
	case "high", "error":
		baseWindow = 45 * time.Second
	case "medium", "warning":
		baseWindow = 30 * time.Second
	default:
		baseWindow = 15 * time.Second
	}

	// Adjust based on event type
	if event.IsKernelEvent() {
		baseWindow = baseWindow / 2 // Kernel events are more time-sensitive
	} else if event.IsKubernetesEvent() {
		baseWindow = baseWindow * 2 // K8s events can have longer correlation windows
	}

	return baseWindow
}

// getUnifiedCorrelationDimension determines correlation dimension for UnifiedEvent
func (st *SemanticOTELTracer) getUnifiedCorrelationDimension(event *domain.UnifiedEvent, group *SemanticTraceGroup) string {
	// Check trace correlation
	if event.TraceContext != nil && group.TraceID == event.TraceContext.TraceID {
		return "trace"
	}

	// Check temporal correlation
	if len(group.UnifiedCausalChain) > 0 {
		lastEvent := group.UnifiedCausalChain[len(group.UnifiedCausalChain)-1]
		if event.Timestamp.Sub(lastEvent.Timestamp).Abs() <= st.getAdaptiveTimeWindowForUnified(event) {
			return "temporal"
		}
	}

	// Check entity correlation
	if event.Entity != nil {
		for _, chainEvent := range group.UnifiedCausalChain {
			if chainEvent.GetEntityID() == event.GetEntityID() {
				return "entity"
			}
		}
	}

	// Check causal correlation
	if event.Correlation != nil && len(event.Correlation.CausalChain) > 0 {
		return "causal"
	}

	return "semantic"
}

// assessUnifiedGroupImpact assesses impact for groups with UnifiedEvents
func (st *SemanticOTELTracer) assessUnifiedGroupImpact(group *SemanticTraceGroup) *ImpactAssessment {
	assessment := &ImpactAssessment{
		AffectedResources:  []string{},
		RecommendedActions: []string{},
		TimeToResolution:   30 * time.Minute,
	}

	if len(group.UnifiedCausalChain) == 0 {
		return assessment
	}

	// Analyze impact across all unified events
	maxBusinessImpact := float32(0)
	severityScore := 0
	resourceMap := make(map[string]bool)

	for _, event := range group.UnifiedCausalChain {
		// Use event's own impact assessment if available
		if event.Impact != nil {
			maxBusinessImpact = max(maxBusinessImpact, float32(event.Impact.BusinessImpact))

			// Track affected services
			for _, service := range event.Impact.AffectedServices {
				resourceMap[service] = true
			}

			// Update severity score
			switch event.Impact.Severity {
			case "critical":
				severityScore = max(severityScore, 4)
			case "high":
				severityScore = max(severityScore, 3)
			case "medium":
				severityScore = max(severityScore, 2)
			case "low":
				severityScore = max(severityScore, 1)
			}
		}

		// Track entity as resource
		if event.Entity != nil && event.Entity.Name != "" {
			resource := event.GetEntityID()
			if resource != "" {
				resourceMap[resource] = true
			}
		}
	}

	assessment.BusinessImpact = maxBusinessImpact

	// Convert severity score
	switch severityScore {
	case 4:
		assessment.TechnicalSeverity = "critical"
	case 3:
		assessment.TechnicalSeverity = "high"
	case 2:
		assessment.TechnicalSeverity = "medium"
	default:
		assessment.TechnicalSeverity = "low"
	}

	// Calculate cascade risk
	assessment.CascadeRisk = float32(len(group.UnifiedCausalChain)) / 5.0
	if assessment.CascadeRisk > 1.0 {
		assessment.CascadeRisk = 1.0
	}

	// Convert resources
	for resource := range resourceMap {
		assessment.AffectedResources = append(assessment.AffectedResources, resource)
	}

	// Generate recommended actions
	assessment.RecommendedActions = st.generateRecommendedActions(group)

	// Estimate resolution time
	assessment.TimeToResolution = st.estimateResolutionTime(group)

	return assessment
}

// classifySemanticType classifies the type of semantic event
func (st *SemanticOTELTracer) classifySemanticType(intent string) string {
	// Map intents to semantic types
	switch intent {
	case "memory_pressure_cascade", "oom_kill":
		return "memory_pressure_cascade"
	case "service_failure", "service_degradation":
		return "service_reliability_event"
	case "network_slowness", "connection_timeout":
		return "network_connectivity_issue"
	case "pod_crash_loop", "pod_failure":
		return "pod_lifecycle_event"
	case "resource_shortage", "scheduling_failure":
		return "resource_constraint_event"
	default:
		return "generic_system_event"
	}
}

// areIntentsRelated checks if two intents are semantically related
func (st *SemanticOTELTracer) areIntentsRelated(intent1, intent2 string) bool {
	// Define related intent groups
	relatedGroups := [][]string{
		{"memory_pressure_cascade", "oom_kill", "memory_exhaustion"},
		{"service_failure", "service_degradation", "service_unavailable"},
		{"network_slowness", "connection_timeout", "network_failure"},
		{"pod_crash_loop", "pod_failure", "container_restart"},
		{"resource_shortage", "scheduling_failure", "node_pressure"},
	}

	// Check if both intents are in the same group
	for _, group := range relatedGroups {
		hasIntent1 := false
		hasIntent2 := false
		for _, intent := range group {
			if intent == intent1 {
				hasIntent1 = true
			}
			if intent == intent2 {
				hasIntent2 = true
			}
		}
		if hasIntent1 && hasIntent2 {
			return true
		}
	}

	return false
}

// convertUnifiedToDomainEvent converts UnifiedEvent to domain.Event for backward compatibility
func (st *SemanticOTELTracer) convertUnifiedToDomainEvent(ue *domain.UnifiedEvent) *domain.Event {
	if ue == nil {
		return nil
	}

	// Map severity
	severity := domain.EventSeverityInfo
	switch ue.GetSeverity() {
	case "critical":
		severity = domain.EventSeverityCritical
	case "high":
		severity = domain.EventSeverityHigh
	case "medium":
		severity = domain.EventSeverityMedium
	case "low":
		severity = domain.EventSeverityLow
	}

	// Extract confidence from semantic context
	confidence := 0.5
	if ue.Semantic != nil {
		confidence = ue.Semantic.Confidence
	}

	// Build event context
	ctx := domain.EventContext{
		TraceID: "",
		SpanID:  "",
		Host:    "",
		Labels:  make(map[string]string),
	}

	// Extract trace context
	if ue.TraceContext != nil {
		ctx.TraceID = ue.TraceContext.TraceID
		ctx.SpanID = ue.TraceContext.SpanID
	}

	// Extract entity info
	if ue.Entity != nil {
		if ue.Entity.Namespace != "" {
			ctx.Namespace = ue.Entity.Namespace
		}
		if ue.Entity.Labels != nil {
			for k, v := range ue.Entity.Labels {
				ctx.Labels[k] = v
			}
		}
	}

	// Create payload based on event type
	payload := domain.GenericEventPayload{
		Type: "unified_event",
		Data: map[string]interface{}{
			"unified_id": ue.ID,
		},
	}

	return &domain.Event{
		ID:         domain.EventID(ue.ID),
		Type:       domain.EventType(ue.Type),
		Timestamp:  ue.Timestamp,
		Source:     domain.SourceType(ue.Source),
		Severity:   severity,
		Confidence: confidence,
		Context:    ctx,
		Payload:    payload,
	}
}
