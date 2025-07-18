package collector

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestSemanticOTELTracer_MultiDimensionalCorrelation(t *testing.T) {
	// Create the revolutionary tracer
	tracer := NewSemanticOTELTracer()
	ctx := context.Background()

	// Test 1: Temporal correlation with adaptive windows
	t.Run("TemporalCorrelation", func(t *testing.T) {
		// Create memory pressure event
		memoryEvent := &domain.Event{
			ID:        "mem-001",
			Type:      "memory_pressure",
			Severity:  "high",
			Timestamp: time.Now(),
			Context: domain.EventContext{
				Namespace: "production",
				Host:      "node-1",
				Labels: domain.Labels{
					"pod": "api-server-abc123",
				},
			},
			Confidence: 0.95,
		}

		// Process first event
		err := tracer.ProcessEventWithSemanticTrace(ctx, memoryEvent)
		require.NoError(t, err)

		// Create related OOM event within adaptive window (30s for memory)
		oomEvent := &domain.Event{
			ID:        "oom-001",
			Type:      "memory_oom",
			Severity:  "critical",
			Timestamp: time.Now().Add(20 * time.Second),
			Context: domain.EventContext{
				Namespace: "production",
				Host:      "node-1",
				Labels: domain.Labels{
					"pod": "api-server-abc123",
				},
			},
			Confidence: 0.98,
		}

		// Process related event
		err = tracer.ProcessEventWithSemanticTrace(ctx, oomEvent)
		require.NoError(t, err)

		// Verify they're in the same semantic group
		groups := tracer.GetSemanticGroups()
		assert.Len(t, groups, 1, "Should have one semantic group")

		// Verify group properties
		var group *SemanticTraceGroup
		for _, g := range groups {
			group = g
			break
		}

		require.NotNil(t, group)
		assert.Equal(t, "memory_exhaustion_investigation", group.Intent)
		assert.Len(t, group.CausalChain, 2, "Should have both events in causal chain")
		assert.Equal(t, memoryEvent.ID, group.RootCause.ID)
		
		// Verify predictions
		assert.NotNil(t, group.PredictedOutcome)
		assert.Equal(t, "oom_kill_cascade", group.PredictedOutcome.Scenario)
		assert.Greater(t, group.PredictedOutcome.Probability, 0.7)
	})

	// Test 2: Spatial correlation (same namespace/pod)
	t.Run("SpatialCorrelation", func(t *testing.T) {
		tracer2 := NewSemanticOTELTracer()

		// Create network failure in same namespace
		networkEvent1 := &domain.Event{
			ID:        "net-001",
			Type:      "network_failure",
			Severity:  "high",
			Timestamp: time.Now(),
			Context: domain.EventContext{
				Namespace: "microservices",
				Host:      "node-2",
				Labels: domain.Labels{
					"pod": "frontend-xyz",
				},
			},
			Confidence: 0.85,
		}

		// Different pod but same namespace
		networkEvent2 := &domain.Event{
			ID:        "net-002",
			Type:      "network_timeout",
			Severity:  "medium",
			Timestamp: time.Now().Add(1 * time.Minute), // Outside temporal window
			Context: domain.EventContext{
				Namespace: "microservices", // Same namespace
				Host:      "node-3",         // Different node
				Labels: domain.Labels{
					"pod": "backend-123", // Different pod
				},
			},
			Confidence: 0.80,
		}

		// Process events
		err := tracer2.ProcessEventWithSemanticTrace(ctx, networkEvent1)
		require.NoError(t, err)
		err = tracer2.ProcessEventWithSemanticTrace(ctx, networkEvent2)
		require.NoError(t, err)

		// Verify spatial grouping
		groups := tracer2.GetSemanticGroups()
		assert.Len(t, groups, 1, "Should group by spatial proximity")

		var group *SemanticTraceGroup
		for _, g := range groups {
			group = g
			break
		}

		assert.Equal(t, "connectivity_degradation", group.Intent)
		assert.Len(t, group.CausalChain, 2)
	})

	// Test 3: Causal correlation with chain tracking
	t.Run("CausalCorrelation", func(t *testing.T) {
		tracer3 := NewSemanticOTELTracer()

		// Create service failure
		serviceEvent := &domain.Event{
			ID:        "svc-001",
			Type:      "service_failure",
			Severity:  "critical",
			Timestamp: time.Now(),
			Context: domain.EventContext{
				Namespace: "backend",
				Labels: domain.Labels{
					"service": "payment-api",
				},
			},
			Confidence: 0.99,
		}

		// Process first event
		err := tracer3.ProcessEventWithSemanticTrace(ctx, serviceEvent)
		require.NoError(t, err)

		// Add causal link
		tracer3.AddCausalLink("svc-001", "pod-restart-001", 0.9, "triggers")

		// Create causally linked pod restart
		podRestartEvent := &domain.Event{
			ID:        "pod-restart-001",
			Type:      "pod_restart",
			Severity:  "high",
			Timestamp: time.Now().Add(5 * time.Minute), // Outside all temporal windows
			Context: domain.EventContext{
				Namespace: "frontend", // Different namespace
				Host:      "node-5",   // Different node
				Labels: domain.Labels{
					"pod": "web-app-456",
				},
			},
			Confidence: 0.87,
		}

		// Process causally linked event
		err = tracer3.ProcessEventWithSemanticTrace(ctx, podRestartEvent)
		require.NoError(t, err)

		// Verify causal grouping despite spatial/temporal distance
		groups := tracer3.GetSemanticGroups()
		assert.Len(t, groups, 1, "Should group by causality")

		var group *SemanticTraceGroup
		for _, g := range groups {
			group = g
			break
		}

		assert.Equal(t, "service_reliability_incident", group.Intent)
		assert.Len(t, group.CausalChain, 2)
		assert.NotNil(t, group.ImpactAssessment)
		assert.Equal(t, "critical", group.ImpactAssessment.TechnicalSeverity)
	})

	// Test 4: Business impact and cascade risk assessment
	t.Run("BusinessImpactAssessment", func(t *testing.T) {
		tracer4 := NewSemanticOTELTracer()

		// Create cascade of critical events
		events := []*domain.Event{
			{
				ID:        "cascade-001",
				Type:      "cpu_throttling",
				Severity:  "high",
				Timestamp: time.Now(),
				Context: domain.EventContext{
					Namespace: "critical-services",
					Labels:    domain.Labels{"pod": "database-primary"},
				},
				Confidence: 0.95,
			},
			{
				ID:        "cascade-002",
				Type:      "service_restart",
				Severity:  "critical",
				Timestamp: time.Now().Add(3 * time.Second),
				Context: domain.EventContext{
					Namespace: "critical-services",
					Labels:    domain.Labels{"pod": "database-primary"},
				},
				Confidence: 0.98,
			},
			{
				ID:        "cascade-003",
				Type:      "pod_evicted",
				Severity:  "critical",
				Timestamp: time.Now().Add(10 * time.Second),
				Context: domain.EventContext{
					Namespace: "critical-services",
					Labels:    domain.Labels{"pod": "api-gateway"},
				},
				Confidence: 0.99,
			},
		}

		// Process cascade
		for _, event := range events {
			err := tracer4.ProcessEventWithSemanticTrace(ctx, event)
			require.NoError(t, err)
		}

		// Verify impact assessment
		groups := tracer4.GetSemanticGroups()
		assert.Len(t, groups, 1)

		var group *SemanticTraceGroup
		for _, g := range groups {
			group = g
			break
		}

		// Check business impact
		assert.Greater(t, group.ImpactAssessment.BusinessImpact, float32(0.7))
		assert.Greater(t, group.ImpactAssessment.CascadeRisk, float32(0.3))
		assert.Len(t, group.ImpactAssessment.AffectedResources, 2)
		assert.NotEmpty(t, group.ImpactAssessment.RecommendedActions)

		// Check predictions
		assert.NotNil(t, group.PredictedOutcome)
		assert.NotEmpty(t, group.PredictedOutcome.PreventionActions)
	})
}

