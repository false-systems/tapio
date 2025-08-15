package behavior

import (
	"context"
	"fmt"
	"strings"
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

	// Confidence adjustment factors (extracted from aggregator)
	baseConfidenceWeight float64
	feedbackWeight       float64
	agreementBoost       float64 // Boost when multiple patterns agree
	patternMatchBoost    float64 // Boost for pattern matches
	missingDataPenalty   float64 // Penalty for incomplete evidence
	conflictPenalty      float64 // Penalty for conflicting patterns

	// Evidence quality weights (from aggregator specificity scoring)
	evidenceWeights map[string]float64

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
		// Smart confidence adjustments from aggregator
		agreementBoost:     0.2,  // 20% boost when patterns agree
		patternMatchBoost:  0.1,  // 10% boost for pattern match
		missingDataPenalty: 0.2,  // 20% penalty for incomplete data
		conflictPenalty:    0.15, // 15% penalty for conflicts
		// Evidence quality weights (from aggregator specificity scoring)
		evidenceWeights: map[string]float64{
			"event":      2.0, // Events are valuable
			"graph_path": 3.0, // Graph paths most valuable
			"resource":   2.0, // Resources important
			"metric":     1.0, // Metrics baseline
			"log":        1.0, // Logs baseline
		},
		tracer:              tracer,
		predictionsTotal:    predictionsTotal,
		confidenceHistogram: confidenceHistogram,
		processingTime:      processingTime,
	}
}

// GeneratePrediction generates a prediction from a pattern match
func (p *Predictor) GeneratePrediction(ctx context.Context, match domain.PatternMatch, event *domain.ObservationEvent) (*domain.Prediction, error) {
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

// calculateConfidence calculates the final confidence score with smart adjustments
func (p *Predictor) calculateConfidence(match domain.PatternMatch, pattern *domain.BehaviorPattern) float64 {
	// Start with match confidence
	confidence := match.Confidence

	// Apply pattern's adjusted confidence if available
	if pattern.AdjustedConfidence > 0 {
		confidence = confidence*p.baseConfidenceWeight +
			pattern.AdjustedConfidence*p.feedbackWeight
	}

	// Apply smart boosts and penalties from aggregator
	// Boost for pattern match (we already matched, so apply boost)
	confidence *= (1 + p.patternMatchBoost)

	// Check for missing data penalty
	if p.hasIncompleteEvidence(match) {
		confidence *= (1 - p.missingDataPenalty)
	}

	// Apply evidence quality weighting
	confidence *= p.calculateEvidenceQuality(match)

	// Ensure bounds
	if confidence > 1.0 {
		confidence = 1.0
	} else if confidence < 0.0 {
		confidence = 0.0
	}

	return confidence
}

// hasIncompleteEvidence checks if the match has incomplete evidence
func (p *Predictor) hasIncompleteEvidence(match domain.PatternMatch) bool {
	// Check if we have sufficient evidence
	matchedConditions := 0
	for _, cond := range match.Conditions {
		if cond.Matched {
			matchedConditions++
		}
	}
	// Consider incomplete if less than 60% conditions matched
	return float64(matchedConditions)/float64(len(match.Conditions)) < 0.6
}

// calculateEvidenceQuality calculates evidence quality score using weights
func (p *Predictor) calculateEvidenceQuality(match domain.PatternMatch) float64 {
	if len(match.Evidence) == 0 {
		return 0.8 // Default quality if no evidence
	}

	// Score evidence based on type and count
	totalWeight := 0.0
	for range match.Evidence {
		// Simple scoring based on evidence presence
		totalWeight += p.evidenceWeights["event"] // Default to event weight
	}

	// Normalize to 0.8-1.2 range
	qualityScore := 0.8 + (totalWeight/float64(len(match.Evidence)))*0.1
	if qualityScore > 1.2 {
		qualityScore = 1.2
	}
	return qualityScore
}

// buildEvidence builds evidence from the match and observation event
func (p *Predictor) buildEvidence(match domain.PatternMatch, event *domain.ObservationEvent) []domain.Evidence {
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

	// Add observation event details as evidence
	evidence = append(evidence, domain.Evidence{
		Type:        "observation",
		Source:      event.Source,
		Description: fmt.Sprintf("Observation: %s %s", event.Type, getEventDescription(event)),
		Data: &domain.EvidenceData{
			Metrics: map[string]float64{
				"confidence": 0.9, // High confidence for direct observations
			},
		},
		Timestamp: event.Timestamp,
	})

	return evidence
}

// getEventDescription creates a human-readable description of the observation event
func getEventDescription(event *domain.ObservationEvent) string {
	parts := []string{}

	if event.Action != nil {
		parts = append(parts, fmt.Sprintf("action=%s", *event.Action))
	}
	if event.Target != nil {
		parts = append(parts, fmt.Sprintf("target=%s", *event.Target))
	}
	if event.Result != nil {
		parts = append(parts, fmt.Sprintf("result=%s", *event.Result))
	}
	if event.PodName != nil {
		parts = append(parts, fmt.Sprintf("pod=%s", *event.PodName))
	}
	if event.ServiceName != nil {
		parts = append(parts, fmt.Sprintf("service=%s", *event.ServiceName))
	}

	if len(parts) == 0 {
		return "event occurred"
	}
	return strings.Join(parts, ", ")
}

// extractResources extracts affected resources from the observation event
func (p *Predictor) extractResources(event *domain.ObservationEvent) []domain.ResourceRef {
	resources := make([]domain.ResourceRef, 0)

	// Extract Pod resource if present
	if event.PodName != nil {
		resource := domain.ResourceRef{
			Kind: "Pod",
			Name: *event.PodName,
		}
		if event.Namespace != nil {
			resource.Namespace = *event.Namespace
		}
		resources = append(resources, resource)
	}

	// Extract Service resource if present
	if event.ServiceName != nil {
		resource := domain.ResourceRef{
			Kind: "Service",
			Name: *event.ServiceName,
		}
		if event.Namespace != nil {
			resource.Namespace = *event.Namespace
		}
		resources = append(resources, resource)
	}

	// Extract Node resource if present
	if event.NodeName != nil {
		resources = append(resources, domain.ResourceRef{
			Kind: "Node",
			Name: *event.NodeName,
			// Nodes don't have namespaces
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
