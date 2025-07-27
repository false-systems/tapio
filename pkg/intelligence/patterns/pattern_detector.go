package patterns

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// PatternDetector detects behavioral patterns in K8s events
type PatternDetector struct {
	library         *K8sPatternLibrary
	eventBuffer     *EventBuffer
	detectionEngine *DetectionEngine
	logger          *zap.Logger

	// Pattern detection state
	activePatterns map[string]*ActivePattern
	mu             sync.RWMutex

	// Metrics
	detectionMetrics *DetectionMetrics
}

// EventBuffer maintains a sliding window of events for pattern detection
type EventBuffer struct {
	events  []*domain.UnifiedEvent
	index   map[string][]*domain.UnifiedEvent // Index by entity
	maxSize int
	window  time.Duration
	mu      sync.RWMutex
}

// ActivePattern tracks an ongoing pattern
type ActivePattern struct {
	Pattern    *K8sPattern
	StartTime  time.Time
	LastSeen   time.Time
	Events     []*domain.UnifiedEvent
	MatchCount int
	Confidence float64
	State      PatternState
}

// PatternState represents the state of a detected pattern
type PatternState string

const (
	PatternStateActive    PatternState = "active"
	PatternStateResolved  PatternState = "resolved"
	PatternStateEscalated PatternState = "escalated"
)

// DetectionEngine runs pattern detection algorithms
type DetectionEngine struct {
	sequenceDetector  *SequenceDetector
	frequencyAnalyzer *FrequencyAnalyzer
	anomalyDetector   *AnomalyDetector
}

// DetectionMetrics tracks pattern detection performance
type DetectionMetrics struct {
	PatternsDetected   int64
	FalsePositives     int64
	MissedPatterns     int64
	DetectionLatency   time.Duration
	ActivePatternCount int
}

// NewPatternDetector creates a new pattern detector
func NewPatternDetector(logger *zap.Logger) *PatternDetector {
	return &PatternDetector{
		library:          NewK8sPatternLibrary(),
		eventBuffer:      NewEventBuffer(10000, 1*time.Hour),
		detectionEngine:  NewDetectionEngine(),
		logger:           logger,
		activePatterns:   make(map[string]*ActivePattern),
		detectionMetrics: &DetectionMetrics{},
	}
}

// Process analyzes an event for patterns
func (d *PatternDetector) Process(ctx context.Context, event *domain.UnifiedEvent) (*PatternDetectionResult, error) {
	// Add event to buffer
	d.eventBuffer.Add(event)

	// Check for pattern matches
	matches := d.library.MatchEvent(event)

	// Process matches
	result := &PatternDetectionResult{
		Event:    event,
		Matches:  matches,
		Patterns: make([]*domain.Pattern, 0),
	}

	for _, match := range matches {
		// Update or create active pattern
		activePattern := d.updateActivePattern(match, event)

		// Convert to domain pattern
		pattern := d.convertToDomainPattern(activePattern, match)
		result.Patterns = append(result.Patterns, pattern)

		// Check if pattern needs escalation
		if d.shouldEscalate(activePattern) {
			// Update the pattern's metadata to indicate escalation
			pattern.Metadata["severity"] = "critical"
			pattern.Metadata["escalated"] = true
			d.escalatePattern(activePattern)
		}
	}

	// Run advanced detection
	d.runAdvancedDetection(ctx, event, result)

	// Update metrics
	d.updateMetrics(result)

	return result, nil
}

// PatternDetectionResult contains pattern detection results
type PatternDetectionResult struct {
	Event     *domain.UnifiedEvent
	Matches   []*PatternMatch
	Patterns  []*domain.Pattern
	Sequences []*SequencePattern
	Anomalies []*AnomalyPattern
}

// updateActivePattern updates or creates an active pattern
func (d *PatternDetector) updateActivePattern(match *PatternMatch, event *domain.UnifiedEvent) *ActivePattern {
	d.mu.Lock()
	defer d.mu.Unlock()

	patternKey := d.generatePatternKey(match.Pattern, event)

	active, exists := d.activePatterns[patternKey]
	if !exists {
		active = &ActivePattern{
			Pattern:   match.Pattern,
			StartTime: event.Timestamp,
			Events:    make([]*domain.UnifiedEvent, 0),
			State:     PatternStateActive,
		}
		d.activePatterns[patternKey] = active
	}

	// Update pattern
	active.LastSeen = event.Timestamp
	active.Events = append(active.Events, event)
	active.MatchCount++
	active.Confidence = match.Confidence

	return active
}

