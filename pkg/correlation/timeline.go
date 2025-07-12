package correlation

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Timeline represents a unified timeline of events from multiple sources
type Timeline struct {
	events     []TimelineEvent
	eventIndex map[string][]int // source -> event indices
	timeRange  TimeRange
	sources    map[string]SourceInfo
	mutex      sync.RWMutex
	maxEvents  int
}

// TimelineEvent represents a single event in the correlation timeline
type TimelineEvent struct {
	ID           string
	Timestamp    time.Time
	Source       SourceType
	EventType    string
	Severity     string
	Message      string
	Entity       EntityReference
	Metadata     map[string]interface{}
	Correlations []string // IDs of correlated events
}

// EntityReference identifies the entity associated with an event
type EntityReference struct {
	Type      string // pod, container, service, node, process
	Name      string
	Namespace string
	UID       string
	Labels    map[string]string
}

// TimeRange represents a time range for filtering
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// SourceInfo tracks information about an event source
type SourceInfo struct {
	Type       SourceType
	EventCount int
	FirstEvent time.Time
	LastEvent  time.Time
	EventTypes map[string]int
}

// NewTimeline creates a new timeline
func NewTimeline(maxEvents int) *Timeline {
	return &Timeline{
		events:     make([]TimelineEvent, 0, maxEvents),
		eventIndex: make(map[string][]int),
		sources:    make(map[string]SourceInfo),
		maxEvents:  maxEvents,
	}
}

// AddEvent adds an event to the timeline
func (t *Timeline) AddEvent(event TimelineEvent) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Generate ID if not provided
	if event.ID == "" {
		event.ID = fmt.Sprintf("%s_%d_%d", event.Source, event.Timestamp.UnixNano(), len(t.events))
	}

	// Check capacity
	if len(t.events) >= t.maxEvents {
		// Remove oldest events (simple FIFO for now)
		t.events = t.events[1:]
		// Rebuild index
		t.rebuildIndex()
	}

	// Add event
	index := len(t.events)
	t.events = append(t.events, event)

	// Update index
	source := string(event.Source)
	t.eventIndex[source] = append(t.eventIndex[source], index)

	// Update source info
	info := t.sources[source]
	info.Type = event.Source
	info.EventCount++
	if info.FirstEvent.IsZero() || event.Timestamp.Before(info.FirstEvent) {
		info.FirstEvent = event.Timestamp
	}
	if event.Timestamp.After(info.LastEvent) {
		info.LastEvent = event.Timestamp
	}
	if info.EventTypes == nil {
		info.EventTypes = make(map[string]int)
	}
	info.EventTypes[event.EventType]++
	t.sources[source] = info

	// Update time range
	if t.timeRange.Start.IsZero() || event.Timestamp.Before(t.timeRange.Start) {
		t.timeRange.Start = event.Timestamp
	}
	if event.Timestamp.After(t.timeRange.End) {
		t.timeRange.End = event.Timestamp
	}

	return nil
}

// GetEvents returns events within the specified time range
func (t *Timeline) GetEvents(timeRange *TimeRange, filters ...EventFilter) []TimelineEvent {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	var result []TimelineEvent

	for _, event := range t.events {
		// Apply time range filter
		if timeRange != nil {
			if event.Timestamp.Before(timeRange.Start) || event.Timestamp.After(timeRange.End) {
				continue
			}
		}

		// Apply additional filters
		include := true
		for _, filter := range filters {
			if !filter(event) {
				include = false
				break
			}
		}

		if include {
			result = append(result, event)
		}
	}

	return result
}

// GetEventsBySource returns events from a specific source
func (t *Timeline) GetEventsBySource(source SourceType, limit int) []TimelineEvent {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	indices, exists := t.eventIndex[string(source)]
	if !exists {
		return nil
	}

	var result []TimelineEvent
	start := 0
	if limit > 0 && len(indices) > limit {
		start = len(indices) - limit
	}

	for i := start; i < len(indices); i++ {
		result = append(result, t.events[indices[i]])
	}

	return result
}

// GetEventsByEntity returns events related to a specific entity
func (t *Timeline) GetEventsByEntity(entityType, entityName string) []TimelineEvent {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	var result []TimelineEvent
	for _, event := range t.events {
		if event.Entity.Type == entityType && event.Entity.Name == entityName {
			result = append(result, event)
		}
	}

	return result
}

// GetCorrelatedEvents returns events correlated with the specified event
func (t *Timeline) GetCorrelatedEvents(eventID string) []TimelineEvent {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	// Find the event
	var targetEvent *TimelineEvent
	for i := range t.events {
		if t.events[i].ID == eventID {
			targetEvent = &t.events[i]
			break
		}
	}

	if targetEvent == nil {
		return nil
	}

	var result []TimelineEvent

	// Get directly correlated events
	for _, correlatedID := range targetEvent.Correlations {
		for _, event := range t.events {
			if event.ID == correlatedID {
				result = append(result, event)
				break
			}
		}
	}

	// Also find events that correlate to this one
	for _, event := range t.events {
		if event.ID == eventID {
			continue
		}
		for _, correlatedID := range event.Correlations {
			if correlatedID == eventID {
				result = append(result, event)
				break
			}
		}
	}

	return result
}

