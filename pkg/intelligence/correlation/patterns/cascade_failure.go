package patterns
import (
	"context"
	"fmt"
	"strings"
	"time"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation/core"
)
// CascadeFailurePattern detects cascade failure patterns across services
type CascadeFailurePattern struct {
	*BasePattern
}
// NewCascadeFailurePattern creates a new cascade failure detection pattern
func NewCascadeFailurePattern() core.CorrelationPattern {
	bp := NewBasePattern(
		"cascade_failure_pattern",
		"Cascade Failure Detection",
		"Detects cascade failure patterns where one service failure leads to dependent service failures",
		core.PatternCategoryCascade,
	)
	// Configure for cascade failure detection
	bp.SetTimeWindow(10 * time.Minute)
	bp.SetMaxEvents(30)
	bp.SetMinConfidence(0.75)
	bp.SetTags([]string{"cascade", "failure", "service", "dependency"})
	bp.SetPriority(core.PatternPriorityCritical)
	bp.SetRequiredSources([]domain.Source{
		domain.SourceKubernetes,
		domain.SourceSystemd,
		domain.SourceJournald,
	})
	return &CascadeFailurePattern{
		BasePattern: bp,
	}
}
// Match implements the cascade failure detection logic
func (c *CascadeFailurePattern) Match(ctx context.Context, events []domain.Event) ([]domain.Correlation, error) {
	if len(events) < 2 {
		return nil, nil // Need at least 2 events for cascade failure
	}
	// Filter failure events
	failureEvents := c.filterFailureEvents(events)
	if len(failureEvents) < 2 {
		return nil, nil
	}
	sortedEvents := c.SortEventsByTimestamp(failureEvents)
	// Find cascade sequences
	cascadeSequences := c.findCascadeSequences(sortedEvents)
	var correlations []domain.Correlation
	for _, sequence := range cascadeSequences {
		correlation := c.analyzeCascadeSequence(sequence)
		if correlation.ID != "" {
			correlations = append(correlations, correlation)
		}
	}
	return correlations, nil
}
// CanMatch checks if an event could be part of a cascade failure pattern
func (c *CascadeFailurePattern) CanMatch(event domain.Event) bool {
	// Check base conditions first
	if !c.BasePattern.CanMatch(event) {
		return false
	}
	// Check if event represents a failure
	return c.isFailureEvent(event)
}
// findCascadeSequences identifies potential cascade sequences in events
func (c *CascadeFailurePattern) findCascadeSequences(events []domain.Event) [][]domain.Event {
	var sequences [][]domain.Event
	// Group events by service/component to track failures
	_ = c.groupEventsByService(events) // Track service groups for future use
	// Look for temporal sequences of failures across different services
	for i := 0; i < len(events); i++ {
		rootEvent := events[i]
		// Find potential cascade starting from this event
		cascade := c.buildCascadeFromRoot(rootEvent, events[i+1:])
		if len(cascade) >= 2 { // Need at least 2 services failing
			sequences = append(sequences, cascade)
		}
	}
	return sequences
}
// buildCascadeFromRoot builds a cascade sequence starting from a root failure
func (c *CascadeFailurePattern) buildCascadeFromRoot(rootEvent domain.Event, subsequentEvents []domain.Event) []domain.Event {
	cascade := []domain.Event{rootEvent}
	rootService := c.extractServiceName(rootEvent)
	maxCascadeWindow := 5 * time.Minute // Maximum time for cascade propagation
	cascadeEndTime := rootEvent.Timestamp.Add(maxCascadeWindow)
	// Look for failures in other services that could be caused by root failure
	for _, event := range subsequentEvents {
		// Stop if we're beyond the cascade window
		if event.Timestamp.After(cascadeEndTime) {
			break
		}
		eventService := c.extractServiceName(event)
		// Skip if it's the same service (not a cascade)
		if eventService == rootService {
			continue
		}
		// Check if this failure could be caused by the root failure
		if c.couldBeCascadeEffect(rootEvent, event) {
			cascade = append(cascade, event)
			// Update cascade window to allow for further propagation
			cascadeEndTime = event.Timestamp.Add(maxCascadeWindow)
		}
	}
	return cascade
}
// analyzeCascadeSequence analyzes a cascade sequence and creates correlation
func (c *CascadeFailurePattern) analyzeCascadeSequence(sequence []domain.Event) domain.Correlation {
	if len(sequence) < 2 {
		return domain.Correlation{}
	}
	// Calculate confidence based on cascade characteristics
	confidence := c.calculateCascadeConfidence(sequence)
	if confidence < c.MinConfidence() {
		return domain.Correlation{}
	}
	description := c.generateCascadeDescription(sequence)
	return c.CreateCorrelation(sequence, confidence, description)
}
// calculateCascadeConfidence calculates confidence for cascade failure pattern
func (c *CascadeFailurePattern) calculateCascadeConfidence(sequence []domain.Event) float64 {
	baseConfidence := 0.5 // Start with moderate confidence
	// Confidence factors:
	// 1. Temporal ordering (failures should occur in logical order)
	if c.hasLogicalTemporalOrdering(sequence) {
		baseConfidence += 0.2
	}
	// 2. Service dependency relationships
	dependencyScore := c.calculateDependencyScore(sequence)
	baseConfidence += dependencyScore * 0.2
	// 3. Failure severity progression (should maintain or increase)
	if c.hasSeverityProgression(sequence) {
		baseConfidence += 0.1
	}
	// 4. Number of affected services (more services = higher confidence)
	uniqueServices := c.countUniqueServices(sequence)
	if uniqueServices >= 3 {
		baseConfidence += 0.1
	}
	// 5. Time spacing (failures too far apart are less likely to be cascades)
	if c.hasAppropriateTimeSpacing(sequence) {
		baseConfidence += 0.1
	}
	// 6. Context similarity (same cluster/namespace)
	if c.hasContextualRelationship(sequence) {
		baseConfidence += 0.1
	}
	// Ensure confidence doesn't exceed 1.0
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}
	return baseConfidence
}
// filterFailureEvents filters events that represent failures
func (c *CascadeFailurePattern) filterFailureEvents(events []domain.Event) []domain.Event {
	var failures []domain.Event
	for _, event := range events {
		if c.isFailureEvent(event) {
			failures = append(failures, event)
		}
	}
	return failures
}
// isFailureEvent checks if an event represents a failure
func (c *CascadeFailurePattern) isFailureEvent(event domain.Event) bool {
	// Check severity
	if event.Severity >= domain.SeverityError {
		return true
	}
	// Check for failure keywords
	failureKeywords := []string{"fail", "error", "crash", "down", "unavailable", "timeout", "unreachable"}
	// Check in metadata annotations
	for _, annotation := range event.Metadata.Annotations {
		for _, keyword := range failureKeywords {
			if c.containsKeywordIgnoreCase(annotation, keyword) {
				return true
			}
		}
	}
	// Check event payload for failure indicators
	switch payload := event.Payload.(type) {
	case domain.ServiceEventPayload:
		return c.isServiceFailure(payload)
	case domain.KubernetesEventPayload:
		return c.isKubernetesFailure(payload)
	case domain.LogEventPayload:
		return c.containsFailureKeywords(payload.Message)
	}
	return false
}
// isServiceFailure checks if a service event represents a failure
func (c *CascadeFailurePattern) isServiceFailure(payload domain.ServiceEventPayload) bool {
	failureStates := []string{"failed", "error", "inactive", "dead"}
	for _, state := range failureStates {
		if strings.EqualFold(payload.State(), state) {
			return true
		}
	}
	// ServiceEventPayload doesn't have Reason/Message fields
	// Check if EventType indicates failure
	return c.containsFailureKeywords(payload.EventType)
}
// isKubernetesFailure checks if a Kubernetes event represents a failure
func (c *CascadeFailurePattern) isKubernetesFailure(payload domain.KubernetesEventPayload) bool {
	failureTypes := []string{"warning", "error"}
	for _, ftype := range failureTypes {
		if strings.EqualFold(payload.EventType, ftype) {
			return c.containsFailureKeywords(payload.Reason) || 
				   c.containsFailureKeywords(payload.Message)
		}
	}
	return false
}
// extractServiceName extracts service name from event
func (c *CascadeFailurePattern) extractServiceName(event domain.Event) string {
	// Try to extract from different payload types
	switch payload := event.Payload.(type) {
	case domain.ServiceEventPayload:
		return payload.ServiceName
	case domain.KubernetesEventPayload:
		return payload.Resource.Name
	case domain.LogEventPayload:
		if payload.Unit != "" {
			return payload.Unit
		}
	}
	// Try to extract from context labels
	if serviceName, exists := event.Context.Labels["service"]; exists {
		return serviceName
	}
	if appName, exists := event.Context.Labels["app"]; exists {
		return appName
	}
	// Fallback to source
	return string(event.Source)
}
// groupEventsByService groups events by service name
func (c *CascadeFailurePattern) groupEventsByService(events []domain.Event) map[string][]domain.Event {
	groups := make(map[string][]domain.Event)
	for _, event := range events {
		service := c.extractServiceName(event)
		groups[service] = append(groups[service], event)
	}
	return groups
}
// couldBeCascadeEffect checks if an event could be a cascade effect
func (c *CascadeFailurePattern) couldBeCascadeEffect(rootEvent, candidateEvent domain.Event) bool {
	// Check if the candidate event occurs after the root event
	if !rootEvent.Timestamp.Before(candidateEvent.Timestamp) {
		return false
	}
	// Check if it's within reasonable cascade time window
	timeDiff := candidateEvent.Timestamp.Sub(rootEvent.Timestamp)
	if timeDiff > 5*time.Minute {
		return false
	}
	// Check if services are different
	rootService := c.extractServiceName(rootEvent)
	candidateService := c.extractServiceName(candidateEvent)
	if rootService == candidateService {
		return false
	}
	// Check for contextual relationship (same cluster, namespace, etc.)
	return c.hasContextualRelationship([]domain.Event{rootEvent, candidateEvent})
}
// hasLogicalTemporalOrdering checks if failures occur in logical order
func (c *CascadeFailurePattern) hasLogicalTemporalOrdering(sequence []domain.Event) bool {
	// For now, just check that events are ordered by timestamp
	// In a more sophisticated implementation, this would check if the ordering
	// makes sense from a dependency perspective
	for i := 1; i < len(sequence); i++ {
		if !sequence[i-1].Timestamp.Before(sequence[i].Timestamp) {
			return false
		}
	}
	return true
}
// calculateDependencyScore calculates a score based on known service dependencies
func (c *CascadeFailurePattern) calculateDependencyScore(sequence []domain.Event) float64 {
	// This is a simplified implementation
	// In a real system, this would consult a service dependency graph
	// For now, use heuristics based on service types and names
	score := 0.0
	for i := 1; i < len(sequence); i++ {
		rootService := c.extractServiceName(sequence[i-1])
		dependentService := c.extractServiceName(sequence[i])
		// Check for known dependency patterns
		if c.hasKnownDependency(rootService, dependentService) {
			score += 1.0
		}
	}
	// Normalize by number of potential dependencies
	if len(sequence) > 1 {
		score = score / float64(len(sequence)-1)
	}
	return score
}
// hasKnownDependency checks if one service is known to depend on another
func (c *CascadeFailurePattern) hasKnownDependency(service1, service2 string) bool {
	// Define common dependency patterns
	dependencies := map[string][]string{
		"database":    {"api", "web", "backend"},
		"redis":       {"api", "web", "cache"},
		"postgres":    {"api", "backend"},
		"mysql":       {"api", "backend"},
		"nginx":       {"web", "frontend"},
		"api":         {"frontend", "web"},
		"auth":        {"api", "web", "backend"},
		"messaging":   {"worker", "processor"},
	}
	// Check if service2 depends on service1
	if dependents, exists := dependencies[strings.ToLower(service1)]; exists {
		for _, dependent := range dependents {
			if strings.Contains(strings.ToLower(service2), dependent) {
				return true
			}
		}
	}
	return false
}
// hasSeverityProgression checks if severity maintains or increases through cascade
func (c *CascadeFailurePattern) hasSeverityProgression(sequence []domain.Event) bool {
	for i := 1; i < len(sequence); i++ {
		// Severity should not decrease in a cascade
		if sequence[i].Severity < sequence[i-1].Severity {
			return false
		}
	}
	return true
}
// countUniqueServices counts unique services in the sequence
func (c *CascadeFailurePattern) countUniqueServices(sequence []domain.Event) int {
	services := make(map[string]bool)
	for _, event := range sequence {
		service := c.extractServiceName(event)
		services[service] = true
	}
	return len(services)
}
// hasAppropriateTimeSpacing checks if failures have appropriate time spacing
func (c *CascadeFailurePattern) hasAppropriateTimeSpacing(sequence []domain.Event) bool {
	if len(sequence) < 2 {
		return true
	}
	for i := 1; i < len(sequence); i++ {
		timeDiff := sequence[i].Timestamp.Sub(sequence[i-1].Timestamp)
		// Failures should be within 1 second to 5 minutes apart
		if timeDiff < time.Second || timeDiff > 5*time.Minute {
			return false
		}
	}
	return true
}
// hasContextualRelationship checks if events share contextual relationships
func (c *CascadeFailurePattern) hasContextualRelationship(sequence []domain.Event) bool {
	if len(sequence) < 2 {
		return true
	}
	firstEvent := sequence[0]
	for i := 1; i < len(sequence); i++ {
		event := sequence[i]
		// Check for shared context
		if firstEvent.Context.Host != "" && event.Context.Host != "" {
			if firstEvent.Context.Host == event.Context.Host {
				return true
			}
		}
		// Check for shared namespace/cluster
		if firstEvent.Context.Labels["namespace"] != "" && 
		   event.Context.Labels["namespace"] != "" {
			if firstEvent.Context.Labels["namespace"] == event.Context.Labels["namespace"] {
				return true
			}
		}
		// Check for shared cluster
		if firstEvent.Context.Labels["cluster"] != "" && 
		   event.Context.Labels["cluster"] != "" {
			if firstEvent.Context.Labels["cluster"] == event.Context.Labels["cluster"] {
				return true
			}
		}
	}
	return false
}
// generateCascadeDescription generates description for cascade failure
func (c *CascadeFailurePattern) generateCascadeDescription(sequence []domain.Event) string {
	if len(sequence) == 0 {
		return "Cascade failure detected"
	}
	rootService := c.extractServiceName(sequence[0])
	affectedServices := c.countUniqueServices(sequence)
	description := fmt.Sprintf("Cascade failure initiated by %s affecting %d services", 
		rootService, affectedServices)
	// Add timeline information
	if len(sequence) > 1 {
		duration := sequence[len(sequence)-1].Timestamp.Sub(sequence[0].Timestamp)
		description += fmt.Sprintf(" over %v", duration.Truncate(time.Second))
	}
	// Add affected services
	if affectedServices > 1 {
		services := make(map[string]bool)
		var serviceList []string
		for _, event := range sequence {
			service := c.extractServiceName(event)
			if !services[service] {
				services[service] = true
				serviceList = append(serviceList, service)
			}
		}
		if len(serviceList) <= 5 {
			description += fmt.Sprintf(". Affected services: %s", strings.Join(serviceList, ", "))
		}
	}
	return description
}
// Helper methods
func (c *CascadeFailurePattern) containsFailureKeywords(text string) bool {
	keywords := []string{"fail", "error", "crash", "down", "unavailable", "timeout", "unreachable", "connection refused"}
	lowerText := strings.ToLower(text)
	for _, keyword := range keywords {
		if strings.Contains(lowerText, keyword) {
			return true
		}
	}
	return false
}
func (c *CascadeFailurePattern) containsKeywordIgnoreCase(text, keyword string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(keyword))
}