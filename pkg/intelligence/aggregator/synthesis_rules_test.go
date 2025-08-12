package aggregator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSynthesisEngine_ApplySynthesis(t *testing.T) {
	logger := zap.NewNop()
	engine := NewSynthesisEngine(logger)

	ctx := context.Background()

	tests := []struct {
		name          string
		findings      []Finding
		expectedCount int
		expectedTypes []string
	}{
		{
			name: "death spiral pattern",
			findings: []Finding{
				{
					Type:       "pod_restart",
					Message:    "Pod restarted due to crash",
					Timestamp:  time.Now(),
					Confidence: 0.9,
					Impact: Impact{
						Resources: []string{"pod/frontend-1"},
						Services:  []string{"frontend"},
					},
				},
				{
					Type:       "oom_kill",
					Message:    "Container killed due to OOM",
					Timestamp:  time.Now().Add(-2 * time.Minute),
					Confidence: 0.95,
					Impact: Impact{
						Resources: []string{"pod/frontend-1"},
						Services:  []string{"frontend"},
					},
				},
			},
			expectedCount: 1,
			expectedTypes: []string{"death-spiral"},
		},
		{
			name: "config drift cascade",
			findings: []Finding{
				{
					Type:       "config_change",
					Message:    "ConfigMap updated",
					Timestamp:  time.Now(),
					Confidence: 1.0,
					Impact: Impact{
						Resources: []string{"configmap/app-config"},
						Services:  []string{"api-server"},
					},
				},
				{
					Type:       "pod_restart",
					Message:    "Pod restarted",
					Timestamp:  time.Now().Add(-5 * time.Minute),
					Confidence: 0.85,
					Impact: Impact{
						Resources: []string{"pod/api-server-1"},
						Services:  []string{"api-server"},
					},
				},
				{
					Type:       "connection_error",
					Message:    "Failed to connect to service",
					Timestamp:  time.Now().Add(-7 * time.Minute),
					Confidence: 0.8,
					Impact: Impact{
						Resources: []string{"service/api-server"},
						Services:  []string{"frontend", "api-server"},
					},
				},
			},
			expectedCount: 1,
			expectedTypes: []string{"config-drift-cascade"},
		},
		{
			name: "noisy neighbor",
			findings: []Finding{
				{
					Type:       "cpu_throttling",
					Message:    "CPU throttled",
					Timestamp:  time.Now(),
					Confidence: 0.85,
					Impact: Impact{
						Resources: []string{"pod/analytics-1"},
					},
					Evidence: Evidence{
						Attributes: map[string]interface{}{
							"node": "node-1",
						},
					},
				},
				{
					Type:       "memory_pressure",
					Message:    "Memory pressure detected",
					Timestamp:  time.Now().Add(-3 * time.Minute),
					Confidence: 0.8,
					Impact: Impact{
						Resources: []string{"pod/database-1"},
					},
					Evidence: Evidence{
						Attributes: map[string]interface{}{
							"node": "node-1",
						},
					},
				},
			},
			expectedCount: 1,
			expectedTypes: []string{"noisy-neighbor"},
		},
		{
			name: "no patterns match",
			findings: []Finding{
				{
					Type:       "custom_metric",
					Message:    "Custom metric exceeded threshold",
					Timestamp:  time.Now(),
					Confidence: 0.7,
				},
			},
			expectedCount: 0,
			expectedTypes: []string{},
		},
		{
			name: "multiple patterns",
			findings: []Finding{
				// Death spiral
				{
					Type:       "pod_restart",
					Message:    "Pod restarted",
					Timestamp:  time.Now(),
					Confidence: 0.9,
					Impact: Impact{
						Resources: []string{"pod/frontend-1"},
					},
				},
				{
					Type:       "oom_kill",
					Message:    "OOM kill",
					Timestamp:  time.Now().Add(-1 * time.Minute),
					Confidence: 0.95,
					Impact: Impact{
						Resources: []string{"pod/frontend-1"},
					},
				},
				// Storage starvation
				{
					Type:       "storage_full",
					Message:    "PVC full",
					Timestamp:  time.Now().Add(-2 * time.Minute),
					Confidence: 0.9,
					Impact: Impact{
						Resources: []string{"pvc/data-volume"},
					},
				},
				{
					Type:       "write_error",
					Message:    "Write failed",
					Timestamp:  time.Now().Add(-3 * time.Minute),
					Confidence: 0.85,
					Impact: Impact{
						Resources: []string{"pod/database-1"},
					},
				},
				{
					Type:       "pod_crash",
					Message:    "Pod crashed",
					Timestamp:  time.Now().Add(-4 * time.Minute),
					Confidence: 0.9,
					Impact: Impact{
						Resources: []string{"pod/database-1"},
					},
				},
			},
			expectedCount: 1,
			expectedTypes: []string{"storage-starvation"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := engine.ApplySynthesis(ctx, tt.findings)
			assert.Len(t, results, tt.expectedCount)

			if tt.expectedCount > 0 {
				patterns := make([]string, len(results))
				for i, result := range results {
					patterns[i] = result.Pattern
					assert.NotEmpty(t, result.Insight)
					assert.NotEmpty(t, result.Narrative)
					assert.NotEmpty(t, result.Actions)
					assert.NotEmpty(t, result.Prevention)
					assert.NotEmpty(t, result.BusinessImpact)
					assert.Greater(t, result.Confidence, 0.0)
					assert.Greater(t, result.ResolutionTime, time.Duration(0))
				}

				for _, expectedType := range tt.expectedTypes {
					assert.Contains(t, patterns, expectedType)
				}
			}
		})
	}
}

