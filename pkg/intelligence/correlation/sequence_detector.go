package correlation

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// SequenceDetector finds sequential patterns in event streams
type SequenceDetector struct {
	logger *zap.Logger

	// Active sequences being tracked
	activeSequences map[string]*ActiveSequence

	// Learned sequence patterns
	patterns *SequencePatternStore

	// Configuration
	config SequenceConfig

	mu sync.RWMutex
}

// SequenceConfig configures sequence detection
type SequenceConfig struct {
	MaxSequenceLength int           // Maximum events in a sequence
	MaxTimeGap        time.Duration // Maximum time between sequence events
	MinOccurrences    int           // Minimum times to see pattern
	MinConfidence     float64       // Minimum confidence threshold
	WindowSize        time.Duration // How far back to look
}

// DefaultSequenceConfig returns sensible defaults
func DefaultSequenceConfig() SequenceConfig {
	return SequenceConfig{
		MaxSequenceLength: 5,
		MaxTimeGap:        5 * time.Minute,
		MinOccurrences:    3,
		MinConfidence:     0.7,
		WindowSize:        30 * time.Minute,
	}
}

// ActiveSequence tracks an ongoing sequence
type ActiveSequence struct {
	ID         string
	Events     []SequenceEvent
	StartTime  time.Time
	LastUpdate time.Time
	State      string // "building", "complete", "timeout"
}

// SequenceEvent is an event in a sequence
type SequenceEvent struct {
	EventID   string
	EventType string
	Entity    string
	Timestamp time.Time
	Metadata  map[string]string
}

// SequencePatternStore manages learned patterns
type SequencePatternStore struct {
	patterns map[string]*SequencePattern
	mu       sync.RWMutex
}

// SequencePattern represents a learned sequence
type SequencePattern struct {
	ID          string
	Pattern     []string // Event type sequence
	Occurrences int
	Confidence  float64
	AvgDuration time.Duration
	LastSeen    time.Time

	// Statistics
	Durations []time.Duration
	Entities  map[string]int // Which entities show this pattern
}

// SequenceCorrelation represents a sequence-based correlation
type SequenceCorrelation struct {
	SequenceID  string
	Pattern     *SequencePattern
	Events      []EventReference
	Confidence  float64
	Duration    time.Duration
	Explanation string
}

// NewSequenceDetector creates a new sequence detector
func NewSequenceDetector(logger *zap.Logger, config SequenceConfig) *SequenceDetector {
	return &SequenceDetector{
		logger:          logger,
		activeSequences: make(map[string]*ActiveSequence),
		patterns: &SequencePatternStore{
			patterns: make(map[string]*SequencePattern),
		},
		config: config,
	}
}

// Process adds an event and detects sequences
func (s *SequenceDetector) Process(event *domain.UnifiedEvent) []SequenceCorrelation {
	// Convert to sequence event
	seqEvent := s.toSequenceEvent(event)

	// Update active sequences
	s.updateActiveSequences(seqEvent)

	// Check if this starts a new sequence
	s.checkSequenceStart(seqEvent)

	// Find completed sequences
	correlations := s.findSequenceCorrelations(event)

	// Clean up old sequences
	s.cleanupSequences()

	return correlations
}

// toSequenceEvent converts UnifiedEvent to SequenceEvent
func (s *SequenceDetector) toSequenceEvent(event *domain.UnifiedEvent) SequenceEvent {
	eventType := s.getEventType(event)
	entity := s.getEntityKey(event)

	metadata := make(map[string]string)
	if event.Kubernetes != nil {
		metadata["reason"] = event.Kubernetes.Reason
		metadata["object"] = event.Kubernetes.Object
	}

	return SequenceEvent{
		EventID:   event.ID,
		EventType: eventType,
		Entity:    entity,
		Timestamp: event.Timestamp,
		Metadata:  metadata,
	}
}

// updateActiveSequences adds event to matching sequences
func (s *SequenceDetector) updateActiveSequences(event SequenceEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	for _, seq := range s.activeSequences {
		// Skip completed or timed out sequences
		if seq.State != "building" {
			continue
		}

		// Check time gap
		timeSinceLastEvent := event.Timestamp.Sub(seq.LastUpdate)
		if timeSinceLastEvent > s.config.MaxTimeGap {
			seq.State = "timeout"
			continue
		}

		// Check if event fits the sequence
		if s.eventFitsSequence(event, seq) {
			seq.Events = append(seq.Events, event)
			seq.LastUpdate = now

			// Check if sequence is complete
			if s.isSequenceComplete(seq) {
				seq.State = "complete"
				s.recordPattern(seq)
			}
		}
	}
}

// checkSequenceStart checks if event starts a new sequence
func (s *SequenceDetector) checkSequenceStart(event SequenceEvent) {
	// Check known patterns
	for _, pattern := range s.patterns.GetPatterns() {
		if pattern.Pattern[0] == event.EventType {
			s.startNewSequence(event, pattern)
		}
	}

	// Always start a potential new sequence for learning
	s.startNewSequence(event, nil)
}

