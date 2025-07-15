package correlation

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/yairfalse/tapio/pkg/events/opinionated"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SemanticEventGrouper creates intelligent OTEL traces that group related events
// This uses Tapio's OpinionatedEvent structure to create the most advanced
// correlation-aware OTEL traces in the industry
type SemanticEventGrouper struct {
	tracer             trace.Tracer
	correlationEngine  *Engine
	semanticGroupCache map[string]*SemanticGroup
	causalityTracker   *CausalityTracker
	intentClassifier   *IntentClassifier
	behavioralAnalyzer *BehavioralAnalyzer
	timeWindowGrouper  *TimeWindowGrouper
	spatialGrouper     *SpatialGrouper
}

// SemanticGroup represents a group of semantically related events
type SemanticGroup struct {
	ID               string                          `json:"id"`
	Intent           string                          `json:"intent"`           // What is this group trying to achieve?
	SemanticType     string                          `json:"semantic_type"`    // memory_cascade, network_failure, etc.
	RootCause        *opinionated.OpinionatedEvent   `json:"root_cause"`       // The triggering event
	CausalChain      []*opinionated.OpinionatedEvent `json:"causal_chain"`     // Events in causal order
	SpatialCluster   []*opinionated.OpinionatedEvent `json:"spatial_cluster"`  // Events in same spatial area
	TemporalCluster  []*opinionated.OpinionatedEvent `json:"temporal_cluster"` // Events in same time window
	ConfidenceScore  float64                         `json:"confidence_score"` // Group coherence confidence
	ImpactAssessment *GroupImpactAssessment          `json:"impact_assessment"`
	PredictedOutcome *PredictedOutcome               `json:"predicted_outcome"`
	TraceID          string                          `json:"trace_id"` // OTEL trace ID
	SpanContext      trace.SpanContext               `json:"-"`        // OTEL span context
}

// GroupImpactAssessment assesses the collective impact of a semantic group
type GroupImpactAssessment struct {
	BusinessImpact     float32        `json:"business_impact"`
	TechnicalSeverity  string         `json:"technical_severity"`
	UserExperience     float32        `json:"user_experience"`
	SecurityRisk       float32        `json:"security_risk"`
	CascadeRisk        float32        `json:"cascade_risk"`
	AffectedResources  []string       `json:"affected_resources"`
	BlastRadius        map[string]int `json:"blast_radius"`
	TimeToResolution   time.Duration  `json:"time_to_resolution"`
	RecommendedActions []string       `json:"recommended_actions"`
}

// PredictedOutcome predicts what will happen based on this semantic group
type PredictedOutcome struct {
	Scenario          string        `json:"scenario"` // "cascade_failure", "recovery", etc.
	Probability       float64       `json:"probability"`
	TimeToOutcome     time.Duration `json:"time_to_outcome"`
	PreventionActions []string      `json:"prevention_actions"`
	MitigationActions []string      `json:"mitigation_actions"`
	ConfidenceLevel   float64       `json:"confidence_level"`
}

// CausalityTracker tracks causality relationships across events
type CausalityTracker struct {
	causalGraph       map[string][]*CausalLink
	timeWindowLookup  time.Duration
	strengthThreshold float64
}

// TimeWindowGrouper groups events by intelligent time windows
type TimeWindowGrouper struct {
	adaptiveWindows map[string]time.Duration // Per event type
	baseWindow      time.Duration
	maxWindow       time.Duration
}

// SpatialGrouper groups events by spatial relationships
type SpatialGrouper struct {
	clusteringRadius int                 // How many "hops" in K8s topology
	entityGraph      map[string][]string // Entity relationship graph
}

// BehavioralAnalyzer analyzes behavioral patterns in event groups
type BehavioralAnalyzer struct {
	behaviorModels   map[string]*BehaviorModel
	anomalyThreshold float64
}

// BehaviorModel represents learned behavior for an entity type
type BehaviorModel struct {
	EntityType        string
	NormalPatterns    []string
	AnomalyIndicators []string
	BaselineMetrics   map[string]float64
}

