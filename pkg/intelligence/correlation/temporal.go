package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// TemporalCorrelator finds time-based patterns in events
type TemporalCorrelator struct {
	logger *zap.Logger

	// Pattern detection
	patterns   map[string]*TemporalPattern
	patternsMu sync.RWMutex

	// Event window for analysis
	eventWindow *TimeWindow

	// Configuration
	config TemporalConfig
}

// TemporalConfig defined in config.go - removing duplicate

// TemporalPattern represents a time-based pattern
type TemporalPattern struct {
	ID          string
	FirstEvent  EventRef
	LastEvent   EventRef
	Occurrences int
	TimeDelta   time.Duration
	Confidence  float64
	LastSeen    time.Time
}

// EventRef references an event
type EventRef struct {
	ID        string
	Type      string
	Timestamp time.Time
	Resource  string
}

// TimeWindow manages events within a time window
type TimeWindow struct {
	mu       sync.RWMutex
	events   []*WindowedEvent
	size     time.Duration
	maxItems int
}

// WindowedEvent wraps an event with window metadata
type WindowedEvent struct {
	Event   *domain.UnifiedEvent
	AddedAt time.Time
}

// NewTemporalCorrelator creates a new temporal correlator
func NewTemporalCorrelator(logger *zap.Logger, config TemporalConfig) *TemporalCorrelator {
	return &TemporalCorrelator{
		logger:   logger,
		patterns: make(map[string]*TemporalPattern),
		eventWindow: &TimeWindow{
			events:   make([]*WindowedEvent, 0),
			size:     config.WindowSize,
			maxItems: MaxTemporalItems,
		},
		config: config,
	}
}

// Process implements the Correlator interface
func (t *TemporalCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	// Validate input
	if event == nil {
		return nil, fmt.Errorf("event is nil")
	}

	// Add event to window
	t.eventWindow.Add(event)

	// Clean old events
	t.eventWindow.Clean()

	// Find temporal correlations
	correlations := t.findTemporalCorrelations(event)

	// Update patterns
	t.updatePatterns(event)

	// Clean old patterns periodically
	t.cleanOldPatterns()

	return correlations, nil
}

// Name returns the correlator name
func (t *TemporalCorrelator) Name() string {
	return "temporal"
}

// findTemporalCorrelations finds time-based patterns
func (t *TemporalCorrelator) findTemporalCorrelations(event *domain.UnifiedEvent) []*CorrelationResult {
	t.eventWindow.mu.RLock()
	defer t.eventWindow.mu.RUnlock()

	var results []*CorrelationResult

	// Look for events that frequently occur together
	eventGroups := t.groupEventsByTimeProximity(event)

	for _, group := range eventGroups {
		if len(group) >= t.config.MinOccurrences {
			result := t.createTemporalCorrelation(event, group)
			if result != nil {
				results = append(results, result)
			}
		}
	}

	// Check against known patterns
	t.patternsMu.RLock()
	for _, pattern := range t.patterns {
		if t.matchesPattern(event, pattern) {
			result := t.createPatternCorrelation(event, pattern)
			if result != nil {
				results = append(results, result)
			}
		}
	}
	t.patternsMu.RUnlock()

	return results
}

// groupEventsByTimeProximity groups events occurring close in time
func (t *TemporalCorrelator) groupEventsByTimeProximity(currentEvent *domain.UnifiedEvent) [][]*domain.UnifiedEvent {
	nearbyEvents := t.findNearbyEvents(currentEvent)
	if len(nearbyEvents) == 0 {
		return nil
	}

	typeGroups := t.groupEventsByType(nearbyEvents)
	return t.filterGroupsBySize(typeGroups)
}

// findNearbyEvents finds events within the proximity time window
func (t *TemporalCorrelator) findNearbyEvents(currentEvent *domain.UnifiedEvent) []*domain.UnifiedEvent {
	proximityWindow := 30 * time.Second
	var nearbyEvents []*domain.UnifiedEvent

	for _, we := range t.eventWindow.events {
		if we.Event.ID == currentEvent.ID {
			continue
		}

		timeDiff := currentEvent.Timestamp.Sub(we.Event.Timestamp).Abs()
		if timeDiff <= proximityWindow {
			nearbyEvents = append(nearbyEvents, we.Event)
		}
	}

	return nearbyEvents
}

