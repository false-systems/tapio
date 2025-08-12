package correlation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// SequenceCorrelator detects sequential patterns in events
type SequenceCorrelator struct {
	logger *zap.Logger

	// Active sequences being tracked
	sequences   map[string]*EventSequence
	sequencesMu sync.RWMutex

	// Known patterns
	patterns []*SequencePattern

	// Configuration
	config SequenceConfig
}

// SequenceConfig defined in config.go - removing duplicate

// EventSequence represents a sequence of related events
type EventSequence struct {
	ID         string
	Events     []*domain.UnifiedEvent
	StartTime  time.Time
	LastUpdate time.Time
	Pattern    string
	Resources  map[string]bool
}

// SequencePattern defines a known sequence pattern
type SequencePattern struct {
	Name        string
	Description string
	Steps       []PatternStep
	Confidence  float64
}

// PatternStep defines a step in a sequence pattern
type PatternStep struct {
	EventType  domain.EventType
	Optional   bool
	Conditions []string
}

// NewSequenceCorrelator creates a new sequence detector
func NewSequenceCorrelator(logger *zap.Logger, config SequenceConfig) *SequenceCorrelator {
	return &SequenceCorrelator{
		logger:    logger,
		sequences: make(map[string]*EventSequence),
		patterns:  initializePatterns(),
		config:    config,
	}
}

// initializePatterns returns known sequence patterns
func initializePatterns() []*SequencePattern {
	return []*SequencePattern{
		{
			Name:        "deployment_rollout",
			Description: "Kubernetes deployment update sequence",
			Steps: []PatternStep{
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Deployment", "spec.replicas changed"}},
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"ReplicaSet", "created", "scaled"}},
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Pod", "created", "scheduled"}},
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Pod", "ready", "running"}},
			},
			Confidence: HighConfidence,
		},
		{
			Name:        "pod_crash_loop",
			Description: "Pod crash loop backoff sequence",
			Steps: []PatternStep{
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Pod", "started"}},
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Pod", "error", "failed"}},
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Pod", "backoff"}},
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Pod", "restarting"}},
			},
			Confidence: CriticalConfidence,
		},
		{
			Name:        "service_disruption",
			Description: "Service endpoint disruption sequence",
			Steps: []PatternStep{
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Service", "endpoints changed"}},
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Pod", "terminating"}},
				{EventType: domain.EventTypeNetwork, Conditions: []string{"connection refused", "timeout"}},
			},
			Confidence: MediumHighConfidence,
		},
		{
			Name:        "resource_pressure",
			Description: "Resource pressure escalation",
			Steps: []PatternStep{
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Node", "memory pressure", "disk pressure"}},
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Pod", "evicted"}},
				{EventType: domain.EventTypeKubernetes, Conditions: []string{"Pod", "failed", "insufficient resources"}},
			},
			Confidence: MediumHighConfidence,
		},
	}
}

// Process implements the Correlator interface
func (s *SequenceCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	// Clean up old sequences
	s.cleanupSequences()

	// Try to add event to existing sequences
	matchedSequences := s.matchEventToSequences(event)

	// Check if event starts a new sequence
	if s.shouldStartNewSequence(event, matchedSequences) {
		s.createNewSequence(event)
	}

	// Check for completed patterns
	var results []*CorrelationResult
	for _, seq := range matchedSequences {
		if result := s.checkSequencePattern(seq); result != nil {
			results = append(results, result)
		}
	}

	return results, nil
}

// Name returns the correlator name
func (s *SequenceCorrelator) Name() string {
	return "sequence"
}

// matchEventToSequences finds sequences this event belongs to
func (s *SequenceCorrelator) matchEventToSequences(event *domain.UnifiedEvent) []*EventSequence {
	s.sequencesMu.Lock()
	defer s.sequencesMu.Unlock()

	var matched []*EventSequence

	for _, seq := range s.sequences {
		if s.belongsToSequence(event, seq) {
			// Add event to sequence
			seq.Events = append(seq.Events, event)
			seq.LastUpdate = time.Now()

			// Track resources
			if event.K8sContext != nil {
				resource := fmt.Sprintf("%s/%s/%s",
					event.K8sContext.Kind,
					event.K8sContext.Namespace,
					event.K8sContext.Name)
				seq.Resources[resource] = true
			}

			matched = append(matched, seq)
		}
	}

	return matched
}