// NewSemanticEventGrouper creates a new semantic event grouper
func NewSemanticEventGrouper(engine *Engine) *SemanticEventGrouper {
	return &SemanticEventGrouper{
		tracer:             otel.Tracer("tapio-semantic-grouping"),
		correlationEngine:  engine,
		semanticGroupCache: make(map[string]*SemanticGroup),
		causalityTracker: &CausalityTracker{
			causalGraph:       make(map[string][]*CausalLink),
			timeWindowLookup:  5 * time.Minute,
			strengthThreshold: 0.7,
		},
		timeWindowGrouper: &TimeWindowGrouper{
			adaptiveWindows: map[string]time.Duration{
				"memory_leak":     30 * time.Second, // Memory issues develop quickly
				"network_failure": 10 * time.Second, // Network cascades fast
				"disk_pressure":   2 * time.Minute,  // Disk issues develop slowly
				"cpu_throttling":  5 * time.Second,  // CPU throttling is immediate
			},
			baseWindow: 30 * time.Second,
			maxWindow:  5 * time.Minute,
		},
		spatialGrouper: &SpatialGrouper{
			clusteringRadius: 2, // Group events within 2 K8s hops
			entityGraph:      make(map[string][]string),
		},
		behavioralAnalyzer: &BehavioralAnalyzer{
			behaviorModels:   make(map[string]*BehaviorModel),
			anomalyThreshold: 0.8,
		},
	}
}

// ProcessEventWithSemanticGrouping processes an event and creates intelligent OTEL traces
func (seg *SemanticEventGrouper) ProcessEventWithSemanticGrouping(ctx context.Context, event *opinionated.OpinionatedEvent) error {
	// Step 1: Classify the semantic intent
	intent := seg.classifySemanticIntent(event)

	// Step 2: Find or create semantic group
	group := seg.findOrCreateSemanticGroup(ctx, event, intent)

	// Step 3: Create the correlation-aware trace
	return seg.createSemanticCorrelationTrace(ctx, event, group)
}

// classifySemanticIntent determines the semantic intent of the event
func (seg *SemanticEventGrouper) classifySemanticIntent(event *opinionated.OpinionatedEvent) string {
	// Use existing semantic context if available
	if event.Semantic != nil && event.Semantic.Intent != "" {
		return event.Semantic.Intent
	}

	// Classify based on behavioral patterns
	if event.Behavioral != nil {
		if event.Behavioral.AnomalyScore > 0.8 {
			return "anomaly_investigation"
		}
		if event.Behavioral.BehaviorTrend == "degrading" {
			return "performance_degradation"
		}
	}

	// Classify based on impact
	if event.Impact != nil {
		if event.Impact.BusinessImpact > 0.7 {
			return "business_critical_incident"
		}
		if event.Impact.SecurityImpact > 0.6 {
			return "security_investigation"
		}
	}

	// Classify based on temporal patterns
	if event.Temporal != nil && event.Temporal.IsPeriodic {
		return "periodic_maintenance"
	}

	// Default classification
	return "operational_monitoring"
}

// findOrCreateSemanticGroup finds existing group or creates new one
func (seg *SemanticEventGrouper) findOrCreateSemanticGroup(ctx context.Context, event *opinionated.OpinionatedEvent, intent string) *SemanticGroup {
	// Check for existing causal relationships
	if existingGroup := seg.findCausallyRelatedGroup(event); existingGroup != nil {
		return existingGroup
	}

	// Check for spatial clustering
	if existingGroup := seg.findSpatiallyRelatedGroup(event); existingGroup != nil {
		return existingGroup
	}

	// Check for temporal clustering
	if existingGroup := seg.findTemporallyRelatedGroup(event); existingGroup != nil {
		return existingGroup
	}

	// Check for behavioral pattern grouping
	if existingGroup := seg.findBehaviorallyRelatedGroup(event); existingGroup != nil {
		return existingGroup
	}

	// Create new semantic group
	return seg.createNewSemanticGroup(ctx, event, intent)
}

