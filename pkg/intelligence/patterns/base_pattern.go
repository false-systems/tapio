package patternrecognition

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// BasePattern provides a base implementation for correlation patterns
type BasePattern struct {
	id              string
	name            string
	description     string
	category        PatternCategory
	timeWindow      time.Duration
	maxEvents       int
	minConfidence   float64
	tags            []string
	priority        PatternPriority
	enabled         bool
	requiredSources []domain.SourceType
	metadata        PatternMetadata
}

// NewBasePattern creates a new base pattern
func NewBasePattern(id, name, description string, category PatternCategory) *BasePattern {
	return &BasePattern{
		id:              id,
		name:            name,
		description:     description,
		category:        category,
		timeWindow:      5 * time.Minute,
		maxEvents:       100,
		minConfidence:   0.7,
		tags:            []string{},
		priority:        PatternPriorityMedium,
		enabled:         true,
		requiredSources: []domain.SourceType{},
		metadata: PatternMetadata{
			Version:    "1.0.0",
			LastUpdate: time.Now(),
		},
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
func (bp *BasePattern) Category() PatternCategory {
	return bp.category
}

// TimeWindow returns the pattern time window
func (bp *BasePattern) TimeWindow() time.Duration {
	return bp.timeWindow
}

// MinConfidence returns the minimum confidence threshold
func (bp *BasePattern) MinConfidence() float64 {
	return bp.minConfidence
}

// Priority returns pattern priority
func (bp *BasePattern) Priority() PatternPriority {
	return bp.priority
}

// RequiredSources returns required event sources
func (bp *BasePattern) RequiredSources() []domain.SourceType {
	return bp.requiredSources
}

// Enabled returns whether the pattern is enabled
func (bp *BasePattern) Enabled() bool {
	return bp.enabled
}

// GetMetadata returns pattern metadata
func (bp *BasePattern) GetMetadata() PatternMetadata {
	return bp.metadata
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
func (bp *BasePattern) SetPriority(priority PatternPriority) {
	bp.priority = priority
}

// SetEnabled sets whether pattern is enabled
func (bp *BasePattern) SetEnabled(enabled bool) {
	bp.enabled = enabled
}

// SetRequiredSources sets required event sources
func (bp *BasePattern) SetRequiredSources(sources []domain.SourceType) {
	bp.requiredSources = sources
}

// Helper methods for pattern implementations

// CreateCorrelation creates a correlation from matched events
func (bp *BasePattern) CreateCorrelation(events []domain.Event, confidence float64, description string) domain.Correlation {
	if len(events) == 0 {
		return domain.Correlation{}
	}

	// Sort events by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	// Create event references
	eventRefs := make([]domain.EventReference, len(events))
	for i, event := range events {
		eventRefs[i] = domain.EventReference{
			EventID:      event.ID,
			Role:         bp.determineEventRole(event, i, len(events)),
			Relationship: "correlated",
			Weight:       bp.calculateEventWeight(event),
		}
	}

	// Determine correlation type based on pattern
	correlationType := bp.mapPatternToCorrelationType()

	return domain.Correlation{
		ID:   domain.CorrelationID(fmt.Sprintf("correlation-%s-%d", bp.id, time.Now().UnixNano())),
		Type: correlationType,
		Pattern: domain.PatternSignature{
			Name:        bp.name,
			Version:     bp.metadata.Version,
			Fingerprint: bp.id,
		},
		Description: description,
		Events:      eventRefs,
		Timestamp:   time.Now(),
		Confidence:  domain.FloatToConfidenceScore(confidence),
		Metadata: domain.CorrelationMetadata{
			Algorithm:     "pattern-matching",
			ProcessedBy:   "patternrecognition-engine",
			ProcessedAt:   time.Now(),
			SchemaVersion: "v1",
		},
	}
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

// FilterEventsByTimeWindow filters events within time window
func (bp *BasePattern) FilterEventsByTimeWindow(events []domain.Event, window time.Duration) []domain.Event {
	if len(events) == 0 {
		return events
	}

	// Find the latest event
	latest := events[0].Timestamp
	for _, event := range events {
		if event.Timestamp.After(latest) {
			latest = event.Timestamp
		}
	}

	// Filter events within window from latest
	cutoff := latest.Add(-window)
	filtered := make([]domain.Event, 0)

	for _, event := range events {
		if event.Timestamp.After(cutoff) {
			filtered = append(filtered, event)
		}
	}

	return filtered
}

// containsKeywordIgnoreCase checks if text contains keyword (case-insensitive)
func (bp *BasePattern) containsKeywordIgnoreCase(text, keyword string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(keyword))
}

// containsAnyKeyword checks if text contains any of the keywords
func (bp *BasePattern) containsAnyKeyword(text string, keywords []string) bool {
	lowerText := strings.ToLower(text)
	for _, keyword := range keywords {
		if strings.Contains(lowerText, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

// Private helper methods

func (bp *BasePattern) determineEventRole(event domain.Event, index, total int) string {
	// First event is typically the trigger
	if index == 0 {
		return "trigger"
	}

	// Last event might be the consequence
	if index == total-1 {
		return "consequence"
	}

	// Middle events are participants
	return "participant"
}

func (bp *BasePattern) calculateEventWeight(event domain.Event) float64 {
	// Weight based on severity
	switch event.Severity {
	case domain.SeverityCritical:
		return 1.0
	case domain.SeverityError:
		return 0.8
	case domain.SeverityWarn:
		return 0.6
	case domain.SeverityInfo:
		return 0.4
	default:
		return 0.2
	}
}

func (bp *BasePattern) mapPatternToCorrelationType() domain.CorrelationType {
	switch bp.category {
	case PatternCategoryResource:
		return domain.CorrelationTypeResource
	case PatternCategoryNetwork:
		return domain.CorrelationTypeNetwork
	case PatternCategoryPerformance:
		return domain.CorrelationTypePerformance
	case PatternCategoryStability:
		return domain.CorrelationTypeCascade
	case PatternCategorySecurity:
		return domain.CorrelationTypeSecurity
	default:
		return domain.CorrelationAnomaly
	}
}
