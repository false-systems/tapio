package correlation

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EventRef is a lightweight reference to an event
type EventRef struct {
	ID        string
	Timestamp time.Time
	Type      string
	Resource  ResourceRef
}

// Relationship describes how two events are related
type Relationship struct {
	Type       string  // same_resource, owner_child, temporal, causal
	Confidence float64 // 0.0 - 1.0
	Evidence   []string
}

// EventSequence tracks events that happen in order
type EventSequence struct {
	ID        string
	Events    []EventRef
	Pattern   string // "cascade", "deployment", "scale"
	StartTime time.Time
	EndTime   time.Time
}

// EventRelationshipTracker tracks relationships between events
type EventRelationshipTracker struct {
	mu sync.RWMutex

	// Which events happened to same resource
	ResourceEvents map[string][]EventRef // resource key → events

	// Which events happened in sequence
	EventSequences map[string]*EventSequence

	// Parent-child event relationships
	EventCausality map[string][]string // parent event ID → child event IDs

	// Temporal relationships
	TemporalBuckets map[int64][]EventRef // timestamp bucket → events

	// K8s relationships
	k8sMap *K8sRelationshipMap

	logger *zap.Logger
}

// NewEventRelationshipTracker creates a new tracker
func NewEventRelationshipTracker(k8sMap *K8sRelationshipMap, logger *zap.Logger) *EventRelationshipTracker {
	return &EventRelationshipTracker{
		ResourceEvents:  make(map[string][]EventRef),
		EventSequences:  make(map[string]*EventSequence),
		EventCausality:  make(map[string][]string),
		TemporalBuckets: make(map[int64][]EventRef),
		k8sMap:          k8sMap,
		logger:          logger,
	}
}

// TrackEvent adds an event to the tracker
func (t *EventRelationshipTracker) TrackEvent(event *domain.UnifiedEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	eventRef := EventRef{
		ID:        event.ID,
		Timestamp: event.Timestamp,
		Type:      string(event.Type),
	}

	// Extract resource reference
	if event.K8sContext != nil {
		eventRef.Resource = ResourceRef{
			Kind:      event.K8sContext.WorkloadKind,
			Namespace: event.K8sContext.Namespace,
			Name:      event.K8sContext.Name,
		}

		// Track by resource
		resKey := fmt.Sprintf("%s/%s/%s",
			eventRef.Resource.Kind,
			eventRef.Resource.Namespace,
			eventRef.Resource.Name)
		t.ResourceEvents[resKey] = append(t.ResourceEvents[resKey], eventRef)
	}

	// Track temporal relationships
	bucket := event.Timestamp.Unix() / 10 // 10-second buckets
	t.TemporalBuckets[bucket] = append(t.TemporalBuckets[bucket], eventRef)

	// Detect sequences
	t.detectSequences(eventRef)

	// Track causality if indicated
	if event.Correlation != nil && event.Correlation.ParentEventID != "" {
		t.EventCausality[event.Correlation.ParentEventID] = append(
			t.EventCausality[event.Correlation.ParentEventID], event.ID)
	}
}

// RelateEvents determines relationship between two events
func (t *EventRelationshipTracker) RelateEvents(eventA, eventB *domain.UnifiedEvent) *Relationship {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Same resource?
	if t.isSameResource(eventA, eventB) {
		return &Relationship{
			Type:       "same_resource",
			Confidence: 1.0,
			Evidence:   []string{"Events affect the same K8s resource"},
		}
	}

	// Owner relationship?
	if rel := t.checkOwnerRelationship(eventA, eventB); rel != nil {
		return rel
	}

	// Service relationship?
	if rel := t.checkServiceRelationship(eventA, eventB); rel != nil {
		return rel
	}

	// Causal relationship? (check before temporal to prioritize causality)
	if rel := t.checkCausalRelationship(eventA, eventB); rel != nil {
		return rel
	}

	// Temporal relationship?
	if rel := t.checkTemporalRelationship(eventA, eventB); rel != nil {
		return rel
	}

	return nil
}