// groupEventsByType groups events by their type and source
func (t *TemporalCorrelator) groupEventsByType(events []*domain.UnifiedEvent) map[string][]*domain.UnifiedEvent {
	typeGroups := make(map[string][]*domain.UnifiedEvent)
	for _, event := range events {
		key := fmt.Sprintf("%s-%s", event.Type, event.Source)
		typeGroups[key] = append(typeGroups[key], event)
	}
	return typeGroups
}

// filterGroupsBySize filters groups by minimum occurrence threshold
func (t *TemporalCorrelator) filterGroupsBySize(typeGroups map[string][]*domain.UnifiedEvent) [][]*domain.UnifiedEvent {
	var groups [][]*domain.UnifiedEvent
	for _, group := range typeGroups {
		if len(group) >= t.config.MinOccurrences-1 { // -1 because current event will be added
			groups = append(groups, group)
		}
	}
	return groups
}

// createTemporalCorrelation creates a correlation from time-grouped events
func (t *TemporalCorrelator) createTemporalCorrelation(current *domain.UnifiedEvent, group []*domain.UnifiedEvent) *CorrelationResult {
	if len(group) == 0 {
		return nil
	}

	// Calculate pattern confidence based on consistency
	confidence := t.calculatePatternConfidence(current, group)
	if confidence < MinConfidenceThreshold {
		return nil
	}

	// Find time pattern
	avgTimeDelta := t.calculateAverageTimeDelta(append(group, current))

	result := &CorrelationResult{
		ID:         fmt.Sprintf("temporal-%s-%d", current.ID, time.Now().UnixNano()),
		Type:       "temporal_pattern",
		Confidence: confidence,
		Events:     append(getEventIDs(group), current.ID),
		Summary:    fmt.Sprintf("Temporal pattern detected: %s events occur every ~%s", current.Type, formatDuration(avgTimeDelta)),
		Details: CorrelationDetails{
			Pattern:        fmt.Sprintf("%s temporal pattern", current.Type),
			Algorithm:      "temporal_pattern_detector",
			ProcessingTime: avgTimeDelta,
			DataPoints:     len(group) + 1,
		},
		Evidence: CreateEvidenceData(
			append(getEventIDs(group), current.ID),
			[]string{}, // would extract resource IDs if needed
			map[string]string{
				"description":      t.buildTemporalDescription(current, group, avgTimeDelta),
				"pattern_interval": formatDuration(avgTimeDelta),
				"event_count":      fmt.Sprintf("%d", len(group)+1),
			},
		),
		StartTime: getEarliestTime(append(group, current)),
		EndTime:   current.Timestamp,
	}

	// Identify root cause (usually the first event)
	result.RootCause = t.identifyTemporalRootCause(current, group)
	result.Impact = t.assessTemporalImpact(current, group)

	return result
}

// createPatternCorrelation creates a correlation from a known pattern
func (t *TemporalCorrelator) createPatternCorrelation(event *domain.UnifiedEvent, pattern *TemporalPattern) *CorrelationResult {
	result := &CorrelationResult{
		ID:         fmt.Sprintf("temporal-pattern-%s-%d", event.ID, time.Now().UnixNano()),
		Type:       "temporal_recurrence",
		Confidence: pattern.Confidence,
		Events:     []string{event.ID, pattern.LastEvent.ID},
		Summary:    fmt.Sprintf("Recurring pattern: %s occurs every ~%s", event.Type, formatDuration(pattern.TimeDelta)),
		Details: CorrelationDetails{
			Pattern:        fmt.Sprintf("%s recurrence pattern", event.Type),
			Algorithm:      "temporal_recurrence_detector",
			ProcessingTime: pattern.TimeDelta,
			DataPoints:     int(pattern.Occurrences + 1),
		},
		Evidence: CreateEvidenceData(
			[]string{event.ID, pattern.LastEvent.ID},
			[]string{},
			map[string]string{
				"pattern_occurrences": fmt.Sprintf("%d", pattern.Occurrences),
				"average_interval":    formatDuration(pattern.TimeDelta),
				"pattern_confidence":  fmt.Sprintf("%.2f%%", pattern.Confidence*100),
				"event_type":          string(event.Type),
			},
		),
		StartTime: pattern.FirstEvent.Timestamp,
		EndTime:   event.Timestamp,
	}

	return result
}

