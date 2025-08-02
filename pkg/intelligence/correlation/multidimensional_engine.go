package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// MultiDimensionalEngine correlates events across multiple K8s dimensions
// This implements our Kantian framework for understanding K8s phenomena
type MultiDimensionalEngine struct {
	logger *zap.Logger

	// Dimension analyzers
	ownership  *OwnershipDimension
	spatial    *SpatialDimension
	temporal   *TemporalDimension
	causal     *CausalDimension
	semantic   *SemanticDimension
	dependency *DependencyDimension

	// Correlation graph
	graph *CorrelationGraph

	// Configuration
	config EngineConfig

	// Metrics
	mu      sync.RWMutex
	metrics EngineMetrics
}

// EngineConfig configures the correlation engine
type EngineConfig struct {
	// Correlation windows
	TemporalWindow time.Duration
	CausalWindow   time.Duration

	// Thresholds
	MinConfidence  float64
	MinCorrelation float64

	// Performance
	MaxGraphSize    int
	MaxCorrelations int

	// Features
	EnableOwnership  bool
	EnableSpatial    bool
	EnableTemporal   bool
	EnableCausal     bool
	EnableSemantic   bool
	EnableDependency bool
}

// EngineMetrics tracks correlation performance
type EngineMetrics struct {
	EventsProcessed   int64
	CorrelationsFound int64
	DimensionHits     map[string]int64
	AverageConfidence float64
	ProcessingTime    time.Duration
}

// MultiDimCorrelationResult represents a multi-dimensional correlation
type MultiDimCorrelationResult struct {
	ID             string
	Type           string
	Confidence     float64
	Events         []string // Event IDs
	Dimensions     []DimensionMatch
	RootCause      *MultiDimRootCauseAnalysis
	Impact         *ImpactAnalysis
	Recommendation string
	CreatedAt      time.Time
}

// DimensionMatch represents a match in a specific dimension
type DimensionMatch struct {
	Dimension  string
	Type       string
	Confidence float64
	Evidence   []string
	Metadata   map[string]interface{}
}

// MultiDimRootCauseAnalysis identifies the root cause
type MultiDimRootCauseAnalysis struct {
	EventID     string
	Confidence  float64
	Reasoning   string
	Evidence    []string
	CausalChain []CausalStep
}

// CausalStep in the causality chain
type CausalStep struct {
	EventID     string
	Timestamp   time.Time
	Description string
	Impact      string
}

// ImpactAnalysis describes correlation impact
type ImpactAnalysis struct {
	Severity             string
	Scope                []string // Affected resources
	InfrastructureImpact float64
	UserImpact           int
	ServiceImpact        []string
	MitigationSteps      []string
}

// NewMultiDimensionalEngine creates a new correlation engine
func NewMultiDimensionalEngine(logger *zap.Logger, config EngineConfig) *MultiDimensionalEngine {
	engine := &MultiDimensionalEngine{
		logger: logger,
		config: config,
		graph:  NewCorrelationGraph(),
		metrics: EngineMetrics{
			DimensionHits: make(map[string]int64),
		},
	}

	// Initialize dimension analyzers
	if config.EnableOwnership {
		engine.ownership = NewOwnershipDimension(logger)
	}
	if config.EnableSpatial {
		engine.spatial = NewSpatialDimension(logger)
	}
	if config.EnableTemporal {
		engine.temporal = NewTemporalDimension(logger, config.TemporalWindow)
	}
	if config.EnableCausal {
		engine.causal = NewCausalDimension(logger, config.CausalWindow)
	}
	if config.EnableSemantic {
		engine.semantic = NewSemanticDimension(logger)
	}
	if config.EnableDependency {
		engine.dependency = NewDependencyDimension(logger)
	}

	return engine
}