// belongsToSequence checks if an event belongs to a sequence
func (s *SequenceCorrelator) belongsToSequence(event *domain.UnifiedEvent, seq *EventSequence) bool {
	// Check time constraints
	if time.Since(seq.LastUpdate) > s.config.MaxSequenceGap {
		return false
	}

	// Check if event is related to sequence resources
	if event.K8sContext != nil {
		// Check direct resource match
		resource := fmt.Sprintf("%s/%s/%s",
			event.K8sContext.Kind,
			event.K8sContext.Namespace,
			event.K8sContext.Name)
		if seq.Resources[resource] {
			return true
		}

		// Check namespace match for namespace-wide events
		for res := range seq.Resources {
			parts := strings.Split(res, "/")
			if len(parts) >= 2 && parts[1] == event.K8sContext.Namespace {
				// Check for ownership relationships
				if s.hasOwnershipRelation(event, seq) {
					return true
				}
			}
		}
	}

	// Check trace context
	if event.TraceContext != nil && len(seq.Events) > 0 {
		for _, seqEvent := range seq.Events {
			if seqEvent.TraceContext != nil &&
				seqEvent.TraceContext.TraceID == event.TraceContext.TraceID {
				return true
			}
		}
	}

	return false
}

// hasOwnershipRelation checks if event has ownership relation to sequence
func (s *SequenceCorrelator) hasOwnershipRelation(event *domain.UnifiedEvent, seq *EventSequence) bool {
	if event.K8sContext == nil {
		return false
	}

	// Check if event resource owns any sequence resource
	eventResource := fmt.Sprintf("%s/%s/%s",
		event.K8sContext.Kind,
		event.K8sContext.Namespace,
		event.K8sContext.Name)

	for _, seqEvent := range seq.Events {
		if seqEvent.K8sContext != nil {
			// Check owner references
			for _, ownerRef := range seqEvent.K8sContext.OwnerReferences {
				ownerResource := fmt.Sprintf("%s/%s/%s",
					ownerRef.Kind,
					seqEvent.K8sContext.Namespace,
					ownerRef.Name)
				if ownerResource == eventResource {
					return true
				}
			}
		}
	}

	return false
}

// shouldStartNewSequence determines if event should start a new sequence
func (s *SequenceCorrelator) shouldStartNewSequence(event *domain.UnifiedEvent, matched []*EventSequence) bool {
	// Don't start new sequence if event already matched existing ones
	if len(matched) > 0 {
		return false
	}

	// Check if event type can start a sequence
	for _, pattern := range s.patterns {
		if len(pattern.Steps) > 0 && pattern.Steps[0].EventType == event.Type {
			return true
		}
	}

	return false
}

// createNewSequence creates a new event sequence
func (s *SequenceCorrelator) createNewSequence(event *domain.UnifiedEvent) {
	s.sequencesMu.Lock()
	defer s.sequencesMu.Unlock()

	// Limit number of active sequences
	if len(s.sequences) >= s.config.MaxActiveSequences {
		s.evictOldestSequence()
	}

	seq := &EventSequence{
		ID:         fmt.Sprintf("seq-%s-%d", event.ID, time.Now().UnixNano()),
		Events:     []*domain.UnifiedEvent{event},
		StartTime:  event.Timestamp,
		LastUpdate: time.Now(),
		Resources:  make(map[string]bool),
	}

	// Add initial resource
	if event.K8sContext != nil {
		resource := fmt.Sprintf("%s/%s/%s",
			event.K8sContext.Kind,
			event.K8sContext.Namespace,
			event.K8sContext.Name)
		seq.Resources[resource] = true
	}

	s.sequences[seq.ID] = seq
}

// checkSequencePattern checks if sequence matches any known pattern
func (s *SequenceCorrelator) checkSequencePattern(seq *EventSequence) *CorrelationResult {
	for _, pattern := range s.patterns {
		if match, confidence := s.matchesPattern(seq, pattern); match {
			return s.createSequenceCorrelation(seq, pattern, confidence)
		}
	}

	// Check for generic sequences
	if len(seq.Events) >= s.config.MinSequenceLength {
		return s.createGenericSequenceCorrelation(seq)
	}

	return nil
}

// matchesPattern checks if sequence matches a specific pattern
func (s *SequenceCorrelator) matchesPattern(seq *EventSequence, pattern *SequencePattern) (bool, float64) {
	if len(seq.Events) < len(pattern.Steps) {
		return false, 0
	}

	matchedSteps := 0
	eventIndex := 0

	for _, step := range pattern.Steps {
		// Find matching event
		found := false
		for i := eventIndex; i < len(seq.Events); i++ {
			if seq.Events[i].Type == step.EventType {
				if s.matchesConditions(seq.Events[i], step.Conditions) {
					found = true
					eventIndex = i + 1
					matchedSteps++
					break
				}
			}
		}

		if !found && !step.Optional {
			return false, 0
		}
	}

	// Calculate confidence based on match quality
	confidence := float64(matchedSteps) / float64(len(pattern.Steps)) * pattern.Confidence

	return matchedSteps >= len(pattern.Steps)/2, confidence
}