// GetTimeRange returns the time range of events in the timeline
func (t *Timeline) GetTimeRange() TimeRange {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.timeRange
}

// GetSourceInfo returns information about event sources
func (t *Timeline) GetSourceInfo() map[string]SourceInfo {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	// Return a copy
	result := make(map[string]SourceInfo)
	for k, v := range t.sources {
		result[k] = v
	}
	return result
}

// GetStatistics returns timeline statistics
func (t *Timeline) GetStatistics() TimelineStatistics {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	stats := TimelineStatistics{
		TotalEvents:      len(t.events),
		TimeRange:        t.timeRange,
		EventsBySeverity: make(map[string]int),
		EventsByType:     make(map[string]int),
		EventsBySource:   make(map[string]int),
	}

	for _, event := range t.events {
		stats.EventsBySeverity[event.Severity]++
		stats.EventsByType[event.EventType]++
		stats.EventsBySource[string(event.Source)]++
	}

	return stats
}

// TimelineStatistics represents statistics about the timeline
type TimelineStatistics struct {
	TotalEvents      int
	TimeRange        TimeRange
	EventsBySeverity map[string]int
	EventsByType     map[string]int
	EventsBySource   map[string]int
}

// EventFilter is a function that filters timeline events
type EventFilter func(TimelineEvent) bool

// SourceFilter creates a filter for specific sources
func SourceFilter(sources ...SourceType) EventFilter {
	sourceMap := make(map[SourceType]bool)
	for _, source := range sources {
		sourceMap[source] = true
	}
	return func(event TimelineEvent) bool {
		return sourceMap[event.Source]
	}
}

// SeverityFilter creates a filter for specific severities
func SeverityFilter(severities ...string) EventFilter {
	severityMap := make(map[string]bool)
	for _, severity := range severities {
		severityMap[severity] = true
	}
	return func(event TimelineEvent) bool {
		return severityMap[event.Severity]
	}
}

// EntityFilter creates a filter for specific entities
func EntityFilter(entityType, entityName string) EventFilter {
	return func(event TimelineEvent) bool {
		return event.Entity.Type == entityType && event.Entity.Name == entityName
	}
}

// rebuildIndex rebuilds the event index after modifications
func (t *Timeline) rebuildIndex() {
	t.eventIndex = make(map[string][]int)
	for i, event := range t.events {
		source := string(event.Source)
		t.eventIndex[source] = append(t.eventIndex[source], i)
	}
}

// Sort sorts events by timestamp
func (t *Timeline) Sort() {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	sort.Slice(t.events, func(i, j int) bool {
		return t.events[i].Timestamp.Before(t.events[j].Timestamp)
	})

	// Rebuild index after sorting
	t.rebuildIndex()
}

// Clear removes all events from the timeline
func (t *Timeline) Clear() {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.events = t.events[:0]
	t.eventIndex = make(map[string][]int)
	t.sources = make(map[string]SourceInfo)
	t.timeRange = TimeRange{}
}

// TimeWindow represents a time window for correlation
type TimeWindow struct {
	Start    time.Time
	End      time.Time
	Duration time.Duration
}

// GetEventsInWindow returns events within a time window around a reference time
func (t *Timeline) GetEventsInWindow(referenceTime time.Time, windowSize time.Duration) []TimelineEvent {
	window := &TimeRange{
		Start: referenceTime.Add(-windowSize / 2),
		End:   referenceTime.Add(windowSize / 2),
	}
	return t.GetEvents(window)
}

// FindPatterns looks for patterns in the timeline
func (t *Timeline) FindPatterns() []EventPattern {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	var patterns []EventPattern

	// Find burst patterns (many events in short time)
	patterns = append(patterns, t.findBurstPatterns()...)

	// Find repeating patterns
	patterns = append(patterns, t.findRepeatingPatterns()...)

	// Find cascade patterns (one event triggering others)
	patterns = append(patterns, t.findCascadePatterns()...)

	return patterns
}

// EventPattern represents a detected pattern in events
type EventPattern struct {
	Type        string
	Description string
	Events      []string // Event IDs
	Confidence  float64
	TimeRange   TimeRange
	Metadata    map[string]interface{}
}

