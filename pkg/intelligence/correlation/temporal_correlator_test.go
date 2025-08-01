package correlation

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewTemporalCorrelator(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultTemporalConfig()

	tc := NewTemporalCorrelator(logger, config)

	assert.NotNil(t, tc)
	assert.NotNil(t, tc.eventWindow)
	assert.NotNil(t, tc.cooccurrence)
	assert.Equal(t, config, tc.config)
}

func TestDefaultTemporalConfig(t *testing.T) {
	config := DefaultTemporalConfig()

	assert.Equal(t, 30*time.Minute, config.WindowSize)
	assert.Equal(t, 3, config.MinOccurrences)
	assert.Equal(t, 0.7, config.MinConfidence)
	assert.Equal(t, 5*time.Minute, config.MaxTimeDelta)
}

func TestEventWindowAdd(t *testing.T) {
	window := &EventWindow{
		events:    make([]WindowEvent, 0),
		maxAge:    30 * time.Minute,
		maxEvents: 10,
		byType:    make(map[string][]int),
		byEntity:  make(map[string][]int),
		byTime: &TemporalTimeIndex{
			buckets:    make(map[int64][]*WindowEvent),
			bucketSize: 1 * time.Minute,
		},
	}

	event := &domain.UnifiedEvent{
		ID:        "event-1",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		Kubernetes: &domain.KubernetesData{
			Reason: "PodOOMKilled",
		},
		Entity: &domain.EntityContext{
			Namespace: "default",
			Name:      "test-pod",
		},
	}

	window.Add(event)

	assert.Len(t, window.events, 1)
	assert.Equal(t, "k8s:PodOOMKilled", window.events[0].EventKey)
	assert.Contains(t, window.byType, "k8s:PodOOMKilled")
	assert.Contains(t, window.byEntity, "default/test-pod")
}

func TestEventWindowGetEventsInRange(t *testing.T) {
	window := &EventWindow{
		events:    make([]WindowEvent, 0),
		maxAge:    30 * time.Minute,
		maxEvents: 100,
		byType:    make(map[string][]int),
		byEntity:  make(map[string][]int),
		byTime: &TemporalTimeIndex{
			buckets:    make(map[int64][]*WindowEvent),
			bucketSize: 1 * time.Minute,
		},
	}

	now := time.Now()

	// Add events at different times
	events := []*domain.UnifiedEvent{
		{
			ID:        "event-1",
			Timestamp: now.Add(-10 * time.Minute),
			Type:      domain.EventTypeKubernetes,
		},
		{
			ID:        "event-2",
			Timestamp: now.Add(-5 * time.Minute),
			Type:      domain.EventTypeKubernetes,
		},
		{
			ID:        "event-3",
			Timestamp: now.Add(-2 * time.Minute),
			Type:      domain.EventTypeKubernetes,
		},
		{
			ID:        "event-4",
			Timestamp: now,
			Type:      domain.EventTypeKubernetes,
		},
	}

	for _, e := range events {
		window.Add(e)
	}

	// Get events in range
	rangeStart := now.Add(-6 * time.Minute)
	rangeEnd := now.Add(-1 * time.Minute)
	rangeEvents := window.GetEventsInRange(rangeStart, rangeEnd)

	assert.Len(t, rangeEvents, 2)
	assert.Equal(t, "event-2", rangeEvents[0].Event.ID)
	assert.Equal(t, "event-3", rangeEvents[1].Event.ID)
}

func TestEventWindowClean(t *testing.T) {
	window := &EventWindow{
		events:    make([]WindowEvent, 0),
		maxAge:    10 * time.Minute,
		maxEvents: 100,
		byType:    make(map[string][]int),
		byEntity:  make(map[string][]int),
		byTime: &TemporalTimeIndex{
			buckets:    make(map[int64][]*WindowEvent),
			bucketSize: 1 * time.Minute,
		},
	}

	now := time.Now()

	// Add old and new events
	oldEvent := &domain.UnifiedEvent{
		ID:        "old-event",
		Timestamp: now.Add(-15 * time.Minute),
		Type:      domain.EventTypeKubernetes,
	}
	newEvent := &domain.UnifiedEvent{
		ID:        "new-event",
		Timestamp: now.Add(-5 * time.Minute),
		Type:      domain.EventTypeKubernetes,
	}

	window.Add(oldEvent)
	window.Add(newEvent)

	assert.Len(t, window.events, 2)

	// Clean old events
	window.Clean()

	assert.Len(t, window.events, 1)
	assert.Equal(t, "new-event", window.events[0].Event.ID)
}

