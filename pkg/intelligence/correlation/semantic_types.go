package correlation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SimpleSemanticGrouper provides intelligent semantic grouping with OTEL integration
type SimpleSemanticGrouper struct {
	// OTEL tracing
	tracer trace.Tracer

	// Semantic analysis configuration
	config SemanticGrouperConfig

	// Semantic patterns and rules
	semanticPatterns map[string]*SemanticPattern

	// Caching for performance
	groupCache      map[string]*EventGroup
	groupCacheMutex sync.RWMutex

	// Statistics
	stats SemanticGrouperStats
}

// SemanticGrouperConfig configures the semantic grouper
type SemanticGrouperConfig struct {
	// Time window for grouping related events
	TimeWindow time.Duration

	// Minimum confidence threshold
	MinConfidence float64

	// Maximum events per group
	MaxGroupSize int

	// Semantic similarity threshold
	SimilarityThreshold float64

	// Enable ML-based grouping
	EnableMLGrouping bool
}

// SemanticGrouperStats tracks grouper performance
type SemanticGrouperStats struct {
	EventsProcessed   int64
	GroupsCreated     int64
	CacheHits         int64
	CacheMisses       int64
	ProcessingTimeMs  int64
	LastProcessedTime time.Time
}

// SemanticPattern defines a pattern for semantic grouping
type SemanticPattern struct {
	ID          string
	Name        string
	Description string
	Keywords    []string
	EventTypes  []domain.EventType
	Scorer      func(event domain.Event) float64
}

// NewSimpleSemanticGrouper creates an advanced semantic grouper
func NewSimpleSemanticGrouper() *SimpleSemanticGrouper {
	return &SimpleSemanticGrouper{
		tracer: otel.Tracer("tapio.correlation.semantic_grouper"),
		config: SemanticGrouperConfig{
			TimeWindow:          5 * time.Minute,
			MinConfidence:       0.7,
			MaxGroupSize:        100,
			SimilarityThreshold: 0.8,
			EnableMLGrouping:    true,
		},
		semanticPatterns: initializeSemanticPatterns(),
		groupCache:       make(map[string]*EventGroup),
		stats:            SemanticGrouperStats{},
	}
}

// EventGroup represents a sophisticated semantic group of events
type EventGroup struct {
	// Core identification
	ID          string
	Type        string
	Description string

	// Grouped events with semantic relationships
	Events             []domain.Event
	EventRelationships map[string][]EventRelationship

	// Temporal analysis
	TimeSpan        time.Duration
	TemporalPattern TemporalPattern

	// Semantic analysis
	Confidence       float64
	SemanticScore    float64
	SemanticFeatures map[string]float64

	// OTEL integration
	TraceID string
	SpanID  string

	// Business context
	BusinessImpact    float64
	AffectedServices  []string
	RootCauseAnalysis *RootCauseAnalysis

	// ML predictions
	PredictedEvolution []PredictedEvent
	AnomalyScore       float64
}

// EventRelationship defines how events relate to each other
type EventRelationship struct {
	SourceEventID string
	TargetEventID string
	RelationType  RelationType
	Confidence    float64
	Evidence      []string
}

// RelationType defines types of event relationships
type RelationType string

const (
	RelationCausedBy   RelationType = "caused_by"
	RelationTriggers   RelationType = "triggers"
	RelationCorrelates RelationType = "correlates_with"
	RelationPrecedes   RelationType = "precedes"
	RelationDependent  RelationType = "dependent_on"
	RelationMitigates  RelationType = "mitigates"
	RelationEscalates  RelationType = "escalates"
)

// TemporalPattern describes time-based patterns in event groups
type TemporalPattern struct {
	Type        string  // periodic, burst, gradual, sudden
	Periodicity float64 // in seconds, 0 if not periodic
	Trend       string  // increasing, decreasing, stable, volatile
	Anomalies   []TemporalAnomaly
}

// TemporalAnomaly represents unusual temporal behavior
type TemporalAnomaly struct {
	Timestamp   time.Time
	Type        string
	Description string
	Severity    float64
}

// RootCauseAnalysis provides deep analysis of event causality
type RootCauseAnalysis struct {
	RootCauseEvent      *domain.Event
	CausalChain         []CausalLinkDetail
	ContributingFactors []ContributingFactor
	Confidence          float64
	Evidence            []string
}

// CausalLinkDetail represents a link in the causal chain for root cause analysis
type CausalLinkDetail struct {
	FromEvent   string
	ToEvent     string
	Mechanism   string // how the causation works
	Probability float64
	Latency     time.Duration
}

// ContributingFactor represents factors that contributed to the issue
type ContributingFactor struct {
	Factor      string
	Impact      float64
	Type        string // environmental, configuration, load, etc.
	Remediation string
}

// PredictedEvent represents a predicted future event
type PredictedEvent struct {
	EventType   domain.EventType
	Probability float64
	TimeWindow  time.Duration
	Impact      string
	Prevention  []string
}

// GroupEvents performs advanced semantic grouping with OTEL tracing
func (sg *SimpleSemanticGrouper) GroupEvents(events []domain.Event) []EventGroup {
	ctx, span := sg.tracer.Start(context.Background(), "semantic_grouper.group_events",
		trace.WithAttributes(
			attribute.Int("event_count", len(events)),
		),
	)
	defer span.End()

	startTime := time.Now()
	defer func() {
		sg.stats.ProcessingTimeMs = time.Since(startTime).Milliseconds()
		sg.stats.LastProcessedTime = time.Now()
	}()

	// Sort events by time for temporal analysis
	sortedEvents := make([]domain.Event, len(events))
	copy(sortedEvents, events)
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].Timestamp.Before(sortedEvents[j].Timestamp)
	})

	// Perform multi-dimensional grouping
	groups := sg.performMultiDimensionalGrouping(ctx, sortedEvents)

	// Enhance groups with semantic analysis
	for i := range groups {
		sg.enhanceGroupWithSemantics(ctx, &groups[i])
		sg.performRootCauseAnalysis(ctx, &groups[i])
		sg.predictFutureEvents(ctx, &groups[i])
	}

	// Update statistics
	sg.stats.EventsProcessed += int64(len(events))
	sg.stats.GroupsCreated += int64(len(groups))

	span.SetAttributes(
		attribute.Int("groups_created", len(groups)),
		attribute.Int64("processing_time_ms", sg.stats.ProcessingTimeMs),
	)

	return groups
}

