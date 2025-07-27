package patterns

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// PatternDiscoveryService discovers new patterns from events
type PatternDiscoveryService struct {
	logger     *zap.Logger
	eventStore EventStore
	repository PatternRepository
	miner      *PatternMiner
	learner    *PatternLearner
}

// EventStore interface for accessing historical events
type EventStore interface {
	Query(ctx context.Context, filter EventFilter) ([]*domain.UnifiedEvent, error)
	GetIncident(ctx context.Context, incidentID string) (*Incident, error)
	ListIncidents(ctx context.Context, filter IncidentFilter) ([]*Incident, error)
}

// EventFilter for querying events
type EventFilter struct {
	StartTime  time.Time
	EndTime    time.Time
	Types      []domain.EventType
	Severities []domain.EventSeverity
	Entities   []string
	Limit      int
}

// Incident represents a collection of related events
type Incident struct {
	ID          string
	Title       string
	Description string
	StartTime   time.Time
	EndTime     time.Time
	Events      []*domain.UnifiedEvent
	RootCause   string
	Resolution  string
	Tags        []string
}

// IncidentFilter for querying incidents
type IncidentFilter struct {
	StartTime time.Time
	EndTime   time.Time
	Resolved  *bool
	Tags      []string
}

// PatternMiner mines patterns from event sequences
type PatternMiner struct {
	minSupport    float64 // Minimum occurrence frequency
	minConfidence float64 // Minimum pattern confidence
}

// PatternLearner uses ML to discover patterns
type PatternLearner struct {
	model interface{} // ML model placeholder
}

// NewPatternDiscoveryService creates a new discovery service
func NewPatternDiscoveryService(
	logger *zap.Logger,
	eventStore EventStore,
	repository PatternRepository,
) *PatternDiscoveryService {
	return &PatternDiscoveryService{
		logger:     logger,
		eventStore: eventStore,
		repository: repository,
		miner:      &PatternMiner{minSupport: 0.1, minConfidence: 0.8},
		learner:    &PatternLearner{},
	}
}

// DiscoverFromIncident discovers patterns from a specific incident
func (s *PatternDiscoveryService) DiscoverFromIncident(ctx context.Context, incidentID string) ([]*K8sPattern, error) {
	incident, err := s.eventStore.GetIncident(ctx, incidentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get incident: %w", err)
	}

	s.logger.Info("Discovering patterns from incident",
		zap.String("incident_id", incidentID),
		zap.Int("event_count", len(incident.Events)),
	)

	var patterns []*K8sPattern

	// 1. Sequence Mining
	if sequencePatterns := s.mineSequencePatterns(incident); len(sequencePatterns) > 0 {
		patterns = append(patterns, sequencePatterns...)
	}

	// 2. Frequency Analysis
	if freqPatterns := s.mineFrequencyPatterns(incident); len(freqPatterns) > 0 {
		patterns = append(patterns, freqPatterns...)
	}

	// 3. Temporal Analysis
	if tempPatterns := s.mineTemporalPatterns(incident); len(tempPatterns) > 0 {
		patterns = append(patterns, tempPatterns...)
	}

	// 4. Causal Analysis
	if causalPatterns := s.mineCausalPatterns(incident); len(causalPatterns) > 0 {
		patterns = append(patterns, causalPatterns...)
	}

	return patterns, nil
}

// mineSequencePatterns finds common event sequences
func (s *PatternDiscoveryService) mineSequencePatterns(incident *Incident) []*K8sPattern {
	var patterns []*K8sPattern

	// Group events by entity
	entityEvents := make(map[string][]*domain.UnifiedEvent)
	for _, event := range incident.Events {
		key := s.getEntityKey(event)
		entityEvents[key] = append(entityEvents[key], event)
	}

	// Find sequences per entity
	for entity, events := range entityEvents {
		if len(events) < 3 { // Need at least 3 events for a sequence
			continue
		}

		// Sort by timestamp
		sort.Slice(events, func(i, j int) bool {
			return events[i].Timestamp.Before(events[j].Timestamp)
		})

		// Extract sequences of reasons/types
		for i := 0; i < len(events)-2; i++ {
			seq := s.extractSequence(events[i:min(i+5, len(events))])
			if seq != nil && s.isSignificantSequence(seq) {
				pattern := s.createSequencePattern(entity, seq, incident)
				patterns = append(patterns, pattern)
			}
		}
	}

	return patterns
}