// generatePatternKey creates a unique key for pattern tracking
func (d *PatternDetector) generatePatternKey(pattern *K8sPattern, event *domain.UnifiedEvent) string {
	entity := "unknown"
	if event.Entity != nil {
		entity = fmt.Sprintf("%s/%s", event.Entity.Namespace, event.Entity.Name)
	} else if event.Kubernetes != nil && event.Kubernetes.Object != "" {
		entity = event.Kubernetes.Object
	}

	return fmt.Sprintf("%s:%s", pattern.ID, entity)
}

// convertToDomainPattern converts internal pattern to domain pattern
func (d *PatternDetector) convertToDomainPattern(active *ActivePattern, match *PatternMatch) *domain.Pattern {
	metadata := make(map[string]interface{})
	metadata["confidence"] = match.Confidence
	metadata["severity"] = active.Pattern.Impact.Severity
	metadata["first_seen"] = active.StartTime
	metadata["last_seen"] = active.LastSeen
	metadata["event_count"] = active.MatchCount
	metadata["state"] = string(active.State)
	metadata["context"] = match.Context

	return &domain.Pattern{
		ID:          active.Pattern.ID,
		Type:        string(active.Pattern.Category),
		Name:        active.Pattern.Name,
		Description: active.Pattern.Description,
		Metadata:    metadata,
	}
}

// shouldEscalate determines if a pattern needs escalation
func (d *PatternDetector) shouldEscalate(pattern *ActivePattern) bool {
	// Escalate if pattern is critical and active for more than 5 minutes
	if pattern.Pattern.Impact.Severity == "critical" {
		duration := pattern.LastSeen.Sub(pattern.StartTime)
		return duration > 5*time.Minute
	}

	// Escalate if pattern has high match count
	if pattern.MatchCount > 10 {
		return true
	}

	// Escalate if confidence is very high
	if pattern.Confidence > 0.9 {
		return true
	}

	return false
}

// escalatePattern handles pattern escalation
func (d *PatternDetector) escalatePattern(pattern *ActivePattern) {
	pattern.State = PatternStateEscalated

	d.logger.Warn("Pattern escalated",
		zap.String("pattern", pattern.Pattern.ID),
		zap.String("name", pattern.Pattern.Name),
		zap.Int("event_count", pattern.MatchCount),
		zap.Float64("confidence", pattern.Confidence),
	)

	// In a real system, this would trigger alerts, notifications, etc.
}

// runAdvancedDetection runs sequence and anomaly detection
func (d *PatternDetector) runAdvancedDetection(ctx context.Context, event *domain.UnifiedEvent, result *PatternDetectionResult) {
	// Get related events from buffer
	relatedEvents := d.eventBuffer.GetRelatedEvents(event, 5*time.Minute)

	// Detect sequences
	if sequences := d.detectionEngine.sequenceDetector.DetectSequences(event, relatedEvents); len(sequences) > 0 {
		result.Sequences = sequences
	}

	// Detect anomalies
	if anomalies := d.detectionEngine.anomalyDetector.DetectAnomalies(event, relatedEvents); len(anomalies) > 0 {
		result.Anomalies = anomalies
	}
}

// updateMetrics updates detection metrics
func (d *PatternDetector) updateMetrics(result *PatternDetectionResult) {
	if len(result.Patterns) > 0 {
		d.detectionMetrics.PatternsDetected += int64(len(result.Patterns))
	}

	d.mu.RLock()
	d.detectionMetrics.ActivePatternCount = len(d.activePatterns)
	d.mu.RUnlock()
}

// GetActivePatterns returns currently active patterns
func (d *PatternDetector) GetActivePatterns() []*ActivePattern {
	d.mu.RLock()
	defer d.mu.RUnlock()

	patterns := make([]*ActivePattern, 0, len(d.activePatterns))
	for _, pattern := range d.activePatterns {
		if pattern.State == PatternStateActive {
			patterns = append(patterns, pattern)
		}
	}

	return patterns
}

