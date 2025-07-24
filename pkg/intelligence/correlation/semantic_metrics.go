package correlation

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// SemanticMetricsCollector provides enhanced metrics for semantic correlation
type SemanticMetricsCollector struct {
	meter metric.Meter

	// Correlation metrics
	correlationAccuracy metric.Float64Histogram
	predictionSuccess   metric.Int64Counter
	predictionFailure   metric.Int64Counter
	groupLifetime       metric.Float64Histogram

	// Business impact metrics
	incidentsPrevented metric.Int64Counter
	mttrReduction      metric.Float64Histogram
	falsePositiveRate  metric.Float64ObservableGauge
	resourcesSaved     metric.Float64Counter

	// Performance metrics
	correlationLatency   metric.Float64Histogram
	groupMergeOperations metric.Int64Counter
	memoryUsage          metric.Int64ObservableGauge

	// Tracking data
	predictions        map[string]PredictionTracker
	correlationHistory []CorrelationRecord
	mu                 sync.RWMutex
}

// PredictionTracker tracks prediction outcomes
type PredictionTracker struct {
	GroupID          string
	PredictedOutcome string
	PredictionTime   time.Time
	ActualOutcome    string
	OutcomeTime      time.Time
	Validated        bool
}

// CorrelationRecord tracks correlation accuracy
type CorrelationRecord struct {
	Timestamp        time.Time
	GroupID          string
	CorrelationType  string
	EventCount       int
	CorrectlyGrouped int
	FalsePositives   int
	FalseNegatives   int
}

