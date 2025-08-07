package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

func TestNewAggregatorInstrumentation(t *testing.T) {
	logger := zap.NewNop()

	instr, err := NewAggregatorInstrumentation(logger)
	require.NoError(t, err)
	require.NotNil(t, instr)

	// Verify all metrics are initialized
	assert.NotNil(t, instr.ServiceInstrumentation)
	assert.NotNil(t, instr.CorrelationsAggregated)
	assert.NotNil(t, instr.ConflictsResolved)
	assert.NotNil(t, instr.ConfidenceScores)
	assert.NotNil(t, instr.AggregationDuration)
	assert.NotNil(t, instr.PatternMatches)
	assert.NotNil(t, instr.CorrelatorOutputs)
	assert.NotNil(t, instr.AgreementScore)
	assert.NotNil(t, instr.DataSufficiency)
	assert.NotNil(t, instr.ConflictTypes)
}

func TestAggregatorInstrumentation_CoreMetrics(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	instr, err := NewAggregatorInstrumentation(logger)
	require.NoError(t, err)

	// Test correlations aggregated
	instr.CorrelationsAggregated.Add(ctx, 1, metric.WithAttributes(
		attribute.String("result", "success"),
	))

	// Test conflicts resolved
	instr.ConflictsResolved.Add(ctx, 1, metric.WithAttributes(
		attribute.String("resolution", "confidence_based"),
	))

	// Test confidence scores
	instr.ConfidenceScores.Record(ctx, 0.95, metric.WithAttributes(
		attribute.String("correlation_type", "config_impact"),
	))

	// Test aggregation duration
	start := time.Now()
	time.Sleep(5 * time.Millisecond)
	duration := time.Since(start).Seconds()
	instr.AggregationDuration.Record(ctx, duration)

	// Test pattern matches
	instr.PatternMatches.Add(ctx, 1, metric.WithAttributes(
		attribute.String("pattern", "config_change_cascade"),
	))
}

func TestAggregatorInstrumentation_DetailedMetrics(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	instr, err := NewAggregatorInstrumentation(logger)
	require.NoError(t, err)

	// Test correlator outputs histogram
	instr.CorrelatorOutputs.Record(ctx, 3) // 3 correlators provided output

	// Test agreement score
	instr.AgreementScore.Record(ctx, 0.8, metric.WithAttributes(
		attribute.String("disagreement_type", "minor"),
	))

	// Test data sufficiency
	instr.DataSufficiency.Record(ctx, 0.9, metric.WithAttributes(
		attribute.Bool("has_k8s_context", true),
		attribute.Bool("has_trace_context", true),
	))

	// Test conflict types
	instr.ConflictTypes.Add(ctx, 1, metric.WithAttributes(
		attribute.String("type", "root_cause_mismatch"),
		attribute.String("resolution", "highest_confidence"),
	))
}

func TestAggregatorInstrumentation_SpanIntegration(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	instr, err := NewAggregatorInstrumentation(logger)
	require.NoError(t, err)

	// Test span creation for aggregation
	ctx, span := instr.StartSpan(ctx, "aggregate_correlations")
	assert.NotNil(t, span)

	// Add span attributes
	span.SetAttributes(
		attribute.Int("correlator_count", 3),
		attribute.Float64("max_confidence", 0.95),
	)

	// End span
	start := time.Now().Add(-10 * time.Millisecond)
	instr.EndSpan(span, start, nil, "aggregate_correlations")
}

func TestAggregatorInstrumentation_ConflictScenarios(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	instr, err := NewAggregatorInstrumentation(logger)
	require.NoError(t, err)

	testCases := []struct {
		name           string
		conflictType   string
		resolution     string
		agreementScore float64
	}{
		{
			name:           "unanimous_agreement",
			conflictType:   "none",
			resolution:     "unanimous",
			agreementScore: 1.0,
		},
		{
			name:           "minor_disagreement",
			conflictType:   "detail_mismatch",
			resolution:     "majority_vote",
			agreementScore: 0.7,
		},
		{
			name:           "major_conflict",
			conflictType:   "root_cause_conflict",
			resolution:     "confidence_based",
			agreementScore: 0.3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.conflictType != "none" {
				instr.ConflictsResolved.Add(ctx, 1, metric.WithAttributes(
					attribute.String("type", tc.conflictType),
					attribute.String("resolution", tc.resolution),
				))
			}
			instr.AgreementScore.Record(ctx, tc.agreementScore)
		})
	}
}

func TestAggregatorInstrumentation_ConcurrentAggregation(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	instr, err := NewAggregatorInstrumentation(logger)
	require.NoError(t, err)

	// Simulate concurrent aggregations
	done := make(chan bool)
	for i := 0; i < 5; i++ {
		go func(id int) {
			start := time.Now()

			// Record various metrics
			instr.CorrelationsAggregated.Add(ctx, 1)
			instr.CorrelatorOutputs.Record(ctx, int64(id+1))
			instr.ConfidenceScores.Record(ctx, 0.8+float64(id)*0.02)

			duration := time.Since(start).Seconds()
			instr.AggregationDuration.Record(ctx, duration)

			done <- true
		}(i)
	}

	// Wait for completion
	for i := 0; i < 5; i++ {
		<-done
	}
}

func BenchmarkAggregatorInstrumentation_RecordMetrics(b *testing.B) {
	logger := zap.NewNop()
	ctx := context.Background()

	instr, err := NewAggregatorInstrumentation(logger)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		instr.CorrelationsAggregated.Add(ctx, 1)
		instr.ConfidenceScores.Record(ctx, 0.85)
		instr.AgreementScore.Record(ctx, 0.9)
	}
}