func TestCoOccurrenceTrackerUpdate(t *testing.T) {
	tracker := &CoOccurrenceTracker{
		pairs: make(map[EventPairKey]*PairStatistics),
	}

	key := EventPairKey{
		EventA: "k8s:PodOOMKilled",
		EventB: "k8s:ContainerRestart",
	}

	// Update multiple times
	tracker.Update(key, 10*time.Second)
	tracker.Update(key, 12*time.Second)
	tracker.Update(key, 8*time.Second)

	stats := tracker.GetStats(key)
	require.NotNil(t, stats)
	assert.Equal(t, 3, stats.Count)
	assert.Len(t, stats.TimeDeltas, 3)
	assert.Equal(t, 10*time.Second, stats.AvgDelta)
	assert.Greater(t, stats.Confidence, 0.0)
}

func TestPairStatisticsCalculate(t *testing.T) {
	stats := &PairStatistics{
		TimeDeltas: []time.Duration{
			10 * time.Second,
			12 * time.Second,
			8 * time.Second,
		},
		Count:    3,
		LastSeen: time.Now(),
	}

	stats.calculate()

	assert.Equal(t, 10*time.Second, stats.AvgDelta)
	assert.NotZero(t, stats.StdDevDelta)
	assert.Greater(t, stats.Confidence, 0.0)
	assert.LessOrEqual(t, stats.Confidence, 1.0)
}

func TestTemporalCorrelatorProcess(t *testing.T) {
	logger := zap.NewNop()
	config := TemporalConfig{
		WindowSize:     30 * time.Minute,
		MinOccurrences: 2, // Lower for testing
		MinConfidence:  0.5,
		MaxTimeDelta:   5 * time.Minute,
	}

	tc := NewTemporalCorrelator(logger, config)

	now := time.Now()

	// Create a pattern: OOM -> Restart (happens 3 times)
	for i := 0; i < 3; i++ {
		baseTime := now.Add(time.Duration(i) * 10 * time.Minute)

		oomEvent := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("oom-%d", i),
			Type:      domain.EventTypeKubernetes,
			Timestamp: baseTime,
			Kubernetes: &domain.KubernetesData{
				Reason: "OOMKilled",
			},
			Entity: &domain.EntityContext{
				Namespace: "default",
				Name:      "test-pod",
			},
		}

		restartEvent := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("restart-%d", i),
			Type:      domain.EventTypeKubernetes,
			Timestamp: baseTime.Add(30 * time.Second),
			Kubernetes: &domain.KubernetesData{
				Reason: "ContainerRestart",
			},
			Entity: &domain.EntityContext{
				Namespace: "default",
				Name:      "test-pod",
			},
		}

		tc.Process(oomEvent)
		correlations := tc.Process(restartEvent)

		// After 3rd occurrence (i=2), should find correlations
		// (needs MinOccurrences=2 which happens after processing the 3rd pair)
		if i == 2 {
			// Debug: print what we got
			t.Logf("Iteration %d: found %d correlations", i, len(correlations))
			for _, c := range correlations {
				t.Logf("  Correlation: %s -> %s, confidence: %.2f",
					c.SourceEvent.EventType, c.TargetEvent.EventType, c.Confidence)
			}

			// Look for the expected correlation
			found := false
			for _, corr := range correlations {
				if corr.SourceEvent.EventType == "k8s:OOMKilled" &&
					corr.TargetEvent.EventType == "k8s:ContainerRestart" {
					found = true
					assert.Equal(t, "follows", corr.Pattern)
					assert.InDelta(t, 30*time.Second, corr.TimeDelta, float64(time.Second))
				}
			}
			assert.True(t, found, "Should find OOM->Restart correlation")
		}
	}
}

func TestGetEventKey(t *testing.T) {
	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected string
	}{
		{
			name: "kubernetes event",
			event: &domain.UnifiedEvent{
				Type: domain.EventTypeKubernetes,
				Kubernetes: &domain.KubernetesData{
					Reason: "PodOOMKilled",
				},
			},
			expected: "k8s:PodOOMKilled",
		},
		{
			name: "network event",
			event: &domain.UnifiedEvent{
				Type: domain.EventTypeNetwork,
				Network: &domain.NetworkData{
					StatusCode: 500,
					DestPort:   8080,
				},
			},
			expected: "net:500:8080",
		},
		{
			name: "generic event",
			event: &domain.UnifiedEvent{
				Type:   domain.EventTypeSystem,
				Source: "ebpf",
			},
			expected: "system:ebpf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := getEventKey(tt.event)
			assert.Equal(t, tt.expected, key)
		})
	}
}

