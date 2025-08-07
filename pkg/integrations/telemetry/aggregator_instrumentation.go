package telemetry

import (
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// AggregatorInstrumentation provides telemetry for the correlation aggregator
type AggregatorInstrumentation struct {
	*ServiceInstrumentation

	// Core aggregation metrics
	CorrelationsAggregated metric.Int64Counter     // Total correlations processed
	ConflictsResolved      metric.Int64Counter     // Conflicts between correlators
	ConfidenceScores       metric.Float64Histogram // Final confidence distribution
	AggregationDuration    metric.Float64Histogram // Time to aggregate
	PatternMatches         metric.Int64Counter     // Known patterns recognized

	// Detailed metrics
	CorrelatorOutputs metric.Int64Histogram   // Number of outputs per aggregation
	AgreementScore    metric.Float64Histogram // How much correlators agree
	DataSufficiency   metric.Float64Histogram // Completeness of data
	ConflictTypes     metric.Int64Counter     // Types of conflicts seen
}

// NewAggregatorInstrumentation creates instrumentation for correlation aggregator
func NewAggregatorInstrumentation(logger *zap.Logger) (*AggregatorInstrumentation, error) {
	base, err := NewServiceInstrumentation("correlation-aggregator", logger)
	if err != nil {
		return nil, err
	}

	meter := base.meter

	// Create core metrics
	correlationsAggregated, err := meter.Int64Counter(
		"tapio.aggregator.correlations.total",
		metric.WithDescription("Total correlations aggregated"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	conflictsResolved, err := meter.Int64Counter(
		"tapio.aggregator.conflicts.resolved",
		metric.WithDescription("Conflicts resolved between correlators"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	confidenceScores, err := meter.Float64Histogram(
		"tapio.aggregator.confidence.score",
		metric.WithDescription("Final confidence scores distribution"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	aggregationDuration, err := meter.Float64Histogram(
		"tapio.aggregator.duration",
		metric.WithDescription("Time to aggregate correlations"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	patternMatches, err := meter.Int64Counter(
		"tapio.aggregator.patterns.matched",
		metric.WithDescription("Known patterns recognized during aggregation"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	// Detailed metrics
	correlatorOutputs, err := meter.Int64Histogram(
		"tapio.aggregator.correlator.outputs",
		metric.WithDescription("Number of correlator outputs per aggregation"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	agreementScore, err := meter.Float64Histogram(
		"tapio.aggregator.agreement.score",
		metric.WithDescription("Agreement level between correlators (0-1)"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	dataSufficiency, err := meter.Float64Histogram(
		"tapio.aggregator.data.sufficiency",
		metric.WithDescription("Data completeness score (0-1)"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	conflictTypes, err := meter.Int64Counter(
		"tapio.aggregator.conflict.types",
		metric.WithDescription("Types of conflicts encountered"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	return &AggregatorInstrumentation{
		ServiceInstrumentation: base,
		CorrelationsAggregated: correlationsAggregated,
		ConflictsResolved:      conflictsResolved,
		ConfidenceScores:       confidenceScores,
		AggregationDuration:    aggregationDuration,
		PatternMatches:         patternMatches,
		CorrelatorOutputs:      correlatorOutputs,
		AgreementScore:         agreementScore,
		DataSufficiency:        dataSufficiency,
		ConflictTypes:          conflictTypes,
	}, nil
}