// updatePatterns updates known patterns with new event
func (t *TemporalCorrelator) updatePatterns(event *domain.UnifiedEvent) {
	t.patternsMu.Lock()
	defer t.patternsMu.Unlock()

	patternKey := fmt.Sprintf("%s-%s", event.Type, event.Source)

	if pattern, exists := t.patterns[patternKey]; exists {
		t.updateExistingPattern(pattern, event)
	} else {
		t.createNewPatternIfEligible(patternKey, event)
	}

	// Limit number of patterns
	if len(t.patterns) > t.config.MaxPatternsTracked {
		t.evictOldestPattern()
	}
}

// updateExistingPattern updates an existing temporal pattern with a new event
func (t *TemporalCorrelator) updateExistingPattern(pattern *TemporalPattern, event *domain.UnifiedEvent) {
	timeDelta := event.Timestamp.Sub(pattern.LastEvent.Timestamp)

	// Update moving average of time delta
	pattern.TimeDelta = (pattern.TimeDelta*time.Duration(pattern.Occurrences) + timeDelta) / time.Duration(pattern.Occurrences+1)
	pattern.Occurrences++
	pattern.LastEvent = t.createEventRef(event)
	pattern.LastSeen = time.Now()

	// Update confidence based on consistency
	pattern.Confidence = t.calculatePatternStability(pattern)
}

// createNewPatternIfEligible creates a new pattern if there are enough similar events
func (t *TemporalCorrelator) createNewPatternIfEligible(patternKey string, event *domain.UnifiedEvent) {
	t.eventWindow.mu.RLock()
	similarEvents := t.findSimilarEvents(event)
	t.eventWindow.mu.RUnlock()

	if len(similarEvents) >= t.config.MinOccurrences-1 {
		t.patterns[patternKey] = t.createNewPattern(patternKey, event)
	}
}

// createEventRef creates an EventRef from a domain event
func (t *TemporalCorrelator) createEventRef(event *domain.UnifiedEvent) EventRef {
	return EventRef{
		ID:        event.ID,
		Type:      string(event.Type),
		Timestamp: event.Timestamp,
		Resource:  t.getResourceName(event),
	}
}

// createNewPattern creates a new temporal pattern from an event
func (t *TemporalCorrelator) createNewPattern(patternKey string, event *domain.UnifiedEvent) *TemporalPattern {
	eventRef := t.createEventRef(event)
	return &TemporalPattern{
		ID:          patternKey,
		FirstEvent:  eventRef,
		LastEvent:   eventRef,
		Occurrences: 1,
		TimeDelta:   0,
		Confidence:  InitialConfidence,
		LastSeen:    time.Now(),
	}
}

// Helper methods

func (t *TemporalCorrelator) calculatePatternConfidence(current *domain.UnifiedEvent, group []*domain.UnifiedEvent) float64 {
	if len(group) < t.config.MinOccurrences-1 {
		return 0
	}

	// Base confidence on occurrence count
	baseConfidence := float64(len(group)) / float64(t.config.MinOccurrences*2)
	if baseConfidence > MaxConfidenceValue {
		baseConfidence = MaxConfidenceValue
	}

	// Adjust based on time consistency
	timeConsistency := t.calculateTimeConsistency(append(group, current))

	// Combine factors
	confidence := baseConfidence*BaseWeightRatio + timeConsistency*TimeWeightRatio

	return confidence
}