// findCausallyRelatedGroup finds group based on causality
func (seg *SemanticEventGrouper) findCausallyRelatedGroup(event *opinionated.OpinionatedEvent) *SemanticGroup {
	if event.Causality == nil || len(event.Causality.CausalChain) == 0 {
		return nil
	}

	// Look for groups containing events in the causal chain
	for _, causalEvent := range event.Causality.CausalChain {
		for _, group := range seg.semanticGroupCache {
			for _, groupEvent := range group.CausalChain {
				if groupEvent.ID == causalEvent.EventID {
					return group
				}
			}
		}
	}

	return nil
}

// findSpatiallyRelatedGroup finds group based on spatial proximity
func (seg *SemanticEventGrouper) findSpatiallyRelatedGroup(event *opinionated.OpinionatedEvent) *SemanticGroup {
	eventEntity := seg.getEntityIdentifier(event)

	for _, group := range seg.semanticGroupCache {
		if seg.areEntitiesSpatiallyRelated(eventEntity, seg.getGroupEntityCluster(group)) {
			return group
		}
	}

	return nil
}

// findTemporallyRelatedGroup finds group based on temporal proximity
func (seg *SemanticEventGrouper) findTemporallyRelatedGroup(event *opinionated.OpinionatedEvent) *SemanticGroup {
	eventTime := event.Timestamp

	// Get adaptive time window for this event type
	window := seg.getAdaptiveTimeWindow(event)

	for _, group := range seg.semanticGroupCache {
		if seg.isWithinTemporalWindow(eventTime, group, window) {
			return group
		}
	}

	return nil
}

// findBehaviorallyRelatedGroup finds group based on behavioral patterns
func (seg *SemanticEventGrouper) findBehaviorallyRelatedGroup(event *opinionated.OpinionatedEvent) *SemanticGroup {
	if event.Behavioral == nil {
		return nil
	}

	for _, group := range seg.semanticGroupCache {
		if seg.hasSimilarBehavioralPattern(event, group) {
			return group
		}
	}

	return nil
}

// createNewSemanticGroup creates a new semantic group
func (seg *SemanticEventGrouper) createNewSemanticGroup(ctx context.Context, event *opinionated.OpinionatedEvent, intent string) *SemanticGroup {
	groupID := fmt.Sprintf("semantic_group_%s_%d", intent, time.Now().UnixNano())

	// Create OTEL trace context for this semantic group
	ctx, span := seg.tracer.Start(ctx, fmt.Sprintf("semantic_group.%s", intent))

	group := &SemanticGroup{
		ID:              groupID,
		Intent:          intent,
		SemanticType:    seg.deriveSemanticType(event),
		RootCause:       event,
		CausalChain:     []*opinionated.OpinionatedEvent{event},
		SpatialCluster:  []*opinionated.OpinionatedEvent{event},
		TemporalCluster: []*opinionated.OpinionatedEvent{event},
		ConfidenceScore: seg.calculateInitialConfidence(event),
		TraceID:         span.SpanContext().TraceID().String(),
		SpanContext:     span.SpanContext(),
	}

	// Assess impact
	group.ImpactAssessment = seg.assessGroupImpact(group)

	// Predict outcome
	group.PredictedOutcome = seg.predictGroupOutcome(group)

	// Cache the group
	seg.semanticGroupCache[groupID] = group

	return group
}