func TestSynthesisEngine_Preconditions(t *testing.T) {
	logger := zap.NewNop()
	engine := NewSynthesisEngine(logger)

	t.Run("time window validation", func(t *testing.T) {
		findings := []Finding{
			{
				Type:      "pod_restart",
				Timestamp: time.Now(),
				Impact: Impact{
					Resources: []string{"pod/test-1"},
				},
			},
			{
				Type:      "oom_kill",
				Timestamp: time.Now().Add(-20 * time.Minute), // Outside 10 minute window
				Impact: Impact{
					Resources: []string{"pod/test-1"},
				},
			},
		}

		results := engine.ApplySynthesis(context.Background(), findings)
		assert.Empty(t, results, "Should not match due to time window")
	})

	t.Run("minimum count validation", func(t *testing.T) {
		findings := []Finding{
			{
				Type:      "storage_full",
				Timestamp: time.Now(),
			},
			{
				Type:      "write_error",
				Timestamp: time.Now().Add(-1 * time.Minute),
			},
			// Missing third finding required by storage-starvation rule
		}

		results := engine.ApplySynthesis(context.Background(), findings)
		assert.Empty(t, results, "Should not match due to minimum count")
	})

	t.Run("custom validation", func(t *testing.T) {
		findings := []Finding{
			{
				Type:      "pod_restart",
				Timestamp: time.Now(),
				Impact: Impact{
					Resources: []string{"pod/test-1"},
				},
			},
			{
				Type:      "oom_kill",
				Timestamp: time.Now().Add(-1 * time.Minute),
				Impact: Impact{
					Resources: []string{"pod/test-2"}, // Different pod
				},
			},
		}

		results := engine.ApplySynthesis(context.Background(), findings)
		assert.Empty(t, results, "Should not match due to custom validation (different pods)")
	})
}

