package aggregator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

func TestNewCorrelationAggregator(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
		TimeoutDuration:    30 * time.Second,
		MaxFindings:        100,
		EnableLearning:     true,
	}

	agg := NewCorrelationAggregator(logger, config)

	assert.NotNil(t, agg)
	assert.NotNil(t, agg.logger)
	assert.NotNil(t, agg.confidenceCalc)
	assert.NotNil(t, agg.conflictResolver)
	assert.NotNil(t, agg.causalityBuilder)
	assert.NotNil(t, agg.patternMatcher)
	assert.NotEmpty(t, agg.rules)
}

func TestAggregate_NoOutputs(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
	}

	agg := NewCorrelationAggregator(logger, config)

	event := &domain.UnifiedEvent{ID: "test-event"}
	result, err := agg.Aggregate(context.Background(), []*CorrelatorOutput{}, event)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no correlator outputs")
}

func TestAggregate_SingleCorrelatorWithFindings(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
	}

	agg := NewCorrelationAggregator(logger, config)

	event := &domain.UnifiedEvent{ID: "test-event"}
	outputs := []*CorrelatorOutput{
		{
			CorrelatorName:    "TestCorrelator",
			CorrelatorVersion: "1.0",
			Findings: []Finding{
				{
					ID:         "finding-1",
					Type:       "memory_exhaustion",
					Severity:   SeverityHigh,
					Confidence: 0.8,
					Message:    "Memory usage at 95%",
					Evidence: Evidence{
						Metrics: []MetricPoint{
							{Name: "memory_usage", Value: 95.0, Timestamp: time.Now()},
						},
					},
					Impact: Impact{
						Scope:      "pod",
						Resources:  []string{"pod-123"},
						UserImpact: "Service degradation",
					},
					Timestamp: time.Now(),
				},
			},
			Confidence: 0.8,
		},
	}

	result, err := agg.Aggregate(context.Background(), outputs, event)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.ID)
	assert.NotEmpty(t, result.RootCause)
	assert.Equal(t, []string{"TestCorrelator"}, result.Correlators)
	assert.Greater(t, result.Confidence, 0.0)
}

func TestAggregate_MultipleCorrelatorsWithAgreement(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
	}

	agg := NewCorrelationAggregator(logger, config)

	event := &domain.UnifiedEvent{ID: "test-event"}
	outputs := []*CorrelatorOutput{
		{
			CorrelatorName: "PerformanceCorrelator",
			Findings: []Finding{
				{
					ID:         "perf-1",
					Type:       "memory_exhaustion",
					Severity:   SeverityHigh,
					Confidence: 0.8,
					Message:    "Memory exhaustion detected",
					Impact:     Impact{Resources: []string{"pod-123"}},
					Timestamp:  time.Now(),
				},
			},
			Confidence: 0.8,
		},
		{
			CorrelatorName: "ResourceCorrelator",
			Findings: []Finding{
				{
					ID:         "res-1",
					Type:       "memory_exhaustion",
					Severity:   SeverityHigh,
					Confidence: 0.85,
					Message:    "Pod approaching memory limit",
					Impact:     Impact{Resources: []string{"pod-123"}},
					Timestamp:  time.Now(),
				},
			},
			Confidence: 0.85,
		},
	}

	result, err := agg.Aggregate(context.Background(), outputs, event)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.RootCause, "memory")
	assert.Equal(t, 2, len(result.Correlators))
	// Confidence should be boosted due to agreement
	assert.Greater(t, result.Confidence, 0.8)
}

func TestAggregate_ConflictResolution(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
	}

	agg := NewCorrelationAggregator(logger, config)

	event := &domain.UnifiedEvent{ID: "test-event"}
	outputs := []*CorrelatorOutput{
		{
			CorrelatorName: "CorrelatorA",
			Findings: []Finding{
				{
					ID:         "a-1",
					Type:       "network_issue",
					Severity:   SeverityHigh,
					Confidence: 0.7,
					Message:    "Network timeout detected",
					Impact:     Impact{Resources: []string{"service-x"}},
					Timestamp:  time.Now(),
				},
			},
			Confidence: 0.7,
		},
		{
			CorrelatorName: "CorrelatorB",
			Findings: []Finding{
				{
					ID:         "b-1",
					Type:       "cpu_throttling",
					Severity:   SeverityHigh,
					Confidence: 0.9,
					Message:    "CPU throttling causing timeouts",
					Impact:     Impact{Resources: []string{"service-x"}},
					Timestamp:  time.Now(),
				},
			},
			Confidence: 0.9,
		},
	}

	result, err := agg.Aggregate(context.Background(), outputs, event)

	require.NoError(t, err)
	require.NotNil(t, result)
	// Should pick CPU throttling due to higher confidence
	assert.Contains(t, result.RootCause, "CPU")
}