func TestSemanticOTELTracer_AdaptiveTimeWindows(t *testing.T) {
	ctx := context.Background()

	// Test adaptive windows for different event types
	testCases := []struct {
		name         string
		eventType    domain.EventType
		delay        time.Duration
		shouldGroup  bool
		expectedWindow time.Duration
	}{
		{
			name:         "MemoryLeakFastWindow",
			eventType:    "memory_leak",
			delay:        25 * time.Second, // Within 30s window
			shouldGroup:  true,
			expectedWindow: 30 * time.Second,
		},
		{
			name:         "NetworkFailureFasterWindow",
			eventType:    "network_failure",
			delay:        8 * time.Second, // Within 10s window
			shouldGroup:  true,
			expectedWindow: 10 * time.Second,
		},
		{
			name:         "DiskPressureSlowWindow",
			eventType:    "disk_pressure",
			delay:        90 * time.Second, // Within 2m window
			shouldGroup:  true,
			expectedWindow: 2 * time.Minute,
		},
		{
			name:         "CPUThrottlingImmediate",
			eventType:    "cpu_throttling",
			delay:        4 * time.Second, // Within 5s window
			shouldGroup:  true,
			expectedWindow: 5 * time.Second,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			localTracer := NewSemanticOTELTracer()

			// Create first event
			event1 := &domain.Event{
				ID:        domain.EventID(tc.name + "-1"),
				Type:      tc.eventType,
				Severity:  "high",
				Timestamp: time.Now(),
				Context: domain.EventContext{
					Namespace: "test",
					Labels:    domain.Labels{"pod": "test-pod"},
				},
				Confidence: 0.9,
			}

			// Create second event after delay
			event2 := &domain.Event{
				ID:        domain.EventID(tc.name + "-2"),
				Type:      tc.eventType,
				Severity:  "high",
				Timestamp: time.Now().Add(tc.delay),
				Context: domain.EventContext{
					Namespace: "test",
					Labels:    domain.Labels{"pod": "test-pod"},
				},
				Confidence: 0.9,
			}

			// Process events
			err := localTracer.ProcessEventWithSemanticTrace(ctx, event1)
			require.NoError(t, err)
			err = localTracer.ProcessEventWithSemanticTrace(ctx, event2)
			require.NoError(t, err)

			// Check grouping
			groups := localTracer.GetSemanticGroups()
			if tc.shouldGroup {
				assert.Len(t, groups, 1, "Events should be grouped within adaptive window")
				var group *SemanticTraceGroup
				for _, g := range groups {
					group = g
					break
				}
				assert.Len(t, group.CausalChain, 2, "Both events should be in the group")
			} else {
				assert.Greater(t, len(groups), 1, "Events should not be grouped outside window")
			}
		})
	}
}