func (t *TemporalCorrelator) calculateTimeConsistency(events []*domain.UnifiedEvent) float64 {
	if len(events) < 2 {
		return 0
	}

	// Sort events by time
	// Calculate variance in time deltas
	var deltas []time.Duration
	for i := 1; i < len(events); i++ {
		delta := events[i].Timestamp.Sub(events[i-1].Timestamp)
		deltas = append(deltas, delta)
	}

	if len(deltas) == 0 {
		return 0
	}

	// Calculate mean
	var sum time.Duration
	for _, d := range deltas {
		sum += d
	}
	mean := sum / time.Duration(len(deltas))

	// Calculate variance
	var variance float64
	for _, d := range deltas {
		diff := float64(d - mean)
		variance += diff * diff
	}
	variance /= float64(len(deltas))

	// Convert to consistency score (lower variance = higher consistency)
	// Normalize to 0-1 range
	consistency := 1.0 / (1.0 + variance/float64(mean*mean))

	return consistency
}

func (t *TemporalCorrelator) calculateAverageTimeDelta(events []*domain.UnifiedEvent) time.Duration {
	if len(events) < 2 {
		return 0
	}

	var totalDelta time.Duration
	count := 0

	for i := 1; i < len(events); i++ {
		delta := events[i].Timestamp.Sub(events[i-1].Timestamp)
		totalDelta += delta
		count++
	}

	if count == 0 {
		return 0
	}

	return totalDelta / time.Duration(count)
}

func (t *TemporalCorrelator) buildTemporalDescription(current *domain.UnifiedEvent, group []*domain.UnifiedEvent, avgDelta time.Duration) string {
	return fmt.Sprintf("Detected temporal correlation: %d occurrences of '%s' events with average interval of %s. "+
		"This pattern suggests a recurring issue or scheduled activity.",
		len(group)+1, current.Type, formatDuration(avgDelta))
}

func (t *TemporalCorrelator) buildTemporalEvidence(current *domain.UnifiedEvent, group []*domain.UnifiedEvent) []string {
	evidence := []string{
		fmt.Sprintf("Event type: %s", current.Type),
		fmt.Sprintf("Occurrences: %d", len(group)+1),
		fmt.Sprintf("Time span: %s", current.Timestamp.Sub(getEarliestTime(group)).String()),
	}

	// Add sample timestamps
	for i, event := range group {
		if i >= 3 {
			evidence = append(evidence, fmt.Sprintf("... and %d more occurrences", len(group)-3))
			break
		}
		evidence = append(evidence, fmt.Sprintf("- %s at %s", event.Type, event.Timestamp.Format(time.RFC3339)))
	}

	return evidence
}

func (t *TemporalCorrelator) identifyTemporalRootCause(current *domain.UnifiedEvent, group []*domain.UnifiedEvent) *RootCause {
	// In temporal patterns, the first occurrence is often the trigger
	earliest := current
	for _, event := range group {
		if event.Timestamp.Before(earliest.Timestamp) {
			earliest = event
		}
	}

	return &RootCause{
		EventID:     earliest.ID,
		Confidence:  MediumLowConfidence,
		Description: fmt.Sprintf("First occurrence of %s pattern", earliest.Type),
		Evidence: CreateEvidenceData(
			[]string{earliest.ID},
			[]string{},
			map[string]string{
				"occurred_at":  earliest.Timestamp.Format(time.RFC3339),
				"pattern_note": "Temporal patterns often indicate scheduled or triggered events",
				"event_type":   string(earliest.Type),
			},
		),
	}
}

func (t *TemporalCorrelator) assessTemporalImpact(current *domain.UnifiedEvent, group []*domain.UnifiedEvent) *Impact {
	impact := &Impact{
		Severity:  current.Severity,
		Resources: make([]string, 0),
		Services:  make([]ServiceReference, 0),
	}

	// Collect affected resources
	resourceMap := make(map[string]bool)
	allEvents := append(group, current)

	for _, event := range allEvents {
		if resource := t.getResourceName(event); resource != "" {
			resourceMap[resource] = true
		}

		// Upgrade severity if pattern shows escalation
		if event.Severity > impact.Severity {
			impact.Severity = event.Severity
		}
	}

	for resource := range resourceMap {
		impact.Resources = append(impact.Resources, resource)
	}

	return impact
}