// NewSemanticMetricsCollector creates metrics collector for semantic correlation
func NewSemanticMetricsCollector(meter metric.Meter) (*SemanticMetricsCollector, error) {
	smc := &SemanticMetricsCollector{
		meter:              meter,
		predictions:        make(map[string]PredictionTracker),
		correlationHistory: make([]CorrelationRecord, 0, 10000),
	}

	// Initialize correlation metrics
	correlationAccuracy, err := meter.Float64Histogram(
		"tapio.semantic.correlation.accuracy",
		metric.WithDescription("Accuracy of semantic correlations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	smc.correlationAccuracy = correlationAccuracy

	predictionSuccess, err := meter.Int64Counter(
		"tapio.semantic.prediction.success",
		metric.WithDescription("Successful predictions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	smc.predictionSuccess = predictionSuccess

	predictionFailure, err := meter.Int64Counter(
		"tapio.semantic.prediction.failure",
		metric.WithDescription("Failed predictions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	smc.predictionFailure = predictionFailure

	groupLifetime, err := meter.Float64Histogram(
		"tapio.semantic.group.lifetime",
		metric.WithDescription("Lifetime of semantic groups"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	smc.groupLifetime = groupLifetime

	// Initialize business impact metrics
	incidentsPrevented, err := meter.Int64Counter(
		"tapio.business.incidents.prevented",
		metric.WithDescription("Incidents prevented by early detection"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	smc.incidentsPrevented = incidentsPrevented

	mttrReduction, err := meter.Float64Histogram(
		"tapio.business.mttr.reduction",
		metric.WithDescription("MTTR reduction in minutes"),
		metric.WithUnit("min"),
	)
	if err != nil {
		return nil, err
	}
	smc.mttrReduction = mttrReduction

	falsePositiveRate, err := meter.Float64ObservableGauge(
		"tapio.semantic.false_positive.rate",
		metric.WithDescription("False positive rate of correlations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	smc.falsePositiveRate = falsePositiveRate

	resourcesSaved, err := meter.Float64Counter(
		"tapio.business.resources.saved",
		metric.WithDescription("Resources saved by predictive actions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	smc.resourcesSaved = resourcesSaved

	// Initialize performance metrics
	correlationLatency, err := meter.Float64Histogram(
		"tapio.semantic.correlation.latency",
		metric.WithDescription("Time to correlate events"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	smc.correlationLatency = correlationLatency

	groupMergeOperations, err := meter.Int64Counter(
		"tapio.semantic.group.merges",
		metric.WithDescription("Number of group merge operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	smc.groupMergeOperations = groupMergeOperations

	memoryUsage, err := meter.Int64ObservableGauge(
		"tapio.semantic.memory.usage",
		metric.WithDescription("Memory usage of semantic correlation"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, err
	}
	smc.memoryUsage = memoryUsage

	// Register observable callbacks
	if err := smc.registerObservableCallbacks(); err != nil {
		return nil, err
	}

	// Start background tracking
	go smc.trackingRoutine()

	return smc, nil
}

// RecordPrediction records a new prediction
func (smc *SemanticMetricsCollector) RecordPrediction(ctx context.Context, groupID, predictedOutcome string, probability float64) {
	smc.mu.Lock()
	defer smc.mu.Unlock()

	smc.predictions[groupID] = PredictionTracker{
		GroupID:          groupID,
		PredictedOutcome: predictedOutcome,
		PredictionTime:   time.Now(),
		Validated:        false,
	}
}

// ValidatePrediction validates a previous prediction
func (smc *SemanticMetricsCollector) ValidatePrediction(ctx context.Context, groupID, actualOutcome string) {
	smc.mu.Lock()
	defer smc.mu.Unlock()

	if tracker, exists := smc.predictions[groupID]; exists {
		tracker.ActualOutcome = actualOutcome
		tracker.OutcomeTime = time.Now()
		tracker.Validated = true

		// Check if prediction was correct
		if tracker.PredictedOutcome == actualOutcome {
			smc.predictionSuccess.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("outcome.type", actualOutcome),
					attribute.Float64("time_to_outcome", tracker.OutcomeTime.Sub(tracker.PredictionTime).Seconds()),
				),
			)
		} else {
			smc.predictionFailure.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("predicted", tracker.PredictedOutcome),
					attribute.String("actual", actualOutcome),
				),
			)
		}

		smc.predictions[groupID] = tracker
	}
}

// RecordCorrelationAccuracy records correlation accuracy metrics
func (smc *SemanticMetricsCollector) RecordCorrelationAccuracy(ctx context.Context, groupID string, accuracy float64, correlationType string) {
	smc.correlationAccuracy.Record(ctx, accuracy,
		metric.WithAttributes(
			attribute.String("correlation.type", correlationType),
		),
	)

	// Store for historical analysis
	smc.mu.Lock()
	smc.correlationHistory = append(smc.correlationHistory, CorrelationRecord{
		Timestamp:       time.Now(),
		GroupID:         groupID,
		CorrelationType: correlationType,
		// Other fields would be populated based on validation
	})
	smc.mu.Unlock()
}

// RecordIncidentPrevented records when an incident was prevented
func (smc *SemanticMetricsCollector) RecordIncidentPrevented(ctx context.Context, incidentType string, estimatedImpact float64) {
	smc.incidentsPrevented.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("incident.type", incidentType),
			attribute.Float64("estimated.impact", estimatedImpact),
		),
	)
}

// RecordMTTRReduction records MTTR improvement
func (smc *SemanticMetricsCollector) RecordMTTRReduction(ctx context.Context, reductionMinutes float64, incidentType string) {
	smc.mttrReduction.Record(ctx, reductionMinutes,
		metric.WithAttributes(
			attribute.String("incident.type", incidentType),
		),
	)
}

// RecordResourcesSaved records resource savings
func (smc *SemanticMetricsCollector) RecordResourcesSaved(ctx context.Context, resourceType string, amount float64) {
	smc.resourcesSaved.Add(ctx, amount,
		metric.WithAttributes(
			attribute.String("resource.type", resourceType),
		),
	)
}

// RecordGroupLifetime records the lifetime of a semantic group
func (smc *SemanticMetricsCollector) RecordGroupLifetime(ctx context.Context, groupID string, lifetime time.Duration, eventCount int) {
	smc.groupLifetime.Record(ctx, lifetime.Seconds(),
		metric.WithAttributes(
			attribute.Int("event.count", eventCount),
		),
	)
}

// RecordGroupMerge records when groups are merged
func (smc *SemanticMetricsCollector) RecordGroupMerge(ctx context.Context, sourceGroups int, mergeReason string) {
	smc.groupMergeOperations.Add(ctx, 1,
		metric.WithAttributes(
			attribute.Int("source.groups", sourceGroups),
			attribute.String("merge.reason", mergeReason),
		),
	)
}

// RecordCorrelationLatency records time taken to correlate events
func (smc *SemanticMetricsCollector) RecordCorrelationLatency(ctx context.Context, latency time.Duration, eventCount int) {
	smc.correlationLatency.Record(ctx, latency.Seconds(),
		metric.WithAttributes(
			attribute.Int("event.count", eventCount),
		),
	)
}

// registerObservableCallbacks registers callbacks for observable metrics
func (smc *SemanticMetricsCollector) registerObservableCallbacks() error {
	// False positive rate callback
	_, err := smc.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			rate := smc.calculateFalsePositiveRate()
			o.ObserveFloat64(smc.falsePositiveRate, rate)
			return nil
		},
		smc.falsePositiveRate,
	)
	if err != nil {
		return err
	}

	// Memory usage callback (placeholder - would need actual memory tracking)
	_, err = smc.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			// In production, this would track actual memory usage
			o.ObserveInt64(smc.memoryUsage, 0)
			return nil
		},
		smc.memoryUsage,
	)

	return err
}

// calculateFalsePositiveRate calculates the current false positive rate
func (smc *SemanticMetricsCollector) calculateFalsePositiveRate() float64 {
	smc.mu.RLock()
	defer smc.mu.RUnlock()

	if len(smc.correlationHistory) == 0 {
		return 0.0
	}

	// Calculate from recent history (last 1000 records)
	start := 0
	if len(smc.correlationHistory) > 1000 {
		start = len(smc.correlationHistory) - 1000
	}

	totalPositives := 0
	falsePositives := 0

	for i := start; i < len(smc.correlationHistory); i++ {
		record := smc.correlationHistory[i]
		totalPositives += record.EventCount
		falsePositives += record.FalsePositives
	}

	if totalPositives == 0 {
		return 0.0
	}

	return float64(falsePositives) / float64(totalPositives)
}

// trackingRoutine performs periodic cleanup and analysis
func (smc *SemanticMetricsCollector) trackingRoutine() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		smc.cleanupOldData()
		smc.analyzePredictionAccuracy()
	}
}

// cleanupOldData removes old tracking data
func (smc *SemanticMetricsCollector) cleanupOldData() {
	smc.mu.Lock()
	defer smc.mu.Unlock()

	// Clean up predictions older than 24 hours
	cutoff := time.Now().Add(-24 * time.Hour)
	for groupID, tracker := range smc.predictions {
		if tracker.PredictionTime.Before(cutoff) {
			delete(smc.predictions, groupID)
		}
	}

	// Keep only last 10000 correlation records
	if len(smc.correlationHistory) > 10000 {
		smc.correlationHistory = smc.correlationHistory[len(smc.correlationHistory)-10000:]
	}
}

// analyzePredictionAccuracy analyzes recent prediction accuracy
func (smc *SemanticMetricsCollector) analyzePredictionAccuracy() {
	smc.mu.RLock()
	defer smc.mu.RUnlock()

	total := 0
	correct := 0

	for _, tracker := range smc.predictions {
		if tracker.Validated {
			total++
			if tracker.PredictedOutcome == tracker.ActualOutcome {
				correct++
			}
		}
	}

	if total > 0 {
		accuracy := float64(correct) / float64(total)
		// Could emit this as a metric or log it
		_ = accuracy
	}
}

// GetMetricsSummary returns a summary of current metrics
func (smc *SemanticMetricsCollector) GetMetricsSummary() map[string]interface{} {
	smc.mu.RLock()
	defer smc.mu.RUnlock()

	// Calculate prediction accuracy
	totalPredictions := 0
	correctPredictions := 0
	for _, tracker := range smc.predictions {
		if tracker.Validated {
			totalPredictions++
			if tracker.PredictedOutcome == tracker.ActualOutcome {
				correctPredictions++
			}
		}
	}

	predictionAccuracy := 0.0
	if totalPredictions > 0 {
		predictionAccuracy = float64(correctPredictions) / float64(totalPredictions)
	}

	return map[string]interface{}{
		"prediction_accuracy":   predictionAccuracy,
		"false_positive_rate":   smc.calculateFalsePositiveRate(),
		"total_predictions":     len(smc.predictions),
		"validated_predictions": totalPredictions,
		"correlation_records":   len(smc.correlationHistory),
	}
}