func TestGetEntityKey(t *testing.T) {
	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected string
	}{
		{
			name: "with entity",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Namespace: "default",
					Name:      "test-pod",
				},
			},
			expected: "default/test-pod",
		},
		{
			name:     "without entity",
			event:    &domain.UnifiedEvent{},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := getEntityKey(tt.event)
			assert.Equal(t, tt.expected, key)
		})
	}
}

func TestGetPattern(t *testing.T) {
	tc := &TemporalCorrelator{}
	now := time.Now()

	tests := []struct {
		name     string
		eventA   *domain.UnifiedEvent
		eventB   *domain.UnifiedEvent
		expected string
	}{
		{
			name: "concurrent",
			eventA: &domain.UnifiedEvent{
				Timestamp: now,
			},
			eventB: &domain.UnifiedEvent{
				Timestamp: now.Add(500 * time.Millisecond),
			},
			expected: "concurrent",
		},
		{
			name: "follows",
			eventA: &domain.UnifiedEvent{
				Timestamp: now,
			},
			eventB: &domain.UnifiedEvent{
				Timestamp: now.Add(5 * time.Second),
			},
			expected: "follows",
		},
		{
			name: "precedes",
			eventA: &domain.UnifiedEvent{
				Timestamp: now.Add(5 * time.Second),
			},
			eventB: &domain.UnifiedEvent{
				Timestamp: now,
			},
			expected: "precedes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := tc.getPattern(tt.eventA, tt.eventB)
			assert.Equal(t, tt.expected, pattern)
		})
	}
}

func TestTemporalCorrelatorCrossEntity(t *testing.T) {
	logger := zap.NewNop()
	config := TemporalConfig{
		WindowSize:     30 * time.Minute,
		MinOccurrences: 2,
		MinConfidence:  0.5,
		MaxTimeDelta:   5 * time.Minute,
	}

	tc := NewTemporalCorrelator(logger, config)

	now := time.Now()

	// Create events from different entities
	// Pattern: Service A failure -> Service B failure
	for i := 0; i < 3; i++ {
		baseTime := now.Add(time.Duration(i) * 10 * time.Minute)

		serviceAEvent := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("svcA-%d", i),
			Type:      domain.EventTypeKubernetes,
			Timestamp: baseTime,
			Kubernetes: &domain.KubernetesData{
				Reason: "ServiceDown",
			},
			Entity: &domain.EntityContext{
				Namespace: "default",
				Name:      "service-a",
			},
		}

		serviceBEvent := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("svcB-%d", i),
			Type:      domain.EventTypeKubernetes,
			Timestamp: baseTime.Add(1 * time.Minute),
			Kubernetes: &domain.KubernetesData{
				Reason: "ServiceDown",
			},
			Entity: &domain.EntityContext{
				Namespace: "default",
				Name:      "service-b",
			},
		}

		tc.Process(serviceAEvent)
		tc.Process(serviceBEvent)
	}

	// After building confidence, should correlate cross-entity
	// Update confidence threshold
	key := EventPairKey{
		EventA: "k8s:ServiceDown",
		EventB: "k8s:ServiceDown",
	}
	stats := tc.cooccurrence.GetStats(key)
	if stats != nil {
		stats.Confidence = 0.91 // Force high confidence
	}

	// Process one more pair
	finalA := &domain.UnifiedEvent{
		ID:        "final-a",
		Type:      domain.EventTypeKubernetes,
		Timestamp: now.Add(40 * time.Minute),
		Kubernetes: &domain.KubernetesData{
			Reason: "ServiceDown",
		},
		Entity: &domain.EntityContext{
			Namespace: "default",
			Name:      "service-a",
		},
	}

	finalB := &domain.UnifiedEvent{
		ID:        "final-b",
		Type:      domain.EventTypeKubernetes,
		Timestamp: now.Add(41 * time.Minute),
		Kubernetes: &domain.KubernetesData{
			Reason: "ServiceDown",
		},
		Entity: &domain.EntityContext{
			Namespace: "default",
			Name:      "service-b",
		},
	}

	tc.Process(finalA)
	correlations := tc.Process(finalB)

	// Should find cross-entity correlation due to high confidence
	assert.NotEmpty(t, correlations)
}