func TestSemanticOTELTracer_CleanupOldGroups(t *testing.T) {
	tracer := NewSemanticOTELTracer()
	ctx := context.Background()

	// Create old event - create timestamp first
	oldTimestamp := time.Now().Add(-2 * time.Hour)
	oldEvent := &domain.Event{
		ID:        "old-001",
		Type:      "memory_pressure",
		Severity:  "low",
		Timestamp: oldTimestamp,
		Context: domain.EventContext{
			Namespace: "test",
		},
		Confidence: 0.7,
	}

	// Create recent event in different namespace to avoid grouping
	recentEvent := &domain.Event{
		ID:        "recent-001",
		Type:      "cpu_throttling",
		Severity:  "medium",
		Timestamp: time.Now().Add(-5 * time.Minute),
		Context: domain.EventContext{
			Namespace: "different-namespace", // Different namespace to avoid spatial grouping
		},
		Confidence: 0.8,
	}

	// Process events
	err := tracer.ProcessEventWithSemanticTrace(ctx, oldEvent)
	require.NoError(t, err)
	err = tracer.ProcessEventWithSemanticTrace(ctx, recentEvent)
	require.NoError(t, err)

	// Verify both groups exist
	groups := tracer.GetSemanticGroups()
	assert.Len(t, groups, 2)

	// Cleanup with 1 hour retention
	tracer.CleanupOldGroups(1 * time.Hour)

	// Verify only recent group remains
	groups = tracer.GetSemanticGroups()
	assert.Len(t, groups, 1)

	// Verify it's the recent group
	var remainingGroup *SemanticTraceGroup
	for _, g := range groups {
		remainingGroup = g
		break
	}
	assert.Equal(t, recentEvent.ID, remainingGroup.RootCause.ID)
}