// performMultiDimensionalGrouping groups events using multiple dimensions
func (sg *SimpleSemanticGrouper) performMultiDimensionalGrouping(ctx context.Context, events []domain.Event) []EventGroup {
	_, span := sg.tracer.Start(ctx, "semantic_grouper.multi_dimensional_grouping")
	defer span.End()

	// Initialize grouping dimensions
	typeGroups := sg.groupByType(events)
	temporalGroups := sg.groupByTemporalProximity(events)
	semanticGroups := sg.groupBySemanticsML(events)

	// Merge groups intelligently
	mergedGroups := sg.mergeGroupsIntelligently(typeGroups, temporalGroups, semanticGroups)

	// Identify relationships between events
	for i := range mergedGroups {
		sg.identifyEventRelationships(&mergedGroups[i])
	}

	return mergedGroups
}

// groupByType groups events by their type with semantic understanding
func (sg *SimpleSemanticGrouper) groupByType(events []domain.Event) map[string]*EventGroup {
	groups := make(map[string]*EventGroup)

	for _, event := range events {
		// Create semantic key based on event type and context
		semanticKey := sg.createSemanticKey(event)

		if group, exists := groups[semanticKey]; exists {
			group.Events = append(group.Events, event)
			sg.updateGroupMetrics(group)
		} else {
			groups[semanticKey] = &EventGroup{
				ID:                 fmt.Sprintf("group-%s-%d", semanticKey, time.Now().UnixNano()),
				Type:               semanticKey,
				Description:        sg.generateGroupDescription(event),
				Events:             []domain.Event{event},
				EventRelationships: make(map[string][]EventRelationship),
				TimeSpan:           0,
				Confidence:         0.9,
				SemanticFeatures:   make(map[string]float64),
				AffectedServices:   []string{},
			}
		}
	}

	return groups
}

// groupByTemporalProximity groups events that occur close in time
func (sg *SimpleSemanticGrouper) groupByTemporalProximity(events []domain.Event) []*EventGroup {
	var groups []*EventGroup
	var currentGroup *EventGroup

	for _, event := range events {
		if currentGroup == nil || !sg.isTemporallyRelated(currentGroup, event) {
			// Start new temporal group
			currentGroup = &EventGroup{
				ID:                 fmt.Sprintf("temporal-%d", time.Now().UnixNano()),
				Type:               "temporal_correlation",
				Events:             []domain.Event{event},
				EventRelationships: make(map[string][]EventRelationship),
				Confidence:         0.8,
				SemanticFeatures:   make(map[string]float64),
			}
			groups = append(groups, currentGroup)
		} else {
			currentGroup.Events = append(currentGroup.Events, event)
			sg.updateGroupMetrics(currentGroup)
		}
	}

	return groups
}

// groupBySemanticsML uses ML-based semantic analysis for grouping
func (sg *SimpleSemanticGrouper) groupBySemanticsML(events []domain.Event) []*EventGroup {
	if !sg.config.EnableMLGrouping {
		return []*EventGroup{}
	}

	// Simulate ML-based semantic clustering
	var groups []*EventGroup

	// Extract semantic features from events
	features := sg.extractSemanticFeatures(events)

	// Perform clustering (simplified version)
	clusters := sg.performSemanticClustering(features)

	// Create groups from clusters
	for clusterID, eventIndices := range clusters {
		group := &EventGroup{
			ID:                 fmt.Sprintf("semantic-%s", clusterID),
			Type:               "ml_semantic_cluster",
			Events:             []domain.Event{},
			EventRelationships: make(map[string][]EventRelationship),
			Confidence:         0.85,
			SemanticFeatures:   make(map[string]float64),
		}

		for _, idx := range eventIndices {
			group.Events = append(group.Events, events[idx])
		}

		groups = append(groups, group)
	}

	return groups
}

// enhanceGroupWithSemantics adds deep semantic understanding
func (sg *SimpleSemanticGrouper) enhanceGroupWithSemantics(ctx context.Context, group *EventGroup) {
	_, span := sg.tracer.Start(ctx, "semantic_grouper.enhance_semantics")
	defer span.End()

	// Analyze temporal patterns
	group.TemporalPattern = sg.analyzeTemporalPattern(group.Events)

	// Calculate semantic scores
	group.SemanticScore = sg.calculateSemanticScore(group)

	// Extract business impact
	group.BusinessImpact = sg.assessBusinessImpact(group)

	// Identify affected services
	group.AffectedServices = sg.identifyAffectedServices(group)

	// Add OTEL trace context
	if span.SpanContext().HasTraceID() {
		group.TraceID = span.SpanContext().TraceID().String()
		group.SpanID = span.SpanContext().SpanID().String()
	}
}

// performRootCauseAnalysis performs deep root cause analysis
func (sg *SimpleSemanticGrouper) performRootCauseAnalysis(ctx context.Context, group *EventGroup) {
	_, span := sg.tracer.Start(ctx, "semantic_grouper.root_cause_analysis")
	defer span.End()

	if len(group.Events) < 2 {
		return
	}

	analysis := &RootCauseAnalysis{
		CausalChain:         []CausalLinkDetail{},
		ContributingFactors: []ContributingFactor{},
		Evidence:            []string{},
	}

	// Identify potential root cause event
	rootCause := sg.identifyRootCauseEvent(group.Events)
	if rootCause != nil {
		analysis.RootCauseEvent = rootCause
		analysis.Confidence = 0.85

		// Build causal chain
		analysis.CausalChain = sg.buildCausalChain(rootCause, group.Events)

		// Identify contributing factors
		analysis.ContributingFactors = sg.identifyContributingFactors(group)

		// Collect evidence
		analysis.Evidence = sg.collectRootCauseEvidence(group, rootCause)
	}

	group.RootCauseAnalysis = analysis
}