// createSemanticCorrelationTrace creates OTEL trace with correlation information
func (seg *SemanticEventGrouper) createSemanticCorrelationTrace(ctx context.Context, event *opinionated.OpinionatedEvent, group *SemanticGroup) error {
	// Use the group's span context
	ctx = trace.ContextWithSpanContext(ctx, group.SpanContext)

	// Create span for this specific event within the semantic group
	ctx, span := seg.tracer.Start(ctx, fmt.Sprintf("event.%s", event.Category),
		trace.WithAttributes(
			// Core event attributes
			attribute.String("event.id", event.ID),
			attribute.String("event.category", string(event.Category)),
			attribute.String("event.severity", string(event.Severity)),
			attribute.Float64("event.confidence", float64(event.Confidence)),

			// Semantic grouping attributes
			attribute.String("semantic.group_id", group.ID),
			attribute.String("semantic.intent", group.Intent),
			attribute.String("semantic.type", group.SemanticType),
			attribute.Float64("semantic.group_confidence", group.ConfidenceScore),
			attribute.Int("semantic.causal_chain_position", len(group.CausalChain)),

			// Correlation attributes
			attribute.Bool("correlation.is_root_cause", event.ID == group.RootCause.ID),
			attribute.Int("correlation.related_events_count", len(group.CausalChain)),

			// Impact attributes
			attribute.Float64("impact.business", float64(group.ImpactAssessment.BusinessImpact)),
			attribute.String("impact.severity", group.ImpactAssessment.TechnicalSeverity),
			attribute.Float64("impact.cascade_risk", float64(group.ImpactAssessment.CascadeRisk)),

			// Prediction attributes
			attribute.String("prediction.scenario", group.PredictedOutcome.Scenario),
			attribute.Float64("prediction.probability", group.PredictedOutcome.Probability),
			attribute.Int64("prediction.time_to_outcome_seconds", int64(group.PredictedOutcome.TimeToOutcome.Seconds())),
		),
	)
	defer span.End()

	// Add semantic context if available
	if event.Semantic != nil {
		span.SetAttributes(
			attribute.String("semantic.domain", event.Semantic.Domain),
			attribute.StringSlice("semantic.concepts", event.Semantic.Concepts),
			attribute.String("semantic.event_type", event.Semantic.EventType),
			attribute.String("semantic.description", event.Semantic.Description),
			attribute.StringSlice("semantic.ontology_tags", event.Semantic.OntologyTags),
		)
	}

	// Add behavioral context if available
	if event.Behavioral != nil {
		span.SetAttributes(
			attribute.Float64("behavioral.anomaly_score", float64(event.Behavioral.AnomalyScore)),
			attribute.String("behavioral.trend", event.Behavioral.BehaviorTrend),
			attribute.Float64("behavioral.deviation", event.Behavioral.BehaviorDeviation),
			attribute.StringSlice("behavioral.patterns", event.Behavioral.Patterns),
		)

		if event.Behavioral.Entity != nil {
			span.SetAttributes(
				attribute.String("behavioral.entity.type", event.Behavioral.Entity.Type),
				attribute.String("behavioral.entity.name", event.Behavioral.Entity.Name),
				attribute.Float64("behavioral.entity.trust_score", event.Behavioral.Entity.TrustScore),
			)
		}
	}

	// Add temporal context if available
	if event.Temporal != nil {
		span.SetAttributes(
			attribute.Bool("temporal.is_periodic", event.Temporal.IsPeriodic),
			attribute.Float64("temporal.frequency_hz", event.Temporal.FrequencyHz),
			attribute.Int64("temporal.period_seconds", int64(event.Temporal.Period.Seconds())),
		)
	}

	// Add causality information if available
	if event.Causality != nil {
		span.SetAttributes(
			attribute.String("causality.root_cause", event.Causality.RootCause),
			attribute.Int("causality.chain_depth", event.Causality.ChainDepth),
			attribute.Float64("causality.confidence", event.Causality.Confidence),
		)

		// Add causal chain as span events
		for i, causalEvent := range event.Causality.CausalChain {
			span.AddEvent(fmt.Sprintf("causal_event_%d", i),
				trace.WithAttributes(
					attribute.String("causal.event_id", causalEvent.EventID),
					attribute.String("causal.description", causalEvent.Description),
					attribute.Float64("causal.confidence", causalEvent.Confidence),
				),
				trace.WithTimestamp(causalEvent.Timestamp),
			)
		}
	}

	// Add correlation vectors for AI analysis
	if event.Correlation != nil {
		for i, vector := range event.Correlation.Vectors {
			if len(vector.Temporal) > 0 {
				span.SetAttributes(
					attribute.String(fmt.Sprintf("correlation.temporal_vector_%d", i), fmt.Sprintf("%v", vector.Temporal)),
					attribute.String(fmt.Sprintf("correlation.spatial_vector_%d", i), fmt.Sprintf("%v", vector.Spatial)),
					attribute.String(fmt.Sprintf("correlation.semantic_vector_%d", i), fmt.Sprintf("%v", vector.Semantic)),
				)
			}
		}

		// Add correlation groups
		for i, group := range event.Correlation.Groups {
			span.SetAttributes(
				attribute.String(fmt.Sprintf("correlation.group_%d.id", i), group.ID),
				attribute.String(fmt.Sprintf("correlation.group_%d.type", i), group.Type),
				attribute.Float64(fmt.Sprintf("correlation.group_%d.score", i), group.Score),
				attribute.Int(fmt.Sprintf("correlation.group_%d.event_count", i), len(group.Events)),
			)
		}
	}

	// Add recommended actions
	for i, action := range group.ImpactAssessment.RecommendedActions {
		span.AddEvent(fmt.Sprintf("recommended_action_%d", i),
			trace.WithAttributes(
				attribute.String("action", action),
				attribute.String("action_type", "recommendation"),
			),
		)
	}

	return nil
}