// FindRelatedEvents finds all events related to a given event
func (t *EventRelationshipTracker) FindRelatedEvents(event *domain.UnifiedEvent, maxResults int) []EventRef {
	t.mu.RLock()
	defer t.mu.RUnlock()

	related := make(map[string]EventRef)

	// 1. Same resource events
	if event.K8sContext != nil {
		resKey := fmt.Sprintf("%s/%s/%s",
			event.K8sContext.WorkloadKind,
			event.K8sContext.Namespace,
			event.K8sContext.Name)

		for _, e := range t.ResourceEvents[resKey] {
			if e.ID != event.ID {
				related[e.ID] = e
			}
		}
	}

	// 2. Owner/child relationships
	if event.K8sContext != nil {
		resource := ResourceRef{
			Kind:      event.K8sContext.WorkloadKind,
			Namespace: event.K8sContext.Namespace,
			Name:      event.K8sContext.Name,
		}

		// Find related pods
		podUIDs := t.k8sMap.GetRelatedPods(resource)
		for _, podUID := range podUIDs {
			// Find events for these pods
			podKey := fmt.Sprintf("Pod/%s/%s", event.K8sContext.Namespace, podUID)
			for _, e := range t.ResourceEvents[podKey] {
				related[e.ID] = e
			}
		}
	}

	// 3. Temporal relationships
	bucket := event.Timestamp.Unix() / 10
	for _, e := range t.TemporalBuckets[bucket] {
		if e.ID != event.ID {
			related[e.ID] = e
		}
	}
	// Check adjacent buckets
	for _, e := range t.TemporalBuckets[bucket-1] {
		related[e.ID] = e
	}
	for _, e := range t.TemporalBuckets[bucket+1] {
		related[e.ID] = e
	}

	// 4. Causal relationships
	if children, ok := t.EventCausality[event.ID]; ok {
		for _, childID := range children {
			// Add child events to related
			// In production, we'd look up the EventRef by ID
			// For now, just track that we found causal relationships
			_ = childID
		}
	}

	// Convert to slice
	result := []EventRef{}
	for _, e := range related {
		result = append(result, e)
		if len(result) >= maxResults {
			break
		}
	}

	return result
}

// detectSequences looks for event patterns
func (t *EventRelationshipTracker) detectSequences(event EventRef) {
	// First check if event belongs to existing sequences
	belongsToExisting := false
	for _, seq := range t.EventSequences {
		if t.belongsToSequence(event, seq) {
			seq.Events = append(seq.Events, event)
			seq.EndTime = event.Timestamp
			belongsToExisting = true
		}
	}

	// Only create new sequence if event doesn't belong to existing one
	if !belongsToExisting && t.isCascadeStart(event) {
		seq := &EventSequence{
			ID:        fmt.Sprintf("cascade-%d", time.Now().UnixNano()),
			Events:    []EventRef{event},
			Pattern:   "cascade",
			StartTime: event.Timestamp,
		}
		t.EventSequences[seq.ID] = seq
	}
}

// Helper methods

func (t *EventRelationshipTracker) isSameResource(eventA, eventB *domain.UnifiedEvent) bool {
	if eventA.K8sContext == nil || eventB.K8sContext == nil {
		return false
	}

	return eventA.K8sContext.WorkloadKind == eventB.K8sContext.WorkloadKind &&
		eventA.K8sContext.Namespace == eventB.K8sContext.Namespace &&
		eventA.K8sContext.Name == eventB.K8sContext.Name
}

func (t *EventRelationshipTracker) checkOwnerRelationship(eventA, eventB *domain.UnifiedEvent) *Relationship {
	if eventA.K8sContext == nil || eventB.K8sContext == nil {
		return nil
	}

	resA := ResourceRef{
		Kind:      eventA.K8sContext.WorkloadKind,
		Namespace: eventA.K8sContext.Namespace,
		Name:      eventA.K8sContext.Name,
		UID:       eventA.K8sContext.UID,
	}

	resB := ResourceRef{
		Kind:      eventB.K8sContext.WorkloadKind,
		Namespace: eventB.K8sContext.Namespace,
		Name:      eventB.K8sContext.Name,
		UID:       eventB.K8sContext.UID,
	}

	if related, relType := t.k8sMap.AreRelated(resA, resB); related {
		confidence := 0.95
		if relType == "same_owner" {
			confidence = 0.9
		}

		return &Relationship{
			Type:       relType,
			Confidence: confidence,
			Evidence: []string{
				fmt.Sprintf("%s relationship via K8s API", relType),
			},
		}
	}

	return nil
}

