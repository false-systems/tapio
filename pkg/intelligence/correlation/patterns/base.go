package patterns
import (
	"context"
	"fmt"
	"sort"
	"time"
	"github.com/falseyair/tapio/pkg/domain"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/core"
)
// BasePattern provides a base implementation for correlation patterns
type BasePattern struct {
	id           string
	name         string
	description  string
	category     core.PatternCategory
	timeWindow   time.Duration
	maxEvents    int
	minConfidence float64
	tags         []string
	priority     core.PatternPriority
	enabled      bool
	requiredSources []domain.Source
}
// NewBasePattern creates a new base pattern
func NewBasePattern(id, name, description string, category core.PatternCategory) *BasePattern {
	return &BasePattern{
		id:           id,
		name:         name,
		description:  description,
		category:     category,
		timeWindow:   5 * time.Minute,
		maxEvents:    100,
		minConfidence: 0.7,
		tags:         []string{},
		priority:     core.PatternPriorityMedium,
		enabled:      true,
		requiredSources: []domain.Source{},
	}
}
// ID returns the pattern ID
func (bp *BasePattern) ID() string {
	return bp.id
}
// Name returns the pattern name
func (bp *BasePattern) Name() string {
	return bp.name
}
// Description returns the pattern description
func (bp *BasePattern) Description() string {
	return bp.description
}
// Category returns the pattern category
func (bp *BasePattern) Category() core.PatternCategory {
	return bp.category
}
// TimeWindow returns the pattern time window
func (bp *BasePattern) TimeWindow() time.Duration {
	return bp.timeWindow
}
// MaxEvents returns the maximum events to consider
func (bp *BasePattern) MaxEvents() int {
	return bp.maxEvents
}
// MinConfidence returns the minimum confidence threshold
func (bp *BasePattern) MinConfidence() float64 {
	return bp.minConfidence
}
// Tags returns pattern tags
func (bp *BasePattern) Tags() []string {
	return bp.tags
}
// Priority returns pattern priority
func (bp *BasePattern) Priority() core.PatternPriority {
	return bp.priority
}
// Enabled returns whether the pattern is enabled
func (bp *BasePattern) Enabled() bool {
	return bp.enabled
}
// RequiredSources returns required event sources
func (bp *BasePattern) RequiredSources() []domain.Source {
	return bp.requiredSources
}
// Configuration methods
// SetTimeWindow sets the pattern time window
func (bp *BasePattern) SetTimeWindow(window time.Duration) {
	bp.timeWindow = window
}
// SetMaxEvents sets the maximum events
func (bp *BasePattern) SetMaxEvents(max int) {
	bp.maxEvents = max
}
// SetMinConfidence sets the minimum confidence
func (bp *BasePattern) SetMinConfidence(min float64) {
	bp.minConfidence = min
}
// SetTags sets pattern tags
func (bp *BasePattern) SetTags(tags []string) {
	bp.tags = tags
}
// SetPriority sets pattern priority
func (bp *BasePattern) SetPriority(priority core.PatternPriority) {
	bp.priority = priority
}
// SetEnabled enables or disables the pattern
func (bp *BasePattern) SetEnabled(enabled bool) {
	bp.enabled = enabled
}
// SetRequiredSources sets required sources
func (bp *BasePattern) SetRequiredSources(sources []domain.Source) {
	bp.requiredSources = sources
}
// AddTag adds a tag to the pattern
func (bp *BasePattern) AddTag(tag string) {
	bp.tags = append(bp.tags, tag)
}
// Default implementation of Match - should be overridden by specific patterns
func (bp *BasePattern) Match(ctx context.Context, events []domain.Event) ([]domain.Correlation, error) {
	// Base implementation returns no correlations
	// Specific patterns should override this method
	return nil, nil
}
// Default implementation of CanMatch - should be overridden by specific patterns
func (bp *BasePattern) CanMatch(event domain.Event) bool {
	// Check if event source is in required sources (if any specified)
	if len(bp.requiredSources) > 0 {
		sourceFound := false
		for _, source := range bp.requiredSources {
			if event.Source == source {
				sourceFound = true
				break
			}
		}
		if !sourceFound {
			return false
		}
	}
	// Base implementation allows all events
	return true
}
// Helper methods for pattern implementations
// CreateCorrelation creates a correlation from matched events
func (bp *BasePattern) CreateCorrelation(events []domain.Event, confidence float64, description string) domain.Correlation {
	if len(events) == 0 {
		return domain.Correlation{}
	}
	// Extract event IDs
	eventIDs := make([]domain.EventID, len(events))
	for i, event := range events {
		eventIDs[i] = event.ID
	}
	// Determine correlation type based on pattern category
	correlationType := bp.categoryToCorrelationType(bp.category)
	// Find the earliest and latest timestamps
	var earliest, latest time.Time
	for i, event := range events {
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
	// Create correlation context from events
	context := bp.createCorrelationContext(events)
	// Convert eventIDs to EventReferences
	eventRefs := make([]domain.EventReference, len(eventIDs))
	for i, eventID := range eventIDs {
		eventRefs[i] = domain.EventReference{
			EventID:      eventID,
			Role:         "participant",
			Relationship: "related",
			Weight:       1.0,
		}
	}
	// Convert float64 confidence to ConfidenceScore
	confidenceScore := domain.ConfidenceScore{
		Overall:     confidence,
		Temporal:    confidence,
		Causal:      confidence,
		Pattern:     confidence,
		Statistical: confidence,
	}
	// Convert EventContext to CorrelationContext
	corrContext := domain.CorrelationContext{
		Host:      context.Host,
		Cluster:   context.Cluster,
		Namespace: context.Namespace,
		Labels:    context.Labels,
		Tags:      context.Tags,
	}
	correlation := domain.Correlation{
		ID:          bp.generateCorrelationID(events),
		Type:        correlationType,
		Events:      eventRefs,
		Confidence:  confidenceScore,
		Description: description,
		Timestamp:   latest,
		Context:     corrContext,
		Findings:    bp.createFindings(events),
		Metadata: domain.CorrelationMetadata{
			SchemaVersion: "1.0",
			ProcessedAt:   time.Now(),
			ProcessedBy:   bp.name,
			Annotations: map[string]string{
				"pattern_id":       bp.id,
				"pattern_name":     bp.name,
				"pattern_category": string(bp.category),
				"time_span":        latest.Sub(earliest).String(),
				"event_count":      string(rune(len(events))),
			},
		},
	}
	return correlation
}
// Helper methods
func (bp *BasePattern) categoryToCorrelationType(category core.PatternCategory) domain.CorrelationType {
	switch category {
	case core.PatternCategoryMemory:
		return domain.CorrelationTypeResource
	case core.PatternCategoryNetwork:
		return domain.CorrelationTypeNetwork
	case core.PatternCategoryCPU:
		return domain.CorrelationTypeResource
	case core.PatternCategoryDisk:
		return domain.CorrelationTypeResource
	case core.PatternCategoryService:
		return domain.CorrelationTypeService
	case core.PatternCategorySecurity:
		return domain.CorrelationTypeSecurity
	case core.PatternCategoryPerformance:
		return domain.CorrelationTypePerformance
	case core.PatternCategoryCascade:
		return domain.CorrelationTypeCascade
	case core.PatternCategoryPredictive:
		return domain.CorrelationTypePredictive
	default:
		return domain.CorrelationTypeGeneral
	}
}
func (bp *BasePattern) generateCorrelationID(events []domain.Event) domain.CorrelationID {
	// Generate a unique correlation ID based on pattern and events
	timestamp := time.Now().UnixNano()
	return domain.CorrelationID(fmt.Sprintf("%s_%d", bp.id, timestamp))
}
func (bp *BasePattern) createCorrelationContext(events []domain.Event) domain.EventContext {
	context := domain.EventContext{
		Labels: make(domain.Labels),
		Tags:   make(domain.Tags, 0),
	}
	// Aggregate common context from events
	hostCounts := make(map[string]int)
	containerCounts := make(map[string]int)
	labelCounts := make(map[string]map[string]int)
	for _, event := range events {
		if event.Context.Host != "" {
			hostCounts[event.Context.Host]++
		}
		if event.Context.Container != "" {
			containerCounts[event.Context.Container]++
		}
		for key, value := range event.Context.Labels {
			if labelCounts[key] == nil {
				labelCounts[key] = make(map[string]int)
			}
			labelCounts[key][value]++
		}
	}
	// Set most common host
	if len(hostCounts) > 0 {
		maxCount := 0
		var mostCommonHost string
		for host, count := range hostCounts {
			if count > maxCount {
				maxCount = count
				mostCommonHost = host
			}
		}
		context.Host = mostCommonHost
	}
	// Set most common container
	if len(containerCounts) > 0 {
		maxCount := 0
		var mostCommonContainer string
		for container, count := range containerCounts {
			if count > maxCount {
				maxCount = count
				mostCommonContainer = container
			}
		}
		context.Container = mostCommonContainer
	}
	// Set most common labels
	for key, valueCounts := range labelCounts {
		maxCount := 0
		var mostCommonValue string
		for value, count := range valueCounts {
			if count > maxCount {
				maxCount = count
				mostCommonValue = value
			}
		}
		// Only include label if it appears in majority of events
		if maxCount > len(events)/2 {
			context.Labels[key] = mostCommonValue
		}
	}
	// Add pattern tags
	context.Tags = append(context.Tags, bp.tags...)
	context.Tags = append(context.Tags, string(bp.category))
	return context
}
func (bp *BasePattern) createFindings(events []domain.Event) []domain.Finding {
	var findings []domain.Finding
	// Create a finding summarizing the pattern match
	finding := domain.Finding{
		ID:          domain.FindingID(fmt.Sprintf("%s_finding_%d", bp.id, time.Now().UnixNano())),
		Title:       bp.name,
		Description: bp.description,
		Severity:    bp.determineFindingSeverity(events),
		Category:    string(bp.category),
		Confidence:  domain.ConfidenceScore{
			Overall:     bp.minConfidence,
			Temporal:    bp.minConfidence,
			Causal:      bp.minConfidence,
			Pattern:     bp.minConfidence,
			Statistical: bp.minConfidence,
		},
		Evidence:    bp.createEvidence(events),
		Timestamp:   time.Now(),
		Metadata: domain.FindingMetadata{
			SchemaVersion: "1.0",
			ProcessedAt:   time.Now(),
			ProcessedBy:   bp.name,
			Annotations: map[string]string{
				"pattern_id":   bp.id,
				"event_count": string(rune(len(events))),
			},
		},
	}
	findings = append(findings, finding)
	return findings
}
func (bp *BasePattern) determineFindingSeverity(events []domain.Event) domain.Severity {
	// Determine severity based on the most severe event
	maxSeverity := domain.SeverityDebug
	for _, event := range events {
		if event.Severity > maxSeverity {
			maxSeverity = event.Severity
		}
	}
	return maxSeverity
}
func (bp *BasePattern) createEvidence(events []domain.Event) []domain.Evidence {
	var evidence []domain.Evidence
	// Create evidence from the events
	for _, event := range events {
		ev := domain.Evidence{
			Type:        "event",
			Source:      event.Source,
			Description: fmt.Sprintf("Event %s from %s", event.ID, event.Source),
			Timestamp:   event.Timestamp,
			Metadata: map[string]interface{}{
				"event_id":   string(event.ID),
				"event_type": string(event.Type),
				"severity":   string(event.Severity),
				"confidence": event.Confidence,
			},
		}
		evidence = append(evidence, ev)
	}
	return evidence
}
// Event filtering helpers
// FilterEventsByTimeWindow filters events to those within the pattern's time window
func (bp *BasePattern) FilterEventsByTimeWindow(events []domain.Event, referenceTime time.Time) []domain.Event {
	var filtered []domain.Event
	windowStart := referenceTime.Add(-bp.timeWindow)
	for _, event := range events {
		if event.Timestamp.After(windowStart) && !event.Timestamp.After(referenceTime) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}
// FilterEventsBySource filters events by required sources
func (bp *BasePattern) FilterEventsBySource(events []domain.Event) []domain.Event {
	if len(bp.requiredSources) == 0 {
		return events
	}
	var filtered []domain.Event
	for _, event := range events {
		for _, source := range bp.requiredSources {
			if event.Source == source {
				filtered = append(filtered, event)
				break
			}
		}
	}
	return filtered
}
// FilterEventsBySeverity filters events by minimum severity
func (bp *BasePattern) FilterEventsBySeverity(events []domain.Event, minSeverity domain.Severity) []domain.Event {
	var filtered []domain.Event
	for _, event := range events {
		if event.Severity >= minSeverity {
			filtered = append(filtered, event)
		}
	}
	return filtered
}
// SortEventsByTimestamp sorts events by timestamp
func (bp *BasePattern) SortEventsByTimestamp(events []domain.Event) []domain.Event {
	sorted := make([]domain.Event, len(events))
	copy(sorted, events)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})
	return sorted
}
// GroupEventsByHost groups events by host
func (bp *BasePattern) GroupEventsByHost(events []domain.Event) map[string][]domain.Event {
	groups := make(map[string][]domain.Event)
	for _, event := range events {
		host := event.Context.Host
		if host == "" {
			host = "unknown"
		}
		groups[host] = append(groups[host], event)
	}
	return groups
}
// GroupEventsBySource groups events by source
func (bp *BasePattern) GroupEventsBySource(events []domain.Event) map[domain.Source][]domain.Event {
	groups := make(map[domain.Source][]domain.Event)
	for _, event := range events {
		groups[event.Source] = append(groups[event.Source], event)
	}
	return groups
}