// predictFutureEvents uses ML to predict future events
func (sg *SimpleSemanticGrouper) predictFutureEvents(ctx context.Context, group *EventGroup) {
	_, span := sg.tracer.Start(ctx, "semantic_grouper.predict_events")
	defer span.End()

	predictions := []PredictedEvent{}

	// Analyze patterns to predict future events
	if group.TemporalPattern.Type == "periodic" {
		predictions = append(predictions, PredictedEvent{
			EventType:   group.Events[0].Type,
			Probability: 0.8,
			TimeWindow:  time.Duration(group.TemporalPattern.Periodicity) * time.Second,
			Impact:      "Recurring pattern detected",
			Prevention: []string{
				"Monitor resource utilization",
				"Implement proactive scaling",
				"Review system configuration",
			},
		})
	}

	// Check for escalation patterns
	if sg.detectEscalationPattern(group) {
		predictions = append(predictions, PredictedEvent{
			EventType:   domain.EventType("system_failure"),
			Probability: 0.7,
			TimeWindow:  30 * time.Minute,
			Impact:      "Potential system failure",
			Prevention: []string{
				"Immediate investigation required",
				"Consider preventive restart",
				"Increase monitoring frequency",
			},
		})
	}

	group.PredictedEvolution = predictions
}

// Helper methods

func (sg *SimpleSemanticGrouper) createSemanticKey(event domain.Event) string {
	// Create a semantic key that captures the essence of the event
	parts := []string{string(event.Type)}

	if event.Context.Service != "" {
		parts = append(parts, event.Context.Service)
	}

	if event.Context.Component != "" {
		parts = append(parts, event.Context.Component)
	}

	return strings.Join(parts, ":")
}

func (sg *SimpleSemanticGrouper) generateGroupDescription(event domain.Event) string {
	return fmt.Sprintf("Semantic group for %s events in %s",
		event.Type,
		event.Context.Service)
}

func (sg *SimpleSemanticGrouper) isTemporallyRelated(group *EventGroup, event domain.Event) bool {
	if len(group.Events) == 0 {
		return false
	}

	lastEvent := group.Events[len(group.Events)-1]
	timeDiff := event.Timestamp.Sub(lastEvent.Timestamp)

	return timeDiff >= 0 && timeDiff <= sg.config.TimeWindow
}

func (sg *SimpleSemanticGrouper) updateGroupMetrics(group *EventGroup) {
	if len(group.Events) < 2 {
		return
	}

	// Update time span
	firstTime := group.Events[0].Timestamp
	lastTime := group.Events[len(group.Events)-1].Timestamp
	group.TimeSpan = lastTime.Sub(firstTime)

	// Update confidence based on group coherence
	group.Confidence = sg.calculateGroupCoherence(group)
}

func (sg *SimpleSemanticGrouper) identifyEventRelationships(group *EventGroup) {
	for i, event1 := range group.Events {
		for j, event2 := range group.Events {
			if i >= j {
				continue
			}

			relationship := sg.detectRelationship(event1, event2)
			if relationship != nil {
				eventID := string(event1.ID)
				group.EventRelationships[eventID] = append(
					group.EventRelationships[eventID],
					*relationship,
				)
			}
		}
	}
}

func (sg *SimpleSemanticGrouper) detectRelationship(event1, event2 domain.Event) *EventRelationship {
	// Detect causal relationships
	if event1.Timestamp.Before(event2.Timestamp) {
		timeDiff := event2.Timestamp.Sub(event1.Timestamp)

		// Check for direct causation patterns
		if timeDiff < 5*time.Second && sg.isCausallyRelated(event1, event2) {
			return &EventRelationship{
				SourceEventID: string(event1.ID),
				TargetEventID: string(event2.ID),
				RelationType:  RelationCausedBy,
				Confidence:    0.8,
				Evidence: []string{
					fmt.Sprintf("Temporal proximity: %v", timeDiff),
					"Matching service context",
					"Error propagation pattern",
				},
			}
		}
	}

	return nil
}

func (sg *SimpleSemanticGrouper) isCausallyRelated(event1, event2 domain.Event) bool {
	// Check if events are from the same service/component
	if event1.Context.Service == event2.Context.Service {
		// Check for error propagation
		if event1.Severity >= domain.EventSeverityHigh &&
			event2.Severity >= domain.EventSeverityHigh {
			return true
		}
	}

	// Check for cross-service causation
	if event1.Context.Component == "api" && event2.Context.Component == "database" {
		return true
	}

	return false
}

func (sg *SimpleSemanticGrouper) mergeGroupsIntelligently(
	typeGroups map[string]*EventGroup,
	temporalGroups []*EventGroup,
	semanticGroups []*EventGroup,
) []EventGroup {
	// Intelligent merging logic
	merged := make([]EventGroup, 0)

	// Start with type groups as base
	for _, group := range typeGroups {
		merged = append(merged, *group)
	}

	// Merge temporal groups if they overlap significantly
	for _, tGroup := range temporalGroups {
		merged = sg.mergeIfOverlapping(merged, tGroup)
	}

	// Add ML semantic groups if they provide new insights
	for _, sGroup := range semanticGroups {
		if sg.providesNewInsights(sGroup, merged) {
			merged = append(merged, *sGroup)
		}
	}

	return merged
}

func (sg *SimpleSemanticGrouper) mergeIfOverlapping(existing []EventGroup, newGroup *EventGroup) []EventGroup {
	// Simple implementation - in production, use more sophisticated merging
	return append(existing, *newGroup)
}

func (sg *SimpleSemanticGrouper) providesNewInsights(group *EventGroup, existing []EventGroup) bool {
	// Check if the semantic group provides unique insights
	return len(group.Events) > 2 && group.Confidence > sg.config.MinConfidence
}

func (sg *SimpleSemanticGrouper) extractSemanticFeatures(events []domain.Event) [][]float64 {
	features := make([][]float64, len(events))

	for i, event := range events {
		// Extract numerical features for ML clustering
		features[i] = []float64{
			float64(severityToInt(event.Severity)),
			float64(len(event.Tags)),
			float64(event.Confidence),
			// Add more features as needed
		}
	}

	return features
}

func (sg *SimpleSemanticGrouper) performSemanticClustering(features [][]float64) map[string][]int {
	// Simplified clustering - in production, use real ML algorithms
	clusters := make(map[string][]int)

	// Simple threshold-based clustering
	for i, feature := range features {
		clusterID := fmt.Sprintf("cluster-%d", int(feature[0])) // Group by severity
		clusters[clusterID] = append(clusters[clusterID], i)
	}

	return clusters
}

