package algorithms
import (
	"fmt"
	"math"
	"sort"
	"time"
	"github.com/falseyair/tapio/pkg/domain"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/core"
)
// causalAnalyzer implements causal correlation analysis
type causalAnalyzer struct {
	config core.AlgorithmConfig
}
// NewCausalAnalyzer creates a new causal analyzer
func NewCausalAnalyzer(config core.AlgorithmConfig) core.CausalAnalyzer {
	return &causalAnalyzer{
		config: config,
	}
}
// DetectCausality detects causal relationship between two events
func (c *causalAnalyzer) DetectCausality(cause, effect domain.Event) (core.CausalRelation, error) {
	relation := core.CausalRelation{
		Cause:  cause.ID,
		Effect: effect.ID,
	}
	// Check temporal ordering (cause must precede effect)
	if !cause.Timestamp.Before(effect.Timestamp) {
		return relation, fmt.Errorf("cause must precede effect in time")
	}
	delay := effect.Timestamp.Sub(cause.Timestamp)
	relation.Delay = delay
	// Compute causal strength based on multiple factors
	strength := c.computeCausalStrength(cause, effect)
	relation.Strength = strength
	// Determine causal type
	relation.Type = c.determineCausalType(cause, effect)
	// Gather evidence for the causal relationship
	evidence := c.gatherEvidence(cause, effect)
	relation.Evidence = evidence
	return relation, nil
}
// FindCausalChains finds causal chains in a set of events
func (c *causalAnalyzer) FindCausalChains(events []domain.Event) ([]core.CausalChain, error) {
	if len(events) < 2 {
		return nil, core.ErrInsufficientData
	}
	// Sort events by timestamp
	sortedEvents := make([]domain.Event, len(events))
	copy(sortedEvents, events)
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].Timestamp.Before(sortedEvents[j].Timestamp)
	})
	// Find all potential causal relations
	var relations []core.CausalRelation
	for i := 0; i < len(sortedEvents); i++ {
		for j := i + 1; j < len(sortedEvents); j++ {
			cause := sortedEvents[i]
			effect := sortedEvents[j]
			// Check if these events could be causally related
			if c.couldBeCausuallyRelated(cause, effect) {
				relation, err := c.DetectCausality(cause, effect)
				if err == nil && relation.Strength >= c.config.MinConfidence {
					relations = append(relations, relation)
				}
			}
		}
	}
	// Build chains from relations
	chains := c.buildChainsFromCausalRelations(events, relations)
	return chains, nil
}
// ComputeCausalStrength computes the strength of causal relationship
func (c *causalAnalyzer) ComputeCausalStrength(cause, effect domain.Event) float64 {
	return c.computeCausalStrength(cause, effect)
}
// InferCauses infers potential causes for an effect event
func (c *causalAnalyzer) InferCauses(effect domain.Event, candidateCauses []domain.Event) ([]core.CausalRelation, error) {
	var relations []core.CausalRelation
	for _, candidate := range candidateCauses {
		// Only consider events that precede the effect
		if !candidate.Timestamp.Before(effect.Timestamp) {
			continue
		}
		// Check if this could be a cause
		if c.couldBeCausuallyRelated(candidate, effect) {
			relation, err := c.DetectCausality(candidate, effect)
			if err == nil && relation.Strength >= c.config.MinConfidence {
				relations = append(relations, relation)
			}
		}
	}
	// Sort by causal strength
	sort.Slice(relations, func(i, j int) bool {
		return relations[i].Strength > relations[j].Strength
	})
	return relations, nil
}
// PredictEffects predicts potential effects of a cause event
func (c *causalAnalyzer) PredictEffects(cause domain.Event, historicalData []domain.Event) ([]core.PredictedEffect, error) {
	var predictions []core.PredictedEffect
	// Analyze historical patterns to predict effects
	patterns := c.analyzeHistoricalPatterns(cause, historicalData)
	for _, pattern := range patterns {
		prediction := core.PredictedEffect{
			Event:         pattern.TypicalEffect,
			Probability:   pattern.Probability,
			EstimatedTime: cause.Timestamp.Add(pattern.TypicalDelay),
			Confidence:    pattern.Confidence,
			BasedOn:       pattern.HistoricalEvents,
		}
		predictions = append(predictions, prediction)
	}
	// Sort by probability
	sort.Slice(predictions, func(i, j int) bool {
		return predictions[i].Probability > predictions[j].Probability
	})
	return predictions, nil
}
// Helper methods
// computeCausalStrength computes the strength of causal relationship between two events
func (c *causalAnalyzer) computeCausalStrength(cause, effect domain.Event) float64 {
	var factors []float64
	// Temporal factor: closer in time = stronger causality (up to a limit)
	temporalFactor := c.computeTemporalFactor(cause, effect)
	factors = append(factors, temporalFactor)
	// Contextual factor: similar context = stronger causality
	contextualFactor := c.computeContextualFactor(cause, effect)
	factors = append(factors, contextualFactor)
	// Severity factor: error/critical events have stronger causal potential
	severityFactor := c.computeSeverityFactor(cause, effect)
	factors = append(factors, severityFactor)
	// Source relationship factor
	sourceFactor := c.computeSourceFactor(cause, effect)
	factors = append(factors, sourceFactor)
	// Domain knowledge factor
	domainFactor := c.computeDomainFactor(cause, effect)
	factors = append(factors, domainFactor)
	// Combine factors using weighted average
	weights := []float64{0.3, 0.2, 0.2, 0.15, 0.15} // temporal, contextual, severity, source, domain
	return c.weightedAverage(factors, weights)
}
// computeTemporalFactor computes causal strength based on temporal relationship
func (c *causalAnalyzer) computeTemporalFactor(cause, effect domain.Event) float64 {
	delay := effect.Timestamp.Sub(cause.Timestamp)
	// Immediate effects (< 1 second) have high causal potential
	if delay < time.Second {
		return 0.9
	}
	// Effects within a minute have good causal potential
	if delay < time.Minute {
		return 0.8
	}
	// Effects within an hour have moderate causal potential
	if delay < time.Hour {
		return 0.6 * math.Exp(-float64(delay.Minutes())/60.0)
	}
	// Effects beyond an hour have decreasing causal potential
	if delay < c.config.TimeWindow {
		hours := delay.Hours()
		return 0.3 * math.Exp(-hours/24.0) // Exponential decay over 24 hours
	}
	return 0.1 // Minimal causal potential for very distant events
}
// computeContextualFactor computes causal strength based on context similarity
func (c *causalAnalyzer) computeContextualFactor(cause, effect domain.Event) float64 {
	score := 0.0
	factors := 0
	// Same host
	if cause.Context.Host != "" && effect.Context.Host != "" {
		factors++
		if cause.Context.Host == effect.Context.Host {
			score += 0.8
		}
	}
	// Same container
	if cause.Context.Container != "" && effect.Context.Container != "" {
		factors++
		if cause.Context.Container == effect.Context.Container {
			score += 0.9
		}
	}
	// Same or related process
	if cause.Context.PID != nil && effect.Context.PID != nil {
		factors++
		if *cause.Context.PID == *effect.Context.PID {
			score += 1.0
		} else if c.areRelatedProcesses(*cause.Context.PID, *effect.Context.PID) {
			score += 0.6
		}
	}
	// Label similarity
	if len(cause.Context.Labels) > 0 && len(effect.Context.Labels) > 0 {
		factors++
		similarity := c.computeLabelSimilarity(cause.Context.Labels, effect.Context.Labels)
		score += similarity
	}
	if factors == 0 {
		return 0.5 // Default moderate score when no context available
	}
	return score / float64(factors)
}
// computeSeverityFactor computes causal strength based on event severities
func (c *causalAnalyzer) computeSeverityFactor(cause, effect domain.Event) float64 {
	// Critical and error events have higher causal potential
	causeSeverityScore := c.severityToScore(cause.Severity)
	effectSeverityScore := c.severityToScore(effect.Severity)
	// Higher severity events are more likely to cause other events
	// Effects with high severity are more likely to be caused by something
	return (causeSeverityScore + effectSeverityScore) / 2.0
}
// computeSourceFactor computes causal strength based on source relationships
func (c *causalAnalyzer) computeSourceFactor(cause, effect domain.Event) float64 {
	// Define causal relationships between sources
	causalMatrix := map[domain.Source]map[domain.Source]float64{
		domain.SourceEBPF: {
			domain.SourceSystemd:    0.8, // eBPF memory issues -> systemd restarts
			domain.SourceKubernetes: 0.7, // eBPF network issues -> K8s failures
			domain.SourceJournald:   0.6, // eBPF events -> log entries
		},
		domain.SourceSystemd: {
			domain.SourceKubernetes: 0.9, // systemd service fails -> K8s pod fails
			domain.SourceJournald:   0.8, // systemd events -> journal logs
			domain.SourceEBPF:      0.4, // systemd restart -> eBPF observations
		},
		domain.SourceKubernetes: {
			domain.SourceJournald: 0.7, // K8s events -> journal logs
			domain.SourceSystemd:  0.6, // K8s scheduling -> systemd events
			domain.SourceEBPF:     0.5, // K8s actions -> eBPF observations
		},
		domain.SourceJournald: {
			domain.SourceEBPF:      0.3, // Logs rarely cause eBPF events
			domain.SourceSystemd:   0.2, // Logs rarely cause systemd events
			domain.SourceKubernetes: 0.2, // Logs rarely cause K8s events
		},
	}
	if sourceMap, exists := causalMatrix[cause.Source]; exists {
		if factor, exists := sourceMap[effect.Source]; exists {
			return factor
		}
	}
	// Same source events can have moderate causal relationship
	if cause.Source == effect.Source {
		return 0.6
	}
	return 0.3 // Default low causal potential between unrelated sources
}
// computeDomainFactor computes causal strength based on domain knowledge
func (c *causalAnalyzer) computeDomainFactor(cause, effect domain.Event) float64 {
	// Define domain-specific causal relationships
	// Memory-related causality
	if c.isMemoryEvent(cause) && c.isSystemFailureEvent(effect) {
		return 0.9 // Memory issues often cause system failures
	}
	// Network-related causality
	if c.isNetworkEvent(cause) && c.isServiceFailureEvent(effect) {
		return 0.8 // Network issues often cause service failures
	}
	// CPU-related causality
	if c.isCPUEvent(cause) && c.isPerformanceEvent(effect) {
		return 0.7 // CPU issues often cause performance problems
	}
	// Disk-related causality
	if c.isDiskEvent(cause) && c.isSystemFailureEvent(effect) {
		return 0.8 // Disk issues often cause system failures
	}
	// Service failure cascades
	if c.isServiceFailureEvent(cause) && c.isServiceFailureEvent(effect) {
		return 0.7 // Service failures often cascade
	}
	return 0.5 // Default moderate score
}
// determineCausalType determines the type of causal relationship
func (c *causalAnalyzer) determineCausalType(cause, effect domain.Event) core.CausalType {
	delay := effect.Timestamp.Sub(cause.Timestamp)
	// Immediate effects suggest direct causality
	if delay < time.Minute {
		return core.CausalTypeDirect
	}
	// Check if this is a necessary condition
	if c.isNecessaryCondition(cause, effect) {
		return core.CausalTypeNecessary
	}
	// Check if this is a sufficient condition
	if c.isSufficientCondition(cause, effect) {
		return core.CausalTypeSufficient
	}
	// Check if this is a contributing factor
	if c.isContributingFactor(cause, effect) {
		return core.CausalTypeContributing
	}
	return core.CausalTypeIndirect
}
// gatherEvidence gathers evidence for a causal relationship
func (c *causalAnalyzer) gatherEvidence(cause, effect domain.Event) []core.Evidence {
	var evidence []core.Evidence
	// Temporal evidence
	delay := effect.Timestamp.Sub(cause.Timestamp)
	evidence = append(evidence, core.Evidence{
		Type:        core.EvidenceTypeTemporal,
		Strength:    c.computeTemporalFactor(cause, effect),
		Description: fmt.Sprintf("Effect occurred %v after cause", delay),
		Source:      "temporal_analysis",
		Metadata:    map[string]interface{}{"delay": delay.String()},
	})
	// Contextual evidence
	contextStrength := c.computeContextualFactor(cause, effect)
	if contextStrength > 0.5 {
		evidence = append(evidence, core.Evidence{
			Type:        core.EvidenceTypeDomain,
			Strength:    contextStrength,
			Description: "Events share significant contextual similarity",
			Source:      "context_analysis",
			Metadata:    map[string]interface{}{"context_similarity": contextStrength},
		})
	}
	// Domain knowledge evidence
	domainStrength := c.computeDomainFactor(cause, effect)
	if domainStrength > 0.6 {
		evidence = append(evidence, core.Evidence{
			Type:        core.EvidenceTypeDomain,
			Strength:    domainStrength,
			Description: "Domain knowledge supports causal relationship",
			Source:      "domain_knowledge",
			Metadata:    map[string]interface{}{"domain_score": domainStrength},
		})
	}
	return evidence
}
// Helper methods for event classification
func (c *causalAnalyzer) isMemoryEvent(event domain.Event) bool {
	return event.Type == domain.EventTypeMemory ||
		c.containsKeywords(event, []string{"memory", "oom", "malloc", "leak"})
}
func (c *causalAnalyzer) isNetworkEvent(event domain.Event) bool {
	return event.Type == domain.EventTypeNetwork ||
		c.containsKeywords(event, []string{"network", "connection", "timeout", "socket"})
}
func (c *causalAnalyzer) isCPUEvent(event domain.Event) bool {
	return event.Type == domain.EventTypeCPU ||
		c.containsKeywords(event, []string{"cpu", "throttling", "load", "usage"})
}
func (c *causalAnalyzer) isDiskEvent(event domain.Event) bool {
	return event.Type == domain.EventTypeDisk ||
		c.containsKeywords(event, []string{"disk", "io", "storage", "filesystem"})
}
func (c *causalAnalyzer) isSystemFailureEvent(event domain.Event) bool {
	return event.Severity >= domain.SeverityError ||
		c.containsKeywords(event, []string{"fail", "error", "crash", "restart"})
}
func (c *causalAnalyzer) isServiceFailureEvent(event domain.Event) bool {
	return event.Type == domain.EventTypeService && event.Severity >= domain.SeverityError
}
func (c *causalAnalyzer) isPerformanceEvent(event domain.Event) bool {
	return c.containsKeywords(event, []string{"slow", "latency", "performance", "degradation"})
}
func (c *causalAnalyzer) containsKeywords(event domain.Event, keywords []string) bool {
	// Check in event metadata annotations
	for _, keyword := range keywords {
		for _, annotation := range event.Metadata.Annotations {
			if c.containsIgnoreCase(annotation, keyword) {
				return true
			}
		}
	}
	return false
}
func (c *causalAnalyzer) containsIgnoreCase(text, substring string) bool {
	// Simple case-insensitive contains check
	return len(text) >= len(substring) && 
		   c.toLower(text[0:len(substring)]) == c.toLower(substring)
}
func (c *causalAnalyzer) toLower(s string) string {
	// Simple ASCII lowercase conversion
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			result[i] = s[i] + 32
		} else {
			result[i] = s[i]
		}
	}
	return string(result)
}
// Utility methods
func (c *causalAnalyzer) severityToScore(severity domain.Severity) float64 {
	switch severity {
	case domain.SeverityCritical:
		return 1.0
	case domain.SeverityError:
		return 0.8
	case domain.SeverityWarn:
		return 0.6
	case domain.SeverityInfo:
		return 0.4
	case domain.SeverityDebug:
		return 0.2
	default:
		return 0.3
	}
}
func (c *causalAnalyzer) weightedAverage(values, weights []float64) float64 {
	if len(values) != len(weights) {
		return 0.0
	}
	var sum, weightSum float64
	for i, value := range values {
		sum += value * weights[i]
		weightSum += weights[i]
	}
	if weightSum == 0 {
		return 0.0
	}
	return sum / weightSum
}
func (c *causalAnalyzer) computeLabelSimilarity(labels1, labels2 domain.Labels) float64 {
	if len(labels1) == 0 || len(labels2) == 0 {
		return 0.0
	}
	matches := 0
	for key, value := range labels1 {
		if labels2[key] == value {
			matches++
		}
	}
	// Jaccard similarity
	union := len(labels1) + len(labels2) - matches
	if union == 0 {
		return 1.0
	}
	return float64(matches) / float64(union)
}
func (c *causalAnalyzer) areRelatedProcesses(pid1, pid2 int32) bool {
	// Simple heuristic: PIDs close in value might be related
	diff := pid1 - pid2
	if diff < 0 {
		diff = -diff
	}
	return diff < 100 // Arbitrary threshold
}
func (c *causalAnalyzer) couldBeCausuallyRelated(cause, effect domain.Event) bool {
	// Basic filters for potential causal relationships
	// Must be temporally ordered
	if !cause.Timestamp.Before(effect.Timestamp) {
		return false
	}
	// Must be within reasonable time window
	delay := effect.Timestamp.Sub(cause.Timestamp)
	if delay > c.config.TimeWindow {
		return false
	}
	// At least one should be error/warning severity
	if cause.Severity < domain.SeverityWarn && effect.Severity < domain.SeverityWarn {
		return false
	}
	return true
}
// Additional helper methods for causal analysis
func (c *causalAnalyzer) isNecessaryCondition(cause, effect domain.Event) bool {
	// This would require historical analysis to determine if the effect
	// always requires this cause. For now, use heuristics.
	return c.isMemoryEvent(cause) && c.isSystemFailureEvent(effect)
}
func (c *causalAnalyzer) isSufficientCondition(cause, effect domain.Event) bool {
	// This would require historical analysis to determine if this cause
	// always leads to this effect. For now, use heuristics.
	return cause.Severity == domain.SeverityCritical && 
		   effect.Severity >= domain.SeverityError
}
func (c *causalAnalyzer) isContributingFactor(cause, effect domain.Event) bool {
	// Most relationships are contributing factors rather than necessary/sufficient
	return true
}
func (c *causalAnalyzer) buildChainsFromCausalRelations(events []domain.Event, relations []core.CausalRelation) []core.CausalChain {
	// Build adjacency graph
	adjacency := make(map[domain.EventID][]core.CausalRelation)
	for _, relation := range relations {
		adjacency[relation.Cause] = append(adjacency[relation.Cause], relation)
	}
	var chains []core.CausalChain
	visited := make(map[domain.EventID]bool)
	// Find chains starting from each unvisited event
	for _, event := range events {
		if !visited[event.ID] {
			chain := c.buildChainFromCausalEvent(event.ID, adjacency, visited)
			if len(chain.Events) >= 2 {
				chain.ID = fmt.Sprintf("causal_chain_%d", len(chains))
				chain.Confidence = c.computeCausalChainConfidence(chain)
				chain.Category = c.determineCausalChainCategory(chain, events)
				chains = append(chains, chain)
			}
		}
	}
	return chains
}
func (c *causalAnalyzer) buildChainFromCausalEvent(startEvent domain.EventID, adjacency map[domain.EventID][]core.CausalRelation, visited map[domain.EventID]bool) core.CausalChain {
	chain := core.CausalChain{
		Events:    []domain.EventID{startEvent},
		Relations: []core.CausalRelation{},
	}
	visited[startEvent] = true
	current := startEvent
	// Follow the strongest causal links
	for {
		relations, hasNext := adjacency[current]
		if !hasNext || len(relations) == 0 {
			break
		}
		// Choose the strongest unvisited relation
		var strongestRelation core.CausalRelation
		maxStrength := 0.0
		found := false
		for _, relation := range relations {
			if !visited[relation.Effect] && relation.Strength > maxStrength {
				strongestRelation = relation
				maxStrength = relation.Strength
				found = true
			}
		}
		if !found {
			break
		}
		chain.Events = append(chain.Events, strongestRelation.Effect)
		chain.Relations = append(chain.Relations, strongestRelation)
		visited[strongestRelation.Effect] = true
		current = strongestRelation.Effect
	}
	return chain
}
func (c *causalAnalyzer) computeCausalChainConfidence(chain core.CausalChain) float64 {
	if len(chain.Relations) == 0 {
		return 0.0
	}
	// Average the strength of all relations in the chain
	var totalStrength float64
	for _, relation := range chain.Relations {
		totalStrength += relation.Strength
	}
	averageStrength := totalStrength / float64(len(chain.Relations))
	// Apply chain length penalty
	lengthPenalty := 1.0 / math.Sqrt(float64(len(chain.Events)))
	return math.Min(1.0, averageStrength*lengthPenalty)
}
func (c *causalAnalyzer) determineCausalChainCategory(chain core.CausalChain, events []domain.Event) core.ChainCategory {
	// Similar to temporal chain category determination but with causal focus
	eventMap := make(map[domain.EventID]domain.Event)
	for _, event := range events {
		eventMap[event.ID] = event
	}
	// Analyze the chain for dominant patterns
	hasFailure := false
	hasResource := false
	hasNetwork := false
	hasSecurity := false
	for _, eventID := range chain.Events {
		if event, exists := eventMap[eventID]; exists {
			if event.Severity >= domain.SeverityError {
				hasFailure = true
			}
			if c.isMemoryEvent(event) || c.isCPUEvent(event) || c.isDiskEvent(event) {
				hasResource = true
			}
			if c.isNetworkEvent(event) {
				hasNetwork = true
			}
			// Add security event detection as needed
		}
	}
	if hasFailure {
		return core.ChainCategoryFailure
	}
	if hasResource {
		return core.ChainCategoryResource
	}
	if hasNetwork {
		return core.ChainCategoryNetwork
	}
	if hasSecurity {
		return core.ChainCategorySecurity
	}
	return core.ChainCategoryPerformance
}
// Pattern analysis for prediction
type historicalPattern struct {
	TypicalEffect     domain.Event
	TypicalDelay      time.Duration
	Probability       float64
	Confidence        float64
	HistoricalEvents  []domain.EventID
}
func (c *causalAnalyzer) analyzeHistoricalPatterns(cause domain.Event, historicalData []domain.Event) []historicalPattern {
	// This is a simplified implementation
	// In a real system, this would analyze large amounts of historical data
	var patterns []historicalPattern
	// Group historical events by similarity to the cause
	similarEvents := c.findSimilarEvents(cause, historicalData)
	// For each similar event, find what typically followed
	for _, similar := range similarEvents {
		// Find events that followed this similar event
		followers := c.findFollowingEvents(similar, historicalData, 24*time.Hour)
		// Create patterns from the followers
		for _, follower := range followers {
			delay := follower.Timestamp.Sub(similar.Timestamp)
			pattern := historicalPattern{
				TypicalEffect:    follower,
				TypicalDelay:     delay,
				Probability:      0.7, // Simplified probability
				Confidence:       0.6, // Simplified confidence
				HistoricalEvents: []domain.EventID{similar.ID},
			}
			patterns = append(patterns, pattern)
		}
	}
	return patterns
}
func (c *causalAnalyzer) findSimilarEvents(target domain.Event, events []domain.Event) []domain.Event {
	var similar []domain.Event
	for _, event := range events {
		// Skip the target event itself
		if event.ID == target.ID {
			continue
		}
		// Check similarity based on multiple factors
		similarity := c.computeEventSimilarity(target, event)
		if similarity > 0.7 {
			similar = append(similar, event)
		}
	}
	return similar
}
func (c *causalAnalyzer) findFollowingEvents(target domain.Event, events []domain.Event, maxDelay time.Duration) []domain.Event {
	var followers []domain.Event
	for _, event := range events {
		// Must occur after the target
		if !target.Timestamp.Before(event.Timestamp) {
			continue
		}
		// Must be within the time window
		delay := event.Timestamp.Sub(target.Timestamp)
		if delay > maxDelay {
			continue
		}
		followers = append(followers, event)
	}
	return followers
}
func (c *causalAnalyzer) computeEventSimilarity(event1, event2 domain.Event) float64 {
	var factors []float64
	// Source similarity
	if event1.Source == event2.Source {
		factors = append(factors, 1.0)
	} else {
		factors = append(factors, 0.3)
	}
	// Type similarity
	if event1.Type == event2.Type {
		factors = append(factors, 1.0)
	} else {
		factors = append(factors, 0.2)
	}
	// Severity similarity
	severityDiff := severityToInt(event1.Severity) - severityToInt(event2.Severity)
	if severityDiff < 0 {
		severityDiff = -severityDiff
	}
	severityScore := math.Max(0, 1.0-float64(severityDiff)*0.2)
	factors = append(factors, severityScore)
	// Context similarity
	contextScore := c.computeContextualFactor(event1, event2)
	factors = append(factors, contextScore)
	// Simple average
	var sum float64
	for _, factor := range factors {
		sum += factor
	}
	return sum / float64(len(factors))
}
// severityToInt converts severity to numeric value for comparison
func severityToInt(severity domain.Severity) int {
	switch severity {
	case domain.SeverityTrace:
		return 1
	case domain.SeverityDebug:
		return 2
	case domain.SeverityInfo:
		return 3
	case domain.SeverityWarn:
		return 4
	case domain.SeverityError:
		return 5
	case domain.SeverityCritical:
		return 6
	default:
		return 3 // Default to info level
	}
}