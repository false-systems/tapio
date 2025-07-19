package patternrecognition

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// MemoryLeakPattern detects memory leak patterns across different sources
type MemoryLeakPattern struct {
	*BasePattern
}

// NewMemoryLeakPattern creates a new memory leak detection pattern
func NewMemoryLeakPattern() Pattern {
	bp := NewBasePattern(
		"memory_leak_pattern",
		"Memory Leak Detection",
		"Detects memory leak patterns by correlating eBPF memory events, systemd service restarts, and Kubernetes pod evictions",
		PatternCategoryResource,
	)

	// Configure for memory leak detection
	bp.SetTimeWindow(30 * time.Minute)
	bp.SetMaxEvents(50)
	bp.SetMinConfidence(0.8)
	bp.SetTags([]string{"memory", "leak", "resource", "stability"})
	bp.SetPriority(PatternPriorityCritical)
	bp.SetRequiredSources([]domain.SourceType{
		domain.SourceEBPF,
		domain.SourceSystemd,
		domain.SourceK8s,
	})

	return &MemoryLeakPattern{
		BasePattern: bp,
	}
}

// Match implements the memory leak detection logic
func (m *MemoryLeakPattern) Match(ctx context.Context, events []domain.Event) ([]domain.Correlation, error) {
	if len(events) < 3 {
		return nil, nil // Need at least 3 events for memory leak pattern
	}

	// Filter and sort events
	relevantEvents := m.filterMemoryRelatedEvents(events)
	if len(relevantEvents) < 3 {
		return nil, nil
	}

	sortedEvents := m.SortEventsByTimestamp(relevantEvents)

	// Group events by host to find memory leaks per host
	hostGroups := m.GroupEventsByHost(sortedEvents)

	var correlations []domain.Correlation

	// Analyze each host group for memory leak patterns
	for host, hostEvents := range hostGroups {
		if len(hostEvents) < 3 {
			continue
		}

		correlation := m.analyzeMemoryLeakSequence(host, hostEvents)
		if correlation.ID != "" {
			correlations = append(correlations, correlation)
		}
	}

	return correlations, nil
}

// CanMatch checks if an event could be part of a memory leak pattern
func (m *MemoryLeakPattern) CanMatch(event domain.Event) bool {
	// Check if event is memory-related
	return m.isMemoryRelatedEvent(event)
}

// analyzeMemoryLeakSequence analyzes events for memory leak indicators
func (m *MemoryLeakPattern) analyzeMemoryLeakSequence(host string, events []domain.Event) domain.Correlation {
	// Look for the memory leak pattern:
	// 1. eBPF memory pressure/usage events
	// 2. systemd service restarts due to memory
	// 3. Kubernetes pod evictions or OOM kills

	var ebpfMemoryEvents []domain.Event
	var systemdRestarts []domain.Event
	var k8sEvictions []domain.Event

	// Categorize events by source and type
	for _, event := range events {
		switch event.Source {
		case domain.SourceEBPF:
			if m.isMemoryPressureEvent(event) {
				ebpfMemoryEvents = append(ebpfMemoryEvents, event)
			}
		case domain.SourceSystemd:
			if m.isMemoryRelatedRestart(event) {
				systemdRestarts = append(systemdRestarts, event)
			}
		case domain.SourceK8s:
			if m.isMemoryEvictionEvent(event) {
				k8sEvictions = append(k8sEvictions, event)
			}
		}
	}

	// Check if we have the required sequence
	if len(ebpfMemoryEvents) == 0 {
		return domain.Correlation{} // No memory pressure detected
	}

	// Calculate confidence based on pattern completeness
	confidence := m.calculateMemoryLeakConfidence(ebpfMemoryEvents, systemdRestarts, k8sEvictions)
	if confidence < m.MinConfidence() {
		return domain.Correlation{}
	}

	// Combine all relevant events
	allEvents := append(ebpfMemoryEvents, systemdRestarts...)
	allEvents = append(allEvents, k8sEvictions...)

	description := m.generateMemoryLeakDescription(host, ebpfMemoryEvents, systemdRestarts, k8sEvictions)

	return m.CreateCorrelation(allEvents, confidence, description)
}

// filterMemoryRelatedEvents filters events that are relevant to memory leak detection
func (m *MemoryLeakPattern) filterMemoryRelatedEvents(events []domain.Event) []domain.Event {
	var filtered []domain.Event

	for _, event := range events {
		if m.isMemoryRelatedEvent(event) {
			filtered = append(filtered, event)
		}
	}

	return filtered
}