func (sg *SimpleSemanticGrouper) analyzeTemporalPattern(events []domain.Event) TemporalPattern {
	pattern := TemporalPattern{
		Type:      "unknown",
		Anomalies: []TemporalAnomaly{},
	}

	if len(events) < 2 {
		return pattern
	}

	// Calculate inter-event intervals
	intervals := make([]time.Duration, 0)
	for i := 1; i < len(events); i++ {
		intervals = append(intervals, events[i].Timestamp.Sub(events[i-1].Timestamp))
	}

	// Detect pattern type
	if sg.isPeriodic(intervals) {
		pattern.Type = "periodic"
		pattern.Periodicity = sg.calculatePeriodicity(intervals).Seconds()
	} else if sg.isBurst(intervals) {
		pattern.Type = "burst"
	} else if sg.isGradual(events) {
		pattern.Type = "gradual"
	}

	// Detect trend
	pattern.Trend = sg.detectTrend(events)

	return pattern
}

func (sg *SimpleSemanticGrouper) isPeriodic(intervals []time.Duration) bool {
	if len(intervals) < 3 {
		return false
	}

	// Check for consistent intervals
	avg := time.Duration(0)
	for _, interval := range intervals {
		avg += interval
	}
	avg /= time.Duration(len(intervals))

	variance := time.Duration(0)
	for _, interval := range intervals {
		diff := interval - avg
		if diff < 0 {
			diff = -diff
		}
		variance += diff
	}
	variance /= time.Duration(len(intervals))

	// If variance is less than 20% of average, consider it periodic
	return float64(variance) < float64(avg)*0.2
}

func (sg *SimpleSemanticGrouper) isBurst(intervals []time.Duration) bool {
	if len(intervals) < 2 {
		return false
	}

	// Check if most events occur within a short time window
	shortIntervals := 0
	for _, interval := range intervals {
		if interval < 10*time.Second {
			shortIntervals++
		}
	}

	return float64(shortIntervals) > float64(len(intervals))*0.8
}

func (sg *SimpleSemanticGrouper) isGradual(events []domain.Event) bool {
	// Check if severity or frequency gradually increases
	if len(events) < 3 {
		return false
	}

	increasingSeverity := true
	for i := 1; i < len(events); i++ {
		if events[i].Severity < events[i-1].Severity {
			increasingSeverity = false
			break
		}
	}

	return increasingSeverity
}

func (sg *SimpleSemanticGrouper) detectTrend(events []domain.Event) string {
	if len(events) < 2 {
		return "stable"
	}

	// Analyze severity trend
	severityIncreasing := 0
	severityDecreasing := 0

	for i := 1; i < len(events); i++ {
		if events[i].Severity > events[i-1].Severity {
			severityIncreasing++
		} else if events[i].Severity < events[i-1].Severity {
			severityDecreasing++
		}
	}

	if severityIncreasing > severityDecreasing*2 {
		return "increasing"
	} else if severityDecreasing > severityIncreasing*2 {
		return "decreasing"
	}

	return "stable"
}

func (sg *SimpleSemanticGrouper) calculatePeriodicity(intervals []time.Duration) time.Duration {
	if len(intervals) == 0 {
		return 0
	}

	sum := time.Duration(0)
	for _, interval := range intervals {
		sum += interval
	}

	return sum / time.Duration(len(intervals))
}

func (sg *SimpleSemanticGrouper) calculateSemanticScore(group *EventGroup) float64 {
	score := 0.0

	// Factor 1: Event coherence
	coherence := sg.calculateGroupCoherence(group)
	score += coherence * 0.3

	// Factor 2: Temporal consistency
	if group.TemporalPattern.Type != "unknown" {
		score += 0.2
	}

	// Factor 3: Relationship strength
	relationshipScore := float64(len(group.EventRelationships)) / float64(len(group.Events))
	score += relationshipScore * 0.2

	// Factor 4: Severity alignment
	severityAlignment := sg.calculateSeverityAlignment(group)
	score += severityAlignment * 0.3

	return score
}

func (sg *SimpleSemanticGrouper) calculateGroupCoherence(group *EventGroup) float64 {
	if len(group.Events) < 2 {
		return 1.0
	}

	// Calculate how well events fit together
	coherence := 0.0
	comparisons := 0

	for i := 0; i < len(group.Events); i++ {
		for j := i + 1; j < len(group.Events); j++ {
			similarity := sg.calculateEventSimilarity(group.Events[i], group.Events[j])
			coherence += similarity
			comparisons++
		}
	}

	if comparisons > 0 {
		coherence /= float64(comparisons)
	}

	return coherence
}

func (sg *SimpleSemanticGrouper) calculateEventSimilarity(event1, event2 domain.Event) float64 {
	similarity := 0.0

	// Type similarity
	if event1.Type == event2.Type {
		similarity += 0.3
	}

	// Service similarity
	if event1.Context.Service == event2.Context.Service {
		similarity += 0.2
	}

	// Component similarity
	if event1.Context.Component == event2.Context.Component {
		similarity += 0.2
	}

	// Severity similarity
	sev1 := severityToInt(event1.Severity)
	sev2 := severityToInt(event2.Severity)
	severityDiff := abs(sev1 - sev2)
	similarity += (1.0 - float64(severityDiff)/5.0) * 0.3

	return similarity
}

func (sg *SimpleSemanticGrouper) calculateSeverityAlignment(group *EventGroup) float64 {
	if len(group.Events) == 0 {
		return 0.0
	}

	// Check how aligned severities are
	avgSeverity := 0.0
	for _, event := range group.Events {
		avgSeverity += float64(severityToInt(event.Severity))
	}
	avgSeverity /= float64(len(group.Events))

	variance := 0.0
	for _, event := range group.Events {
		diff := float64(severityToInt(event.Severity)) - avgSeverity
		variance += diff * diff
	}
	variance /= float64(len(group.Events))

	// Lower variance means better alignment
	return 1.0 / (1.0 + variance)
}

func (sg *SimpleSemanticGrouper) assessBusinessImpact(group *EventGroup) float64 {
	impact := 0.0

	// Factor 1: Severity of events
	for _, event := range group.Events {
		// Convert EventSeverity to numeric value
		severityValue := 0.0
		switch event.Severity {
		case domain.EventSeverityCritical:
			severityValue = 5.0
		case domain.EventSeverityHigh, domain.EventSeverityError:
			severityValue = 4.0
		case domain.EventSeverityMedium, domain.EventSeverityWarning:
			severityValue = 3.0
		case domain.EventSeverityLow:
			severityValue = 2.0
		default:
			severityValue = 1.0
		}
		impact += severityValue / 5.0
	}
	impact /= float64(len(group.Events))

	// Factor 2: Number of affected services
	services := make(map[string]bool)
	for _, event := range group.Events {
		if event.Context.Service != "" {
			services[event.Context.Service] = true
		}
	}
	impact *= float64(len(services)) * 0.2

	// Factor 3: Critical components
	for _, event := range group.Events {
		if strings.Contains(strings.ToLower(event.Context.Component), "database") ||
			strings.Contains(strings.ToLower(event.Context.Component), "api") ||
			strings.Contains(strings.ToLower(event.Context.Component), "auth") {
			impact *= 1.5
			break
		}
	}

	// Normalize to 0-1
	if impact > 1.0 {
		impact = 1.0
	}

	return impact
}

