package correlation

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// TemporalCorrelator finds time-based correlations between events
type TemporalCorrelator struct {
	logger *zap.Logger

	// Sliding window of recent events
	eventWindow *EventWindow

	// Co-occurrence tracking
	cooccurrence *CoOccurrenceTracker

	// Configuration
	config TemporalConfig
}

// TemporalConfig configures temporal correlation
type TemporalConfig struct {
	WindowSize     time.Duration // How far back to look
	MinOccurrences int           // Minimum times to see pattern
	MinConfidence  float64       // Minimum confidence threshold
	MaxTimeDelta   time.Duration // Maximum time between correlated events
}

// DefaultTemporalConfig returns sensible defaults
func DefaultTemporalConfig() TemporalConfig {
	return TemporalConfig{
		WindowSize:     30 * time.Minute,
		MinOccurrences: 3,
		MinConfidence:  0.7,
		MaxTimeDelta:   5 * time.Minute,
	}
}

// EventWindow maintains a sliding window of events
type EventWindow struct {
	events    []WindowEvent
	maxAge    time.Duration
	maxEvents int
	mu        sync.RWMutex

	// Indexes for fast lookup
	byType   map[string][]int // event type -> indexes
	byEntity map[string][]int // entity -> indexes
	byTime   *TimeIndex       // time-based index
}

// WindowEvent is an event in the window
type WindowEvent struct {
	Event     *domain.UnifiedEvent
	EventKey  string // Quick lookup key
	Timestamp time.Time
	Index     int // Position in window
}

// TimeIndex allows efficient time-range queries
type TimeIndex struct {
	buckets    map[int64][]int // timestamp bucket -> event indexes
	bucketSize time.Duration
}

// CoOccurrenceTracker tracks event co-occurrences
type CoOccurrenceTracker struct {
	pairs map[EventPairKey]*PairStatistics
	mu    sync.RWMutex
}

// EventPairKey identifies a pair of event types
type EventPairKey struct {
	EventA string
	EventB string
}

// PairStatistics tracks statistics for an event pair
type PairStatistics struct {
	Count      int
	TimeDeltas []time.Duration
	LastSeen   time.Time

	// Calculated stats
	AvgDelta    time.Duration
	StdDevDelta time.Duration
	Confidence  float64
}

// NewTemporalCorrelator creates a new temporal correlator
func NewTemporalCorrelator(logger *zap.Logger, config TemporalConfig) *TemporalCorrelator {
	return &TemporalCorrelator{
		logger: logger,
		eventWindow: &EventWindow{
			events:    make([]WindowEvent, 0, 10000),
			maxAge:    config.WindowSize,
			maxEvents: 10000,
			byType:    make(map[string][]int),
			byEntity:  make(map[string][]int),
			byTime: &TimeIndex{
				buckets:    make(map[int64][]int),
				bucketSize: 1 * time.Minute,
			},
		},
		cooccurrence: &CoOccurrenceTracker{
			pairs: make(map[EventPairKey]*PairStatistics),
		},
		config: config,
	}
}

// Process adds an event and finds temporal correlations
func (t *TemporalCorrelator) Process(event *domain.UnifiedEvent) []TemporalCorrelation {
	// Add event to window
	t.eventWindow.Add(event)

	// Find correlated events
	correlations := t.findCorrelations(event)

	// Update co-occurrence statistics
	t.updateCoOccurrences(event)

	// Clean old data
	t.eventWindow.Clean()

	return correlations
}

// TemporalCorrelation represents a time-based correlation
type TemporalCorrelation struct {
	SourceEvent EventReference
	TargetEvent EventReference
	TimeDelta   time.Duration
	Confidence  float64
	Occurrences int
	Pattern     string // "precedes", "follows", "concurrent"
	Explanation string
}

// EventReference is a lightweight event reference
type EventReference struct {
	EventID   string
	EventType string
	Entity    string
	Timestamp time.Time
}