// findBurstPatterns finds burst patterns in events
func (t *Timeline) findBurstPatterns() []EventPattern {
	var patterns []EventPattern

	// Group events by entity and time windows
	entityBursts := make(map[string][]TimelineEvent)

	for _, event := range t.events {
		key := fmt.Sprintf("%s:%s", event.Entity.Type, event.Entity.Name)
		entityBursts[key] = append(entityBursts[key], event)
	}

	// Check for bursts
	for entity, events := range entityBursts {
		if len(events) < 3 {
			continue
		}

		// Check for events within 1 minute windows
		for i := 0; i < len(events)-2; i++ {
			window := 1 * time.Minute
			count := 1
			var eventIDs []string
			eventIDs = append(eventIDs, events[i].ID)

			for j := i + 1; j < len(events); j++ {
				if events[j].Timestamp.Sub(events[i].Timestamp) <= window {
					count++
					eventIDs = append(eventIDs, events[j].ID)
				} else {
					break
				}
			}

			if count >= 3 {
				patterns = append(patterns, EventPattern{
					Type:        "burst",
					Description: fmt.Sprintf("Burst of %d events for %s", count, entity),
					Events:      eventIDs,
					Confidence:  float64(count) / 10.0,
					TimeRange: TimeRange{
						Start: events[i].Timestamp,
						End:   events[i+count-1].Timestamp,
					},
					Metadata: map[string]interface{}{
						"entity":      entity,
						"event_count": count,
						"duration":    events[i+count-1].Timestamp.Sub(events[i].Timestamp),
					},
				})
			}
		}
	}

	return patterns
}

// findRepeatingPatterns finds repeating event patterns
func (t *Timeline) findRepeatingPatterns() []EventPattern {
	var patterns []EventPattern

	// Group events by type and entity
	eventGroups := make(map[string][]TimelineEvent)

	for _, event := range t.events {
		key := fmt.Sprintf("%s:%s:%s", event.Source, event.EventType, event.Entity.Name)
		eventGroups[key] = append(eventGroups[key], event)
	}

	// Look for regular intervals
	for key, events := range eventGroups {
		if len(events) < 3 {
			continue
		}

		// Calculate intervals
		var intervals []time.Duration
		for i := 1; i < len(events); i++ {
			intervals = append(intervals, events[i].Timestamp.Sub(events[i-1].Timestamp))
		}

		// Check if intervals are similar (within 20% variance)
		if isRegularInterval(intervals) {
			avgInterval := averageDuration(intervals)
			patterns = append(patterns, EventPattern{
				Type:        "repeating",
				Description: fmt.Sprintf("Repeating pattern for %s every %v", key, avgInterval),
				Events:      extractEventIDs(events),
				Confidence:  0.8,
				TimeRange: TimeRange{
					Start: events[0].Timestamp,
					End:   events[len(events)-1].Timestamp,
				},
				Metadata: map[string]interface{}{
					"key":              key,
					"average_interval": avgInterval,
					"occurrences":      len(events),
				},
			})
		}
	}

	return patterns
}

// findCascadePatterns finds cascade patterns (events triggering other events)
func (t *Timeline) findCascadePatterns() []EventPattern {
	var patterns []EventPattern

	// Look for events that frequently occur together within a short time window
	cascadeWindow := 30 * time.Second

	for i, event := range t.events {
		if event.Severity != "error" && event.Severity != "critical" {
			continue
		}

		// Find events that follow within cascade window
		var cascadeEvents []TimelineEvent
		cascadeEvents = append(cascadeEvents, event)

		for j := i + 1; j < len(t.events); j++ {
			if t.events[j].Timestamp.Sub(event.Timestamp) > cascadeWindow {
				break
			}

			// Check if related entity or correlated
			if isRelatedEvent(event, t.events[j]) {
				cascadeEvents = append(cascadeEvents, t.events[j])
			}
		}

		if len(cascadeEvents) >= 3 {
			patterns = append(patterns, EventPattern{
				Type:        "cascade",
				Description: fmt.Sprintf("Cascade pattern starting with %s", event.EventType),
				Events:      extractEventIDs(cascadeEvents),
				Confidence:  0.7,
				TimeRange: TimeRange{
					Start: cascadeEvents[0].Timestamp,
					End:   cascadeEvents[len(cascadeEvents)-1].Timestamp,
				},
				Metadata: map[string]interface{}{
					"trigger_event":  event.ID,
					"cascade_length": len(cascadeEvents),
					"duration":       cascadeEvents[len(cascadeEvents)-1].Timestamp.Sub(cascadeEvents[0].Timestamp),
				},
			})
		}
	}

	return patterns
}

// Helper functions

func isRegularInterval(intervals []time.Duration) bool {
	if len(intervals) < 2 {
		return false
	}

	avg := averageDuration(intervals)
	variance := 0.2 // 20% variance allowed

	for _, interval := range intervals {
		diff := float64(interval - avg)
		if diff < 0 {
			diff = -diff
		}
		if diff > float64(avg)*variance {
			return false
		}
	}

	return true
}

func averageDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range durations {
		total += d
	}

	return total / time.Duration(len(durations))
}

func extractEventIDs(events []TimelineEvent) []string {
	var ids []string
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}

func isRelatedEvent(event1, event2 TimelineEvent) bool {
	// Same entity
	if event1.Entity.Type == event2.Entity.Type && event1.Entity.Name == event2.Entity.Name {
		return true
	}

	// Check correlations
	for _, id := range event1.Correlations {
		if id == event2.ID {
			return true
		}
	}

	// Check namespace for Kubernetes resources
	if event1.Entity.Namespace != "" && event1.Entity.Namespace == event2.Entity.Namespace {
		return true
	}

	return false
}