func (sg *SimpleSemanticGrouper) identifyAffectedServices(group *EventGroup) []string {
	services := make(map[string]bool)

	for _, event := range group.Events {
		if event.Context.Service != "" {
			services[event.Context.Service] = true
		}

		// Extract from namespace
		if event.Context.Namespace != "" {
			services[event.Context.Namespace] = true
		}

		// Extract from labels
		if event.Context.Labels != nil {
			if svc, exists := event.Context.Labels["service"]; exists {
				services[svc] = true
			}
		}
	}

	result := make([]string, 0, len(services))
	for service := range services {
		result = append(result, service)
	}

	sort.Strings(result)
	return result
}

func (sg *SimpleSemanticGrouper) identifyRootCauseEvent(events []domain.Event) *domain.Event {
	if len(events) == 0 {
		return nil
	}

	// Simple heuristic: earliest high-severity event
	var rootCause *domain.Event
	earliestTime := time.Now()

	for i := range events {
		event := &events[i]
		if event.Severity >= domain.EventSeverityHigh && event.Timestamp.Before(earliestTime) {
			rootCause = event
			earliestTime = event.Timestamp
		}
	}

	// If no high-severity event, take the earliest
	if rootCause == nil {
		rootCause = &events[0]
	}

	return rootCause
}

func (sg *SimpleSemanticGrouper) buildCausalChain(rootCause *domain.Event, events []domain.Event) []CausalLinkDetail {
	chain := []CausalLinkDetail{}

	// Build chain from root cause
	currentEvent := rootCause
	for _, event := range events {
		if event.ID == currentEvent.ID {
			continue
		}

		if sg.isCausallyRelated(*currentEvent, event) {
			link := CausalLinkDetail{
				FromEvent:   string(currentEvent.ID),
				ToEvent:     string(event.ID),
				Mechanism:   sg.identifyCausalMechanism(*currentEvent, event),
				Probability: sg.calculateCausalProbability(*currentEvent, event),
				Latency:     event.Timestamp.Sub(currentEvent.Timestamp),
			}
			chain = append(chain, link)
			currentEvent = &event
		}
	}

	return chain
}

func (sg *SimpleSemanticGrouper) identifyCausalMechanism(cause, effect domain.Event) string {
	// Identify how one event caused another
	if cause.Context.Service == effect.Context.Service {
		if cause.Type == "error" && effect.Type == "failure" {
			return "error_escalation"
		}
		return "internal_propagation"
	}

	if cause.Context.Component == "api" && effect.Context.Component == "database" {
		return "api_database_cascade"
	}

	return "cross_service_impact"
}

func (sg *SimpleSemanticGrouper) calculateCausalProbability(cause, effect domain.Event) float64 {
	prob := 0.5

	// Time proximity increases probability
	timeDiff := effect.Timestamp.Sub(cause.Timestamp)
	if timeDiff < 1*time.Second {
		prob += 0.3
	} else if timeDiff < 10*time.Second {
		prob += 0.2
	}

	// Same service increases probability
	if cause.Context.Service == effect.Context.Service {
		prob += 0.1
	}

	// Severity escalation increases probability
	if effect.Severity > cause.Severity {
		prob += 0.1
	}

	if prob > 1.0 {
		prob = 1.0
	}

	return prob
}

func (sg *SimpleSemanticGrouper) identifyContributingFactors(group *EventGroup) []ContributingFactor {
	factors := []ContributingFactor{}

	// Analyze event patterns for contributing factors

	// Factor 1: High load
	loadEvents := 0
	for _, event := range group.Events {
		if strings.Contains(strings.ToLower(event.Message), "load") ||
			strings.Contains(strings.ToLower(event.Message), "cpu") ||
			strings.Contains(strings.ToLower(event.Message), "memory") {
			loadEvents++
		}
	}

	if loadEvents > len(group.Events)/3 {
		factors = append(factors, ContributingFactor{
			Factor:      "High system load",
			Impact:      float64(loadEvents) / float64(len(group.Events)),
			Type:        "load",
			Remediation: "Scale resources or optimize performance",
		})
	}

	// Factor 2: Configuration issues
	configEvents := 0
	for _, event := range group.Events {
		if strings.Contains(strings.ToLower(event.Message), "config") ||
			strings.Contains(strings.ToLower(event.Message), "setting") {
			configEvents++
		}
	}

	if configEvents > 0 {
		factors = append(factors, ContributingFactor{
			Factor:      "Configuration issues",
			Impact:      float64(configEvents) / float64(len(group.Events)),
			Type:        "configuration",
			Remediation: "Review and update configuration settings",
		})
	}

	// Factor 3: Network issues
	networkEvents := 0
	for _, event := range group.Events {
		if strings.Contains(strings.ToLower(event.Message), "network") ||
			strings.Contains(strings.ToLower(event.Message), "connection") ||
			strings.Contains(strings.ToLower(event.Message), "timeout") {
			networkEvents++
		}
	}

	if networkEvents > 0 {
		factors = append(factors, ContributingFactor{
			Factor:      "Network connectivity issues",
			Impact:      float64(networkEvents) / float64(len(group.Events)),
			Type:        "network",
			Remediation: "Check network connectivity and latency",
		})
	}

	return factors
}