func (t *TemporalCorrelator) matchesPattern(event *domain.UnifiedEvent, pattern *TemporalPattern) bool {
	// Check if event matches pattern type
	if string(event.Type) != pattern.LastEvent.Type {
		return false
	}

	// Check if timing matches pattern (within 20% variance)
	if pattern.TimeDelta > 0 {
		timeSinceLastEvent := event.Timestamp.Sub(pattern.LastEvent.Timestamp)
		variance := float64(timeSinceLastEvent-pattern.TimeDelta) / float64(pattern.TimeDelta)
		varianceThreshold := 0.2
		if variance > varianceThreshold || variance < -varianceThreshold {
			return false
		}
	}

	return true
}

func (t *TemporalCorrelator) calculatePatternStability(pattern *TemporalPattern) float64 {
	// Stability based on:
	// 1. Number of occurrences
	// 2. Consistency of time deltas
	// 3. Recency

	occurrenceScore := float64(pattern.Occurrences) / float64(t.config.MinOccurrences*5)
	if occurrenceScore > MaxConfidenceValue {
		occurrenceScore = MaxConfidenceValue
	}

	// Recency score (patterns seen recently are more reliable)
	recencyScore := MaxConfidenceValue
	hoursSinceLastSeen := time.Since(pattern.LastSeen).Hours()
	if hoursSinceLastSeen > 1 {
		recencyScore = MaxConfidenceValue / (MaxConfidenceValue + hoursSinceLastSeen/24)
	}

	// Combine scores
	stability := occurrenceScore*BaseWeightRatio + recencyScore*TimeWeightRatio

	return stability
}

func (t *TemporalCorrelator) findSimilarEvents(event *domain.UnifiedEvent) []*domain.UnifiedEvent {
	var similar []*domain.UnifiedEvent

	for _, we := range t.eventWindow.events {
		if we.Event.Type == event.Type && we.Event.Source == event.Source {
			similar = append(similar, we.Event)
		}
	}

	return similar
}

func (t *TemporalCorrelator) cleanOldPatterns() {
	t.patternsMu.Lock()
	defer t.patternsMu.Unlock()

	cutoff := time.Now().Add(-t.config.PatternTimeout)

	for key, pattern := range t.patterns {
		if pattern.LastSeen.Before(cutoff) {
			delete(t.patterns, key)
		}
	}
}

func (t *TemporalCorrelator) evictOldestPattern() {
	var oldestKey string
	var oldestTime time.Time

	for key, pattern := range t.patterns {
		if oldestKey == "" || pattern.LastSeen.Before(oldestTime) {
			oldestKey = key
			oldestTime = pattern.LastSeen
		}
	}

	if oldestKey != "" {
		delete(t.patterns, oldestKey)
	}
}

func (t *TemporalCorrelator) getResourceName(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil {
		return fmt.Sprintf("%s/%s/%s", event.K8sContext.Kind, event.K8sContext.Namespace, event.K8sContext.Name)
	}
	return ""
}

// TimeWindow methods

func (tw *TimeWindow) Add(event *domain.UnifiedEvent) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	tw.events = append(tw.events, &WindowedEvent{
		Event:   event,
		AddedAt: time.Now(),
	})

	// Limit size
	if len(tw.events) > tw.maxItems {
		tw.events = tw.events[len(tw.events)-tw.maxItems:]
	}
}

func (tw *TimeWindow) Clean() {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	cutoff := time.Now().Add(-tw.size)

	// Find first event that's within window
	firstValid := 0
	for i, we := range tw.events {
		if we.Event.Timestamp.After(cutoff) {
			firstValid = i
			break
		}
	}

	// Keep only valid events
	if firstValid > 0 {
		tw.events = tw.events[firstValid:]
	}
}

// Utility functions

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	} else if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
}

func getEarliestTime(events []*domain.UnifiedEvent) time.Time {
	if len(events) == 0 {
		return time.Now()
	}

	earliest := events[0].Timestamp
	for _, e := range events[1:] {
		if e.Timestamp.Before(earliest) {
			earliest = e.Timestamp
		}
	}
	return earliest
}
