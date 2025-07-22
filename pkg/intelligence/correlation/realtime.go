package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ProcessorConfig holds configuration for the real-time processor
type ProcessorConfig struct {
	BufferSize        int
	TimeWindow        time.Duration
	CorrelationWindow time.Duration
}

// EventPattern defines a pattern to match in the event stream
type EventPattern struct {
	ID             string
	Name           string
	Description    string
	Severity       string
	EventTypes     []domain.EventType
	TimeWindow     time.Duration
	MinOccurrences int
}

// CorrelationResult represents the result of pattern matching
type CorrelationResult struct {
	ID            string
	PatternID     string
	PatternType   string
	Score         float64
	MatchedEvents []*domain.UnifiedEvent
	TriggerEvent  *domain.UnifiedEvent
	StartTime     time.Time
	EndTime       time.Time
	Metadata      map[string]interface{}
}

// PatternMatcher defines the interface for pattern matching algorithms
type PatternMatcher interface {
	Name() string
	Match(events []*BufferedEvent) (bool, float64)
}

// RealTimeProcessor processes events in real-time and detects patterns
type RealTimeProcessor struct {
	mu              sync.RWMutex
	buffer          *CircularBuffer
	patterns        map[string]*EventPattern
	activePatterns  map[string]*CorrelationResult
	config          *ProcessorConfig
	patternMatchers []PatternMatcher
}

// NewRealTimeProcessor creates a new real-time processor
func NewRealTimeProcessor(config *ProcessorConfig) (*RealTimeProcessor, error) {
	buffer, err := NewCircularBuffer(config.BufferSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create buffer: %w", err)
	}

	processor := &RealTimeProcessor{
		buffer:         buffer,
		patterns:       make(map[string]*EventPattern),
		activePatterns: make(map[string]*CorrelationResult),
		config:         config,
		patternMatchers: []PatternMatcher{
			&EscalationPatternMatcher{}, // Most specific first
			&AnomalyPatternMatcher{},
			&SequencePatternMatcher{},
			&TemporalPatternMatcher{},
		},
	}

	return processor, nil
}

// ProcessEvent processes a single event and returns correlation results
func (proc *RealTimeProcessor) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) *CorrelationResult {
	if event == nil {
		return nil
	}

	// Ensure event has an ID
	if event.ID == "" {
		event.ID = domain.GenerateEventID()
	}

	// Add event to buffer
	proc.buffer.Add(event)

	// Get recent events for pattern matching
	recentEvents := proc.buffer.GetByTimeWindow(proc.config.TimeWindow)

	// Run pattern matchers
	var bestResult *CorrelationResult
	bestScore := 0.0

	for _, matcher := range proc.patternMatchers {
		matched, score := matcher.Match(recentEvents)
		if matched && score > bestScore {
			bestScore = score
			bestResult = &CorrelationResult{
				ID:            domain.GenerateEventID(),
				PatternType:   matcher.Name(),
				Score:         score,
				TriggerEvent:  event,
				MatchedEvents: extractEvents(recentEvents),
				StartTime:     time.Now().Add(-proc.config.TimeWindow),
				EndTime:       time.Now(),
				Metadata: map[string]interface{}{
					"pattern": matcher.Name(),
					"score":   score,
				},
			}
		}
	}

	// If no pattern matched, create a basic result
	if bestResult == nil {
		bestResult = &CorrelationResult{
			ID:            domain.GenerateEventID(),
			PatternType:   "single-event",
			Score:         0.1,
			TriggerEvent:  event,
			MatchedEvents: []*domain.UnifiedEvent{event},
			StartTime:     event.Timestamp,
			EndTime:       event.Timestamp,
			Metadata:      make(map[string]interface{}),
		}
	}

	// Store active pattern
	proc.mu.Lock()
	proc.activePatterns[bestResult.ID] = bestResult
	proc.mu.Unlock()

	return bestResult
}

// RegisterPattern registers a new pattern for detection
func (proc *RealTimeProcessor) RegisterPattern(pattern *EventPattern) error {
	if pattern.ID == "" {
		return fmt.Errorf("pattern ID is required")
	}

	proc.mu.Lock()
	defer proc.mu.Unlock()

	if _, exists := proc.patterns[pattern.ID]; exists {
		return fmt.Errorf("pattern %s already registered", pattern.ID)
	}

	proc.patterns[pattern.ID] = pattern
	return nil
}