func (t *EventRelationshipTracker) checkServiceRelationship(eventA, eventB *domain.UnifiedEvent) *Relationship {
	// Check if events are related through service dependencies
	// TODO: Implement service mesh topology checking
	return nil
}

func (t *EventRelationshipTracker) checkTemporalRelationship(eventA, eventB *domain.UnifiedEvent) *Relationship {
	timeDiff := eventB.Timestamp.Sub(eventA.Timestamp).Abs()

	// Events within 5 seconds
	if timeDiff < 5*time.Second {
		return &Relationship{
			Type:       "temporal_proximity",
			Confidence: 0.8,
			Evidence: []string{
				fmt.Sprintf("Events occurred within %v", timeDiff),
			},
		}
	}

	// Events within 30 seconds with same severity
	if timeDiff < 30*time.Second && eventA.Severity == eventB.Severity {
		return &Relationship{
			Type:       "temporal_correlation",
			Confidence: 0.6,
			Evidence: []string{
				fmt.Sprintf("Same severity events within %v", timeDiff),
			},
		}
	}

	return nil
}

func (t *EventRelationshipTracker) checkCausalRelationship(eventA, eventB *domain.UnifiedEvent) *Relationship {
	// Check if eventB is caused by eventA
	if children, ok := t.EventCausality[eventA.ID]; ok {
		for _, childID := range children {
			if childID == eventB.ID {
				return &Relationship{
					Type:       "causal",
					Confidence: 0.95,
					Evidence: []string{
						"Direct causality indicated",
					},
				}
			}
		}
	}

	// Check patterns (e.g., OOM → Pod restart)
	// Handle both string types and domain.EventType
	eventAType := string(eventA.Type)
	eventBType := string(eventB.Type)

	if eventAType == "kernel" && eventBType == "kubernetes" {
		if eventA.Kernel != nil && eventA.Kernel.Syscall == "oom_kill" {
			if eventB.Kubernetes != nil && eventB.Kubernetes.Reason == "OOMKilled" {
				return &Relationship{
					Type:       "causal_pattern",
					Confidence: 0.85,
					Evidence: []string{
						"OOM kill followed by pod termination",
					},
				}
			}
		}
	}

	return nil
}

func (t *EventRelationshipTracker) isCascadeStart(event EventRef) bool {
	// Detect if this event could start a cascade
	// - Resource exhaustion
	// - Critical errors
	// - Service failures
	return event.Type == "error" || event.Type == "critical"
}

func (t *EventRelationshipTracker) belongsToSequence(event EventRef, seq *EventSequence) bool {
	// Check if event belongs to sequence
	if seq.Pattern == "cascade" {
		// Within 2 minutes of last event
		if len(seq.Events) > 0 {
			lastEvent := seq.Events[len(seq.Events)-1]
			if event.Timestamp.Sub(lastEvent.Timestamp) < 2*time.Minute {
				// Check for related resources
				// TODO: Implement resource relationship checking
				return true
			}
		}
	}

	return false
}

// GetEventSequences returns all active event sequences
func (t *EventRelationshipTracker) GetEventSequences() []*EventSequence {
	t.mu.RLock()
	defer t.mu.RUnlock()

	sequences := make([]*EventSequence, 0, len(t.EventSequences))
	for _, seq := range t.EventSequences {
		sequences = append(sequences, seq)
	}
	return sequences
}

// CleanupOldData removes old events and sequences
func (t *EventRelationshipTracker) CleanupOldData(cutoffTime time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Clean temporal buckets
	cutoffBucket := cutoffTime.Unix() / 10
	for bucket := range t.TemporalBuckets {
		if bucket < cutoffBucket {
			delete(t.TemporalBuckets, bucket)
		}
	}

	// Clean old sequences
	for id, seq := range t.EventSequences {
		if seq.EndTime.Before(cutoffTime) {
			delete(t.EventSequences, id)
		}
	}

	// Clean resource events (keep only recent)
	for resKey, events := range t.ResourceEvents {
		var filtered []EventRef
		for _, e := range events {
			if e.Timestamp.After(cutoffTime) {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			delete(t.ResourceEvents, resKey)
		} else {
			t.ResourceEvents[resKey] = filtered
		}
	}

	t.logger.Debug("Cleaned up old event data",
		zap.Time("cutoff", cutoffTime),
		zap.Int("sequences_remaining", len(t.EventSequences)),
		zap.Int("resources_tracked", len(t.ResourceEvents)))
}
