package correlation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewCorrelationResult(t *testing.T) {
	events := []*domain.UnifiedEvent{
		{
			ID:        "event-1",
			Type:      domain.EventTypeKubernetes,
			Message:   "Pod OOMKilled",
			Timestamp: time.Now().Add(-5 * time.Minute),
		},
		{
			ID:        "event-2",
			Type:      domain.EventTypeKubernetes,
			Message:   "Pod restarted",
			Timestamp: time.Now().Add(-3 * time.Minute),
		},
	}

	result := NewCorrelationResult("cascade_failure", events)

	assert.NotEmpty(t, result.ID)
	assert.Equal(t, "cascade_failure", result.Type)
	assert.Equal(t, "active", result.Status)
	assert.Len(t, result.Timeline, 2)
	// Check that times were properly set (accounting for small time differences)
	assert.WithinDuration(t, events[0].Timestamp, result.StartTime, time.Second)
	assert.WithinDuration(t, events[1].Timestamp, result.EndTime, time.Second)
}

func TestAddTimelineEntry(t *testing.T) {
	result := &CorrelationResult{
		Timeline: []TimelineEntry{},
	}

	// Test with K8s context
	event1 := &domain.UnifiedEvent{
		ID:        "event-1",
		Type:      domain.EventTypeKubernetes,
		Message:   "Pod created",
		Timestamp: time.Now(),
		K8sContext: &domain.K8sContext{
			WorkloadKind: "Pod",
			Namespace:    "default",
			Name:         "test-pod",
			UID:          "pod-uid",
			Labels:       map[string]string{"app": "test"},
		},
		Metrics: &domain.MetricsData{
			MetricName: "memory_usage",
			Value:      90.5,
			Unit:       "percent",
		},
	}

	result.AddTimelineEntry(event1)

	assert.Len(t, result.Timeline, 1)
	entry := result.Timeline[0]
	assert.Equal(t, "event-1", entry.EventID)
	assert.Equal(t, "kubernetes", entry.EventType)
	assert.Equal(t, "Pod created", entry.Description)
	require.NotNil(t, entry.Resource)
	assert.Equal(t, "Pod", entry.Resource.Kind)
	assert.Equal(t, "default", entry.Resource.Namespace)
	assert.Equal(t, "test-pod", entry.Resource.Name)
	assert.Equal(t, "pod-uid", entry.Resource.UID)
	assert.Equal(t, "test", entry.Resource.Labels["app"])
	require.NotNil(t, entry.Metrics)
	assert.Equal(t, "memory_usage", entry.Metrics["metric_name"])
	assert.Equal(t, 90.5, entry.Metrics["value"])

	// Test with Entity context
	event2 := &domain.UnifiedEvent{
		ID:        "event-2",
		Type:      domain.EventTypeKubernetes,
		Message:   "Deployment scaled",
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type:      "Deployment",
			Namespace: "production",
			Name:      "api-server",
			UID:       "deploy-uid",
			Labels:    map[string]string{"tier": "backend"},
		},
	}

	result.AddTimelineEntry(event2)

	assert.Len(t, result.Timeline, 2)
	entry2 := result.Timeline[1]
	require.NotNil(t, entry2.Resource)
	assert.Equal(t, "Deployment", entry2.Resource.Kind)
	assert.Equal(t, "production", entry2.Resource.Namespace)
	assert.Equal(t, "api-server", entry2.Resource.Name)
}

func TestAddImpactedResource(t *testing.T) {
	result := &CorrelationResult{
		ImpactedItems: []ImpactedResource{},
		StartTime:     time.Now().Add(-10 * time.Minute),
		EndTime:       time.Now(),
	}

	resource := ResourceIdentifier{
		Kind:      "Pod",
		Namespace: "default",
		Name:      "test-pod",
		UID:       "pod-123",
	}

	result.AddImpactedResource(resource, "unavailable", "critical", 5)

	assert.Len(t, result.ImpactedItems, 1)
	impacted := result.ImpactedItems[0]
	assert.Equal(t, "Pod", impacted.Resource.Kind)
	assert.Equal(t, "unavailable", impacted.ImpactType)
	assert.Equal(t, "critical", impacted.ImpactLevel)
	assert.Equal(t, 5, impacted.EventCount)
	assert.Equal(t, result.StartTime, impacted.FirstSeen)
	assert.Equal(t, result.EndTime, impacted.LastSeen)
}

func TestAddEvidence(t *testing.T) {
	result := &CorrelationResult{
		Evidence: []CorrelationEvidence{},
	}

	eventIDs := []string{"event-1", "event-2", "event-3"}
	result.AddEvidence("pattern_match", "OOM kill cascade detected", 0.85, eventIDs)

	assert.Len(t, result.Evidence, 1)
	evidence := result.Evidence[0]
	assert.Equal(t, "pattern_match", evidence.Type)
	assert.Equal(t, "OOM kill cascade detected", evidence.Description)
	assert.Equal(t, 0.85, evidence.Confidence)
	assert.Equal(t, eventIDs, evidence.EventIDs)
}

