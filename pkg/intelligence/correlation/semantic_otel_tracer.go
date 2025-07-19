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
	tracer             trace.Tracer
	semanticGroups     map[string]*SemanticTraceGroup
	adaptiveWindows    map[string]time.Duration
	causalityTracker   *SimpleCausalityTracker
	spatialRadius      int
}

// SemanticTraceGroup represents events grouped by meaning and causality
type SemanticTraceGroup struct {
	ID               string
	Intent           string    // What is this group trying to achieve?
	SemanticType     string    // memory_cascade, network_failure, etc.
	RootCause        *domain.Event
	CausalChain      []*domain.Event
	ConfidenceScore  float64
	ImpactAssessment *ImpactAssessment
	PredictedOutcome *PredictedOutcome
	TraceID          string
	SpanContext      trace.SpanContext
}

// ImpactAssessment assesses business and technical impact
type ImpactAssessment struct {
	BusinessImpact     float32
	TechnicalSeverity  string
	CascadeRisk        float32
	AffectedResources  []string
	TimeToResolution   time.Duration
	RecommendedActions []string
}

// PredictedOutcome predicts what will happen
type PredictedOutcome struct {
	Scenario          string        // "cascade_failure", "recovery", etc.
	Probability       float64
	TimeToOutcome     time.Duration
	PreventionActions []string
	ConfidenceLevel   float64
}

// SimpleCausalityTracker tracks causal relationships
type SimpleCausalityTracker struct {
	causalLinks       map[string][]CausalLink
	timeWindow        time.Duration
	strengthThreshold float64
}

// CausalLink represents a causal relationship
type CausalLink struct {
	SourceEventID string
	TargetEventID string
	Strength      float64
	Type          string  // "triggers", "causes", "correlates"
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
	assessment.CascadeRisk = float32(len(group.CausalChain)) / 5.0  // More sensitive to cascade size
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