// GetActiveCorrelations returns currently active correlations
func (proc *RealTimeProcessor) GetActiveCorrelations() []*CorrelationResult {
	proc.mu.RLock()
	defer proc.mu.RUnlock()

	results := make([]*CorrelationResult, 0, len(proc.activePatterns))
	for _, result := range proc.activePatterns {
		results = append(results, result)
	}

	return results
}

// Helper function to extract events from buffered events
func extractEvents(bufferedEvents []*BufferedEvent) []*domain.UnifiedEvent {
	events := make([]*domain.UnifiedEvent, 0, len(bufferedEvents))
	for _, be := range bufferedEvents {
		if be != nil && be.Event != nil {
			events = append(events, be.Event)
		}
	}
	return events
}

// SequencePatternMatcher detects sequential patterns from the same service
type SequencePatternMatcher struct{}

func (s *SequencePatternMatcher) Name() string {
	return "sequence"
}

func (s *SequencePatternMatcher) Match(events []*BufferedEvent) (bool, float64) {
	if len(events) < 3 {
		return false, 0
	}

	// Check if any service has 3+ events of the same type/category
	// Also check if they're actually similar events
	serviceEvents := make(map[string][]*BufferedEvent)
	for _, event := range events {
		if event.Event.Entity != nil {
			serviceEvents[event.Event.Entity.Name] = append(serviceEvents[event.Event.Entity.Name], event)
		}
	}

	for _, svcEvents := range serviceEvents {
		if len(svcEvents) >= 3 {
			// Check if events are similar (same category)
			categoryCounts := make(map[string]int)
			for _, e := range svcEvents {
				if e.Event.Semantic != nil {
					categoryCounts[e.Event.Semantic.Category]++
				}
			}

			// Only count as sequence if majority are same category
			for _, count := range categoryCounts {
				if count >= 3 {
					score := float64(count) / float64(len(events)) * 0.8 // Scale down to allow escalation to win
					return true, score
				}
			}
		}
	}

	return false, 0
}

// TemporalPatternMatcher detects burst patterns
type TemporalPatternMatcher struct{}

func (t *TemporalPatternMatcher) Name() string {
	return "temporal-burst"
}

func (t *TemporalPatternMatcher) Match(events []*BufferedEvent) (bool, float64) {
	if len(events) < 5 {
		return false, 0
	}

	// Check if events occurred within a short time window (1 minute)
	if len(events) > 0 {
		firstTime := events[0].Timestamp
		lastTime := events[len(events)-1].Timestamp
		duration := lastTime.Sub(firstTime)

		if duration < 1*time.Minute && len(events) >= 5 {
			score := float64(len(events)) / 10.0
			if score > 1.0 {
				score = 1.0
			}
			return true, score
		}
	}

	return false, 0
}

// AnomalyPatternMatcher detects anomalous events
type AnomalyPatternMatcher struct{}

func (a *AnomalyPatternMatcher) Name() string {
	return "anomaly"
}

func (a *AnomalyPatternMatcher) Match(events []*BufferedEvent) (bool, float64) {
	for _, event := range events {
		// Check for crash or critical events
		if event.Event.Semantic != nil &&
			(event.Event.Semantic.Category == "crash" ||
				event.Event.Semantic.Category == "critical") {
			return true, 0.9
		}

		// Check for system events in production
		if event.Event.Type == domain.EventTypeSystem &&
			event.Event.Entity != nil &&
			event.Event.Entity.Namespace == "prod" {
			return true, 0.8
		}
	}

	return false, 0
}

// EscalationPatternMatcher detects escalating severity
type EscalationPatternMatcher struct{}

func (e *EscalationPatternMatcher) Name() string {
	return "escalation"
}

func (e *EscalationPatternMatcher) Match(events []*BufferedEvent) (bool, float64) {
	if len(events) < 3 {
		return false, 0
	}

	// Check for warning -> error -> crash pattern
	hasWarning := false
	hasError := false
	hasCrash := false

	for _, event := range events {
		if event.Event.Semantic != nil {
			switch event.Event.Semantic.Category {
			case "warning":
				hasWarning = true
			case "error":
				if hasWarning {
					hasError = true
				}
			case "crash":
				if hasError {
					hasCrash = true
				}
			}
		}
	}

	if hasWarning && hasError && hasCrash {
		return true, 0.95
	}

	return false, 0
}