func TestSetRootCause(t *testing.T) {
	result := &CorrelationResult{}

	resource := ResourceIdentifier{
		Kind:      "Pod",
		Namespace: "default",
		Name:      "memory-hog",
	}

	result.SetRootCause("event-1", "resource_exhaustion", "Pod consuming excessive memory", resource)

	require.NotNil(t, result.RootCause)
	assert.Equal(t, "event-1", result.RootCause.EventID)
	assert.Equal(t, "resource_exhaustion", result.RootCause.Type)
	assert.Equal(t, "Pod consuming excessive memory", result.RootCause.Description)
	assert.Equal(t, "memory-hog", result.RootCause.Resource.Name)
}

func TestCalculateConfidence(t *testing.T) {
	tests := []struct {
		name               string
		evidence           []CorrelationEvidence
		expectedConfidence float64
	}{
		{
			name:               "no evidence",
			evidence:           []CorrelationEvidence{},
			expectedConfidence: 0.5,
		},
		{
			name: "single evidence",
			evidence: []CorrelationEvidence{
				{Confidence: 0.8},
			},
			expectedConfidence: 0.8,
		},
		{
			name: "multiple evidence",
			evidence: []CorrelationEvidence{
				{Confidence: 0.8},
				{Confidence: 0.9},
				{Confidence: 0.7},
			},
			expectedConfidence: 0.8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &CorrelationResult{
				Evidence: tt.evidence,
			}
			result.CalculateConfidence()
			assert.InDelta(t, tt.expectedConfidence, result.Confidence, 0.0001)
		})
	}
}

func TestDetermineSeverity(t *testing.T) {
	tests := []struct {
		name             string
		impactedItems    []ImpactedResource
		expectedSeverity domain.EventSeverity
	}{
		{
			name:             "no impacted items",
			impactedItems:    []ImpactedResource{},
			expectedSeverity: domain.EventSeverityInfo,
		},
		{
			name: "critical impact",
			impactedItems: []ImpactedResource{
				{ImpactLevel: "medium"},
				{ImpactLevel: "critical"},
				{ImpactLevel: "low"},
			},
			expectedSeverity: domain.EventSeverityCritical,
		},
		{
			name: "high impact",
			impactedItems: []ImpactedResource{
				{ImpactLevel: "medium"},
				{ImpactLevel: "high"},
				{ImpactLevel: "low"},
			},
			expectedSeverity: domain.EventSeverityError,
		},
		{
			name: "medium impact",
			impactedItems: []ImpactedResource{
				{ImpactLevel: "medium"},
				{ImpactLevel: "low"},
			},
			expectedSeverity: domain.EventSeverityWarning,
		},
		{
			name: "low impact",
			impactedItems: []ImpactedResource{
				{ImpactLevel: "low"},
			},
			expectedSeverity: domain.EventSeverityInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &CorrelationResult{
				ImpactedItems: tt.impactedItems,
			}
			result.DetermineSeverity()
			assert.Equal(t, tt.expectedSeverity, result.Severity)
		})
	}
}

func TestCorrelationResultStatus(t *testing.T) {
	result := &CorrelationResult{
		Status: "active",
	}

	assert.True(t, result.IsActive())

	result.MarkResolved()
	assert.Equal(t, "resolved", result.Status)
	assert.False(t, result.IsActive())
}

func TestGetDuration(t *testing.T) {
	startTime := time.Now().Add(-30 * time.Minute)
	endTime := time.Now()

	result := &CorrelationResult{
		StartTime: startTime,
		EndTime:   endTime,
	}

	duration := result.GetDuration()
	assert.InDelta(t, 30*time.Minute, duration, float64(time.Second))
}

func TestGetEventIDs(t *testing.T) {
	result := &CorrelationResult{
		Timeline: []TimelineEntry{
			{EventID: "event-1"},
			{EventID: "event-2"},
		},
		Evidence: []CorrelationEvidence{
			{EventIDs: []string{"event-2", "event-3"}},
			{EventIDs: []string{"event-4"}},
		},
		RootCause: &RootCause{
			EventID: "event-1",
		},
	}

	eventIDs := result.GetEventIDs()

	// Should contain all unique event IDs
	assert.Contains(t, eventIDs, "event-1")
	assert.Contains(t, eventIDs, "event-2")
	assert.Contains(t, eventIDs, "event-3")
	assert.Contains(t, eventIDs, "event-4")

	// Check for duplicates
	uniqueIDs := make(map[string]bool)
	for _, id := range eventIDs {
		assert.False(t, uniqueIDs[id], "Duplicate event ID found: %s", id)
		uniqueIDs[id] = true
	}
}

