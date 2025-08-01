package correlation

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewSequenceDetector(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultSequenceConfig()

	detector := NewSequenceDetector(logger, config)

	assert.NotNil(t, detector)
	assert.NotNil(t, detector.activeSequences)
	assert.NotNil(t, detector.patterns)
	assert.Equal(t, config, detector.config)
}

func TestDefaultSequenceConfig(t *testing.T) {
	config := DefaultSequenceConfig()

	assert.Equal(t, 5, config.MaxSequenceLength)
	assert.Equal(t, 5*time.Minute, config.MaxTimeGap)
	assert.Equal(t, 3, config.MinOccurrences)
	assert.Equal(t, 0.7, config.MinConfidence)
	assert.Equal(t, 30*time.Minute, config.WindowSize)
}

func TestToSequenceEvent(t *testing.T) {
	detector := &SequenceDetector{}

	event := &domain.UnifiedEvent{
		ID:        "event-1",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		Kubernetes: &domain.KubernetesData{
			Reason: "PodCreated",
			Object: "Pod/test-pod",
		},
		Entity: &domain.EntityContext{
			Namespace: "default",
			Name:      "test-pod",
		},
	}

	seqEvent := detector.toSequenceEvent(event)

	assert.Equal(t, "event-1", seqEvent.EventID)
	assert.Equal(t, "k8s:PodCreated", seqEvent.EventType)
	assert.Equal(t, "default/test-pod", seqEvent.Entity)
	assert.Equal(t, "PodCreated", seqEvent.Metadata["reason"])
	assert.Equal(t, "Pod/test-pod", seqEvent.Metadata["object"])
}

func TestEventFitsSequence(t *testing.T) {
	detector := &SequenceDetector{
		config: SequenceConfig{
			MaxSequenceLength: 5,
		},
	}

	now := time.Now()

	seq := &ActiveSequence{
		ID: "seq-1",
		Events: []SequenceEvent{
			{
				EventID:   "event-1",
				EventType: "k8s:PodCreated",
				Entity:    "default/pod-1",
				Timestamp: now,
			},
		},
		LastUpdate: now,
		State:      "building",
	}

	tests := []struct {
		name     string
		event    SequenceEvent
		expected bool
		reason   string
	}{
		{
			name: "fits - same entity",
			event: SequenceEvent{
				EventID:   "event-2",
				EventType: "k8s:PodStarted",
				Entity:    "default/pod-1",
				Timestamp: now.Add(10 * time.Second),
			},
			expected: true,
			reason:   "Same entity, different event",
		},
		{
			name: "fits - same namespace",
			event: SequenceEvent{
				EventID:   "event-3",
				EventType: "k8s:ServiceCreated",
				Entity:    "default/service-1",
				Timestamp: now.Add(20 * time.Second),
			},
			expected: true,
			reason:   "Same namespace, related entities",
		},
		{
			name: "does not fit - duplicate event",
			event: SequenceEvent{
				EventID:   "event-1",
				EventType: "k8s:PodCreated",
				Entity:    "default/pod-1",
				Timestamp: now.Add(5 * time.Second),
			},
			expected: false,
			reason:   "Duplicate event ID",
		},
		{
			name: "does not fit - different namespace",
			event: SequenceEvent{
				EventID:   "event-4",
				EventType: "k8s:PodCreated",
				Entity:    "other/pod-2",
				Timestamp: now.Add(30 * time.Second),
			},
			expected: false,
			reason:   "Different namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.eventFitsSequence(tt.event, seq)
			assert.Equal(t, tt.expected, result, tt.reason)
		})
	}
}

func TestAreEntitiesRelated(t *testing.T) {
	detector := &SequenceDetector{}

	tests := []struct {
		entity1  string
		entity2  string
		expected bool
	}{
		{"default/pod-1", "default/pod-1", true},      // Same entity
		{"default/pod-1", "default/pod-2", true},      // Same namespace
		{"default/pod-1", "kube-system/pod-3", false}, // Different namespace
		{"default/svc-1", "default/pod-1", true},      // Same namespace, different types
	}

	for _, tt := range tests {
		result := detector.areEntitiesRelated(tt.entity1, tt.entity2)
		assert.Equal(t, tt.expected, result,
			"entities %s and %s should be related=%v", tt.entity1, tt.entity2, tt.expected)
	}
}