// Helper methods for semantic grouping logic

func (seg *SemanticEventGrouper) getEntityIdentifier(event *opinionated.OpinionatedEvent) string {
	if event.Context.Namespace != "" && event.Context.Pod != "" {
		return fmt.Sprintf("%s/%s", event.Context.Namespace, event.Context.Pod)
	}
	if event.Context.ProcessName != "" {
		return fmt.Sprintf("process:%s", event.Context.ProcessName)
	}
	return "unknown"
}

func (seg *SemanticEventGrouper) getGroupEntityCluster(group *SemanticGroup) []string {
	entities := make(map[string]bool)
	for _, event := range group.SpatialCluster {
		entity := seg.getEntityIdentifier(event)
		entities[entity] = true
	}

	var result []string
	for entity := range entities {
		result = append(result, entity)
	}
	return result
}

func (seg *SemanticEventGrouper) areEntitiesSpatiallyRelated(eventEntity string, groupEntities []string) bool {
	// Check if event entity is within clustering radius of any group entity
	for _, groupEntity := range groupEntities {
		if seg.calculateSpatialDistance(eventEntity, groupEntity) <= seg.spatialGrouper.clusteringRadius {
			return true
		}
	}
	return false
}

func (seg *SemanticEventGrouper) calculateSpatialDistance(entity1, entity2 string) int {
	// Simple implementation - in production would use K8s topology graph
	if entity1 == entity2 {
		return 0
	}

	// Same namespace = distance 1
	ns1 := seg.extractNamespace(entity1)
	ns2 := seg.extractNamespace(entity2)
	if ns1 == ns2 && ns1 != "" {
		return 1
	}

	// Different namespace = distance 2
	return 2
}

func (seg *SemanticEventGrouper) extractNamespace(entity string) string {
	// Extract namespace from entity identifier
	if entity == "" {
		return ""
	}

	// Format: namespace/pod
	parts := strings.Split(entity, "/")
	if len(parts) >= 2 {
		return parts[0]
	}
	return ""
}

func (seg *SemanticEventGrouper) getAdaptiveTimeWindow(event *opinionated.OpinionatedEvent) time.Duration {
	// Get adaptive window based on event characteristics
	eventType := seg.deriveSemanticType(event)

	if window, exists := seg.timeWindowGrouper.adaptiveWindows[eventType]; exists {
		return window
	}

	// Adjust based on severity
	baseWindow := seg.timeWindowGrouper.baseWindow
	switch event.Severity {
	case "critical":
		return baseWindow / 2 // Tight grouping for critical events
	case "high":
		return baseWindow
	case "medium":
		return baseWindow * 2
	case "low":
		return baseWindow * 3
	default:
		return baseWindow
	}
}

func (seg *SemanticEventGrouper) isWithinTemporalWindow(eventTime time.Time, group *SemanticGroup, window time.Duration) bool {
	// Check if event is within temporal window of any event in the group
	for _, groupEvent := range group.TemporalCluster {
		if eventTime.Sub(groupEvent.Timestamp).Abs() <= window {
			return true
		}
	}
	return false
}