func TestToInsight(t *testing.T) {
	now := time.Now()
	result := &CorrelationResult{
		ID:         "corr-123",
		Type:       "cascade_failure",
		Title:      "Pod Cascade Failure",
		Summary:    "Multiple pod failures detected",
		Confidence: 0.85,
		Severity:   domain.EventSeverityError,
		StartTime:  now.Add(-15 * time.Minute),
		EndTime:    now,
		RootCause: &RootCause{
			EventID:     "event-1",
			Type:        "oom",
			Description: "Memory exhaustion",
		},
		ImpactedItems: []ImpactedResource{
			{
				Resource: ResourceIdentifier{
					Kind: "Pod",
					Name: "test-pod",
				},
				ImpactLevel: "critical",
			},
		},
		Timeline: []TimelineEntry{
			{EventID: "event-1"},
			{EventID: "event-2"},
		},
		Evidence: []CorrelationEvidence{
			{
				Type:       "pattern_match",
				Confidence: 0.85,
			},
		},
	}

	insight := result.ToInsight()

	assert.Equal(t, "corr-123", insight.ID)
	assert.Equal(t, "cascade_failure", insight.Type)
	assert.Equal(t, "Pod Cascade Failure", insight.Title)
	assert.Equal(t, "Multiple pod failures detected", insight.Description)
	assert.Equal(t, domain.SeverityLevel(domain.EventSeverityError), insight.Severity)
	assert.Equal(t, 0.85, insight.Confidence)
	assert.Equal(t, "correlation_engine", insight.Source)
	assert.Equal(t, result.StartTime, insight.Timestamp)

	// Check data fields
	assert.NotNil(t, insight.Data["root_cause"])
	assert.NotNil(t, insight.Data["impacted_items"])
	assert.NotNil(t, insight.Data["timeline"])
	assert.NotNil(t, insight.Data["evidence"])
	assert.NotNil(t, insight.Data["duration"])
	assert.Equal(t, 2, insight.Data["event_count"])
}

func TestCorrelationResultIntegration(t *testing.T) {
	// Create a complete correlation result
	events := []*domain.UnifiedEvent{
		{
			ID:        "oom-event",
			Type:      domain.EventTypeKubernetes,
			Message:   "OOMKilled",
			Timestamp: time.Now().Add(-10 * time.Minute),
			Severity:  domain.EventSeverityError,
			K8sContext: &domain.K8sContext{
				WorkloadKind: "Pod",
				Namespace:    "default",
				Name:         "memory-pod",
				UID:          "pod-1",
			},
		},
		{
			ID:        "restart-event",
			Type:      domain.EventTypeKubernetes,
			Message:   "Pod restarted",
			Timestamp: time.Now().Add(-8 * time.Minute),
			Severity:  domain.EventSeverityWarning,
			K8sContext: &domain.K8sContext{
				WorkloadKind: "Pod",
				Namespace:    "default",
				Name:         "memory-pod",
				UID:          "pod-1",
			},
		},
		{
			ID:        "cascade-event",
			Type:      domain.EventTypeKubernetes,
			Message:   "Dependent service failed",
			Timestamp: time.Now().Add(-5 * time.Minute),
			Severity:  domain.EventSeverityCritical,
			K8sContext: &domain.K8sContext{
				WorkloadKind: "Service",
				Namespace:    "default",
				Name:         "dependent-svc",
			},
		},
	}

	// Create and populate result
	result := NewCorrelationResult("cascade_failure", events)
	result.Title = "Memory Exhaustion Cascade"
	result.Summary = "Pod OOM led to service failures"

	// Set root cause
	result.SetRootCause("oom-event", "memory_exhaustion", "Pod exceeded memory limits",
		ResourceIdentifier{
			Kind:      "Pod",
			Namespace: "default",
			Name:      "memory-pod",
			UID:       "pod-1",
		})

	// Add impacted resources
	result.AddImpactedResource(
		ResourceIdentifier{Kind: "Pod", Namespace: "default", Name: "memory-pod"},
		"unavailable", "critical", 2)
	result.AddImpactedResource(
		ResourceIdentifier{Kind: "Service", Namespace: "default", Name: "dependent-svc"},
		"degraded", "high", 1)

	// Add evidence
	result.AddEvidence("pattern_match", "OOM followed by restart pattern", 0.9,
		[]string{"oom-event", "restart-event"})
	result.AddEvidence("temporal_correlation", "Service failed within 5 minutes", 0.7,
		[]string{"restart-event", "cascade-event"})

	// Calculate final values
	result.CalculateConfidence()
	result.DetermineSeverity()

	// Assertions
	assert.Equal(t, "Memory Exhaustion Cascade", result.Title)
	assert.Equal(t, 0.8, result.Confidence)
	assert.Equal(t, domain.EventSeverityCritical, result.Severity)
	assert.Len(t, result.Timeline, 3)
	assert.Len(t, result.ImpactedItems, 2)
	assert.Len(t, result.Evidence, 2)
	assert.NotNil(t, result.RootCause)
	assert.True(t, result.IsActive())

	// Test conversion to insight
	insight := result.ToInsight()
	assert.Equal(t, "cascade_failure", insight.Type)
	assert.Equal(t, domain.SeverityLevel(domain.EventSeverityCritical), insight.Severity)
}