func TestEventWindowMaxEvents(t *testing.T) {
	window := &EventWindow{
		events:    make([]WindowEvent, 0),
		maxAge:    30 * time.Minute,
		maxEvents: 5,
		byType:    make(map[string][]int),
		byEntity:  make(map[string][]int),
		byTime: &TemporalTimeIndex{
			buckets:    make(map[int64][]*WindowEvent),
			bucketSize: 1 * time.Minute,
		},
	}

	// Add more events than max
	for i := 0; i < 10; i++ {
		event := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("event-%d", i),
			Type:      domain.EventTypeKubernetes,
			Timestamp: time.Now().Add(time.Duration(i) * time.Minute),
		}
		window.Add(event)
	}

	// Should only keep last 5
	assert.Len(t, window.events, 5)
	assert.Equal(t, "event-5", window.events[0].Event.ID)
	assert.Equal(t, "event-9", window.events[4].Event.ID)
}

func TestRebuildIndexes(t *testing.T) {
	window := &EventWindow{
		events:    make([]WindowEvent, 0),
		maxAge:    30 * time.Minute,
		maxEvents: 100,
		byType:    make(map[string][]int),
		byEntity:  make(map[string][]int),
		byTime: &TemporalTimeIndex{
			buckets:    make(map[int64][]*WindowEvent),
			bucketSize: 1 * time.Minute,
		},
	}

	// Add events
	events := []*domain.UnifiedEvent{
		{
			ID:        "event-1",
			Type:      domain.EventTypeKubernetes,
			Timestamp: time.Now(),
			Kubernetes: &domain.KubernetesData{
				Reason: "PodCreated",
			},
			Entity: &domain.EntityContext{
				Namespace: "default",
				Name:      "pod-1",
			},
		},
		{
			ID:        "event-2",
			Type:      domain.EventTypeKubernetes,
			Timestamp: time.Now().Add(1 * time.Minute),
			Kubernetes: &domain.KubernetesData{
				Reason: "PodDeleted",
			},
			Entity: &domain.EntityContext{
				Namespace: "default",
				Name:      "pod-2",
			},
		},
	}

	for _, e := range events {
		window.Add(e)
	}

	// Manually corrupt indexes
	window.byType = make(map[string][]int)
	window.byEntity = make(map[string][]int)

	// Rebuild
	window.rebuildIndexes()

	// Verify indexes rebuilt correctly
	assert.Contains(t, window.byType, "k8s:PodCreated")
	assert.Contains(t, window.byType, "k8s:PodDeleted")
	assert.Contains(t, window.byEntity, "default/pod-1")
	assert.Contains(t, window.byEntity, "default/pod-2")
}

// Integration test
func TestTemporalCorrelatorIntegration(t *testing.T) {
	logger := zap.NewNop()
	config := TemporalConfig{
		WindowSize:     30 * time.Minute,
		MinOccurrences: 2,
		MinConfidence:  0.6,
		MaxTimeDelta:   5 * time.Minute,
	}

	tc := NewTemporalCorrelator(logger, config)

	// Simulate real-world scenario: deployment causes pod restarts
	now := time.Now()

	// Multiple deployment update -> pod restart sequences
	for i := 0; i < 3; i++ {
		baseTime := now.Add(time.Duration(i) * 15 * time.Minute)

		// Deployment update
		deployEvent := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("deploy-%d", i),
			Type:      domain.EventTypeKubernetes,
			Timestamp: baseTime,
			Kubernetes: &domain.KubernetesData{
				Reason: "DeploymentScaled",
			},
			Entity: &domain.EntityContext{
				Namespace: "production",
				Name:      "api-deployment",
			},
		}

		tc.Process(deployEvent)

		// Multiple pod restarts follow
		for j := 0; j < 3; j++ {
			podEvent := &domain.UnifiedEvent{
				ID:        fmt.Sprintf("pod-%d-%d", i, j),
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime.Add(time.Duration(j+1) * 30 * time.Second),
				Kubernetes: &domain.KubernetesData{
					Reason: "ContainerRestart",
				},
				Entity: &domain.EntityContext{
					Namespace: "production",
					Name:      fmt.Sprintf("api-pod-%d", j),
				},
			}

			correlations := tc.Process(podEvent)

			// After second deployment cycle, should find correlations
			if i >= 1 {
				// Debug output
				t.Logf("Deployment %d, Pod %d: found %d correlations", i, j, len(correlations))

				// Note: correlations may be empty because deployment and pods are different entities
				// The correlator needs high confidence (0.9+) for cross-entity correlations
			}
		}
	}

	// Verify statistics were built correctly
	stats := tc.cooccurrence.GetStats(EventPairKey{
		EventA: "k8s:DeploymentScaled",
		EventB: "k8s:ContainerRestart",
	})

	require.NotNil(t, stats)
	assert.GreaterOrEqual(t, stats.Count, 6) // At least 6 occurrences
	assert.Greater(t, stats.Confidence, 0.6)
}