// matchesConditions checks if event matches step conditions
func (s *SequenceCorrelator) matchesConditions(event *domain.UnifiedEvent, conditions []string) bool {
	if len(conditions) == 0 {
		return true
	}

	// Convert event message to searchable text
	eventText := strings.ToLower(event.Message)

	// Check if any condition matches
	for _, condition := range conditions {
		if strings.Contains(eventText, strings.ToLower(condition)) {
			return true
		}
	}

	return false
}

// createSequenceCorrelation creates a correlation result for a pattern match
func (s *SequenceCorrelator) createSequenceCorrelation(seq *EventSequence, pattern *SequencePattern, confidence float64) *CorrelationResult {
	result := &CorrelationResult{
		ID:         fmt.Sprintf("sequence-%s-%s", pattern.Name, seq.ID),
		Type:       "sequence_pattern",
		Confidence: confidence,
		Events:     getEventIDs(seq.Events),
		Summary:    fmt.Sprintf("Detected %s: %s", pattern.Name, pattern.Description),
		Details: CorrelationDetails{
			Pattern:        pattern.Name,
			Algorithm:      "sequence_pattern_matcher",
			ProcessingTime: time.Since(seq.StartTime),
			DataPoints:     len(seq.Events),
		},
		Evidence: CreateEvidenceData(
			getEventIDs(seq.Events),
			[]string{}, // resource IDs would be extracted from events if needed
			map[string]string{
				"pattern_name": pattern.Name,
				"description":  s.buildSequenceDescription(seq, pattern),
				"sequence_id":  seq.ID,
			},
		),
		StartTime: seq.StartTime,
		EndTime:   seq.Events[len(seq.Events)-1].Timestamp,
	}

	// Identify root cause (first event in sequence)
	result.RootCause = &RootCause{
		EventID:     seq.Events[0].ID,
		Confidence:  MediumConfidence,
		Description: fmt.Sprintf("Sequence started with %s", seq.Events[0].Type),
		Evidence: CreateEvidenceData(
			[]string{seq.Events[0].ID},
			[]string{},
			map[string]string{
				"first_event_time": seq.Events[0].Timestamp.Format(time.RFC3339),
				"pattern_name":     pattern.Name,
			},
		),
	}

	// Assess impact
	result.Impact = s.assessSequenceImpact(seq)

	return result
}

// createGenericSequenceCorrelation creates a correlation for generic sequences
func (s *SequenceCorrelator) createGenericSequenceCorrelation(seq *EventSequence) *CorrelationResult {
	// Build event type sequence
	var eventTypes []string
	for _, event := range seq.Events {
		eventTypes = append(eventTypes, string(event.Type))
	}

	result := &CorrelationResult{
		ID:         fmt.Sprintf("sequence-generic-%s", seq.ID),
		Type:       "sequence_generic",
		Confidence: LowConfidence,
		Events:     getEventIDs(seq.Events),
		Summary:    fmt.Sprintf("Event sequence detected: %s", strings.Join(eventTypes, " → ")),
		Details: CorrelationDetails{
			Pattern:        "Generic event sequence",
			Algorithm:      "generic_sequence_detector",
			ProcessingTime: seq.Events[len(seq.Events)-1].Timestamp.Sub(seq.StartTime),
			DataPoints:     len(seq.Events),
		},
		Evidence: CreateEvidenceData(
			getEventIDs(seq.Events),
			[]string{},
			map[string]string{
				"event_types":       strings.Join(eventTypes, " → "),
				"sequence_duration": seq.Events[len(seq.Events)-1].Timestamp.Sub(seq.StartTime).String(),
				"event_count":       fmt.Sprintf("%d", len(seq.Events)),
			},
		),
		StartTime: seq.StartTime,
		EndTime:   seq.Events[len(seq.Events)-1].Timestamp,
	}

	// Root cause is first high-severity event
	result.RootCause = s.findSequenceRootCause(seq)
	result.Impact = s.assessSequenceImpact(seq)

	return result
}

// Helper methods

func (s *SequenceCorrelator) buildSequenceDescription(seq *EventSequence, pattern *SequencePattern) string {
	duration := seq.Events[len(seq.Events)-1].Timestamp.Sub(seq.StartTime)
	return fmt.Sprintf("The %s pattern was detected over %s involving %d events across %d resources. %s",
		pattern.Name, formatDuration(duration), len(seq.Events), len(seq.Resources), pattern.Description)
}

