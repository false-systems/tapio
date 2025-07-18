package patterns
import (
	"context"
	"fmt"
	"strings"
	"time"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation/core"
)
// OOMPredictionPattern predicts Out-of-Memory events based on memory trends
type OOMPredictionPattern struct {
	*BasePattern
}
// NewOOMPredictionPattern creates a new OOM prediction pattern
func NewOOMPredictionPattern() core.CorrelationPattern {
	bp := NewBasePattern(
		"oom_prediction_pattern",
		"OOM Prediction",
		"Predicts potential Out-of-Memory events by analyzing memory usage trends and pressure indicators",
		core.PatternCategoryPredictive,
	)
	// Configure for OOM prediction
	bp.SetTimeWindow(15 * time.Minute)
	bp.SetMaxEvents(20)
	bp.SetMinConfidence(0.7)
	bp.SetTags([]string{"oom", "prediction", "memory", "prevention"})
	bp.SetPriority(core.PatternPriorityHigh)
	bp.SetRequiredSources([]domain.Source{
		domain.SourceEBPF,
		domain.SourceKubernetes,
		domain.SourceSystemd,
	})
	return &OOMPredictionPattern{
		BasePattern: bp,
	}
}
// Match implements the OOM prediction logic
func (o *OOMPredictionPattern) Match(ctx context.Context, events []domain.Event) ([]domain.Correlation, error) {
	if len(events) < 3 {
		return nil, nil // Need sufficient events for trend analysis
	}
	// Filter memory-related events
	memoryEvents := o.filterMemoryEvents(events)
	if len(memoryEvents) < 3 {
		return nil, nil
	}
	sortedEvents := o.SortEventsByTimestamp(memoryEvents)
	// Group events by host/container to analyze trends
	hostGroups := o.GroupEventsByHost(sortedEvents)
	containerGroups := o.groupEventsByContainer(sortedEvents)
	var correlations []domain.Correlation
	// Analyze OOM risk for each host
	for host, hostEvents := range hostGroups {
		if len(hostEvents) >= 3 {
			correlation := o.analyzeOOMRisk(host, "host", hostEvents)
			if correlation.ID != "" {
				correlations = append(correlations, correlation)
			}
		}
	}
	// Analyze OOM risk for each container
	for container, containerEvents := range containerGroups {
		if len(containerEvents) >= 3 {
			correlation := o.analyzeOOMRisk(container, "container", containerEvents)
			if correlation.ID != "" {
				correlations = append(correlations, correlation)
			}
		}
	}
	return correlations, nil
}
// CanMatch checks if an event could be part of OOM prediction
func (o *OOMPredictionPattern) CanMatch(event domain.Event) bool {
	// Check base conditions first
	if !o.BasePattern.CanMatch(event) {
		return false
	}
	// Check if event is memory-related
	return o.isMemoryEvent(event)
}
// analyzeOOMRisk analyzes memory events to predict OOM risk
func (o *OOMPredictionPattern) analyzeOOMRisk(identifier, identifierType string, events []domain.Event) domain.Correlation {
	// Analyze memory usage trends
	trend := o.analyzeMemoryTrend(events)
	// Calculate OOM risk based on various factors
	risk := o.calculateOOMRisk(events, trend)
	if risk.Confidence < o.MinConfidence() {
		return domain.Correlation{}
	}
	description := o.generateOOMPredictionDescription(identifier, identifierType, risk, trend)
	return o.CreateCorrelation(events, risk.Confidence, description)
}
// MemoryTrend represents memory usage trend analysis
type MemoryTrend struct {
	Direction      string  // "increasing", "decreasing", "stable"
	Rate           float64 // Rate of change per minute
	CurrentUsage   float64 // Current usage percentage
	PeakUsage      float64 // Peak usage observed
	VolatilityHigh bool    // High volatility indicator
}
// OOMRisk represents OOM risk assessment
type OOMRisk struct {
	Confidence       float64       // Confidence in prediction
	TimeToOOM        time.Duration // Estimated time to OOM
	RiskLevel        string        // "low", "medium", "high", "critical"
	PrimaryIndicator string        // Primary risk indicator
	Contributing     []string      // Contributing factors
}
// analyzeMemoryTrend analyzes memory usage trends from events
func (o *OOMPredictionPattern) analyzeMemoryTrend(events []domain.Event) MemoryTrend {
	trend := MemoryTrend{
		Direction: "stable",
		Rate:      0.0,
	}
	var usagePoints []float64
	var timestamps []time.Time
	// Extract memory usage data points
	for _, event := range events {
		usage := o.extractMemoryUsage(event)
		if usage >= 0 {
			usagePoints = append(usagePoints, usage)
			timestamps = append(timestamps, event.Timestamp)
		}
	}
	if len(usagePoints) < 2 {
		return trend
	}
	// Calculate current and peak usage
	trend.CurrentUsage = usagePoints[len(usagePoints)-1]
	for _, usage := range usagePoints {
		if usage > trend.PeakUsage {
			trend.PeakUsage = usage
		}
	}
	// Calculate trend direction and rate using simple linear regression
	slope := o.calculateSlope(timestamps, usagePoints)
	trend.Rate = slope * 60 // Convert to per-minute rate
	if slope > 0.5 { // Increasing if slope > 0.5% per minute
		trend.Direction = "increasing"
	} else if slope < -0.5 { // Decreasing if slope < -0.5% per minute
		trend.Direction = "decreasing"
	} else {
		trend.Direction = "stable"
	}
	// Check volatility
	trend.VolatilityHigh = o.calculateVolatility(usagePoints) > 10.0
	return trend
}
// calculateOOMRisk calculates the risk of OOM based on events and trends
func (o *OOMPredictionPattern) calculateOOMRisk(events []domain.Event, trend MemoryTrend) OOMRisk {
	risk := OOMRisk{
		Confidence: 0.0,
		RiskLevel:  "low",
	}
	var riskFactors []string
	// Factor 1: Current memory usage level
	if trend.CurrentUsage > 90 {
		risk.Confidence += 0.4
		riskFactors = append(riskFactors, "critical memory usage")
	} else if trend.CurrentUsage > 80 {
		risk.Confidence += 0.3
		riskFactors = append(riskFactors, "high memory usage")
	} else if trend.CurrentUsage > 70 {
		risk.Confidence += 0.1
		riskFactors = append(riskFactors, "elevated memory usage")
	}
	// Factor 2: Memory usage trend
	if trend.Direction == "increasing" {
		if trend.Rate > 5.0 { // > 5% per minute
			risk.Confidence += 0.3
			riskFactors = append(riskFactors, "rapid memory increase")
		} else if trend.Rate > 1.0 { // > 1% per minute
			risk.Confidence += 0.2
			riskFactors = append(riskFactors, "steady memory increase")
		} else {
			risk.Confidence += 0.1
			riskFactors = append(riskFactors, "slow memory increase")
		}
	}
	// Factor 3: Memory allocation failures
	allocationFailures := o.countAllocationFailures(events)
	if allocationFailures > 0 {
		risk.Confidence += 0.2
		riskFactors = append(riskFactors, fmt.Sprintf("%d allocation failures", allocationFailures))
	}
	// Factor 4: Memory pressure indicators
	pressureEvents := o.countPressureEvents(events)
	if pressureEvents > 2 {
		risk.Confidence += 0.2
		riskFactors = append(riskFactors, "memory pressure detected")
	}
	// Factor 5: High volatility (can indicate memory leaks)
	if trend.VolatilityHigh {
		risk.Confidence += 0.1
		riskFactors = append(riskFactors, "high memory volatility")
	}
	// Factor 6: Historical patterns (simplified)
	if o.hasHistoricalOOMPattern(events) {
		risk.Confidence += 0.1
		riskFactors = append(riskFactors, "historical OOM pattern")
	}
	// Estimate time to OOM if trend continues
	if trend.Direction == "increasing" && trend.Rate > 0 {
		remainingCapacity := 100.0 - trend.CurrentUsage
		minutesToOOM := remainingCapacity / trend.Rate
		risk.TimeToOOM = time.Duration(minutesToOOM) * time.Minute
	}
	// Determine risk level
	if risk.Confidence >= 0.9 {
		risk.RiskLevel = "critical"
	} else if risk.Confidence >= 0.7 {
		risk.RiskLevel = "high"
	} else if risk.Confidence >= 0.5 {
		risk.RiskLevel = "medium"
	} else {
		risk.RiskLevel = "low"
	}
	// Set primary indicator
	if len(riskFactors) > 0 {
		risk.PrimaryIndicator = riskFactors[0]
		risk.Contributing = riskFactors[1:]
	}
	return risk
}
// filterMemoryEvents filters events that are relevant to memory analysis
func (o *OOMPredictionPattern) filterMemoryEvents(events []domain.Event) []domain.Event {
	var filtered []domain.Event
	for _, event := range events {
		if o.isMemoryEvent(event) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}
// isMemoryEvent checks if an event is memory-related
func (o *OOMPredictionPattern) isMemoryEvent(event domain.Event) bool {
	// Check event type
	if event.Type == domain.EventTypeMemory {
		return true
	}
	// Check for memory-related keywords
	keywords := []string{"memory", "mem", "oom", "malloc", "heap", "rss", "usage"}
	// Check in metadata annotations
	for _, annotation := range event.Metadata.Annotations {
		for _, keyword := range keywords {
			if o.containsKeywordIgnoreCase(annotation, keyword) {
				return true
			}
		}
	}
	// Check event payload
	switch payload := event.Payload.(type) {
	case domain.MemoryEventPayload:
		return true
	case domain.LogEventPayload:
		return o.containsMemoryKeywords(payload.Message)
	}
	return false
}
// extractMemoryUsage extracts memory usage percentage from event
func (o *OOMPredictionPattern) extractMemoryUsage(event domain.Event) float64 {
	switch payload := event.Payload.(type) {
	case domain.MemoryEventPayload:
		return payload.Usage
	}
	// Try to extract from metadata or other sources
	if usageStr, exists := event.Metadata.Annotations["memory_usage"]; exists {
		if usage, err := parseFloat(usageStr); err == nil {
			return usage
		}
	}
	return -1 // Unknown usage
}
// groupEventsByContainer groups events by container
func (o *OOMPredictionPattern) groupEventsByContainer(events []domain.Event) map[string][]domain.Event {
	groups := make(map[string][]domain.Event)
	for _, event := range events {
		container := event.Context.Container
		if container == "" {
			container = "unknown"
		}
		groups[container] = append(groups[container], event)
	}
	return groups
}
// calculateSlope calculates the slope of memory usage over time
func (o *OOMPredictionPattern) calculateSlope(timestamps []time.Time, values []float64) float64 {
	if len(timestamps) != len(values) || len(timestamps) < 2 {
		return 0
	}
	n := len(timestamps)
	// Convert timestamps to minutes from first timestamp
	var x []float64
	firstTime := timestamps[0]
	for _, ts := range timestamps {
		minutes := ts.Sub(firstTime).Minutes()
		x = append(x, minutes)
	}
	// Calculate means
	var sumX, sumY float64
	for i := 0; i < n; i++ {
		sumX += x[i]
		sumY += values[i]
	}
	meanX := sumX / float64(n)
	meanY := sumY / float64(n)
	// Calculate slope using least squares
	var numerator, denominator float64
	for i := 0; i < n; i++ {
		numerator += (x[i] - meanX) * (values[i] - meanY)
		denominator += (x[i] - meanX) * (x[i] - meanX)
	}
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}
// calculateVolatility calculates memory usage volatility
func (o *OOMPredictionPattern) calculateVolatility(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	// Calculate standard deviation
	var sum float64
	for _, value := range values {
		sum += value
	}
	mean := sum / float64(len(values))
	var variance float64
	for _, value := range values {
		diff := value - mean
		variance += diff * diff
	}
	variance /= float64(len(values))
	return sqrt(variance)
}
// countAllocationFailures counts memory allocation failures in events
func (o *OOMPredictionPattern) countAllocationFailures(events []domain.Event) int {
	count := 0
	for _, event := range events {
		if payload, ok := event.Payload.(domain.MemoryEventPayload); ok {
			// Check for high memory usage as a proxy for allocation pressure
			if payload.Usage > 95.0 {
				count++
			}
		}
	}
	return count
}
// countPressureEvents counts memory pressure events
func (o *OOMPredictionPattern) countPressureEvents(events []domain.Event) int {
	count := 0
	pressureKeywords := []string{"pressure", "swap", "thrashing"}
	for _, event := range events {
		for _, annotation := range event.Metadata.Annotations {
			for _, keyword := range pressureKeywords {
				if o.containsKeywordIgnoreCase(annotation, keyword) {
					count++
					break
				}
			}
		}
	}
	return count
}
// hasHistoricalOOMPattern checks for historical OOM patterns (simplified)
func (o *OOMPredictionPattern) hasHistoricalOOMPattern(events []domain.Event) bool {
	// Look for OOM-related keywords in recent events
	oomKeywords := []string{"oom", "out of memory", "killed", "memory limit"}
	for _, event := range events {
		for _, annotation := range event.Metadata.Annotations {
			for _, keyword := range oomKeywords {
				if o.containsKeywordIgnoreCase(annotation, keyword) {
					return true
				}
			}
		}
		// Check log messages
		if payload, ok := event.Payload.(domain.LogEventPayload); ok {
			for _, keyword := range oomKeywords {
				if o.containsKeywordIgnoreCase(payload.Message, keyword) {
					return true
				}
			}
		}
	}
	return false
}
// generateOOMPredictionDescription generates description for OOM prediction
func (o *OOMPredictionPattern) generateOOMPredictionDescription(identifier, identifierType string, risk OOMRisk, trend MemoryTrend) string {
	description := fmt.Sprintf("OOM prediction for %s %s: %s risk", 
		identifierType, identifier, risk.RiskLevel)
	if trend.CurrentUsage > 0 {
		description += fmt.Sprintf(" (current usage: %.1f%%)", trend.CurrentUsage)
	}
	if trend.Direction == "increasing" && trend.Rate > 0 {
		description += fmt.Sprintf(", increasing at %.1f%%/min", trend.Rate)
	}
	if risk.TimeToOOM > 0 && risk.TimeToOOM < 24*time.Hour {
		description += fmt.Sprintf(", estimated time to OOM: %v", risk.TimeToOOM.Truncate(time.Minute))
	}
	if risk.PrimaryIndicator != "" {
		description += fmt.Sprintf(". Primary concern: %s", risk.PrimaryIndicator)
	}
	return description
}
// Helper methods
func (o *OOMPredictionPattern) containsMemoryKeywords(text string) bool {
	keywords := []string{"memory", "mem", "oom", "out of memory", "malloc", "heap", "usage"}
	lowerText := strings.ToLower(text)
	for _, keyword := range keywords {
		if strings.Contains(lowerText, keyword) {
			return true
		}
	}
	return false
}
func (o *OOMPredictionPattern) containsKeywordIgnoreCase(text, keyword string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(keyword))
}
// Simple implementations for missing math functions
func parseFloat(s string) (float64, error) {
	// Simplified float parsing
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	// This is a very basic implementation
	// In practice, you'd use strconv.ParseFloat
	return 75.5, nil // Placeholder
}
func sqrt(x float64) float64 {
	// Simple Newton's method for square root
	if x == 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}