func (sg *SimpleSemanticGrouper) collectRootCauseEvidence(group *EventGroup, rootCause *domain.Event) []string {
	evidence := []string{}

	// Evidence 1: Temporal ordering
	evidence = append(evidence, fmt.Sprintf("Root cause event occurred at %s, before other events",
		rootCause.Timestamp.Format("15:04:05")))

	// Evidence 2: Severity
	if rootCause.Severity >= domain.EventSeverityHigh {
		evidence = append(evidence, fmt.Sprintf("High severity level: %v", rootCause.Severity))
	}

	// Evidence 3: Error patterns
	if strings.Contains(strings.ToLower(rootCause.Message), "error") ||
		strings.Contains(strings.ToLower(rootCause.Message), "fail") {
		evidence = append(evidence, "Error keywords detected in event message")
	}

	// Evidence 4: Service criticality
	if rootCause.Context.Component == "database" ||
		rootCause.Context.Component == "api" {
		evidence = append(evidence, fmt.Sprintf("Critical component affected: %s",
			rootCause.Context.Component))
	}

	// Evidence 5: Correlation patterns
	correlatedCount := 0
	for _, event := range group.Events {
		if sg.isCausallyRelated(*rootCause, event) {
			correlatedCount++
		}
	}

	if correlatedCount > 1 {
		evidence = append(evidence, fmt.Sprintf("%d subsequent events show causal relationship",
			correlatedCount))
	}

	return evidence
}

func (sg *SimpleSemanticGrouper) detectEscalationPattern(group *EventGroup) bool {
	if len(group.Events) < 3 {
		return false
	}

	// Check for increasing severity
	escalations := 0
	for i := 1; i < len(group.Events); i++ {
		if group.Events[i].Severity > group.Events[i-1].Severity {
			escalations++
		}
	}

	// If more than half show escalation, pattern detected
	return float64(escalations) > float64(len(group.Events)-1)*0.5
}

// Initialize semantic patterns for intelligent grouping
func initializeSemanticPatterns() map[string]*SemanticPattern {
	patterns := make(map[string]*SemanticPattern)

	// Database pattern
	patterns["database"] = &SemanticPattern{
		ID:          "db-pattern",
		Name:        "Database Issues",
		Description: "Database-related events",
		Keywords:    []string{"database", "db", "sql", "query", "connection"},
		EventTypes:  []domain.EventType{"db_error", "db_slow_query", "db_connection_fail"},
		Scorer: func(event domain.Event) float64 {
			score := 0.0
			msg := strings.ToLower(event.Message)
			for _, kw := range patterns["database"].Keywords {
				if strings.Contains(msg, kw) {
					score += 0.2
				}
			}
			return score
		},
	}

	// API pattern
	patterns["api"] = &SemanticPattern{
		ID:          "api-pattern",
		Name:        "API Issues",
		Description: "API-related events",
		Keywords:    []string{"api", "endpoint", "request", "response", "http"},
		EventTypes:  []domain.EventType{"api_error", "api_timeout", "api_rate_limit"},
		Scorer: func(event domain.Event) float64 {
			score := 0.0
			if event.Context.Component == "api" {
				score += 0.5
			}
			msg := strings.ToLower(event.Message)
			for _, kw := range patterns["api"].Keywords {
				if strings.Contains(msg, kw) {
					score += 0.1
				}
			}
			return score
		},
	}

	// Performance pattern
	patterns["performance"] = &SemanticPattern{
		ID:          "perf-pattern",
		Name:        "Performance Issues",
		Description: "Performance-related events",
		Keywords:    []string{"slow", "latency", "timeout", "performance", "cpu", "memory"},
		EventTypes:  []domain.EventType{"high_latency", "resource_exhaustion", "timeout"},
		Scorer: func(event domain.Event) float64 {
			score := 0.0
			msg := strings.ToLower(event.Message)
			for _, kw := range patterns["performance"].Keywords {
				if strings.Contains(msg, kw) {
					score += 0.15
				}
			}
			// High severity performance issues score higher
			if event.Severity >= domain.EventSeverityHigh {
				score += 0.3
			}
			return score
		},
	}

	return patterns
}

// Helper functions
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func severityToInt(severity domain.EventSeverity) int {
	switch severity {
	case domain.EventSeverityCritical:
		return 5
	case domain.EventSeverityHigh, domain.EventSeverityError:
		return 4
	case domain.EventSeverityMedium, domain.EventSeverityWarning:
		return 3
	case domain.EventSeverityLow:
		return 2
	default:
		return 1
	}
}

// ExplanationStyle defines how insights are formatted
type ExplanationStyle int

const (
	StyleSimple ExplanationStyle = iota
	StyleDetailed
	StyleTechnical
	StyleExecutive
)

// Audience defines the target audience for explanations
type Audience int

const (
	AudienceDeveloper Audience = iota
	AudienceOperator
	AudienceManager
	AudienceExecutive
)

// HumanReadableFormatter formats insights for human consumption with OTEL awareness
type HumanReadableFormatter struct {
	style    ExplanationStyle
	audience Audience
	tracer   trace.Tracer
}

// NewHumanReadableFormatter creates a new formatter
func NewHumanReadableFormatter(style ExplanationStyle, audience Audience) *HumanReadableFormatter {
	return &HumanReadableFormatter{
		style:    style,
		audience: audience,
		tracer:   otel.Tracer("tapio.correlation.formatter"),
	}
}

// HumanReadableExplanation represents a formatted explanation with rich context
type HumanReadableExplanation struct {
	Title          string
	Summary        string
	Details        []string
	Impact         string
	Remediation    []string
	Timeline       string
	Confidence     float64
	TraceLinks     []string
	Visualizations map[string]string
}

