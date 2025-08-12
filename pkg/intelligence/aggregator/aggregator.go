package aggregator

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// CorrelationAggregator combines outputs from multiple correlators into a single answer
type CorrelationAggregator struct {
	logger *zap.Logger

	// OTEL instrumentation
	tracer              trace.Tracer
	aggregationDuration metric.Float64Histogram
}

// NewCorrelationAggregator creates a new aggregator
func NewCorrelationAggregator(logger *zap.Logger) *CorrelationAggregator {
	tracer := otel.Tracer("correlation-aggregator")
	meter := otel.Meter("correlation-aggregator")

	aggregationDuration, err := meter.Float64Histogram(
		"aggregator_aggregation_duration_ms",
		metric.WithDescription("Aggregation processing duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create aggregation duration histogram", zap.Error(err))
	}

	return &CorrelationAggregator{
		logger:              logger,
		tracer:              tracer,
		aggregationDuration: aggregationDuration,
	}
}

// Aggregate combines multiple correlator outputs into a final result
func (a *CorrelationAggregator) Aggregate(ctx context.Context, outputs []*CorrelatorOutput, event *domain.UnifiedEvent) (*FinalResult, error) {
	ctx, span := a.tracer.Start(ctx, "aggregator.aggregate")
	defer span.End()

	start := time.Now()
	defer func() {
		if a.aggregationDuration != nil {
			duration := time.Since(start).Seconds() * 1000
			a.aggregationDuration.Record(ctx, duration)
		}
	}()

	// Validate inputs
	if len(outputs) == 0 {
		return nil, fmt.Errorf("no correlator outputs to aggregate")
	}

	a.logger.Info("Starting aggregation", zap.String("event_id", event.ID))

	// Step 1: Extract all findings
	// Pre-allocate slice with total capacity to avoid reallocation

	totalFindings := 0
	for _, output := range outputs {
		totalFindings += len(output.Findings)
	}

	allFindings := make([]Finding, 0, totalFindings)
	for _, output := range outputs {
		allFindings = append(allFindings, output.Findings...)
	}

	if len(allFindings) == 0 {
		return &FinalResult{
			ID:         fmt.Sprintf("empty-%s", event.ID),
			Summary:    "No findings from correlators",
			RootCause:  "Unable to determine root cause",
			Confidence: 0.0,
			Timestamp:  time.Now(),
		}, nil
	}

	// Step 2: Pick the first finding with highest confidence
	bestFinding := allFindings[0]
	for _, finding := range allFindings {
		if finding.Confidence > bestFinding.Confidence {
			bestFinding = finding
		}
	}

	// Step 3: Build simple result
	result := &FinalResult{
		ID:             fmt.Sprintf("agg-%d", time.Now().Unix()),
		Summary:        bestFinding.Message,
		RootCause:      bestFinding.Message,
		Confidence:     bestFinding.Confidence,
		Timestamp:      time.Now(),
		ProcessingTime: time.Since(start),
		Remediation: Remediation{
			Automatic: false,
			Steps:     []string{"Log: " + bestFinding.Message},
		},
	}

	a.logger.Info("Aggregation complete", zap.String("root_cause", result.RootCause))
	return result, nil
}