// isMemoryRelatedEvent checks if an event is related to memory
func (m *MemoryLeakPattern) isMemoryRelatedEvent(event domain.Event) bool {
	// Check event type
	if event.Type == domain.EventTypeMemory {
		return true
	}

	// Check for memory-related keywords in various fields
	keywords := []string{"memory", "oom", "malloc", "leak", "heap", "rss", "vss", "swap"}

	// Check in metadata annotations
	for _, annotation := range event.Metadata.Annotations {
		if m.containsAnyKeyword(annotation, keywords) {
			return true
		}
	}

	// Check event payload for memory indicators
	switch payload := event.Payload.(type) {
	case domain.MemoryEventPayload:
		return true
	case domain.ServiceEventPayload:
		// Check service event for memory-related reasons
		return m.containsAnyKeyword(payload.EventType, keywords)
	case domain.KubernetesEventPayload:
		// Check K8s event for memory-related reasons
		return m.containsAnyKeyword(payload.Reason, keywords) ||
			m.containsAnyKeyword(payload.Message, keywords)
	}

	return false
}

// isMemoryPressureEvent checks if an eBPF event indicates memory pressure
func (m *MemoryLeakPattern) isMemoryPressureEvent(event domain.Event) bool {
	if event.Source != domain.SourceEBPF {
		return false
	}

	// Check for memory event payload
	if payload, ok := event.Payload.(domain.MemoryEventPayload); ok {
		// High memory usage indicates pressure
		return payload.Usage > 80.0
	}

	// Check severity and keywords
	return event.Severity >= domain.SeverityWarn && m.isMemoryRelatedEvent(event)
}

// isMemoryRelatedRestart checks if a systemd restart is memory-related
func (m *MemoryLeakPattern) isMemoryRelatedRestart(event domain.Event) bool {
	if event.Source != domain.SourceSystemd {
		return false
	}

	if payload, ok := event.Payload.(domain.ServiceEventPayload); ok {
		// Check if restart reason is memory-related
		if payload.State() == "failed" || payload.State() == "restart" {
			keywords := []string{"memory", "oom", "killed"}
			return m.containsAnyKeyword(payload.EventType, keywords)
		}
	}

	return false
}

// isMemoryEvictionEvent checks if a K8s event is a memory-related eviction
func (m *MemoryLeakPattern) isMemoryEvictionEvent(event domain.Event) bool {
	if event.Source != domain.SourceK8s {
		return false
	}

	if payload, ok := event.Payload.(domain.KubernetesEventPayload); ok {
		// Check for eviction or OOM reasons
		evictionReasons := []string{"evicted", "oom", "memory", "limit", "OOMKilled"}
		for _, reason := range evictionReasons {
			if m.containsKeywordIgnoreCase(payload.Reason, reason) ||
				m.containsKeywordIgnoreCase(payload.Message, reason) {
				return true
			}
		}
	}

	return false
}

// calculateMemoryLeakConfidence calculates confidence score for memory leak pattern
func (m *MemoryLeakPattern) calculateMemoryLeakConfidence(ebpfEvents, systemdEvents, k8sEvents []domain.Event) float64 {
	baseConfidence := 0.0

	// Base confidence from eBPF memory events
	if len(ebpfEvents) > 0 {
		baseConfidence += 0.4

		// Bonus for multiple memory events (indicates sustained pressure)
		if len(ebpfEvents) > 2 {
			baseConfidence += 0.1
		}

		// Bonus for high severity memory events
		for _, event := range ebpfEvents {
			if event.Severity >= domain.SeverityError {
				baseConfidence += 0.1
				break
			}
		}
	}

	// Confidence boost from systemd restarts
	if len(systemdEvents) > 0 {
		baseConfidence += 0.3

		// Extra confidence for multiple restarts
		if len(systemdEvents) > 1 {
			baseConfidence += 0.1
		}
	}

	// Confidence boost from K8s evictions
	if len(k8sEvents) > 0 {
		baseConfidence += 0.3

		// Extra confidence for multiple evictions
		if len(k8sEvents) > 1 {
			baseConfidence += 0.1
		}
	}

	// Temporal correlation bonus
	if m.eventsAreTemporallyCorrelated(ebpfEvents, systemdEvents, k8sEvents) {
		baseConfidence += 0.1
	}

	// Ensure confidence doesn't exceed 1.0
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}

	return baseConfidence
}