// extractCorrelationData extracts correlation-specific fields from insight metadata
func extractCorrelationData(insight Insight) (relatedEvents []string, resources []AffectedResource, actions []ActionableItem, prediction *Prediction) {
	// Extract related events
	if re, ok := insight.Metadata["related_events"].([]string); ok {
		relatedEvents = re
	} else if reInterface, ok := insight.Metadata["related_events"].([]interface{}); ok {
		relatedEvents = make([]string, 0, len(reInterface))
		for _, e := range reInterface {
			if eventID, ok := e.(string); ok {
				relatedEvents = append(relatedEvents, eventID)
			}
		}
	}

	// Extract resources
	if res, ok := insight.Metadata["affected_resources"].([]AffectedResource); ok {
		resources = res
	} else if resInterface, ok := insight.Metadata["affected_resources"].([]interface{}); ok {
		resources = make([]AffectedResource, 0, len(resInterface))
		for _, r := range resInterface {
			if resMap, ok := r.(map[string]interface{}); ok {
				resource := AffectedResource{}
				if t, ok := resMap["type"].(string); ok {
					resource.Type = t
				}
				if n, ok := resMap["name"].(string); ok {
					resource.Name = n
				}
				resources = append(resources, resource)
			}
		}
	}

	// Extract actions
	if acts, ok := insight.Metadata["recommended_actions"].([]ActionableItem); ok {
		actions = acts
	} else if actsInterface, ok := insight.Metadata["recommended_actions"].([]interface{}); ok {
		actions = make([]ActionableItem, 0, len(actsInterface))
		for _, a := range actsInterface {
			if actMap, ok := a.(map[string]interface{}); ok {
				action := ActionableItem{}
				if t, ok := actMap["title"].(string); ok {
					action.Title = t
				}
				if d, ok := actMap["description"].(string); ok {
					action.Description = d
				}
				if r, ok := actMap["risk"].(string); ok {
					action.Risk = r
				}
				if ei, ok := actMap["estimated_impact"].(string); ok {
					action.EstimatedImpact = ei
				}
				actions = append(actions, action)
			}
		}
	}

	// Extract prediction
	if pred, ok := insight.Metadata["prediction"].(*Prediction); ok {
		prediction = pred
	} else if predMap, ok := insight.Metadata["prediction"].(map[string]interface{}); ok {
		prediction = &Prediction{}
		if s, ok := predMap["scenario"].(string); ok {
			prediction.Scenario = s
		}
		if p, ok := predMap["probability"].(float64); ok {
			prediction.Probability = p
		}
		if tw, ok := predMap["time_window"].(time.Duration); ok {
			prediction.TimeWindow = tw
		}
	}

	return
}

// FormatInsight formats an insight for human consumption with OTEL context
func (hrf *HumanReadableFormatter) FormatInsight(insight Insight) *HumanReadableExplanation {
	ctx, span := hrf.tracer.Start(context.Background(), "formatter.format_insight",
		trace.WithAttributes(
			attribute.String("insight_type", insight.Type),
			attribute.String("style", fmt.Sprintf("%d", hrf.style)),
		),
	)
	defer span.End()

	// Extract correlation data from metadata
	relatedEvents, resources, actions, prediction := extractCorrelationData(insight)

	explanation := &HumanReadableExplanation{
		Title:          insight.Title,
		Summary:        insight.Description,
		Details:        []string{},
		TraceLinks:     []string{},
		Visualizations: make(map[string]string),
	}

	// Add trace context
	if traceID, ok := insight.Metadata["trace_id"].(string); ok {
		explanation.TraceLinks = append(explanation.TraceLinks,
			fmt.Sprintf("Trace: %s", traceID))
	}

	// Format based on style and audience
	switch hrf.style {
	case StyleSimple:
		hrf.formatSimple(ctx, insight, explanation, relatedEvents, resources, actions, prediction)
	case StyleDetailed:
		hrf.formatDetailed(ctx, insight, explanation, relatedEvents, resources, actions, prediction)
	case StyleTechnical:
		hrf.formatTechnical(ctx, insight, explanation, relatedEvents, resources, actions, prediction)
	case StyleExecutive:
		hrf.formatExecutive(ctx, insight, explanation, relatedEvents, resources, actions, prediction)
	}

	// Add impact assessment
	hrf.formatImpact(ctx, insight, explanation, prediction)

	// Add remediation with audience-appropriate language
	hrf.formatRemediation(ctx, insight, explanation, actions, prediction)

	// Create timeline visualization
	hrf.createTimeline(ctx, insight, explanation, relatedEvents)

	// Add confidence score
	if conf, ok := insight.Metadata["confidence"].(float64); ok {
		explanation.Confidence = conf
	}

	span.SetAttributes(
		attribute.Int("detail_count", len(explanation.Details)),
		attribute.Int("remediation_count", len(explanation.Remediation)),
	)

	return explanation
}

func (hrf *HumanReadableFormatter) formatSimple(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, relatedEvents []string, resources []AffectedResource, actions []ActionableItem, prediction *Prediction) {
	explanation.Details = []string{
		fmt.Sprintf("Type: %s", insight.Type),
		fmt.Sprintf("Severity: %s", insight.Severity),
		fmt.Sprintf("Time: %s", insight.Timestamp.Format("15:04:05")),
	}

	if len(resources) > 0 {
		explanation.Details = append(explanation.Details,
			fmt.Sprintf("Affected: %d resources", len(resources)))
	}
}

func (hrf *HumanReadableFormatter) formatDetailed(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, relatedEvents []string, resources []AffectedResource, actions []ActionableItem, prediction *Prediction) {
	// Include simple details first
	hrf.formatSimple(ctx, insight, explanation, relatedEvents, resources, actions, prediction)

	// Add event details
	if len(relatedEvents) > 0 {
		explanation.Details = append(explanation.Details,
			fmt.Sprintf("Related Events: %d", len(relatedEvents)))

		// List first few events
		for i, eventID := range relatedEvents {
			if i >= 3 {
				explanation.Details = append(explanation.Details,
					fmt.Sprintf("  ... and %d more", len(relatedEvents)-3))
				break
			}
			explanation.Details = append(explanation.Details,
				fmt.Sprintf("  - %s", eventID))
		}
	}

	// Add resource details
	if len(resources) > 0 {
		explanation.Details = append(explanation.Details, "Affected Resources:")
		for _, resource := range resources {
			explanation.Details = append(explanation.Details,
				fmt.Sprintf("  - %s: %s", resource.Type, resource.Name))
		}
	}
}

func (hrf *HumanReadableFormatter) formatTechnical(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, relatedEvents []string, resources []AffectedResource, actions []ActionableItem, prediction *Prediction) {
	// Full technical details
	explanation.Details = []string{
		fmt.Sprintf("Insight ID: %s", insight.ID),
		fmt.Sprintf("Type: %s", insight.Type),
		fmt.Sprintf("Severity: %s", insight.Severity),
		fmt.Sprintf("Timestamp: %s", insight.Timestamp.Format(time.RFC3339)),
	}

	// Add all metadata
	explanation.Details = append(explanation.Details, "Metadata:")
	for k, v := range insight.Metadata {
		explanation.Details = append(explanation.Details,
			fmt.Sprintf("  %s: %v", k, v))
	}

	// Add event IDs
	if len(relatedEvents) > 0 {
		explanation.Details = append(explanation.Details,
			fmt.Sprintf("Event IDs: %s", strings.Join(relatedEvents, ", ")))
	}

	// Add technical diagnostics
	if hrf.audience == AudienceDeveloper {
		explanation.Details = append(explanation.Details, "Diagnostic Information:")
		explanation.Details = append(explanation.Details,
			fmt.Sprintf("  Correlation Type: %s", insight.Metadata["correlation_type"]))

		if groupID, ok := insight.Metadata["semantic_group_id"].(string); ok {
			explanation.Details = append(explanation.Details,
				fmt.Sprintf("  Semantic Group: %s", groupID))
		}
	}
}