// mineFrequencyPatterns finds high-frequency event patterns
func (s *PatternDiscoveryService) mineFrequencyPatterns(incident *Incident) []*K8sPattern {
	var patterns []*K8sPattern

	// Count event frequencies by type and reason
	eventCounts := make(map[string]int)
	timeWindows := make(map[string]time.Duration)

	for i, event := range incident.Events {
		key := s.getEventKey(event)
		eventCounts[key]++

		// Calculate time window for this event type
		if i > 0 {
			for j := i - 1; j >= 0 && j >= i-10; j-- {
				if s.getEventKey(incident.Events[j]) == key {
					window := event.Timestamp.Sub(incident.Events[j].Timestamp)
					timeWindows[key] = window
					break
				}
			}
		}
	}

	// Create patterns for high-frequency events
	totalEvents := len(incident.Events)
	for key, count := range eventCounts {
		frequency := float64(count) / float64(totalEvents)
		if frequency > s.miner.minSupport && count > 3 {
			pattern := s.createFrequencyPattern(key, count, timeWindows[key], incident)
			patterns = append(patterns, pattern)
		}
	}

	return patterns
}

// mineTemporalPatterns finds time-based patterns
func (s *PatternDiscoveryService) mineTemporalPatterns(incident *Incident) []*K8sPattern {
	var patterns []*K8sPattern

	// Look for periodic events
	periodicEvents := s.findPeriodicEvents(incident.Events)
	for _, periodic := range periodicEvents {
		pattern := s.createPeriodicPattern(periodic, incident)
		patterns = append(patterns, pattern)
	}

	// Look for time-correlated events
	correlatedEvents := s.findTimeCorrelatedEvents(incident.Events)
	for _, correlated := range correlatedEvents {
		pattern := s.createTimeCorrelationPattern(correlated, incident)
		patterns = append(patterns, pattern)
	}

	return patterns
}

// findTimeCorrelatedEvents finds events that correlate in time
func (s *PatternDiscoveryService) findTimeCorrelatedEvents(events []*domain.UnifiedEvent) [][]interface{} {
	// Stub implementation
	return nil
}

// createTimeCorrelationPattern creates a pattern from time correlated events
func (s *PatternDiscoveryService) createTimeCorrelationPattern(correlated []interface{}, incident *Incident) *K8sPattern {
	// Stub implementation
	return &K8sPattern{
		ID:           fmt.Sprintf("time-correlation-%s", incident.ID),
		Name:         "Time Correlated Events",
		Category:     CategoryResource, // Use temporal pattern category
		Description:  "Events that occur together in time",
		Indicators:   []PatternIndicator{},
		Impact:       PatternImpact{Severity: "low"},
		Correlations: []string{},
	}
}

// mineCausalPatterns finds cause-effect relationships
func (s *PatternDiscoveryService) mineCausalPatterns(incident *Incident) []*K8sPattern {
	var patterns []*K8sPattern

	// Simple causality: event A always followed by event B
	for i := 0; i < len(incident.Events)-1; i++ {
		cause := incident.Events[i]
		effect := incident.Events[i+1]

		// Check if this is a consistent pattern
		if s.isCausalRelationship(cause, effect, incident.Events) {
			pattern := s.createCausalPattern(cause, effect, incident)
			patterns = append(patterns, pattern)
		}
	}

	return patterns
}

// Helper methods