// eventsAreTemporallyCorrelated checks if events are properly sequenced in time
func (m *MemoryLeakPattern) eventsAreTemporallyCorrelated(ebpfEvents, systemdEvents, k8sEvents []domain.Event) bool {
	// Memory leak pattern should show:
	// 1. eBPF memory pressure first
	// 2. systemd restarts follow
	// 3. K8s evictions may follow

	if len(ebpfEvents) == 0 {
		return false
	}

	// Get earliest eBPF event
	earliestEBPF := ebpfEvents[0].Timestamp
	for _, event := range ebpfEvents {
		if event.Timestamp.Before(earliestEBPF) {
			earliestEBPF = event.Timestamp
		}
	}

	// Check if systemd restarts happened after eBPF detection
	for _, event := range systemdEvents {
		if event.Timestamp.Before(earliestEBPF) {
			return false // Restart before memory pressure doesn't fit pattern
		}
	}

	// Check if K8s evictions happened after eBPF detection
	for _, event := range k8sEvents {
		if event.Timestamp.Before(earliestEBPF) {
			return false // Eviction before memory pressure doesn't fit pattern
		}
	}

	return true
}

// generateMemoryLeakDescription generates a human-readable description
func (m *MemoryLeakPattern) generateMemoryLeakDescription(host string, ebpfEvents, systemdEvents, k8sEvents []domain.Event) string {
	parts := []string{
		fmt.Sprintf("Memory leak detected on host %s", host),
	}

	// Add eBPF memory details
	if len(ebpfEvents) > 0 {
		avgUsage := m.calculateAverageMemoryUsage(ebpfEvents)
		parts = append(parts, fmt.Sprintf("Memory usage averaged %.1f%% across %d observations", avgUsage, len(ebpfEvents)))
	}

	// Add systemd restart details
	if len(systemdEvents) > 0 {
		services := m.extractAffectedServices(systemdEvents)
		parts = append(parts, fmt.Sprintf("%d service restart(s) detected: %s", len(systemdEvents), strings.Join(services, ", ")))
	}

	// Add K8s eviction details
	if len(k8sEvents) > 0 {
		pods := m.extractEvictedPods(k8sEvents)
		parts = append(parts, fmt.Sprintf("%d pod eviction(s): %s", len(k8sEvents), strings.Join(pods, ", ")))
	}

	// Add time span
	if len(ebpfEvents) > 0 {
		span := m.calculateTimeSpan(append(append(ebpfEvents, systemdEvents...), k8sEvents...))
		parts = append(parts, fmt.Sprintf("Pattern observed over %s", span))
	}

	return strings.Join(parts, ". ")
}

// Helper methods for description generation

func (m *MemoryLeakPattern) calculateAverageMemoryUsage(events []domain.Event) float64 {
	total := 0.0
	count := 0

	for _, event := range events {
		if payload, ok := event.Payload.(domain.MemoryEventPayload); ok {
			total += payload.Usage
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return total / float64(count)
}

func (m *MemoryLeakPattern) extractAffectedServices(events []domain.Event) []string {
	services := make(map[string]bool)

	for _, event := range events {
		if payload, ok := event.Payload.(domain.ServiceEventPayload); ok {
			services[payload.ServiceName] = true
		}
	}

	result := make([]string, 0, len(services))
	for service := range services {
		result = append(result, service)
	}

	return result
}

func (m *MemoryLeakPattern) extractEvictedPods(events []domain.Event) []string {
	pods := make(map[string]bool)

	for _, event := range events {
		if payload, ok := event.Payload.(domain.KubernetesEventPayload); ok {
			if payload.Resource.Kind == "Pod" {
				pods[payload.Resource.Name] = true
			}
		}
	}

	result := make([]string, 0, len(pods))
	for pod := range pods {
		result = append(result, pod)
	}

	return result
}

func (m *MemoryLeakPattern) calculateTimeSpan(events []domain.Event) time.Duration {
	if len(events) == 0 {
		return 0
	}

	earliest := events[0].Timestamp
	latest := events[0].Timestamp

	for _, event := range events {
		if event.Timestamp.Before(earliest) {
			earliest = event.Timestamp
		}
		if event.Timestamp.After(latest) {
			latest = event.Timestamp
		}
	}

	return latest.Sub(earliest)
}