// Process analyzes an event for correlations
func (e *MultiDimensionalEngine) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*MultiDimCorrelationResult, error) {
	start := time.Now()
	defer func() {
		e.updateMetrics(time.Since(start))
	}()

	// Skip if no K8s context
	if event.K8sContext == nil {
		return nil, nil
	}

	// Add event to graph
	e.graph.AddEvent(event)

	// Find correlations in each dimension
	var allMatches []DimensionMatch

	if e.ownership != nil {
		if matches := e.ownership.FindCorrelations(event, e.graph); len(matches) > 0 {
			allMatches = append(allMatches, matches...)
			e.recordDimensionHit("ownership")
		}
	}

	if e.spatial != nil {
		if matches := e.spatial.FindCorrelations(event, e.graph); len(matches) > 0 {
			allMatches = append(allMatches, matches...)
			e.recordDimensionHit("spatial")
		}
	}

	if e.temporal != nil {
		if matches := e.temporal.FindCorrelations(event, e.graph); len(matches) > 0 {
			allMatches = append(allMatches, matches...)
			e.recordDimensionHit("temporal")
		}
	}

	if e.causal != nil {
		if matches := e.causal.FindCorrelations(event, e.graph); len(matches) > 0 {
			allMatches = append(allMatches, matches...)
			e.recordDimensionHit("causal")
		}
	}

	if e.semantic != nil {
		if matches := e.semantic.FindCorrelations(event, e.graph); len(matches) > 0 {
			allMatches = append(allMatches, matches...)
			e.recordDimensionHit("semantic")
		}
	}

	if e.dependency != nil {
		if matches := e.dependency.FindCorrelations(event, e.graph); len(matches) > 0 {
			allMatches = append(allMatches, matches...)
			e.recordDimensionHit("dependency")
		}
	}

	// Build correlation results
	results := e.buildCorrelations(event, allMatches)

	// Perform root cause analysis
	for _, result := range results {
		e.analyzeRootCause(result)
		e.analyzeImpact(result)
		e.generateRecommendation(result)
	}

	// Update event with correlation references
	for _, result := range results {
		event.Correlations = append(event.Correlations, domain.CorrelationRef{
			CorrelationID: result.ID,
			Type:          result.Type,
			Confidence:    result.Confidence,
		})
	}

	return results, nil
}

// buildCorrelations creates correlation results from dimension matches
func (e *MultiDimensionalEngine) buildCorrelations(event *domain.UnifiedEvent, matches []DimensionMatch) []*MultiDimCorrelationResult {
	// Group matches by correlation pattern
	correlationGroups := e.groupMatches(matches)

	var results []*MultiDimCorrelationResult
	for pattern, group := range correlationGroups {
		// Calculate combined confidence
		confidence := e.calculateConfidence(group)

		if confidence < e.config.MinConfidence {
			continue
		}

		// Extract unique event IDs
		eventIDs := e.extractEventIDs(group)
		eventIDs = append(eventIDs, event.ID) // Include current event

		result := &MultiDimCorrelationResult{
			ID:         fmt.Sprintf("corr-%s-%d", event.ID, time.Now().UnixNano()),
			Type:       pattern,
			Confidence: confidence,
			Events:     unique(eventIDs),
			Dimensions: group,
			CreatedAt:  time.Now(),
		}

		results = append(results, result)
		e.recordCorrelation()
	}

	return results
}

// groupMatches groups dimension matches by pattern
func (e *MultiDimensionalEngine) groupMatches(matches []DimensionMatch) map[string][]DimensionMatch {
	groups := make(map[string][]DimensionMatch)

	for _, match := range matches {
		pattern := fmt.Sprintf("%s_%s", match.Dimension, match.Type)
		groups[pattern] = append(groups[pattern], match)
	}

	// Also create cross-dimension groups
	if len(matches) > 1 {
		// Look for multi-dimension patterns
		multiPattern := e.detectMultiDimensionPattern(matches)
		if multiPattern != "" {
			groups[multiPattern] = matches
		}
	}

	return groups
}

// detectMultiDimensionPattern identifies cross-dimension patterns
func (e *MultiDimensionalEngine) detectMultiDimensionPattern(matches []DimensionMatch) string {
	dimensions := make(map[string]bool)
	for _, m := range matches {
		dimensions[m.Dimension] = true
	}

	// Common multi-dimension patterns
	if dimensions["ownership"] && dimensions["temporal"] {
		return "cascading_failure"
	}
	if dimensions["spatial"] && dimensions["causal"] {
		return "cross_zone_impact"
	}
	if dimensions["dependency"] && dimensions["causal"] {
		return "dependency_failure"
	}
	if dimensions["semantic"] && dimensions["temporal"] {
		return "recurring_issue"
	}

	if len(dimensions) >= 3 {
		return "complex_correlation"
	}

	return ""
}