func (s *PatternDiscoveryService) getEntityKey(event *domain.UnifiedEvent) string {
	if event.Entity != nil {
		return fmt.Sprintf("%s/%s", event.Entity.Namespace, event.Entity.Name)
	}
	if event.Kubernetes != nil && event.Kubernetes.Object != "" {
		return event.Kubernetes.Object
	}
	return "unknown"
}

func (s *PatternDiscoveryService) getEventKey(event *domain.UnifiedEvent) string {
	parts := []string{string(event.Type)}

	if event.Kubernetes != nil && event.Kubernetes.Reason != "" {
		parts = append(parts, event.Kubernetes.Reason)
	}

	return strings.Join(parts, ":")
}

type eventSequence struct {
	events []string
	delays []time.Duration
}

func (s *PatternDiscoveryService) extractSequence(events []*domain.UnifiedEvent) *eventSequence {
	if len(events) < 2 {
		return nil
	}

	seq := &eventSequence{
		events: make([]string, len(events)),
		delays: make([]time.Duration, len(events)-1),
	}

	for i, event := range events {
		seq.events[i] = s.getEventKey(event)
		if i > 0 {
			seq.delays[i-1] = event.Timestamp.Sub(events[i-1].Timestamp)
		}
	}

	return seq
}

func (s *PatternDiscoveryService) isCausalRelationship(cause, effect *domain.UnifiedEvent, allEvents []*domain.UnifiedEvent) bool {
	// Simple causality check: cause must precede effect and be within reasonable time window
	if !cause.Timestamp.Before(effect.Timestamp) {
		return false
	}

	timeDiff := effect.Timestamp.Sub(cause.Timestamp)
	if timeDiff > 5*time.Minute {
		return false // Too far apart to be causal
	}

	// Check if they're related (same entity, namespace, or explicit dependency)
	if cause.Entity != nil && effect.Entity != nil {
		if cause.Entity.Name == effect.Entity.Name {
			return true
		}
		if cause.Entity.Namespace == effect.Entity.Namespace {
			return true
		}
	}

	// Check severity escalation (errors often cause more errors)
	if cause.Severity == domain.EventSeverityError && effect.Severity == domain.EventSeverityError {
		return true
	}

	return false
}

func (s *PatternDiscoveryService) createCausalPattern(cause, effect *domain.UnifiedEvent, incident *Incident) *K8sPattern {
	return &K8sPattern{
		ID:          fmt.Sprintf("discovered-causal-%s-%d", cause.ID, time.Now().Unix()),
		Name:        fmt.Sprintf("Causal Pattern: %s -> %s", cause.Type, effect.Type),
		Category:    CategoryFailure,
		Description: fmt.Sprintf("Discovered causal relationship: %s leads to %s", cause.Message, effect.Message),
		Indicators: []PatternIndicator{
			{
				Type:      IndicatorCausality,
				Field:     "event.causal",
				Condition: "matches",
				Value: map[string]interface{}{
					"cause":  cause.Type,
					"effect": effect.Type,
					"window": "5m",
				},
			},
		},
		Impact: PatternImpact{
			Severity:   "high",
			Scope:      "service",
			UserImpact: true,
		},
		RootCause: &RootCausePattern{
			EventType:   string(cause.Type),
			Indicators:  []string{cause.Message},
			Probability: 0.8,
		},
	}
}

func (s *PatternDiscoveryService) isSignificantSequence(seq *eventSequence) bool {
	// Check if sequence has enough variety
	uniqueEvents := make(map[string]bool)
	for _, event := range seq.events {
		uniqueEvents[event] = true
	}

	return len(uniqueEvents) >= 2 && len(seq.events) >= 3
}