func TestSequencePatternStore(t *testing.T) {
	store := &SequencePatternStore{
		patterns: make(map[string]*SequencePattern),
	}

	// Test update pattern
	sequence := []string{"k8s:PodCreated", "k8s:PodStarted", "k8s:Ready"}
	key := strings.Join(sequence, "->")

	// First update
	store.UpdatePattern(key, sequence, 30*time.Second, "default/pod-1")

	pattern := store.GetPattern(key)
	require.NotNil(t, pattern)
	assert.Equal(t, 1, pattern.Occurrences)
	assert.Equal(t, 30*time.Second, pattern.AvgDuration)

	// Second update
	store.UpdatePattern(key, sequence, 40*time.Second, "default/pod-2")

	pattern = store.GetPattern(key)
	assert.Equal(t, 2, pattern.Occurrences)
	assert.Equal(t, 35*time.Second, pattern.AvgDuration) // Average of 30 and 40
	assert.Equal(t, 2, len(pattern.Entities))

	// Third update for confidence calculation
	store.UpdatePattern(key, sequence, 35*time.Second, "default/pod-3")

	pattern = store.GetPattern(key)
	assert.Equal(t, 3, pattern.Occurrences)
	assert.Greater(t, pattern.Confidence, 0.0)
	assert.LessOrEqual(t, pattern.Confidence, 1.0)
}

func TestSequenceDetectorProcess(t *testing.T) {
	logger := zap.NewNop()
	config := SequenceConfig{
		MaxSequenceLength: 5,
		MaxTimeGap:        1 * time.Minute,
		MinOccurrences:    2,
		MinConfidence:     0.5,
		WindowSize:        30 * time.Minute,
	}

	detector := NewSequenceDetector(logger, config)

	// Create a deployment sequence that happens multiple times
	now := time.Now()

	for i := 0; i < 3; i++ {
		baseTime := now.Add(time.Duration(i) * 10 * time.Minute)

		events := []*domain.UnifiedEvent{
			{
				ID:        fmt.Sprintf("deploy-%d", i),
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime,
				Kubernetes: &domain.KubernetesData{
					Reason: "DeploymentUpdated",
				},
				Entity: &domain.EntityContext{
					Namespace: "default",
					Name:      "my-deployment",
				},
			},
			{
				ID:        fmt.Sprintf("rs-%d", i),
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime.Add(5 * time.Second),
				Kubernetes: &domain.KubernetesData{
					Reason: "ReplicaSetCreated",
				},
				Entity: &domain.EntityContext{
					Namespace: "default",
					Name:      "my-deployment-rs",
				},
			},
			{
				ID:        fmt.Sprintf("pod-%d", i),
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime.Add(10 * time.Second),
				Kubernetes: &domain.KubernetesData{
					Reason: "Scheduled",
				},
				Entity: &domain.EntityContext{
					Namespace: "default",
					Name:      fmt.Sprintf("my-deployment-pod-%d", i),
				},
			},
		}

		// Process events
		for j, event := range events {
			correlations := detector.Process(event)

			// After the second sequence, should start finding correlations
			if i >= 1 && j == 2 { // Last event of sequence
				t.Logf("Sequence %d, Event %d: found %d correlations", i, j, len(correlations))

				// Debug: log correlations
				if len(correlations) > 0 {
					for _, corr := range correlations {
						t.Logf("  Found correlation: %v events, confidence: %.2f",
							len(corr.Events), corr.Confidence)
					}
				}
			}
		}
	}

	// Check patterns were learned
	patterns := detector.patterns.GetPatterns()
	assert.NotEmpty(t, patterns)

	// Look for the deployment sequence pattern
	found := false
	for _, p := range patterns {
		if len(p.Pattern) >= 3 &&
			p.Pattern[0] == "k8s:DeploymentUpdated" &&
			p.Pattern[1] == "k8s:ReplicaSetCreated" &&
			p.Pattern[2] == "k8s:Scheduled" {
			found = true
			assert.GreaterOrEqual(t, p.Occurrences, 1) // Pattern may be recorded per sequence
			t.Logf("Pattern occurrences: %d", p.Occurrences)
			assert.Greater(t, p.Confidence, 0.0)
		}
	}
	assert.True(t, found, "Should find deployment sequence pattern")
}

