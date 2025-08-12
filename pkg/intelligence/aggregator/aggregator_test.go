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

	agg := NewCorrelationAggregator(logger)

	assert.NotNil(t, agg)
	assert.NotNil(t, agg.logger)
	assert.NotNil(t, agg.tracer)
}

func TestAggregate_NoOutputs(t *testing.T) {
	logger := zap.NewNop()
	agg := NewCorrelationAggregator(logger)

	event := &domain.UnifiedEvent{ID: "test-event"}
	result, err := agg.Aggregate(context.Background(), []*CorrelatorOutput{}, event)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no correlator outputs")
}

func TestAggregate_NoFindings(t *testing.T) {
	logger := zap.NewNop()
	agg := NewCorrelationAggregator(logger)

	event := &domain.UnifiedEvent{ID: "test-event"}
	outputs := []*CorrelatorOutput{
		{
			CorrelatorName: "TestCorrelator",
			Findings:       []Finding{},
			Confidence:     0.8,
		},
	}

	result, err := agg.Aggregate(context.Background(), outputs, event)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "No findings from correlators", result.Summary)
	assert.Equal(t, 0.0, result.Confidence)
}

func TestAggregate_WithFindings(t *testing.T) {
	logger := zap.NewNop()
	agg := NewCorrelationAggregator(logger)

	event := &domain.UnifiedEvent{ID: "test-event"}
	outputs := []*CorrelatorOutput{
		{
			CorrelatorName: "TestCorrelator",
			Findings: []Finding{
				{
					ID:         "finding-1",
					Type:       "memory_exhaustion",
					Severity:   SeverityHigh,
					Confidence: 0.8,
					Message:    "Memory usage at 95%",
					Timestamp:  time.Now(),
				},
			},
			Confidence: 0.8,
		},
	}

	result, err := agg.Aggregate(context.Background(), outputs, event)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.ID)
	assert.Equal(t, "Memory usage at 95%", result.RootCause)
	assert.Equal(t, 0.8, result.Confidence)
	assert.NotNil(t, result.Remediation)
	assert.Equal(t, []string{"Log: Memory usage at 95%"}, result.Remediation.Steps)
}

func TestAggregate_MultipleFindings_PicksHighestConfidence(t *testing.T) {
	logger := zap.NewNop()
	agg := NewCorrelationAggregator(logger)

	event := &domain.UnifiedEvent{ID: "test-event"}
	outputs := []*CorrelatorOutput{
		{
			CorrelatorName: "CorrelatorA",
			Findings: []Finding{
				{
					ID:         "finding-1",
					Type:       "memory_exhaustion",
					Confidence: 0.7,
					Message:    "Memory at 70%",
					Timestamp:  time.Now(),
				},
			},
		},
		{
			CorrelatorName: "CorrelatorB",
			Findings: []Finding{
				{
					ID:         "finding-2",
					Type:       "cpu_exhaustion",
					Confidence: 0.9,
					Message:    "CPU at 95%",
					Timestamp:  time.Now(),
				},
			},
		},
	}

	result, err := agg.Aggregate(context.Background(), outputs, event)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "CPU at 95%", result.RootCause)
	assert.Equal(t, 0.9, result.Confidence)
}