// CleanupInactivePatterns removes old inactive patterns
func (d *PatternDetector) CleanupInactivePatterns(maxAge time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for key, pattern := range d.activePatterns {
		if now.Sub(pattern.LastSeen) > maxAge {
			delete(d.activePatterns, key)
		}
	}
}

// EventBuffer implementation

func NewEventBuffer(maxSize int, window time.Duration) *EventBuffer {
	return &EventBuffer{
		events:  make([]*domain.UnifiedEvent, 0, maxSize),
		index:   make(map[string][]*domain.UnifiedEvent),
		maxSize: maxSize,
		window:  window,
	}
}

func (b *EventBuffer) Add(event *domain.UnifiedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Add to main buffer
	b.events = append(b.events, event)

	// Maintain max size
	if len(b.events) > b.maxSize {
		b.events = b.events[1:]
	}

	// Update index
	entityKey := b.getEntityKey(event)
	b.index[entityKey] = append(b.index[entityKey], event)

	// Clean old events
	b.cleanOldEvents()
}

func (b *EventBuffer) GetRelatedEvents(event *domain.UnifiedEvent, window time.Duration) []*domain.UnifiedEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	entityKey := b.getEntityKey(event)
	events := b.index[entityKey]

	// Filter by time window
	startTime := event.Timestamp.Add(-window)
	var related []*domain.UnifiedEvent

	for _, e := range events {
		if e.Timestamp.After(startTime) && e.ID != event.ID {
			related = append(related, e)
		}
	}

	return related
}

func (b *EventBuffer) getEntityKey(event *domain.UnifiedEvent) string {
	if event.Entity != nil {
		return fmt.Sprintf("%s/%s", event.Entity.Namespace, event.Entity.Name)
	}
	if event.Kubernetes != nil && event.Kubernetes.Object != "" {
		return event.Kubernetes.Object
	}
	return "unknown"
}

func (b *EventBuffer) cleanOldEvents() {
	cutoff := time.Now().Add(-b.window)

	// Clean main buffer
	newEvents := make([]*domain.UnifiedEvent, 0, len(b.events))
	for _, event := range b.events {
		if event.Timestamp.After(cutoff) {
			newEvents = append(newEvents, event)
		}
	}
	b.events = newEvents

	// Clean index
	for key, events := range b.index {
		newIndexEvents := make([]*domain.UnifiedEvent, 0, len(events))
		for _, event := range events {
			if event.Timestamp.After(cutoff) {
				newIndexEvents = append(newIndexEvents, event)
			}
		}
		if len(newIndexEvents) > 0 {
			b.index[key] = newIndexEvents
		} else {
			delete(b.index, key)
		}
	}
}

// DetectionEngine implementation

func NewDetectionEngine() *DetectionEngine {
	return &DetectionEngine{
		sequenceDetector:  NewSequenceDetector(),
		frequencyAnalyzer: NewFrequencyAnalyzer(),
		anomalyDetector:   NewAnomalyDetector(),
	}
}

// SequenceDetector detects event sequences

type SequenceDetector struct{}

func NewSequenceDetector() *SequenceDetector {
	return &SequenceDetector{}
}

// SequencePattern is defined in learning_core.go

func (s *SequenceDetector) DetectSequences(event *domain.UnifiedEvent, relatedEvents []*domain.UnifiedEvent) []*SequencePattern {
	// Implement sequence detection logic
	// For now, return empty
	return nil
}

// FrequencyAnalyzer analyzes event frequency

type FrequencyAnalyzer struct{}

func NewFrequencyAnalyzer() *FrequencyAnalyzer {
	return &FrequencyAnalyzer{}
}

// AnomalyDetector detects anomalous patterns

type AnomalyDetector struct{}

func NewAnomalyDetector() *AnomalyDetector {
	return &AnomalyDetector{}
}

func (a *AnomalyDetector) DetectAnomalies(event *domain.UnifiedEvent, relatedEvents []*domain.UnifiedEvent) []*AnomalyPattern {
	// Implement anomaly detection logic
	// For now, return empty
	return nil
}