// findCorrelations finds events correlated with the given event
func (t *TemporalCorrelator) findCorrelations(event *domain.UnifiedEvent) []TemporalCorrelation {
	correlations := []TemporalCorrelation{}

	// Get events in time window
	windowStart := event.Timestamp.Add(-t.config.MaxTimeDelta)
	windowEnd := event.Timestamp.Add(t.config.MaxTimeDelta)
	nearbyEvents := t.eventWindow.GetEventsInRange(windowStart, windowEnd)

	eventKey := getEventKey(event)

	// Check each nearby event
	for _, nearby := range nearbyEvents {
		if nearby.Event.ID == event.ID {
			continue // Skip self
		}

		// Check co-occurrence statistics
		pairKey := EventPairKey{
			EventA: getEventKey(nearby.Event),
			EventB: eventKey,
		}

		if stats := t.cooccurrence.GetStats(pairKey); stats != nil && stats.Confidence >= t.config.MinConfidence {
			correlation := TemporalCorrelation{
				SourceEvent: EventReference{
					EventID:   nearby.Event.ID,
					EventType: getEventKey(nearby.Event),
					Entity:    getEntityKey(nearby.Event),
					Timestamp: nearby.Event.Timestamp,
				},
				TargetEvent: EventReference{
					EventID:   event.ID,
					EventType: eventKey,
					Entity:    getEntityKey(event),
					Timestamp: event.Timestamp,
				},
				TimeDelta:   event.Timestamp.Sub(nearby.Event.Timestamp),
				Confidence:  stats.Confidence,
				Occurrences: stats.Count,
				Pattern:     t.getPattern(nearby.Event, event),
				Explanation: t.explainCorrelation(nearby.Event, event, stats),
			}

			correlations = append(correlations, correlation)
		}
	}

	return correlations
}

// updateCoOccurrences updates co-occurrence statistics
func (t *TemporalCorrelator) updateCoOccurrences(event *domain.UnifiedEvent) {
	// Get recent events
	windowStart := event.Timestamp.Add(-t.config.MaxTimeDelta)
	recentEvents := t.eventWindow.GetEventsInRange(windowStart, event.Timestamp)

	eventKey := getEventKey(event)

	// Update statistics for each pair
	for _, recent := range recentEvents {
		if recent.Event.ID == event.ID {
			continue
		}

		pairKey := EventPairKey{
			EventA: getEventKey(recent.Event),
			EventB: eventKey,
		}

		timeDelta := event.Timestamp.Sub(recent.Event.Timestamp)
		t.cooccurrence.Update(pairKey, timeDelta)
	}
}

// EventWindow methods

func (w *EventWindow) Add(event *domain.UnifiedEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Create window event
	we := WindowEvent{
		Event:     event,
		EventKey:  getEventKey(event),
		Timestamp: event.Timestamp,
		Index:     len(w.events),
	}

	// Add to main list
	w.events = append(w.events, we)

	// Update indexes
	w.byType[we.EventKey] = append(w.byType[we.EventKey], we.Index)

	entityKey := getEntityKey(event)
	w.byEntity[entityKey] = append(w.byEntity[entityKey], we.Index)

	// Update time index
	bucket := we.Timestamp.Unix() / int64(w.byTime.bucketSize.Seconds())
	w.byTime.buckets[bucket] = append(w.byTime.buckets[bucket], we.Index)

	// Maintain size limit
	if len(w.events) > w.maxEvents {
		// Remove oldest events
		w.removeOldest(len(w.events) - w.maxEvents)
	}
}

func (w *EventWindow) GetEventsInRange(start, end time.Time) []WindowEvent {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var result []WindowEvent

	// Use time index for efficient lookup
	startBucket := start.Unix() / int64(w.byTime.bucketSize.Seconds())
	endBucket := end.Unix() / int64(w.byTime.bucketSize.Seconds())

	for bucket := startBucket; bucket <= endBucket; bucket++ {
		if indexes, exists := w.byTime.buckets[bucket]; exists {
			for _, idx := range indexes {
				if idx < len(w.events) {
					event := w.events[idx]
					if event.Timestamp.After(start) && event.Timestamp.Before(end) {
						result = append(result, event)
					}
				}
			}
		}
	}

	return result
}

func (w *EventWindow) Clean() {
	w.mu.Lock()
	defer w.mu.Unlock()

	cutoff := time.Now().Add(-w.maxAge)

	// Find first event that's not expired
	firstValid := 0
	for i, event := range w.events {
		if event.Timestamp.After(cutoff) {
			firstValid = i
			break
		}
	}

	if firstValid > 0 {
		w.removeOldest(firstValid)
	}
}

