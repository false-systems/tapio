package patterns
import (
	"context"
	"fmt"
	"strings"
	"time"
	"github.com/falseyair/tapio/pkg/domain"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/core"
)
// NetworkFailurePattern detects network failure correlations
type NetworkFailurePattern struct {
	*BasePattern
}
// NewNetworkFailurePattern creates a new network failure correlation pattern
func NewNetworkFailurePattern() core.CorrelationPattern {
	bp := NewBasePattern(
		"network_failure_pattern",
		"Network Failure Correlation",
		"Correlates eBPF network events with Kubernetes service failures and connection timeouts",
		core.PatternCategoryNetwork,
	)
	// Configure for network failure detection
	bp.SetTimeWindow(5 * time.Minute)
	bp.SetMaxEvents(25)
	bp.SetMinConfidence(0.75)
	bp.SetTags([]string{"network", "failure", "connectivity", "timeout"})
	bp.SetPriority(core.PatternPriorityHigh)
	bp.SetRequiredSources([]domain.Source{
		domain.SourceEBPF,
		domain.SourceKubernetes,
		domain.SourceJournald,
	})
	return &NetworkFailurePattern{
		BasePattern: bp,
	}
}
// Match implements the network failure detection logic
func (n *NetworkFailurePattern) Match(ctx context.Context, events []domain.Event) ([]domain.Correlation, error) {
	if len(events) < 2 {
		return nil, nil
	}
	// Filter network-related events
	networkEvents := n.filterNetworkEvents(events)
	if len(networkEvents) < 2 {
		return nil, nil
	}
	sortedEvents := n.SortEventsByTimestamp(networkEvents)
	// Group events to find network failure patterns
	correlations := n.findNetworkFailureCorrelations(sortedEvents)
	return correlations, nil
}
// CanMatch checks if an event could be part of a network failure pattern
func (n *NetworkFailurePattern) CanMatch(event domain.Event) bool {
	// Check base conditions first
	if !n.BasePattern.CanMatch(event) {
		return false
	}
	// Check if event is network-related
	return n.isNetworkEvent(event)
}
// findNetworkFailureCorrelations finds network failure correlations
func (n *NetworkFailurePattern) findNetworkFailureCorrelations(events []domain.Event) []domain.Correlation {
	var correlations []domain.Correlation
	// Group events by different criteria to find patterns
	hostGroups := n.GroupEventsByHost(events)
	serviceGroups := n.groupEventsByService(events)
	// Analyze per-host network issues
	for host, hostEvents := range hostGroups {
		if len(hostEvents) >= 2 {
			correlation := n.analyzeHostNetworkPattern(host, hostEvents)
			if correlation.ID != "" {
				correlations = append(correlations, correlation)
			}
		}
	}
	// Analyze per-service network issues
	for service, serviceEvents := range serviceGroups {
		if len(serviceEvents) >= 2 {
			correlation := n.analyzeServiceNetworkPattern(service, serviceEvents)
			if correlation.ID != "" {
				correlations = append(correlations, correlation)
			}
		}
	}
	// Look for network-wide patterns
	if len(events) >= 3 {
		correlation := n.analyzeNetworkWidePattern(events)
		if correlation.ID != "" {
			correlations = append(correlations, correlation)
		}
	}
	return correlations
}
// analyzeHostNetworkPattern analyzes network patterns for a specific host
func (n *NetworkFailurePattern) analyzeHostNetworkPattern(host string, events []domain.Event) domain.Correlation {
	// Categorize events by type
	var ebpfEvents []domain.Event
	var k8sEvents []domain.Event
	var logEvents []domain.Event
	for _, event := range events {
		switch event.Source {
		case domain.SourceEBPF:
			if n.isNetworkFailureEvent(event) {
				ebpfEvents = append(ebpfEvents, event)
			}
		case domain.SourceKubernetes:
			if n.isNetworkRelatedK8sEvent(event) {
				k8sEvents = append(k8sEvents, event)
			}
		case domain.SourceJournald:
			if n.isNetworkRelatedLogEvent(event) {
				logEvents = append(logEvents, event)
			}
		}
	}
	// Calculate confidence based on event types and correlation
	confidence := n.calculateNetworkFailureConfidence(ebpfEvents, k8sEvents, logEvents)
	if confidence < n.MinConfidence() {
		return domain.Correlation{}
	}
	allEvents := append(ebpfEvents, k8sEvents...)
	allEvents = append(allEvents, logEvents...)
	description := n.generateNetworkFailureDescription("host", host, ebpfEvents, k8sEvents, logEvents)
	return n.CreateCorrelation(allEvents, confidence, description)
}
// analyzeServiceNetworkPattern analyzes network patterns for a specific service
func (n *NetworkFailurePattern) analyzeServiceNetworkPattern(service string, events []domain.Event) domain.Correlation {
	// Look for patterns indicating service connectivity issues
	var connectivityEvents []domain.Event
	var timeoutEvents []domain.Event
	var errorEvents []domain.Event
	for _, event := range events {
		if n.isConnectivityEvent(event) {
			connectivityEvents = append(connectivityEvents, event)
		}
		if n.isTimeoutEvent(event) {
			timeoutEvents = append(timeoutEvents, event)
		}
		if n.isNetworkErrorEvent(event) {
			errorEvents = append(errorEvents, event)
		}
	}
	// Calculate confidence based on pattern strength
	confidence := n.calculateServiceNetworkConfidence(connectivityEvents, timeoutEvents, errorEvents)
	if confidence < n.MinConfidence() {
		return domain.Correlation{}
	}
	allEvents := append(connectivityEvents, timeoutEvents...)
	allEvents = append(allEvents, errorEvents...)
	description := n.generateServiceNetworkDescription(service, connectivityEvents, timeoutEvents, errorEvents)
	return n.CreateCorrelation(allEvents, confidence, description)
}
// analyzeNetworkWidePattern analyzes network-wide patterns
func (n *NetworkFailurePattern) analyzeNetworkWidePattern(events []domain.Event) domain.Correlation {
	// Look for indicators of network-wide issues
	affectedHosts := make(map[string]bool)
	affectedServices := make(map[string]bool)
	var criticalEvents []domain.Event
	for _, event := range events {
		if event.Severity >= domain.SeverityError {
			criticalEvents = append(criticalEvents, event)
		}
		if event.Context.Host != "" {
			affectedHosts[event.Context.Host] = true
		}
		service := n.extractServiceName(event)
		if service != "" {
			affectedServices[service] = true
		}
	}
	// Network-wide issue if multiple hosts/services affected
	if len(affectedHosts) < 2 && len(affectedServices) < 2 {
		return domain.Correlation{}
	}
	confidence := n.calculateNetworkWideConfidence(len(affectedHosts), len(affectedServices), len(criticalEvents), len(events))
	if confidence < n.MinConfidence() {
		return domain.Correlation{}
	}
	description := fmt.Sprintf("Network-wide issues detected affecting %d hosts and %d services", 
		len(affectedHosts), len(affectedServices))
	return n.CreateCorrelation(events, confidence, description)
}
// filterNetworkEvents filters events relevant to network analysis
func (n *NetworkFailurePattern) filterNetworkEvents(events []domain.Event) []domain.Event {
	var filtered []domain.Event
	for _, event := range events {
		if n.isNetworkEvent(event) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}
// isNetworkEvent checks if an event is network-related
func (n *NetworkFailurePattern) isNetworkEvent(event domain.Event) bool {
	// Check event type
	if event.Type == domain.EventTypeNetwork {
		return true
	}
	// Check for network-related keywords
	keywords := []string{"network", "connection", "timeout", "socket", "tcp", "udp", "dns", "http"}
	// Check in metadata annotations
	for _, annotation := range event.Metadata.Annotations {
		for _, keyword := range keywords {
			if n.containsKeywordIgnoreCase(annotation, keyword) {
				return true
			}
		}
	}
	// Check event payload
	switch payload := event.Payload.(type) {
	case domain.NetworkEventPayload:
		return true
	case domain.LogEventPayload:
		return n.containsNetworkKeywords(payload.Message)
	case domain.KubernetesEventPayload:
		return n.containsNetworkKeywords(payload.Reason) || n.containsNetworkKeywords(payload.Message)
	}
	return false
}
// isNetworkFailureEvent checks if an eBPF event indicates network failure
func (n *NetworkFailurePattern) isNetworkFailureEvent(event domain.Event) bool {
	if event.Source != domain.SourceEBPF || event.Type != domain.EventTypeNetwork {
		return false
	}
	// Check severity
	if event.Severity >= domain.SeverityWarn {
		return true
	}
	// Check for failure indicators in network payload
	if payload, ok := event.Payload.(domain.NetworkEventPayload); ok {
		return payload.PacketsDropped > 0 || payload.Errors > 0 || payload.ConnectionsFailed > 0
	}
	return false
}
// isNetworkRelatedK8sEvent checks if a K8s event is network-related
func (n *NetworkFailurePattern) isNetworkRelatedK8sEvent(event domain.Event) bool {
	if event.Source != domain.SourceKubernetes {
		return false
	}
	if payload, ok := event.Payload.(domain.KubernetesEventPayload); ok {
		networkReasons := []string{
			"failedMount", "failedSync", "unhealthy", "probeError", 
			"networkNotReady", "cniFailure", "dnsFailure",
		}
		for _, reason := range networkReasons {
			if strings.Contains(strings.ToLower(payload.Reason), reason) {
				return true
			}
		}
		return n.containsNetworkKeywords(payload.Message)
	}
	return false
}
// isNetworkRelatedLogEvent checks if a log event is network-related
func (n *NetworkFailurePattern) isNetworkRelatedLogEvent(event domain.Event) bool {
	if event.Source != domain.SourceJournald {
		return false
	}
	if payload, ok := event.Payload.(domain.LogEventPayload); ok {
		return n.containsNetworkKeywords(payload.Message)
	}
	return false
}
// isConnectivityEvent checks if event indicates connectivity issues
func (n *NetworkFailurePattern) isConnectivityEvent(event domain.Event) bool {
	keywords := []string{"connection", "connect", "unreachable", "refused"}
	return n.containsAnyKeyword(event, keywords)
}
// isTimeoutEvent checks if event indicates timeout issues
func (n *NetworkFailurePattern) isTimeoutEvent(event domain.Event) bool {
	keywords := []string{"timeout", "timed out", "deadline exceeded"}
	return n.containsAnyKeyword(event, keywords)
}
// isNetworkErrorEvent checks if event indicates network errors
func (n *NetworkFailurePattern) isNetworkErrorEvent(event domain.Event) bool {
	keywords := []string{"network error", "socket error", "dns error", "resolve failed"}
	return n.containsAnyKeyword(event, keywords)
}
// calculateNetworkFailureConfidence calculates confidence for network failure correlation
func (n *NetworkFailurePattern) calculateNetworkFailureConfidence(ebpfEvents, k8sEvents, logEvents []domain.Event) float64 {
	baseConfidence := 0.0
	// Base confidence from eBPF network events
	if len(ebpfEvents) > 0 {
		baseConfidence += 0.4
		// Bonus for multiple network events
		if len(ebpfEvents) > 1 {
			baseConfidence += 0.1
		}
	}
	// Confidence boost from K8s events
	if len(k8sEvents) > 0 {
		baseConfidence += 0.3
		// Extra confidence for critical K8s events
		for _, event := range k8sEvents {
			if event.Severity >= domain.SeverityError {
				baseConfidence += 0.1
				break
			}
		}
	}
	// Confidence boost from log events
	if len(logEvents) > 0 {
		baseConfidence += 0.2
	}
	// Temporal correlation bonus
	if n.eventsAreTemporallyCorrelated(ebpfEvents, k8sEvents, logEvents) {
		baseConfidence += 0.1
	}
	// Severity-based bonus
	allEvents := append(ebpfEvents, k8sEvents...)
	allEvents = append(allEvents, logEvents...)
	if n.hasHighSeverityEvents(allEvents) {
		baseConfidence += 0.1
	}
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}
	return baseConfidence
}
// calculateServiceNetworkConfidence calculates confidence for service network issues
func (n *NetworkFailurePattern) calculateServiceNetworkConfidence(connectivityEvents, timeoutEvents, errorEvents []domain.Event) float64 {
	baseConfidence := 0.0
	// Connectivity issues
	if len(connectivityEvents) > 0 {
		baseConfidence += 0.4
	}
	// Timeout issues
	if len(timeoutEvents) > 0 {
		baseConfidence += 0.3
	}
	// Network errors
	if len(errorEvents) > 0 {
		baseConfidence += 0.3
	}
	// Multiple issue types indicate stronger correlation
	issueTypes := 0
	if len(connectivityEvents) > 0 {
		issueTypes++
	}
	if len(timeoutEvents) > 0 {
		issueTypes++
	}
	if len(errorEvents) > 0 {
		issueTypes++
	}
	if issueTypes > 1 {
		baseConfidence += 0.1
	}
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}
	return baseConfidence
}
// calculateNetworkWideConfidence calculates confidence for network-wide issues
func (n *NetworkFailurePattern) calculateNetworkWideConfidence(hostCount, serviceCount, criticalEventCount, totalEventCount int) float64 {
	baseConfidence := 0.0
	// Multiple hosts affected
	if hostCount >= 3 {
		baseConfidence += 0.4
	} else if hostCount >= 2 {
		baseConfidence += 0.2
	}
	// Multiple services affected
	if serviceCount >= 3 {
		baseConfidence += 0.3
	} else if serviceCount >= 2 {
		baseConfidence += 0.2
	}
	// High ratio of critical events
	if totalEventCount > 0 {
		criticalRatio := float64(criticalEventCount) / float64(totalEventCount)
		if criticalRatio > 0.5 {
			baseConfidence += 0.2
		}
	}
	// Large number of events
	if totalEventCount >= 5 {
		baseConfidence += 0.1
	}
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}
	return baseConfidence
}
// generateNetworkFailureDescription generates description for network failure
func (n *NetworkFailurePattern) generateNetworkFailureDescription(scope, identifier string, ebpfEvents, k8sEvents, logEvents []domain.Event) string {
	description := fmt.Sprintf("Network failure pattern detected for %s %s", scope, identifier)
	var components []string
	if len(ebpfEvents) > 0 {
		components = append(components, fmt.Sprintf("%d network events", len(ebpfEvents)))
	}
	if len(k8sEvents) > 0 {
		components = append(components, fmt.Sprintf("%d K8s events", len(k8sEvents)))
	}
	if len(logEvents) > 0 {
		components = append(components, fmt.Sprintf("%d log events", len(logEvents)))
	}
	if len(components) > 0 {
		description += " with " + strings.Join(components, ", ")
	}
	return description
}
// generateServiceNetworkDescription generates description for service network issues
func (n *NetworkFailurePattern) generateServiceNetworkDescription(service string, connectivityEvents, timeoutEvents, errorEvents []domain.Event) string {
	description := fmt.Sprintf("Network issues detected for service %s", service)
	var issues []string
	if len(connectivityEvents) > 0 {
		issues = append(issues, fmt.Sprintf("%d connectivity issues", len(connectivityEvents)))
	}
	if len(timeoutEvents) > 0 {
		issues = append(issues, fmt.Sprintf("%d timeout events", len(timeoutEvents)))
	}
	if len(errorEvents) > 0 {
		issues = append(issues, fmt.Sprintf("%d network errors", len(errorEvents)))
	}
	if len(issues) > 0 {
		description += ": " + strings.Join(issues, ", ")
	}
	return description
}
// Helper methods
func (n *NetworkFailurePattern) groupEventsByService(events []domain.Event) map[string][]domain.Event {
	groups := make(map[string][]domain.Event)
	for _, event := range events {
		service := n.extractServiceName(event)
		if service == "" {
			service = "unknown"
		}
		groups[service] = append(groups[service], event)
	}
	return groups
}
func (n *NetworkFailurePattern) extractServiceName(event domain.Event) string {
	// Try different payload types
	switch payload := event.Payload.(type) {
	case domain.KubernetesEventPayload:
		return payload.Resource.Name
	case domain.LogEventPayload:
		if payload.Unit != "" {
			return payload.Unit
		}
	}
	// Try context labels
	if service, exists := event.Context.Labels["service"]; exists {
		return service
	}
	if app, exists := event.Context.Labels["app"]; exists {
		return app
	}
	return ""
}
func (n *NetworkFailurePattern) containsNetworkKeywords(text string) bool {
	keywords := []string{
		"network", "connection", "timeout", "socket", "tcp", "udp", 
		"dns", "http", "unreachable", "refused", "reset",
	}
	lowerText := strings.ToLower(text)
	for _, keyword := range keywords {
		if strings.Contains(lowerText, keyword) {
			return true
		}
	}
	return false
}
func (n *NetworkFailurePattern) containsKeywordIgnoreCase(text, keyword string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(keyword))
}
func (n *NetworkFailurePattern) containsAnyKeyword(event domain.Event, keywords []string) bool {
	// Check in metadata annotations
	for _, annotation := range event.Metadata.Annotations {
		for _, keyword := range keywords {
			if n.containsKeywordIgnoreCase(annotation, keyword) {
				return true
			}
		}
	}
	// Check in payload based on type
	switch payload := event.Payload.(type) {
	case domain.LogEventPayload:
		for _, keyword := range keywords {
			if n.containsKeywordIgnoreCase(payload.Message, keyword) {
				return true
			}
		}
	case domain.KubernetesEventPayload:
		for _, keyword := range keywords {
			if n.containsKeywordIgnoreCase(payload.Reason, keyword) ||
				n.containsKeywordIgnoreCase(payload.Message, keyword) {
				return true
			}
		}
	}
	return false
}
func (n *NetworkFailurePattern) eventsAreTemporallyCorrelated(ebpfEvents, k8sEvents, logEvents []domain.Event) bool {
	allEvents := append(ebpfEvents, k8sEvents...)
	allEvents = append(allEvents, logEvents...)
	if len(allEvents) < 2 {
		return false
	}
	// Check if events occur within reasonable time window
	var earliest, latest time.Time
	for i, event := range allEvents {
		if i == 0 {
			earliest = event.Timestamp
			latest = event.Timestamp
		} else {
			if event.Timestamp.Before(earliest) {
				earliest = event.Timestamp
			}
			if event.Timestamp.After(latest) {
				latest = event.Timestamp
			}
		}
	}
	// Events should be within 5 minutes of each other for good correlation
	return latest.Sub(earliest) <= 5*time.Minute
}
func (n *NetworkFailurePattern) hasHighSeverityEvents(events []domain.Event) bool {
	for _, event := range events {
		if event.Severity >= domain.SeverityError {
			return true
		}
	}
	return false
}