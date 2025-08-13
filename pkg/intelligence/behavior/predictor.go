package behavior

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Predictor generates predictions from pattern matches
type Predictor struct {
	logger *zap.Logger

	// Pattern loader for finding patterns
	patternLoader *PatternLoader

	// Confidence adjustment factors
	baseConfidenceWeight float64
	feedbackWeight       float64

	// OTEL instrumentation
	tracer              trace.Tracer
	predictionsTotal    metric.Int64Counter
	confidenceHistogram metric.Float64Histogram
	processingTime      metric.Float64Histogram
}

// NewPredictor creates a new predictor
func NewPredictor(logger *zap.Logger) *Predictor {
	// Initialize OTEL
	tracer := otel.Tracer("behavior-predictor")
	meter := otel.Meter("behavior-predictor")

	predictionsTotal, _ := meter.Int64Counter(
		"behavior_predictions_total",
		metric.WithDescription("Total predictions generated"),
	)

	confidenceHistogram, _ := meter.Float64Histogram(
		"behavior_prediction_confidence",
		metric.WithDescription("Prediction confidence distribution"),
	)

	processingTime, _ := meter.Float64Histogram(
		"behavior_prediction_duration_ms",
		metric.WithDescription("Prediction generation duration"),
		metric.WithUnit("ms"),
	)

	return &Predictor{
		logger:               logger,
		baseConfidenceWeight: 0.7,
		feedbackWeight:       0.3,
		tracer:               tracer,
		predictionsTotal:     predictionsTotal,
		confidenceHistogram:  confidenceHistogram,
		processingTime:       processingTime,
	}
}

// GeneratePrediction generates a prediction from a pattern match
func (p *Predictor) GeneratePrediction(ctx context.Context, match domain.PatternMatch, event *domain.UnifiedEvent) (*domain.Prediction, error) {
	ctx, span := p.tracer.Start(ctx, "predictor.generate")
	defer span.End()

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Milliseconds()
		if p.processingTime != nil {
			p.processingTime.Record(ctx, float64(duration))
		}
	}()

	// Find the pattern for this match
	pattern := p.findPattern(match.PatternID)
	if pattern == nil {
		return nil, fmt.Errorf("pattern %s not found", match.PatternID)
	}

	// Get the prediction template from the pattern
	predictionTemplate := pattern.PredictionTemplate

	// Calculate final confidence
	confidence := p.calculateConfidence(match, pattern)

	// Parse time horizon
	timeHorizon, err := time.ParseDuration(predictionTemplate.TimeHorizon)
	if err != nil {
		timeHorizon = 10 * time.Minute // Default
	}

	// Build evidence
	evidence := p.buildEvidence(match, event)

	// Extract affected resources
	resources := p.extractResources(event)

	// Create prediction
	prediction := &domain.Prediction{
		ID:          uuid.New().String(),
		PatternID:   match.PatternID,
		PatternName: match.PatternName,
		EventID:     event.ID,
		Type:        predictionTemplate.Type,
		Confidence:  confidence,
		TimeHorizon: timeHorizon,
		Message:     predictionTemplate.Message,
		Impact:      predictionTemplate.Impact,
		Severity:    predictionTemplate.Severity,
		Resources:   resources,
		Evidence:    evidence,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(timeHorizon),
		Status:      domain.PredictionStatusActive,
	}

	// Add remediation if available
	if pattern.Remediation != nil {
		prediction.Remediation = pattern.Remediation
	}

	// Record metrics
	if p.predictionsTotal != nil {
		p.predictionsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("pattern", match.PatternName),
			attribute.String("type", string(predictionTemplate.Type)),
		))
	}

	if p.confidenceHistogram != nil {
		p.confidenceHistogram.Record(ctx, confidence)
	}

	span.SetAttributes(
		attribute.String("prediction.id", prediction.ID),
		attribute.String("pattern.name", match.PatternName),
		attribute.Float64("confidence", confidence),
	)

	p.logger.Info("Prediction generated",
		zap.String("prediction_id", prediction.ID),
		zap.String("pattern", match.PatternName),
		zap.Float64("confidence", confidence),
		zap.Duration("time_horizon", timeHorizon),
	)

	return prediction, nil
}

