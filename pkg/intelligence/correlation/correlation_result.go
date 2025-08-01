package correlation

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CorrelationResult represents the outcome of correlating events
type CorrelationResult struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"` // cascade_failure, deployment_issue, network_storm, resource_exhaustion
	Title         string                 `json:"title"`
	Summary       string                 `json:"summary"`
	RootCause     *RootCause             `json:"root_cause"`
	ImpactedItems []ImpactedResource     `json:"impacted_items"`
	Timeline      []TimelineEntry        `json:"timeline"`
	Evidence      []CorrelationEvidence  `json:"evidence"`
	Confidence    float64                `json:"confidence"` // 0.0 - 1.0
	Severity      domain.EventSeverity   `json:"severity"`
	StartTime     time.Time              `json:"start_time"`
	EndTime       time.Time              `json:"end_time"`
	Status        string                 `json:"status"` // active, resolved, mitigated
	Tags          []string               `json:"tags"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// RootCause identifies the primary cause of the correlated issue
type RootCause struct {
	EventID     string                 `json:"event_id"`
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Resource    ResourceIdentifier     `json:"resource"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

// ImpactedResource represents a resource affected by the correlation
type ImpactedResource struct {
	Resource    ResourceIdentifier     `json:"resource"`
	ImpactType  string                 `json:"impact_type"`  // unavailable, degraded, cascading_failure
	ImpactLevel string                 `json:"impact_level"` // critical, high, medium, low
	FirstSeen   time.Time              `json:"first_seen"`
	LastSeen    time.Time              `json:"last_seen"`
	EventCount  int                    `json:"event_count"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

// TimelineEntry represents a point in the correlation timeline
type TimelineEntry struct {
	Timestamp   time.Time              `json:"timestamp"`
	EventID     string                 `json:"event_id"`
	EventType   string                 `json:"event_type"`
	Description string                 `json:"description"`
	Resource    *ResourceIdentifier    `json:"resource,omitempty"`
	Metrics     map[string]interface{} `json:"metrics,omitempty"`
}

// CorrelationEvidence represents supporting evidence for the correlation
type CorrelationEvidence struct {
	Type        string                 `json:"type"` // pattern_match, temporal_correlation, ownership_chain, metric_anomaly
	Description string                 `json:"description"`
	Confidence  float64                `json:"confidence"`
	EventIDs    []string               `json:"event_ids"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

// ResourceIdentifier uniquely identifies a resource
type ResourceIdentifier struct {
	Kind      string            `json:"kind"`
	Namespace string            `json:"namespace,omitempty"`
	Name      string            `json:"name"`
	UID       string            `json:"uid,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Node      string            `json:"node,omitempty"`
}

// NewCorrelationResult creates a new correlation result
func NewCorrelationResult(correlationType string, events []*domain.UnifiedEvent) *CorrelationResult {
	now := time.Now()
	result := &CorrelationResult{
		ID:            fmt.Sprintf("corr-%d", now.UnixNano()),
		Type:          correlationType,
		ImpactedItems: []ImpactedResource{},
		Timeline:      []TimelineEntry{},
		Evidence:      []CorrelationEvidence{},
		Status:        "active",
		Tags:          []string{},
		Metadata:      make(map[string]interface{}),
	}

	// Initialize times with first event or current time
	if len(events) > 0 {
		result.StartTime = events[0].Timestamp
		result.EndTime = events[0].Timestamp
	} else {
		result.StartTime = now
		result.EndTime = now
	}

	// Process events to build timeline
	for _, event := range events {
		result.AddTimelineEntry(event)
		if event.Timestamp.Before(result.StartTime) {
			result.StartTime = event.Timestamp
		}
		if event.Timestamp.After(result.EndTime) {
			result.EndTime = event.Timestamp
		}
	}

	return result
}

// AddTimelineEntry adds an event to the timeline
func (cr *CorrelationResult) AddTimelineEntry(event *domain.UnifiedEvent) {
	entry := TimelineEntry{
		Timestamp:   event.Timestamp,
		EventID:     event.ID,
		EventType:   string(event.Type),
		Description: event.Message,
	}

	// Add resource info if available
	if event.K8sContext != nil {
		entry.Resource = &ResourceIdentifier{
			Kind:      event.K8sContext.WorkloadKind,
			Namespace: event.K8sContext.Namespace,
			Name:      event.K8sContext.Name,
			UID:       event.K8sContext.UID,
			Labels:    event.K8sContext.Labels,
		}
	} else if event.Entity != nil {
		entry.Resource = &ResourceIdentifier{
			Kind:      event.Entity.Type,
			Namespace: event.Entity.Namespace,
			Name:      event.Entity.Name,
			UID:       event.Entity.UID,
			Labels:    event.Entity.Labels,
		}
	}

	// Add metrics if available
	if event.Metrics != nil {
		entry.Metrics = map[string]interface{}{
			"metric_name": event.Metrics.MetricName,
			"value":       event.Metrics.Value,
			"unit":        event.Metrics.Unit,
		}
	}

	cr.Timeline = append(cr.Timeline, entry)
}

// AddImpactedResource adds an impacted resource to the result
func (cr *CorrelationResult) AddImpactedResource(resource ResourceIdentifier, impactType string, impactLevel string, eventCount int) {
	impacted := ImpactedResource{
		Resource:    resource,
		ImpactType:  impactType,
		ImpactLevel: impactLevel,
		FirstSeen:   cr.StartTime,
		LastSeen:    cr.EndTime,
		EventCount:  eventCount,
		Details:     make(map[string]interface{}),
	}

	cr.ImpactedItems = append(cr.ImpactedItems, impacted)
}

// AddEvidence adds supporting evidence to the correlation
func (cr *CorrelationResult) AddEvidence(evidenceType string, description string, confidence float64, eventIDs []string) {
	evidence := CorrelationEvidence{
		Type:        evidenceType,
		Description: description,
		Confidence:  confidence,
		EventIDs:    eventIDs,
		Details:     make(map[string]interface{}),
	}

	cr.Evidence = append(cr.Evidence, evidence)
}

// SetRootCause sets the root cause of the correlation
func (cr *CorrelationResult) SetRootCause(eventID string, causeType string, description string, resource ResourceIdentifier) {
	cr.RootCause = &RootCause{
		EventID:     eventID,
		Type:        causeType,
		Description: description,
		Resource:    resource,
		Details:     make(map[string]interface{}),
	}
}

// CalculateConfidence calculates overall confidence based on evidence
func (cr *CorrelationResult) CalculateConfidence() {
	if len(cr.Evidence) == 0 {
		cr.Confidence = 0.5
		return
	}

	var totalConfidence float64
	for _, evidence := range cr.Evidence {
		totalConfidence += evidence.Confidence
	}

	cr.Confidence = totalConfidence / float64(len(cr.Evidence))
}

// DetermineSeverity determines the severity based on impacted resources
func (cr *CorrelationResult) DetermineSeverity() {
	maxSeverity := domain.EventSeverityInfo

	for _, impacted := range cr.ImpactedItems {
		switch impacted.ImpactLevel {
		case "critical":
			maxSeverity = domain.EventSeverityCritical
		case "high":
			if maxSeverity != domain.EventSeverityCritical {
				maxSeverity = domain.EventSeverityError
			}
		case "medium":
			if maxSeverity == domain.EventSeverityInfo {
				maxSeverity = domain.EventSeverityWarning
			}
		}
	}

	cr.Severity = maxSeverity
}

// IsActive returns true if the correlation is still active
func (cr *CorrelationResult) IsActive() bool {
	return cr.Status == "active"
}

// MarkResolved marks the correlation as resolved
func (cr *CorrelationResult) MarkResolved() {
	cr.Status = "resolved"
	cr.EndTime = time.Now()
}

// GetDuration returns the duration of the correlation
func (cr *CorrelationResult) GetDuration() time.Duration {
	return cr.EndTime.Sub(cr.StartTime)
}

// GetEventIDs returns all event IDs in this correlation
func (cr *CorrelationResult) GetEventIDs() []string {
	eventIDs := make(map[string]bool)

	// From timeline
	for _, entry := range cr.Timeline {
		eventIDs[entry.EventID] = true
	}

	// From evidence
	for _, evidence := range cr.Evidence {
		for _, id := range evidence.EventIDs {
			eventIDs[id] = true
		}
	}

	// From root cause
	if cr.RootCause != nil {
		eventIDs[cr.RootCause.EventID] = true
	}

	result := make([]string, 0, len(eventIDs))
	for id := range eventIDs {
		result = append(result, id)
	}

	return result
}

// ToInsight converts CorrelationResult to domain.Insight for backward compatibility
func (cr *CorrelationResult) ToInsight() domain.Insight {
	return domain.Insight{
		ID:          cr.ID,
		Type:        cr.Type,
		Title:       cr.Title,
		Description: cr.Summary,
		Severity:    domain.SeverityLevel(cr.Severity),
		Confidence:  cr.Confidence,
		Source:      "correlation_engine",
		Data: map[string]interface{}{
			"root_cause":     cr.RootCause,
			"impacted_items": cr.ImpactedItems,
			"timeline":       cr.Timeline,
			"evidence":       cr.Evidence,
			"duration":       cr.GetDuration().String(),
			"event_count":    len(cr.GetEventIDs()),
		},
		Metadata:  cr.Metadata,
		Timestamp: cr.StartTime,
	}
}