// startNewSequence creates a new active sequence
func (s *SequenceDetector) startNewSequence(event SequenceEvent, pattern *SequencePattern) {
	s.mu.Lock()
	defer s.mu.Unlock()

	seqID := fmt.Sprintf("seq_%s_%d", event.EventID, time.Now().UnixNano())

	s.activeSequences[seqID] = &ActiveSequence{
		ID:         seqID,
		Events:     []SequenceEvent{event},
		StartTime:  event.Timestamp,
		LastUpdate: event.Timestamp,
		State:      "building",
	}
}

// eventFitsSequence checks if event can be added to sequence
func (s *SequenceDetector) eventFitsSequence(event SequenceEvent, seq *ActiveSequence) bool {
	// Don't add duplicate events
	for _, e := range seq.Events {
		if e.EventID == event.EventID {
			return false
		}
	}

	// Check if sequence is getting too long
	if len(seq.Events) >= s.config.MaxSequenceLength {
		return false
	}

	// K8s specific: Check entity relationship
	if event.Entity != "" && len(seq.Events) > 0 {
		lastEvent := seq.Events[len(seq.Events)-1]
		if !s.areEntitiesRelated(event.Entity, lastEvent.Entity) {
			return false
		}
	}

	return true
}

// areEntitiesRelated checks if two entities are related
func (s *SequenceDetector) areEntitiesRelated(entity1, entity2 string) bool {
	// Same entity
	if entity1 == entity2 {
		return true
	}

	// Same namespace
	parts1 := strings.Split(entity1, "/")
	parts2 := strings.Split(entity2, "/")
	if len(parts1) >= 2 && len(parts2) >= 2 && parts1[0] == parts2[0] {
		return true
	}

	// TODO: Check owner references, selectors, etc.

	return false
}

// isSequenceComplete checks if sequence matches a known pattern
func (s *SequenceDetector) isSequenceComplete(seq *ActiveSequence) bool {
	if len(seq.Events) < 2 {
		return false
	}

	// Extract event type sequence
	typeSequence := make([]string, len(seq.Events))
	for i, e := range seq.Events {
		typeSequence[i] = e.EventType
	}

	// Check against known patterns
	patternKey := strings.Join(typeSequence, "->")
	pattern := s.patterns.GetPattern(patternKey)

	if pattern != nil && pattern.Confidence >= s.config.MinConfidence {
		return true
	}

	// Check if this is a new pattern worth recording
	if len(seq.Events) >= 3 {
		return true
	}

	return false
}

// recordPattern records a completed sequence as a pattern
func (s *SequenceDetector) recordPattern(seq *ActiveSequence) {
	if len(seq.Events) < 2 {
		return
	}

	// Extract pattern
	typeSequence := make([]string, len(seq.Events))
	for i, e := range seq.Events {
		typeSequence[i] = e.EventType
	}

	patternKey := strings.Join(typeSequence, "->")
	duration := seq.Events[len(seq.Events)-1].Timestamp.Sub(seq.Events[0].Timestamp)

	s.patterns.UpdatePattern(patternKey, typeSequence, duration, seq.Events[0].Entity)
}

// findSequenceCorrelations finds correlations for the event
func (s *SequenceDetector) findSequenceCorrelations(event *domain.UnifiedEvent) []SequenceCorrelation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	correlations := []SequenceCorrelation{}
	eventType := s.getEventType(event)

	// Check completed sequences that include this event
	for _, seq := range s.activeSequences {
		if seq.State != "complete" {
			continue
		}

		// Check if this event is in the sequence
		inSequence := false
		position := -1
		for i, e := range seq.Events {
			if e.EventID == event.ID || e.EventType == eventType {
				inSequence = true
				position = i
				break
			}
		}

		if inSequence {
			// Get the pattern
			typeSequence := make([]string, len(seq.Events))
			for i, e := range seq.Events {
				typeSequence[i] = e.EventType
			}
			patternKey := strings.Join(typeSequence, "->")
			pattern := s.patterns.GetPattern(patternKey)

			if pattern != nil && pattern.Confidence >= s.config.MinConfidence {
				// Convert to event references
				refs := make([]EventReference, len(seq.Events))
				for i, e := range seq.Events {
					refs[i] = EventReference{
						EventID:   e.EventID,
						EventType: e.EventType,
						Entity:    e.Entity,
						Timestamp: e.Timestamp,
					}
				}

				correlation := SequenceCorrelation{
					SequenceID:  seq.ID,
					Pattern:     pattern,
					Events:      refs,
					Confidence:  pattern.Confidence,
					Duration:    seq.Events[len(seq.Events)-1].Timestamp.Sub(seq.Events[0].Timestamp),
					Explanation: s.explainSequence(pattern, position),
				}

				correlations = append(correlations, correlation)
			}
		}
	}

	return correlations
}