func (w *EventWindow) removeOldest(count int) {
	// Remove from main list
	w.events = w.events[count:]

	// Rebuild indexes
	w.rebuildIndexes()
}

// CoOccurrenceTracker methods

func (c *CoOccurrenceTracker) Update(key EventPairKey, timeDelta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats, exists := c.pairs[key]
	if !exists {
		stats = &PairStatistics{
			TimeDeltas: make([]time.Duration, 0),
		}
		c.pairs[key] = stats
	}

	// Update statistics
	stats.Count++
	stats.TimeDeltas = append(stats.TimeDeltas, timeDelta)
	stats.LastSeen = time.Now()

	// Recalculate stats
	stats.calculate()
}

func (c *CoOccurrenceTracker) GetStats(key EventPairKey) *PairStatistics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.pairs[key]
}

func (s *PairStatistics) calculate() {
	if len(s.TimeDeltas) == 0 {
		return
	}

	// Calculate average
	var sum time.Duration
	for _, delta := range s.TimeDeltas {
		sum += delta
	}
	s.AvgDelta = sum / time.Duration(len(s.TimeDeltas))

	// Calculate standard deviation
	var variance float64
	avgNanos := float64(s.AvgDelta.Nanoseconds())
	for _, delta := range s.TimeDeltas {
		diff := float64(delta.Nanoseconds()) - avgNanos
		variance += diff * diff
	}
	variance /= float64(len(s.TimeDeltas))
	s.StdDevDelta = time.Duration(math.Sqrt(variance))

	// Calculate confidence
	// Based on: count, consistency, recency
	countScore := math.Min(float64(s.Count)/10.0, 1.0)

	// Consistency: lower stddev = higher consistency
	consistencyScore := 1.0
	if s.AvgDelta > 0 {
		relativeStdDev := float64(s.StdDevDelta) / float64(s.AvgDelta)
		consistencyScore = 1.0 / (1.0 + relativeStdDev)
	}

	// Recency: more recent = higher score
	recencyScore := 1.0 / (1.0 + time.Since(s.LastSeen).Hours()/24.0)

	s.Confidence = (countScore + consistencyScore + recencyScore) / 3.0
}

// Helper methods

func getEventKey(event *domain.UnifiedEvent) string {
	// Create a key representing event type
	if event.Kubernetes != nil && event.Kubernetes.Reason != "" {
		return fmt.Sprintf("k8s:%s", event.Kubernetes.Reason)
	}

	if event.Network != nil {
		return fmt.Sprintf("net:%d:%d", event.Network.StatusCode, event.Network.DestPort)
	}

	return fmt.Sprintf("%s:%s", event.Type, event.Source)
}

func getEntityKey(event *domain.UnifiedEvent) string {
	if event.Entity != nil {
		return fmt.Sprintf("%s/%s", event.Entity.Namespace, event.Entity.Name)
	}
	return "unknown"
}

func (t *TemporalCorrelator) getPattern(eventA, eventB *domain.UnifiedEvent) string {
	delta := eventB.Timestamp.Sub(eventA.Timestamp)

	if math.Abs(delta.Seconds()) < 1 {
		return "concurrent"
	} else if delta > 0 {
		return "follows"
	} else {
		return "precedes"
	}
}

func (t *TemporalCorrelator) explainCorrelation(eventA, eventB *domain.UnifiedEvent, stats *PairStatistics) string {
	return fmt.Sprintf(
		"%s consistently %s %s by ~%v (observed %d times with %.0f%% confidence)",
		getEventKey(eventA),
		t.getPattern(eventA, eventB),
		getEventKey(eventB),
		stats.AvgDelta.Round(time.Second),
		stats.Count,
		stats.Confidence*100,
	)
}

func (w *EventWindow) rebuildIndexes() {
	// Clear indexes
	w.byType = make(map[string][]int)
	w.byEntity = make(map[string][]int)
	w.byTime.buckets = make(map[int64][]int)

	// Rebuild
	for i, event := range w.events {
		w.byType[event.EventKey] = append(w.byType[event.EventKey], i)

		entityKey := getEntityKey(event.Event)
		w.byEntity[entityKey] = append(w.byEntity[entityKey], i)

		bucket := event.Timestamp.Unix() / int64(w.byTime.bucketSize.Seconds())
		w.byTime.buckets[bucket] = append(w.byTime.buckets[bucket], i)
	}
}