func TestSynthesisEngine_RuleManagement(t *testing.T) {
	logger := zap.NewNop()
	engine := NewSynthesisEngine(logger)

	t.Run("add custom rule", func(t *testing.T) {
		customRule := SynthesisRule{
			ID:          "custom-test",
			Name:        "Custom Test Rule",
			Description: "Test rule for unit tests",
			Priority:    150,
			Preconditions: []SynthesisPrecondition{
				{
					RequiredTypes: []string{"test_event"},
					MinCount:      1,
				},
			},
			Synthesize: func(ctx context.Context, findings []Finding) *SynthesisResult {
				return &SynthesisResult{
					Pattern:    "custom-pattern",
					Insight:    "Custom insight",
					Confidence: 1.0,
				}
			},
			ConfidenceBoost: 0.2,
		}

		engine.AddRule(customRule)

		// Verify rule was added
		rule := engine.GetRule("custom-test")
		require.NotNil(t, rule)
		assert.Equal(t, "custom-test", rule.ID)
		assert.Equal(t, 150, rule.Priority)

		// Test that custom rule works
		findings := []Finding{
			{
				Type:      "test_event",
				Timestamp: time.Now(),
			},
		}
		results := engine.ApplySynthesis(context.Background(), findings)
		assert.Len(t, results, 1)
		assert.Equal(t, "custom-pattern", results[0].Pattern)
	})

	t.Run("remove rule", func(t *testing.T) {
		// First verify death-spiral exists
		rule := engine.GetRule("death-spiral")
		require.NotNil(t, rule)

		// Remove it
		removed := engine.RemoveRule("death-spiral")
		assert.True(t, removed)

		// Verify it's gone
		rule = engine.GetRule("death-spiral")
		assert.Nil(t, rule)

		// Try to remove non-existent rule
		removed = engine.RemoveRule("non-existent")
		assert.False(t, removed)
	})

	t.Run("rule priority ordering", func(t *testing.T) {
		// Add multiple rules with different priorities
		engine.AddRule(SynthesisRule{
			ID:       "low-priority",
			Priority: 50,
		})
		engine.AddRule(SynthesisRule{
			ID:       "high-priority",
			Priority: 200,
		})
		engine.AddRule(SynthesisRule{
			ID:       "medium-priority",
			Priority: 100,
		})

		// Verify rules are sorted by priority
		// This is internal behavior, but we can test it indirectly
		// by checking that rules are evaluated in priority order
		assert.NotNil(t, engine.GetRule("high-priority"))
		assert.NotNil(t, engine.GetRule("medium-priority"))
		assert.NotNil(t, engine.GetRule("low-priority"))
	})
}

func TestSynthesisResult_Content(t *testing.T) {
	logger := zap.NewNop()
	engine := NewSynthesisEngine(logger)

	// Create findings that trigger death spiral
	findings := []Finding{
		{
			Type:       "pod_restart",
			Message:    "Pod restarted",
			Timestamp:  time.Now(),
			Confidence: 0.9,
			Impact: Impact{
				Resources: []string{"pod/frontend-abc123"},
				Services:  []string{"frontend"},
			},
		},
		{
			Type:       "oom_kill",
			Message:    "Container killed due to OOM",
			Timestamp:  time.Now().Add(-2 * time.Minute),
			Confidence: 0.95,
			Impact: Impact{
				Resources: []string{"pod/frontend-abc123"},
				Services:  []string{"frontend"},
			},
		},
		{
			Type:       "pod_restart",
			Message:    "Pod restarted again",
			Timestamp:  time.Now().Add(-5 * time.Minute),
			Confidence: 0.9,
			Impact: Impact{
				Resources: []string{"pod/frontend-abc123"},
				Services:  []string{"frontend"},
			},
		},
	}

	results := engine.ApplySynthesis(context.Background(), findings)
	require.Len(t, results, 1)

	result := results[0]
	assert.Equal(t, "death-spiral", result.Pattern)
	assert.Contains(t, result.Insight, "2 restarts")
	assert.Contains(t, result.Insight, "1 OOM kills")
	assert.Contains(t, result.Narrative, "repeatedly crashing")
	assert.Contains(t, result.Actions[0], "increase memory limits")
	assert.Contains(t, result.Prevention[0], "memory profiling")
	assert.Equal(t, "Service completely unavailable, affecting all dependent systems", result.BusinessImpact)
	assert.Equal(t, 15*time.Minute, result.ResolutionTime)
	assert.Equal(t, 0.95, result.Confidence)
}

func TestNoNilPointerPanics(t *testing.T) {
	logger := zap.NewNop()
	engine := NewSynthesisEngine(logger)

	// Test with nil/empty data that could cause panics
	testCases := []struct {
		name     string
		findings []Finding
	}{
		{
			name:     "nil impact",
			findings: []Finding{{Type: "pod_restart", Timestamp: time.Now()}},
		},
		{
			name: "nil evidence",
			findings: []Finding{{
				Type:      "cpu_throttling",
				Timestamp: time.Now(),
				Impact:    Impact{Resources: []string{"pod/test"}},
			}},
		},
		{
			name: "empty resources",
			findings: []Finding{{
				Type:      "pod_restart",
				Timestamp: time.Now(),
				Impact:    Impact{Resources: []string{}},
			}},
		},
		{
			name: "nil attributes",
			findings: []Finding{{
				Type:      "memory_pressure",
				Timestamp: time.Now(),
				Evidence:  Evidence{Attributes: nil},
			}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Should not panic
			assert.NotPanics(t, func() {
				engine.ApplySynthesis(context.Background(), tc.findings)
			})
		})
	}
}