// calculateConfidence computes combined confidence
func (e *MultiDimensionalEngine) calculateConfidence(matches []DimensionMatch) float64 {
	if len(matches) == 0 {
		return 0
	}

	// Weighted average with boost for multiple dimensions
	var sum float64
	for _, m := range matches {
		sum += m.Confidence
	}

	avg := sum / float64(len(matches))

	// Boost for multiple dimension correlation
	dimensionBoost := 1.0 + (float64(len(matches)-1) * 0.1)

	return min(avg*dimensionBoost, 1.0)
}

// extractEventIDs gets unique event IDs from matches
func (e *MultiDimensionalEngine) extractEventIDs(matches []DimensionMatch) []string {
	eventMap := make(map[string]bool)

	for _, match := range matches {
		if events, ok := match.Metadata["events"].([]string); ok {
			for _, id := range events {
				eventMap[id] = true
			}
		}
	}

	var ids []string
	for id := range eventMap {
		ids = append(ids, id)
	}
	return ids
}

// analyzeRootCause performs root cause analysis
func (e *MultiDimensionalEngine) analyzeRootCause(result *MultiDimCorrelationResult) {
	// Get all correlated events
	events := e.graph.GetEvents(result.Events)
	if len(events) == 0 {
		return
	}

	// Find earliest event with highest severity
	var rootEvent *domain.UnifiedEvent
	var rootScore float64

	for _, event := range events {
		score := e.calculateRootCauseScore(event, events)
		if score > rootScore {
			rootScore = score
			rootEvent = event
		}
	}

	if rootEvent == nil {
		return
	}

	// Build causal chain
	chain := e.buildCausalChain(rootEvent, events)

	result.RootCause = &MultiDimRootCauseAnalysis{
		EventID:     rootEvent.ID,
		Confidence:  rootScore,
		Reasoning:   e.explainRootCause(rootEvent, result),
		Evidence:    e.gatherEvidence(rootEvent, events),
		CausalChain: chain,
	}
}

// calculateRootCauseScore scores an event as potential root cause
func (e *MultiDimensionalEngine) calculateRootCauseScore(event *domain.UnifiedEvent, allEvents []*domain.UnifiedEvent) float64 {
	score := 0.0

	// Earlier events more likely to be root cause
	if earliest := findEarliest(allEvents); earliest != nil {
		timeDiff := event.Timestamp.Sub(earliest.Timestamp)
		score += 1.0 - (timeDiff.Seconds() / 300) // 5 minute window
	}

	// High severity events
	switch event.GetSeverity() {
	case "critical":
		score += 0.8
	case "error":
		score += 0.6
	case "warning":
		score += 0.4
	}

	// System-level events
	if event.Kernel != nil {
		score += 0.3 // Kernel events often root causes
	}

	// Resource events
	if event.Type == domain.EventTypeKubernetes {
		if event.Kubernetes != nil && event.Kubernetes.Reason == "OOMKilling" {
			score += 0.7
		}
	}

	return min(score, 1.0)
}

// buildCausalChain constructs the causality chain
func (e *MultiDimensionalEngine) buildCausalChain(root *domain.UnifiedEvent, events []*domain.UnifiedEvent) []CausalStep {
	var chain []CausalStep

	// Sort events by time
	sorted := sortByTime(events)

	for _, event := range sorted {
		chain = append(chain, CausalStep{
			EventID:     event.ID,
			Timestamp:   event.Timestamp,
			Description: e.describeEvent(event),
			Impact:      e.describeImpact(event),
		})
	}

	return chain
}

// analyzeImpact determines correlation impact
func (e *MultiDimensionalEngine) analyzeImpact(result *MultiDimCorrelationResult) {
	events := e.graph.GetEvents(result.Events)

	impact := &ImpactAnalysis{
		Severity:             e.calculateSeverity(events),
		Scope:                e.calculateScope(events),
		InfrastructureImpact: e.calculateInfrastructureImpact(events),
		UserImpact:           e.calculateUserImpact(events),
		ServiceImpact:        e.calculateServiceImpact(events),
	}

	// Generate mitigation steps based on pattern
	impact.MitigationSteps = e.generateMitigationSteps(result.Type, events)

	result.Impact = impact
}