func (seg *SemanticEventGrouper) hasSimilarBehavioralPattern(event *opinionated.OpinionatedEvent, group *SemanticGroup) bool {
	if event.Behavioral == nil {
		return false
	}

	// Check if event has similar behavioral patterns to group events
	for _, groupEvent := range group.CausalChain {
		if groupEvent.Behavioral != nil {
			similarity := seg.calculateBehavioralSimilarity(event.Behavioral, groupEvent.Behavioral)
			if similarity > seg.behavioralAnalyzer.anomalyThreshold {
				return true
			}
		}
	}

	return false
}

func (seg *SemanticEventGrouper) calculateBehavioralSimilarity(behavioral1, behavioral2 *opinionated.BehavioralContext) float64 {
	// Simple similarity calculation - in production would use ML
	score := 0.0

	// Compare anomaly scores
	if behavioral1.AnomalyScore > 0 && behavioral2.AnomalyScore > 0 {
		diff := abs(behavioral1.AnomalyScore - behavioral2.AnomalyScore)
		score += (1.0 - float64(diff)) * 0.3
	}

	// Compare behavior trends
	if behavioral1.BehaviorTrend == behavioral2.BehaviorTrend {
		score += 0.3
	}

	// Compare entity trust scores
	if behavioral1.Entity != nil && behavioral2.Entity != nil {
		trustDiff := abs(float32(behavioral1.Entity.TrustScore - behavioral2.Entity.TrustScore))
		score += (1.0 - float64(trustDiff)) * 0.2
	}

	// Compare common patterns
	commonPatterns := seg.countCommonPatterns(behavioral1.Patterns, behavioral2.Patterns)
	if len(behavioral1.Patterns) > 0 && len(behavioral2.Patterns) > 0 {
		patternSimilarity := float64(commonPatterns) / float64(max(len(behavioral1.Patterns), len(behavioral2.Patterns)))
		score += patternSimilarity * 0.2
	}

	return score
}

func (seg *SemanticEventGrouper) deriveSemanticType(event *opinionated.OpinionatedEvent) string {
	// Derive semantic type from event characteristics
	if event.Semantic != nil && event.Semantic.EventType != "" {
		return event.Semantic.EventType
	}

	// Derive from category and context
	category := string(event.Category)

	// Add context-specific classification
	if event.Context.Container != "" {
		return fmt.Sprintf("%s_container", category)
	}
	if event.Context.Pod != "" {
		return fmt.Sprintf("%s_pod", category)
	}
	if event.Context.ProcessName != "" {
		return fmt.Sprintf("%s_process", category)
	}

	return category
}