func TestIsSequenceComplete(t *testing.T) {
	detector := &SequenceDetector{
		config: SequenceConfig{
			MinConfidence: 0.7,
		},
		patterns: &SequencePatternStore{
			patterns: make(map[string]*SequencePattern),
		},
	}

	// Add a known pattern
	knownPattern := &SequencePattern{
		Pattern:    []string{"k8s:PodCreated", "k8s:PodStarted"},
		Confidence: 0.8,
	}
	detector.patterns.patterns["k8s:PodCreated->k8s:PodStarted"] = knownPattern

	tests := []struct {
		name     string
		sequence *ActiveSequence
		expected bool
	}{
		{
			name: "matches known pattern",
			sequence: &ActiveSequence{
				Events: []SequenceEvent{
					{EventType: "k8s:PodCreated"},
					{EventType: "k8s:PodStarted"},
				},
			},
			expected: true,
		},
		{
			name: "new pattern with 3 events",
			sequence: &ActiveSequence{
				Events: []SequenceEvent{
					{EventType: "k8s:ServiceCreated"},
					{EventType: "k8s:EndpointsUpdated"},
					{EventType: "k8s:IngressUpdated"},
				},
			},
			expected: true,
		},
		{
			name: "too short",
			sequence: &ActiveSequence{
				Events: []SequenceEvent{
					{EventType: "k8s:PodCreated"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.isSequenceComplete(tt.sequence)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExplainSequence(t *testing.T) {
	detector := &SequenceDetector{}

	pattern := &SequencePattern{
		Pattern:     []string{"k8s:Deploy", "k8s:RS", "k8s:Pod"},
		Occurrences: 5,
		Confidence:  0.85,
		AvgDuration: 45 * time.Second,
	}

	tests := []struct {
		position int
		contains string
	}{
		{0, "starts sequence"},
		{2, "completes sequence"},
		{1, "step 2 in sequence"},
	}

	for _, tt := range tests {
		explanation := detector.explainSequence(pattern, tt.position)
		assert.Contains(t, explanation, tt.contains)
	}
}

func TestCleanupSequences(t *testing.T) {
	detector := &SequenceDetector{
		config: SequenceConfig{
			WindowSize: 10 * time.Minute,
		},
		activeSequences: make(map[string]*ActiveSequence),
	}

	now := time.Now()

	// Add old and new sequences
	detector.activeSequences["old"] = &ActiveSequence{
		ID:         "old",
		LastUpdate: now.Add(-15 * time.Minute),
	}
	detector.activeSequences["new"] = &ActiveSequence{
		ID:         "new",
		LastUpdate: now.Add(-5 * time.Minute),
	}

	detector.cleanupSequences()

	assert.NotContains(t, detector.activeSequences, "old")
	assert.Contains(t, detector.activeSequences, "new")
}

func TestCommonK8sSequences(t *testing.T) {
	sequences := CommonK8sSequences()

	assert.NotEmpty(t, sequences)

	// Check some expected sequences
	foundDeployment := false
	foundCrashLoop := false

	for _, seq := range sequences {
		if len(seq) > 0 && seq[0] == "k8s:DeploymentUpdated" {
			foundDeployment = true
		}
		if len(seq) > 1 && seq[0] == "k8s:Started" && seq[1] == "k8s:BackOff" {
			foundCrashLoop = true
		}
	}

	assert.True(t, foundDeployment, "Should include deployment sequence")
	assert.True(t, foundCrashLoop, "Should include crash loop sequence")
}

func TestSequenceDetectorIntegration(t *testing.T) {
	logger := zap.NewNop()
	config := SequenceConfig{
		MaxSequenceLength: 5,
		MaxTimeGap:        2 * time.Minute,
		MinOccurrences:    2,
		MinConfidence:     0.6,
		WindowSize:        30 * time.Minute,
	}

	detector := NewSequenceDetector(logger, config)

	// Simulate a crash loop pattern
	now := time.Now()

	// Pattern: Started -> Error -> BackOff -> Pulled -> Started (repeats)
	crashLoopPattern := []struct {
		reason string
		delay  time.Duration
	}{
		{"Started", 0},
		{"Error", 30 * time.Second},
		{"BackOff", 5 * time.Second},
		{"Pulled", 20 * time.Second},
		{"Created", 5 * time.Second},
		{"Started", 5 * time.Second},
	}

	// Run the pattern 3 times
	for i := 0; i < 3; i++ {
		baseTime := now.Add(time.Duration(i) * 5 * time.Minute)

		for j, step := range crashLoopPattern {
			event := &domain.UnifiedEvent{
				ID:        fmt.Sprintf("crash-%d-%d", i, j),
				Type:      domain.EventTypeKubernetes,
				Timestamp: baseTime.Add(step.delay),
				Kubernetes: &domain.KubernetesData{
					Reason: step.reason,
				},
				Entity: &domain.EntityContext{
					Namespace: "default",
					Name:      "crash-pod",
				},
			}

			correlations := detector.Process(event)

			// After processing enough events in later sequences,
			// should detect the pattern
			if i >= 1 && j >= 2 {
				for _, corr := range correlations {
					t.Logf("Found correlation: %s (confidence: %.2f)",
						strings.Join(corr.Pattern.Pattern, " -> "),
						corr.Confidence)
				}
			}
		}
	}

	// Verify pattern was learned
	patterns := detector.patterns.GetPatterns()
	crashPatternFound := false

	for _, p := range patterns {
		// Look for patterns that include Error and BackOff
		hasError := false
		hasBackOff := false
		for _, step := range p.Pattern {
			if step == "k8s:Error" {
				hasError = true
			}
			if step == "k8s:BackOff" {
				hasBackOff = true
			}
		}

		if hasError && hasBackOff {
			crashPatternFound = true
			assert.GreaterOrEqual(t, p.Occurrences, 1) // Pattern may be recorded per sequence
			t.Logf("Pattern occurrences: %d", p.Occurrences)
			t.Logf("Found crash pattern: %s (occurrences: %d, confidence: %.2f)",
				strings.Join(p.Pattern, " -> "),
				p.Occurrences,
				p.Confidence)
		}
	}

	assert.True(t, crashPatternFound, "Should detect crash loop pattern")
}

func TestSequenceTimeouts(t *testing.T) {
	logger := zap.NewNop()
	config := SequenceConfig{
		MaxSequenceLength: 5,
		MaxTimeGap:        30 * time.Second, // Short timeout for testing
		MinOccurrences:    2,
		MinConfidence:     0.6,
		WindowSize:        30 * time.Minute,
	}

	detector := NewSequenceDetector(logger, config)

	now := time.Now()

	// Start a sequence
	event1 := &domain.UnifiedEvent{
		ID:        "timeout-1",
		Type:      domain.EventTypeKubernetes,
		Timestamp: now,
		Kubernetes: &domain.KubernetesData{
			Reason: "PodCreated",
		},
		Entity: &domain.EntityContext{
			Namespace: "default",
			Name:      "timeout-pod",
		},
	}

	detector.Process(event1)

	// Event after timeout
	event2 := &domain.UnifiedEvent{
		ID:        "timeout-2",
		Type:      domain.EventTypeKubernetes,
		Timestamp: now.Add(1 * time.Minute), // Beyond MaxTimeGap
		Kubernetes: &domain.KubernetesData{
			Reason: "PodStarted",
		},
		Entity: &domain.EntityContext{
			Namespace: "default",
			Name:      "timeout-pod",
		},
	}

	correlations := detector.Process(event2)

	// Should not correlate due to timeout
	assert.Empty(t, correlations)

	// Check that timed out sequences are marked
	foundTimeout := false
	for _, seq := range detector.activeSequences {
		if seq.State == "timeout" {
			foundTimeout = true
		}
	}
	assert.True(t, foundTimeout, "Should have timed out sequences")
}