func TestAggregate_RuleMatching(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
	}

	agg := NewCorrelationAggregator(logger, config)

	event := &domain.UnifiedEvent{ID: "test-event"}
	outputs := []*CorrelatorOutput{
		{
			CorrelatorName: "ConfigCorrelator",
			Findings: []Finding{
				{
					ID:         "config-1",
					Type:       "config_change",
					Severity:   SeverityMedium,
					Confidence: 0.8,
					Message:    "ConfigMap updated",
					Timestamp:  time.Now(),
				},
			},
			Confidence: 0.8,
		},
		{
			CorrelatorName: "ServiceCorrelator",
			Findings: []Finding{
				{
					ID:         "service-1",
					Type:       "cascade_failure",
					Severity:   SeverityHigh,
					Confidence: 0.85,
					Message:    "Services failing in cascade",
					Timestamp:  time.Now(),
				},
			},
			Confidence: 0.85,
		},
	}

	result, err := agg.Aggregate(context.Background(), outputs, event)

	require.NoError(t, err)
	require.NotNil(t, result)
	// Should match ConfigCascade rule
	assert.Contains(t, result.Summary, "Configuration change triggered")
	assert.Equal(t, 0.9, result.Confidence)
}

func TestAggregate_DegradedAnalysis(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: ConflictResolutionHighestConfidence,
	}

	agg := NewCorrelationAggregator(logger, config)

	event := &domain.UnifiedEvent{ID: "test-event"}
	outputs := []*CorrelatorOutput{
		{
			CorrelatorName: "EmptyCorrelator",
			Findings:       []Finding{}, // No findings
			Confidence:     0.0,
		},
	}

	result, err := agg.Aggregate(context.Background(), outputs, event)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Summary, "Limited analysis")
	assert.Less(t, result.Confidence, 0.5)
}

func TestBuildTimeline(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{}
	agg := NewCorrelationAggregator(logger, config)

	now := time.Now()
	findings := []Finding{
		{
			Message:   "Event 2",
			Type:      "type2",
			Severity:  SeverityMedium,
			Timestamp: now.Add(1 * time.Minute),
			Impact:    Impact{Resources: []string{"res2"}},
		},
		{
			Message:   "Event 1",
			Type:      "type1",
			Severity:  SeverityHigh,
			Timestamp: now,
			Impact:    Impact{Resources: []string{"res1"}},
		},
	}

	timeline := agg.buildTimeline(findings)

	assert.Len(t, timeline, 2)
	// Should be sorted by time
	assert.Equal(t, "Event 1", timeline[0].Event)
	assert.Equal(t, "Event 2", timeline[1].Event)
}

func TestDetermineRootCause(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{}
	agg := NewCorrelationAggregator(logger, config)

	findings := []Finding{
		{
			Message:    "Low priority issue",
			Severity:   SeverityLow,
			Confidence: 0.9,
		},
		{
			Message:    "Critical issue",
			Severity:   SeverityCritical,
			Confidence: 0.7,
		},
	}

	rootCause := agg.determineRootCause(findings, []CausalLink{})

	assert.Equal(t, "Critical issue", rootCause)
}

func TestGenerateRemediation(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{}
	agg := NewCorrelationAggregator(logger, config)

	findings := []Finding{
		{
			Type:    "config_change",
			Message: "ConfigMap modified",
		},
		{
			Type:    "resource_exhaustion",
			Message: "Memory exhausted",
		},
	}

	remediation := agg.generateRemediation(findings, "ConfigMap issue")

	assert.NotEmpty(t, remediation.Steps)
	assert.NotEmpty(t, remediation.Commands)
	assert.NotEmpty(t, remediation.Preventive)
	assert.Equal(t, false, remediation.Automatic)
}

func TestUpdateCorrelatorAccuracy(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{}
	agg := NewCorrelationAggregator(logger, config)

	// Test positive feedback
	agg.UpdateCorrelatorAccuracy("TestCorrelator", true)
	assert.Equal(t, 1.05, agg.correlatorAccuracy["TestCorrelator"])

	// Test negative feedback
	agg.UpdateCorrelatorAccuracy("TestCorrelator", false)
	assert.InDelta(t, 0.9975, agg.correlatorAccuracy["TestCorrelator"], 0.001)

	// Test maximum cap
	for i := 0; i < 20; i++ {
		agg.UpdateCorrelatorAccuracy("MaxCorrelator", true)
	}
	assert.Equal(t, 1.5, agg.correlatorAccuracy["MaxCorrelator"])

	// Test minimum floor
	for i := 0; i < 20; i++ {
		agg.UpdateCorrelatorAccuracy("MinCorrelator", false)
	}
	assert.Equal(t, 0.5, agg.correlatorAccuracy["MinCorrelator"])
}

func TestAddRule(t *testing.T) {
	logger := zap.NewNop()
	config := AggregatorConfig{}
	agg := NewCorrelationAggregator(logger, config)

	initialRuleCount := len(agg.rules)

	newRule := AggregationRule{
		Name:        "TestRule",
		Priority:    150,
		Description: "Test rule",
		Condition:   func(outputs []*CorrelatorOutput) bool { return true },
		Aggregate:   func(outputs []*CorrelatorOutput) *FinalResult { return nil },
	}

	agg.AddRule(newRule)

	assert.Equal(t, initialRuleCount+1, len(agg.rules))
	// Should be sorted by priority
	assert.Equal(t, "TestRule", agg.rules[0].Name)
}