func (s *PatternDiscoveryService) createSequencePattern(entity string, seq *eventSequence, incident *Incident) *K8sPattern {
	return &K8sPattern{
		ID:          fmt.Sprintf("discovered-seq-%s-%d", entity, time.Now().Unix()),
		Name:        fmt.Sprintf("Sequence Pattern: %s", strings.Join(seq.events[:3], " -> ")),
		Category:    CategoryFailure,
		Description: fmt.Sprintf("Discovered from incident %s: sequence of events leading to failure", incident.ID),
		Indicators: []PatternIndicator{
			{
				Type:      IndicatorSequence,
				Field:     "event.sequence",
				Condition: "matches",
				Value:     seq.events,
			},
		},
		Impact: PatternImpact{
			Severity:   "medium",
			Scope:      "pod",
			UserImpact: true,
		},
	}
}

func (s *PatternDiscoveryService) createFrequencyPattern(key string, count int, window time.Duration, incident *Incident) *K8sPattern {
	return &K8sPattern{
		ID:          fmt.Sprintf("discovered-freq-%s-%d", key, time.Now().Unix()),
		Name:        fmt.Sprintf("High Frequency Pattern: %s", key),
		Category:    CategoryFailure,
		Description: fmt.Sprintf("Event %s occurred %d times in incident %s", key, count, incident.ID),
		Indicators: []PatternIndicator{
			{
				Type:       IndicatorFrequency,
				Field:      fmt.Sprintf("event.%s", key),
				Threshold:  float64(count),
				TimeWindow: window,
			},
		},
		Impact: PatternImpact{
			Severity: "high",
			Scope:    "service",
		},
	}
}

type periodicEvent struct {
	eventKey string
	period   time.Duration
	count    int
}

func (s *PatternDiscoveryService) findPeriodicEvents(events []*domain.UnifiedEvent) []periodicEvent {
	// Group events by key
	eventGroups := make(map[string][]*domain.UnifiedEvent)
	for _, event := range events {
		key := s.getEventKey(event)
		eventGroups[key] = append(eventGroups[key], event)
	}

	var periodic []periodicEvent

	// Check each group for periodicity
	for key, group := range eventGroups {
		if len(group) < 3 {
			continue
		}

		// Calculate intervals
		intervals := make([]time.Duration, len(group)-1)
		for i := 1; i < len(group); i++ {
			intervals[i-1] = group[i].Timestamp.Sub(group[i-1].Timestamp)
		}

		// Check if intervals are consistent
		if period := s.findPeriod(intervals); period > 0 {
			periodic = append(periodic, periodicEvent{
				eventKey: key,
				period:   period,
				count:    len(group),
			})
		}
	}

	return periodic
}

func (s *PatternDiscoveryService) findPeriod(intervals []time.Duration) time.Duration {
	if len(intervals) < 2 {
		return 0
	}

	// Simple check: are all intervals similar?
	avg := time.Duration(0)
	for _, interval := range intervals {
		avg += interval
	}
	avg /= time.Duration(len(intervals))

	// Check variance
	for _, interval := range intervals {
		diff := float64(interval - avg)
		if diff < 0 {
			diff = -diff
		}
		if diff/float64(avg) > 0.2 { // More than 20% variance
			return 0
		}
	}

	return avg
}

func (s *PatternDiscoveryService) createPeriodicPattern(periodic periodicEvent, incident *Incident) *K8sPattern {
	return &K8sPattern{
		ID:          fmt.Sprintf("discovered-periodic-%s-%d", periodic.eventKey, time.Now().Unix()),
		Name:        fmt.Sprintf("Periodic Pattern: %s every %v", periodic.eventKey, periodic.period),
		Category:    CategoryPerformance,
		Description: fmt.Sprintf("Event occurs every %v (found %d times in incident %s)", periodic.period, periodic.count, incident.ID),
		Indicators: []PatternIndicator{
			{
				Type:       IndicatorFrequency,
				Field:      periodic.eventKey,
				TimeWindow: periodic.period * 2,
				Threshold:  2,
			},
		},
		Impact: PatternImpact{
			Severity:        "medium",
			Scope:           "pod",
			PerformanceRisk: true,
		},
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Additional helper methods would go here...