func (s *SequenceCorrelator) buildSequenceEvidence(seq *EventSequence, pattern *SequencePattern) []string {
	evidence := []string{
		fmt.Sprintf("Pattern: %s", pattern.Name),
		fmt.Sprintf("Duration: %s", seq.Events[len(seq.Events)-1].Timestamp.Sub(seq.StartTime).String()),
		fmt.Sprintf("Events: %d", len(seq.Events)),
		fmt.Sprintf("Resources affected: %d", len(seq.Resources)),
	}

	// Add event progression
	for i, event := range seq.Events {
		if i >= 5 {
			evidence = append(evidence, fmt.Sprintf("... and %d more events", len(seq.Events)-5))
			break
		}
		evidence = append(evidence, fmt.Sprintf("%d. %s: %s", i+1, event.Type, event.Message))
	}

	return evidence
}

func (s *SequenceCorrelator) buildGenericEvidence(seq *EventSequence) []string {
	evidence := []string{
		fmt.Sprintf("Sequence length: %d events", len(seq.Events)),
		fmt.Sprintf("Duration: %s", seq.Events[len(seq.Events)-1].Timestamp.Sub(seq.StartTime).String()),
		fmt.Sprintf("Resources involved: %d", len(seq.Resources)),
	}

	// Add resource list
	i := 0
	for resource := range seq.Resources {
		if i >= 3 {
			evidence = append(evidence, fmt.Sprintf("... and %d more resources", len(seq.Resources)-3))
			break
		}
		evidence = append(evidence, fmt.Sprintf("- %s", resource))
		i++
	}

	return evidence
}

func (s *SequenceCorrelator) findSequenceRootCause(seq *EventSequence) *RootCause {
	// Find first high-severity event
	var rootEvent *domain.UnifiedEvent
	for _, event := range seq.Events {
		if rootEvent == nil || event.Severity > rootEvent.Severity {
			rootEvent = event
		}
	}

	if rootEvent == nil {
		rootEvent = seq.Events[0]
	}

	return &RootCause{
		EventID:     rootEvent.ID,
		Confidence:  LowConfidence,
		Description: fmt.Sprintf("%s: %s", rootEvent.Type, rootEvent.Message),
		Evidence: CreateEvidenceData(
			[]string{rootEvent.ID},
			[]string{},
			map[string]string{
				"severity": string(rootEvent.Severity),
				"position": fmt.Sprintf("%d", s.getEventPosition(rootEvent, seq)),
			},
		),
	}
}

func (s *SequenceCorrelator) getEventPosition(event *domain.UnifiedEvent, seq *EventSequence) int {
	for i, e := range seq.Events {
		if e.ID == event.ID {
			return i + 1
		}
	}
	return 0
}

func (s *SequenceCorrelator) assessSequenceImpact(seq *EventSequence) *Impact {
	impact := &Impact{
		Severity:  domain.EventSeverityLow,
		Resources: make([]string, 0),
		Services:  make([]ServiceReference, 0),
	}

	// Find highest severity
	for _, event := range seq.Events {
		if event.Severity > impact.Severity {
			impact.Severity = event.Severity
		}
	}

	// Collect resources
	for resource := range seq.Resources {
		impact.Resources = append(impact.Resources, resource)

		// Extract service names
		parts := strings.Split(resource, "/")
		if len(parts) >= 3 && (parts[0] == "Service" || parts[0] == "Deployment") {
			impact.Services = append(impact.Services, ServiceReference{
				Name:      parts[2],
				Namespace: "", // Would need to be extracted from resource context
				Type:      strings.ToLower(parts[0]),
				Version:   "", // Not available from resource string
			})
		}
	}

	return impact
}

func (s *SequenceCorrelator) cleanupSequences() {
	s.sequencesMu.Lock()
	defer s.sequencesMu.Unlock()

	cutoff := time.Now().Add(-s.config.MaxSequenceAge)

	for id, seq := range s.sequences {
		if seq.LastUpdate.Before(cutoff) {
			delete(s.sequences, id)
		}
	}
}

func (s *SequenceCorrelator) evictOldestSequence() {
	var oldestID string
	var oldestTime time.Time

	for id, seq := range s.sequences {
		if oldestID == "" || seq.LastUpdate.Before(oldestTime) {
			oldestID = id
			oldestTime = seq.LastUpdate
		}
	}

	if oldestID != "" {
		delete(s.sequences, oldestID)
	}
}