// calculateConfidence calculates the final confidence score
func (p *Predictor) calculateConfidence(match domain.PatternMatch, pattern *domain.BehaviorPattern) float64 {
	// Start with match confidence
	confidence := match.Confidence

	// Apply pattern's adjusted confidence if available
	if pattern.AdjustedConfidence > 0 {
		confidence = confidence*p.baseConfidenceWeight +
			pattern.AdjustedConfidence*p.feedbackWeight
	}

	// Ensure bounds
	if confidence > 1.0 {
		confidence = 1.0
	} else if confidence < 0.0 {
		confidence = 0.0
	}

	return confidence
}

// buildEvidence builds evidence from the match and event
func (p *Predictor) buildEvidence(match domain.PatternMatch, event *domain.UnifiedEvent) []domain.Evidence {
	evidence := make([]domain.Evidence, 0)

	// Add matched conditions as evidence
	for _, condition := range match.Conditions {
		if condition.Matched {
			evidence = append(evidence, domain.Evidence{
				Type:        "condition",
				Source:      match.PatternName,
				Description: condition.Message,
				Data: &domain.EvidenceData{
					Metrics: map[string]float64{
						"confidence": 1.0,
					},
				},
				Timestamp: event.Timestamp,
			})
		}
	}

	// Add event details as evidence
	if event.Semantic != nil {
		evidence = append(evidence, domain.Evidence{
			Type:        "semantic",
			Source:      "event",
			Description: event.Semantic.Narrative,
			Data: &domain.EvidenceData{
				Metrics: map[string]float64{
					"confidence": event.Semantic.Confidence,
				},
			},
			Timestamp: event.Timestamp,
		})
	}

	return evidence
}

// extractResources extracts affected resources from the event
func (p *Predictor) extractResources(event *domain.UnifiedEvent) []domain.ResourceRef {
	resources := make([]domain.ResourceRef, 0)

	if event.K8sContext != nil {
		resources = append(resources, domain.ResourceRef{
			Kind:      event.K8sContext.Kind,
			Name:      event.K8sContext.Name,
			Namespace: event.K8sContext.Namespace,
		})
	}

	// Extract from entity context if available
	if event.Entity != nil {
		resources = append(resources, domain.ResourceRef{
			Kind:      event.Entity.Type,
			Name:      event.Entity.Name,
			Namespace: event.Entity.Namespace,
		})
	}

	return resources
}

// findPattern finds a pattern by ID
func (p *Predictor) findPattern(patternID string) *domain.BehaviorPattern {
	if p.patternLoader == nil {
		p.logger.Warn("Pattern loader not initialized")
		return nil
	}
	pattern, exists := p.patternLoader.GetPattern(patternID)
	if !exists {
		return nil
	}
	return pattern
}

// AdjustConfidenceFromFeedback adjusts prediction confidence based on feedback
func (p *Predictor) AdjustConfidenceFromFeedback(ctx context.Context, feedback *domain.UserFeedback) error {
	ctx, span := p.tracer.Start(ctx, "predictor.adjust_confidence")
	defer span.End()

	// Calculate adjustment factor based on feedback
	var adjustmentFactor float64
	if feedback.Accurate {
		adjustmentFactor = 1.1 // Increase confidence
	} else {
		adjustmentFactor = 0.9 // Decrease confidence
	}

	// Apply rating weight
	switch feedback.Rating {
	case domain.RatingThumbsUp:
		adjustmentFactor *= 1.05
	case domain.RatingThumbsDown:
		adjustmentFactor *= 0.95
	}

	span.SetAttributes(
		attribute.String("pattern_id", feedback.PatternID),
		attribute.Bool("accurate", feedback.Accurate),
		attribute.Float64("adjustment", adjustmentFactor),
	)

	p.logger.Info("Confidence adjusted from feedback",
		zap.String("pattern_id", feedback.PatternID),
		zap.Bool("accurate", feedback.Accurate),
		zap.Float64("adjustment", adjustmentFactor),
	)

	return nil
}