// generateRecommendation creates actionable recommendations
func (e *MultiDimensionalEngine) generateRecommendation(result *MultiDimCorrelationResult) {
	switch result.Type {
	case "cascading_failure":
		result.Recommendation = "Cascading failure detected. Check resource limits and implement circuit breakers to prevent cascade."

	case "cross_zone_impact":
		result.Recommendation = "Cross-zone communication issues. Consider zone-aware routing and increase replicas per zone."

	case "dependency_failure":
		result.Recommendation = "Dependency failure propagation. Review health checks and add fallback mechanisms."

	case "recurring_issue":
		result.Recommendation = "Recurring pattern detected. This issue has happened before. Check for environmental triggers."

	case "complex_correlation":
		result.Recommendation = "Complex multi-system correlation. Investigate each affected component systematically."

	default:
		if result.RootCause != nil {
			result.Recommendation = fmt.Sprintf("Root cause identified in %s. Focus remediation efforts there.",
				result.RootCause.EventID)
		} else {
			result.Recommendation = "Multiple correlated events detected. Check system logs for additional context."
		}
	}
}

// Helper methods

func (e *MultiDimensionalEngine) updateMetrics(duration time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.metrics.EventsProcessed++
	e.metrics.ProcessingTime += duration
}

func (e *MultiDimensionalEngine) recordDimensionHit(dimension string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.metrics.DimensionHits[dimension]++
}

func (e *MultiDimensionalEngine) recordCorrelation() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.metrics.CorrelationsFound++
}

func (e *MultiDimensionalEngine) describeEvent(event *domain.UnifiedEvent) string {
	if event.Message != "" {
		return event.Message
	}

	if event.Semantic != nil && event.Semantic.Narrative != "" {
		return event.Semantic.Narrative
	}

	return fmt.Sprintf("%s event in %s/%s", event.Type, event.K8sContext.Namespace, event.K8sContext.Name)
}

func (e *MultiDimensionalEngine) describeImpact(event *domain.UnifiedEvent) string {
	if event.Impact != nil && event.Impact.Severity != "" {
		return fmt.Sprintf("%s severity, affecting %d services",
			event.Impact.Severity, len(event.Impact.AffectedServices))
	}
	return "Unknown impact"
}

func (e *MultiDimensionalEngine) explainRootCause(root *domain.UnifiedEvent, result *MultiDimCorrelationResult) string {
	explanation := fmt.Sprintf("Event %s identified as root cause based on: ", root.ID)

	reasons := []string{}

	// Timing
	reasons = append(reasons, "earliest timestamp")

	// Severity
	if root.GetSeverity() == "critical" || root.GetSeverity() == "error" {
		reasons = append(reasons, fmt.Sprintf("%s severity", root.GetSeverity()))
	}

	// Event type
	if root.Kernel != nil {
		reasons = append(reasons, "kernel-level event")
	}

	return explanation + joinStrings(reasons, ", ")
}

func (e *MultiDimensionalEngine) gatherEvidence(root *domain.UnifiedEvent, events []*domain.UnifiedEvent) []string {
	var evidence []string

	// Temporal evidence
	evidence = append(evidence,
		fmt.Sprintf("Started at %s", root.Timestamp.Format(time.RFC3339)))

	// Cascade evidence
	affectedCount := len(events) - 1
	if affectedCount > 0 {
		evidence = append(evidence,
			fmt.Sprintf("Cascaded to %d other events", affectedCount))
	}

	// Resource evidence
	if root.K8sContext != nil {
		evidence = append(evidence,
			fmt.Sprintf("Originated in %s/%s", root.K8sContext.Namespace, root.K8sContext.Name))
	}

	return evidence
}

func (e *MultiDimensionalEngine) calculateSeverity(events []*domain.UnifiedEvent) string {
	// Take highest severity
	severities := map[string]int{
		"critical": 4,
		"error":    3,
		"warning":  2,
		"info":     1,
	}

	maxSeverity := "info"
	maxScore := 0

	for _, event := range events {
		sev := event.GetSeverity()
		if score, ok := severities[sev]; ok && score > maxScore {
			maxScore = score
			maxSeverity = sev
		}
	}

	return maxSeverity
}