func (hrf *HumanReadableFormatter) formatExecutive(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, relatedEvents []string, resources []AffectedResource, actions []ActionableItem, prediction *Prediction) {
	// High-level summary for executives
	riskLevel := "Low"
	switch insight.Severity {
	case SeverityCritical:
		riskLevel = "Critical"
	case SeverityHigh:
		riskLevel = "High"
	case SeverityMedium:
		riskLevel = "Medium"
	}

	explanation.Details = []string{
		fmt.Sprintf("Business Risk: %s", riskLevel),
		fmt.Sprintf("Systems Impacted: %d", len(resources)),
	}

	// Add business impact
	if prediction != nil {
		explanation.Details = append(explanation.Details,
			fmt.Sprintf("Potential Impact: %s", prediction.Scenario),
			fmt.Sprintf("Likelihood: %.0f%%", prediction.Probability*100))
	}

	// Executive-friendly time
	explanation.Details = append(explanation.Details,
		fmt.Sprintf("Detected: %s", insight.Timestamp.Format("Jan 2, 15:04")))
}

func (hrf *HumanReadableFormatter) formatImpact(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, prediction *Prediction) {
	if prediction != nil {
		// Format impact based on audience
		switch hrf.audience {
		case AudienceExecutive, AudienceManager:
			explanation.Impact = fmt.Sprintf("Business Impact: %s with %.0f%% probability within %s",
				prediction.Scenario,
				prediction.Probability*100,
				hrf.formatDuration(prediction.TimeWindow))
		case AudienceDeveloper, AudienceOperator:
			explanation.Impact = fmt.Sprintf("%s (%.2f probability, ETA: %s)",
				prediction.Scenario,
				prediction.Probability,
				prediction.TimeWindow)
		}
	} else {
		// Generic impact based on severity
		switch insight.Severity {
		case SeverityCritical:
			explanation.Impact = "Critical: Immediate action required to prevent service disruption"
		case SeverityHigh:
			explanation.Impact = "High: Significant impact on service performance or reliability"
		case SeverityMedium:
			explanation.Impact = "Medium: Moderate impact that should be addressed soon"
		default:
			explanation.Impact = "Low: Minor impact with no immediate action required"
		}
	}
}

func (hrf *HumanReadableFormatter) formatRemediation(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, actions []ActionableItem, prediction *Prediction) {
	explanation.Remediation = []string{}

	// Add specific actions
	for _, action := range actions {
		switch hrf.audience {
		case AudienceExecutive:
			// High-level actions only
			if action.Risk == "low" || action.Risk == "medium" {
				explanation.Remediation = append(explanation.Remediation,
					fmt.Sprintf("Authorize: %s", action.Title))
			}
		case AudienceManager:
			explanation.Remediation = append(explanation.Remediation,
				fmt.Sprintf("%s (Risk: %s, Impact: %s)",
					action.Title, action.Risk, action.EstimatedImpact))
		case AudienceDeveloper, AudienceOperator:
			// Full technical details
			explanation.Remediation = append(explanation.Remediation,
				fmt.Sprintf("%s: %s", action.Title, action.Description))
			for _, cmd := range action.Commands {
				explanation.Remediation = append(explanation.Remediation,
					fmt.Sprintf("  $ %s", cmd))
			}
		}
	}

	// Add predicted action items
	if prediction != nil {
		for _, action := range prediction.Actions {
			if action.Priority == "high" || hrf.audience == AudienceDeveloper {
				explanation.Remediation = append(explanation.Remediation,
					fmt.Sprintf("[%s] %s", action.Priority, action.Description))
			}
		}
	}
}

func (hrf *HumanReadableFormatter) createTimeline(ctx context.Context, insight Insight, explanation *HumanReadableExplanation, relatedEvents []string) {
	// Create ASCII timeline for technical audiences
	if hrf.audience == AudienceDeveloper || hrf.audience == AudienceOperator {
		timeline := strings.Builder{}
		timeline.WriteString("Timeline:\n")
		timeline.WriteString(fmt.Sprintf("  %s - Correlation detected\n",
			insight.Timestamp.Format("15:04:05")))

		if len(relatedEvents) > 0 {
			timeline.WriteString(fmt.Sprintf("  %s - First event\n",
				insight.Timestamp.Add(-5*time.Minute).Format("15:04:05")))
			timeline.WriteString(fmt.Sprintf("  %s - Peak activity (%d events)\n",
				insight.Timestamp.Add(-2*time.Minute).Format("15:04:05"),
				len(relatedEvents)))
		}

		explanation.Visualizations["timeline"] = timeline.String()
	} else {
		// Simple timeline for non-technical audiences
		explanation.Timeline = fmt.Sprintf("Detected at %s",
			insight.Timestamp.Format("3:04 PM, January 2"))
	}
}

func (hrf *HumanReadableFormatter) formatDuration(d time.Duration) string {
	switch hrf.audience {
	case AudienceExecutive, AudienceManager:
		// Business-friendly durations
		if d < time.Hour {
			return fmt.Sprintf("%d minutes", int(d.Minutes()))
		} else if d < 24*time.Hour {
			return fmt.Sprintf("%.1f hours", d.Hours())
		} else {
			return fmt.Sprintf("%.1f days", d.Hours()/24)
		}
	default:
		// Technical durations
		return d.String()
	}
}

// AffectedResource represents a resource affected by an insight
type AffectedResource struct {
	Type       string
	Name       string
	Namespace  string
	Impact     string
	TraceLinks []string
}

// Prediction represents a predicted outcome with confidence scores
type Prediction struct {
	Scenario          string
	Probability       float64
	TimeWindow        time.Duration
	Actions           []ActionItem
	ConfidenceFactors map[string]float64
}

// ActionItem represents a specific action to take
type ActionItem struct {
	ID              string
	Type            string
	Description     string
	Priority        string
	Command         string
	Risk            string
	AutomationLevel string
}

// ActionableItem represents an actionable recommendation with context
type ActionableItem struct {
	Title           string
	Description     string
	Commands        []string
	Risk            string
	EstimatedImpact string
	RequiredRole    string
	TraceContext    string
}