func (seg *SemanticEventGrouper) calculateInitialConfidence(event *opinionated.OpinionatedEvent) float64 {
	// Calculate initial confidence for new semantic group
	confidence := float64(event.Confidence)

	// Boost confidence based on semantic richness
	if event.Semantic != nil {
		confidence += 0.1
	}
	if event.Behavioral != nil {
		confidence += 0.1
	}
	if event.Causality != nil {
		confidence += 0.15
	}
	if event.Temporal != nil {
		confidence += 0.05
	}

	// Ensure confidence stays within bounds
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func (seg *SemanticEventGrouper) assessGroupImpact(group *SemanticGroup) *GroupImpactAssessment {
	// Assess the collective impact of the semantic group
	assessment := &GroupImpactAssessment{
		BlastRadius:        make(map[string]int),
		RecommendedActions: []string{},
	}

	// Aggregate impact from all events in the group
	maxBusinessImpact := float32(0)
	maxSecurityRisk := float32(0)
	severityScore := 0

	affectedResourcesMap := make(map[string]bool)

	for _, event := range group.CausalChain {
		if event.Impact != nil {
			if event.Impact.BusinessImpact > maxBusinessImpact {
				maxBusinessImpact = event.Impact.BusinessImpact
			}
			if event.Impact.SecurityImpact > maxSecurityRisk {
				maxSecurityRisk = event.Impact.SecurityImpact
			}

			// Track affected resources
			resource := seg.getEntityIdentifier(event)
			affectedResourcesMap[resource] = true
		}

		// Calculate severity score
		switch event.Severity {
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

	assessment.BusinessImpact = maxBusinessImpact
	assessment.SecurityRisk = maxSecurityRisk

	// Convert severity score to string
	switch severityScore {
	case 4:
		assessment.TechnicalSeverity = "critical"
	case 3:
		assessment.TechnicalSeverity = "high"
	case 2:
		assessment.TechnicalSeverity = "medium"
	case 1:
		assessment.TechnicalSeverity = "low"
	default:
		assessment.TechnicalSeverity = "info"
	}

	// Calculate cascade risk based on causal chain length
	assessment.CascadeRisk = float32(len(group.CausalChain)) / 10.0
	if assessment.CascadeRisk > 1.0 {
		assessment.CascadeRisk = 1.0
	}

	// Convert affected resources map to slice
	for resource := range affectedResourcesMap {
		assessment.AffectedResources = append(assessment.AffectedResources, resource)
	}

	// Generate recommended actions based on semantic type
	assessment.RecommendedActions = seg.generateRecommendedActions(group)

	return assessment
}

func (seg *SemanticEventGrouper) predictGroupOutcome(group *SemanticGroup) *PredictedOutcome {
	// Predict the likely outcome of this semantic group
	outcome := &PredictedOutcome{
		PreventionActions: []string{},
		MitigationActions: []string{},
	}

	// Predict based on semantic type and patterns
	switch group.SemanticType {
	case "memory_leak", "system_health_container":
		outcome.Scenario = "oom_kill_cascade"
		outcome.Probability = 0.8
		outcome.TimeToOutcome = 5 * time.Minute
		outcome.PreventionActions = []string{
			"Increase memory limits",
			"Implement memory monitoring",
			"Scale horizontally",
		}

	case "network_failure", "network_health_pod":
		outcome.Scenario = "service_isolation"
		outcome.Probability = 0.7
		outcome.TimeToOutcome = 2 * time.Minute
		outcome.PreventionActions = []string{
			"Check network policies",
			"Verify service mesh configuration",
			"Restart affected pods",
		}

	case "performance_issue":
		outcome.Scenario = "performance_degradation"
		outcome.Probability = 0.6
		outcome.TimeToOutcome = 10 * time.Minute
		outcome.PreventionActions = []string{
			"Scale up resources",
			"Optimize queries",
			"Enable caching",
		}

	default:
		outcome.Scenario = "unknown"
		outcome.Probability = 0.3
		outcome.TimeToOutcome = 15 * time.Minute
	}

	// Adjust confidence based on group coherence
	outcome.ConfidenceLevel = group.ConfidenceScore * 0.8

	return outcome
}

func (seg *SemanticEventGrouper) generateRecommendedActions(group *SemanticGroup) []string {
	actions := []string{}

	// Generate actions based on semantic type
	switch group.SemanticType {
	case "memory_leak":
		actions = append(actions, "kubectl top pods -n "+seg.extractNamespace(seg.getEntityIdentifier(group.RootCause)))
		actions = append(actions, "kubectl describe pod "+group.RootCause.Context.Pod)
		actions = append(actions, "kubectl logs "+group.RootCause.Context.Pod+" --previous")

	case "network_failure":
		actions = append(actions, "kubectl get networkpolicies -n "+seg.extractNamespace(seg.getEntityIdentifier(group.RootCause)))
		actions = append(actions, "kubectl get svc -n "+seg.extractNamespace(seg.getEntityIdentifier(group.RootCause)))
		actions = append(actions, "kubectl get endpoints -n "+seg.extractNamespace(seg.getEntityIdentifier(group.RootCause)))

	case "performance_issue":
		actions = append(actions, "kubectl top nodes")
		actions = append(actions, "kubectl get hpa -n "+seg.extractNamespace(seg.getEntityIdentifier(group.RootCause)))

	default:
		actions = append(actions, "kubectl get events --sort-by=.metadata.creationTimestamp")
	}

	return actions
}

// Utility functions

func abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (seg *SemanticEventGrouper) countCommonPatterns(patterns1, patterns2 []string) int {
	patternMap := make(map[string]bool)
	for _, pattern := range patterns1 {
		patternMap[pattern] = true
	}

	common := 0
	for _, pattern := range patterns2 {
		if patternMap[pattern] {
			common++
		}
	}

	return common
}