// cleanupSequences removes old sequences
func (s *SequenceDetector) cleanupSequences() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.config.WindowSize)

	for id, seq := range s.activeSequences {
		if seq.LastUpdate.Before(cutoff) {
			delete(s.activeSequences, id)
		}
	}
}

// Helper methods

func (s *SequenceDetector) getEventType(event *domain.UnifiedEvent) string {
	if event.Kubernetes != nil && event.Kubernetes.Reason != "" {
		return fmt.Sprintf("k8s:%s", event.Kubernetes.Reason)
	}

	if event.Network != nil {
		return fmt.Sprintf("net:%d", event.Network.StatusCode)
	}

	return fmt.Sprintf("%s:%s", event.Type, event.Source)
}

func (s *SequenceDetector) getEntityKey(event *domain.UnifiedEvent) string {
	if event.Entity != nil {
		return fmt.Sprintf("%s/%s", event.Entity.Namespace, event.Entity.Name)
	}
	return "unknown"
}

func (s *SequenceDetector) explainSequence(pattern *SequencePattern, position int) string {
	if position == 0 {
		return fmt.Sprintf("Event starts sequence %s (seen %d times, %.0f%% confidence)",
			strings.Join(pattern.Pattern, " → "),
			pattern.Occurrences,
			pattern.Confidence*100)
	} else if position == len(pattern.Pattern)-1 {
		return fmt.Sprintf("Event completes sequence %s (avg duration: %v)",
			strings.Join(pattern.Pattern, " → "),
			pattern.AvgDuration.Round(time.Second))
	} else {
		return fmt.Sprintf("Event is step %d in sequence %s",
			position+1,
			strings.Join(pattern.Pattern, " → "))
	}
}

// SequencePatternStore methods

func (p *SequencePatternStore) GetPatterns() []*SequencePattern {
	p.mu.RLock()
	defer p.mu.RUnlock()

	patterns := make([]*SequencePattern, 0, len(p.patterns))
	for _, pattern := range p.patterns {
		patterns = append(patterns, pattern)
	}
	return patterns
}

func (p *SequencePatternStore) GetPattern(key string) *SequencePattern {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.patterns[key]
}

func (p *SequencePatternStore) UpdatePattern(key string, sequence []string, duration time.Duration, entity string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pattern, exists := p.patterns[key]
	if !exists {
		pattern = &SequencePattern{
			ID:          key,
			Pattern:     sequence,
			Occurrences: 0,
			Durations:   make([]time.Duration, 0),
			Entities:    make(map[string]int),
		}
		p.patterns[key] = pattern
	}

	// Update statistics
	pattern.Occurrences++
	pattern.Durations = append(pattern.Durations, duration)
	pattern.LastSeen = time.Now()
	pattern.Entities[entity]++

	// Calculate average duration
	var sum time.Duration
	for _, d := range pattern.Durations {
		sum += d
	}
	pattern.AvgDuration = sum / time.Duration(len(pattern.Durations))

	// Calculate confidence
	// Based on occurrences and consistency
	occurrenceScore := float64(pattern.Occurrences) / float64(10) // Normalize to 0-1
	if occurrenceScore > 1.0 {
		occurrenceScore = 1.0
	}

	// Consistency score based on duration variance
	consistencyScore := 1.0
	if len(pattern.Durations) > 1 {
		// Calculate standard deviation
		var variance float64
		avgNanos := float64(pattern.AvgDuration.Nanoseconds())
		for _, d := range pattern.Durations {
			diff := float64(d.Nanoseconds()) - avgNanos
			variance += diff * diff
		}
		variance /= float64(len(pattern.Durations))
		stdDev := time.Duration(variance)

		// Lower variance = higher consistency
		if pattern.AvgDuration > 0 {
			relativeStdDev := float64(stdDev) / float64(pattern.AvgDuration)
			consistencyScore = 1.0 / (1.0 + relativeStdDev)
		}
	}

	pattern.Confidence = (occurrenceScore + consistencyScore) / 2.0
}

// Common K8s sequences to look for
func CommonK8sSequences() [][]string {
	return [][]string{
		// Deployment update sequence
		{"k8s:DeploymentUpdated", "k8s:ReplicaSetCreated", "k8s:Scheduled", "k8s:Pulling", "k8s:Started"},

		// Pod crash loop
		{"k8s:Started", "k8s:BackOff", "k8s:Pulled", "k8s:Created", "k8s:Started"},

		// ConfigMap update cascade
		{"k8s:ConfigMapUpdated", "k8s:Killing", "k8s:Pulled", "k8s:Created", "k8s:Started"},

		// Resource pressure
		{"k8s:NodeNotReady", "k8s:EvictingPod", "k8s:Killing", "k8s:FailedScheduling"},

		// Service discovery
		{"k8s:ServiceCreated", "k8s:EndpointsUpdated", "k8s:IngressUpdated"},
	}
}