func (e *MultiDimensionalEngine) calculateScope(events []*domain.UnifiedEvent) []string {
	scope := make(map[string]bool)

	for _, event := range events {
		if event.K8sContext != nil {
			// Add namespace
			scope[fmt.Sprintf("namespace:%s", event.K8sContext.Namespace)] = true

			// Add workload
			if event.K8sContext.WorkloadName != "" {
				scope[fmt.Sprintf("%s:%s", event.K8sContext.WorkloadKind, event.K8sContext.WorkloadName)] = true
			}

			// Add node
			if event.K8sContext.NodeName != "" {
				scope[fmt.Sprintf("node:%s", event.K8sContext.NodeName)] = true
			}
		}
	}

	var result []string
	for s := range scope {
		result = append(result, s)
	}
	return result
}

func (e *MultiDimensionalEngine) calculateInfrastructureImpact(events []*domain.UnifiedEvent) float64 {
	maxImpact := 0.0

	for _, event := range events {
		if event.Impact != nil && event.Impact.InfrastructureImpact > maxImpact {
			maxImpact = event.Impact.InfrastructureImpact
		}
	}

	return maxImpact
}

func (e *MultiDimensionalEngine) calculateUserImpact(events []*domain.UnifiedEvent) int {
	// Since we don't have AffectedUsers in the domain, we'll use AffectedComponents as a proxy
	maxComponents := 0

	for _, event := range events {
		if event.Impact != nil && event.Impact.AffectedComponents > maxComponents {
			maxComponents = event.Impact.AffectedComponents
		}
	}

	return maxComponents
}

func (e *MultiDimensionalEngine) calculateServiceImpact(events []*domain.UnifiedEvent) []string {
	services := make(map[string]bool)

	for _, event := range events {
		if event.Impact != nil {
			for _, svc := range event.Impact.AffectedServices {
				services[svc] = true
			}
		}
	}

	var result []string
	for svc := range services {
		result = append(result, svc)
	}
	return result
}

func (e *MultiDimensionalEngine) generateMitigationSteps(pattern string, events []*domain.UnifiedEvent) []string {
	switch pattern {
	case "cascading_failure":
		return []string{
			"1. Identify and isolate the failing component",
			"2. Check resource limits (CPU, memory) on affected pods",
			"3. Review recent deployments or configuration changes",
			"4. Implement circuit breakers to prevent cascade",
			"5. Scale up healthy replicas if needed",
		}

	case "cross_zone_impact":
		return []string{
			"1. Check network connectivity between zones",
			"2. Verify zone-aware routing configuration",
			"3. Ensure sufficient replicas in each zone",
			"4. Review inter-zone latency metrics",
			"5. Consider enabling topology-aware routing",
		}

	case "dependency_failure":
		return []string{
			"1. Check health of dependent services",
			"2. Verify service discovery (DNS) is working",
			"3. Review connection pool settings",
			"4. Implement retry logic with backoff",
			"5. Add fallback mechanisms for critical paths",
		}

	default:
		return []string{
			"1. Review correlated events for common patterns",
			"2. Check system resources and limits",
			"3. Examine recent changes in affected components",
			"4. Monitor for recurrence",
			"5. Document findings for future reference",
		}
	}
}

// Utility functions

func unique(items []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func findEarliest(events []*domain.UnifiedEvent) *domain.UnifiedEvent {
	if len(events) == 0 {
		return nil
	}

	earliest := events[0]
	for _, e := range events[1:] {
		if e.Timestamp.Before(earliest.Timestamp) {
			earliest = e
		}
	}

	return earliest
}

func sortByTime(events []*domain.UnifiedEvent) []*domain.UnifiedEvent {
	// Simple bubble sort for small arrays
	sorted := make([]*domain.UnifiedEvent, len(events))
	copy(sorted, events)

	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			if sorted[j].Timestamp.After(sorted[j+1].Timestamp) {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	return sorted
}

func joinStrings(items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}

	result := items[0]
	for i := 1; i < len(items); i++ {
		result += sep + items[i]
	}

	return result
}
