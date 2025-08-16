package intelligence

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/domain"
)

// SafetyScorer analyzes deployment events and calculates safety scores
type SafetyScorer struct {
	logger *zap.Logger

	// OTEL instrumentation
	tracer           trace.Tracer
	scoresCalculated metric.Int64Counter
	scoringTime      metric.Float64Histogram
	averageScore     metric.Float64Gauge

	// Configuration
	config ScoringConfig

	// Historical data for pattern detection
	deploymentHistory map[string]*DeploymentHistory

	// Output channel for safety scores
	safetyScores chan *domain.SafetyScore
}

// ScoringConfig contains configuration for the safety scorer
type ScoringConfig struct {
	// Weight factors for different risk signals (0.0 to 1.0)
	ImageChangeWeight       float64
	ScaleChangeWeight       float64
	FrequencyWeight         float64
	TimeOfDayWeight         float64
	HistoricalFailureWeight float64

	// Thresholds
	HighFrequencyThreshold time.Duration // Deployments closer than this are risky
	RiskyHoursStart        int           // Hour of day (0-23)
	RiskyHoursEnd          int           // Hour of day (0-23)

	// History settings
	MaxHistoryEntries      int
	HistoryRetentionPeriod time.Duration
}

// DefaultScoringConfig returns default scoring configuration
func DefaultScoringConfig() ScoringConfig {
	return ScoringConfig{
		ImageChangeWeight:       0.3,
		ScaleChangeWeight:       0.2,
		FrequencyWeight:         0.25,
		TimeOfDayWeight:         0.15,
		HistoricalFailureWeight: 0.1,
		HighFrequencyThreshold:  5 * time.Minute,
		RiskyHoursStart:         17, // 5 PM
		RiskyHoursEnd:           20, // 8 PM
		MaxHistoryEntries:       100,
		HistoryRetentionPeriod:  7 * 24 * time.Hour,
	}
}

// DeploymentHistory tracks historical deployment data
type DeploymentHistory struct {
	LastDeployment   time.Time
	DeploymentCount  int
	FailureCount     int
	LastFailure      time.Time
	AverageInterval  time.Duration
	LastImages       []string
	LastReplicaCount int32
}

// NewSafetyScorer creates a new safety scorer
func NewSafetyScorer(logger *zap.Logger, config ScoringConfig) (*SafetyScorer, error) {
	tracer := otel.Tracer("tapio.intelligence.safety_scorer")
	meter := otel.Meter("tapio.intelligence.safety_scorer")

	scoresCalculated, err := meter.Int64Counter(
		"safety_scorer_scores_calculated_total",
		metric.WithDescription("Total number of safety scores calculated"),
	)
	if err != nil {
		logger.Warn("Failed to create scores calculated counter", zap.Error(err))
	}

	scoringTime, err := meter.Float64Histogram(
		"safety_scorer_scoring_duration_ms",
		metric.WithDescription("Time taken to calculate safety score in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create scoring time histogram", zap.Error(err))
	}

	averageScore, err := meter.Float64Gauge(
		"safety_scorer_average_score",
		metric.WithDescription("Running average of safety scores (0=safe, 1=risky)"),
	)
	if err != nil {
		logger.Warn("Failed to create average score gauge", zap.Error(err))
	}

	return &SafetyScorer{
		logger:            logger,
		tracer:            tracer,
		scoresCalculated:  scoresCalculated,
		scoringTime:       scoringTime,
		averageScore:      averageScore,
		config:            config,
		deploymentHistory: make(map[string]*DeploymentHistory),
		safetyScores:      make(chan *domain.SafetyScore, 1000),
	}, nil
}

// CalculateScore calculates a safety score for a deployment event
func (s *SafetyScorer) CalculateScore(ctx context.Context, event *domain.DeploymentEvent) (*domain.SafetyScore, error) {
	ctx, span := s.tracer.Start(ctx, "safety_scorer.calculate_score")
	defer span.End()

	start := time.Now()
	defer func() {
		if s.scoringTime != nil {
			duration := time.Since(start).Seconds() * 1000
			s.scoringTime.Record(ctx, duration)
		}
	}()

	span.SetAttributes(
		attribute.String("deployment.name", event.Name),
		attribute.String("deployment.namespace", event.Namespace),
		attribute.String("deployment.action", string(event.Action)),
	)

	// Initialize score factors
	factors := []domain.ScoreFactor{}
	totalScore := 0.0
	totalWeight := 0.0

	// Factor 1: Image change risk
	if event.HasImageChange() {
		imageRisk := s.calculateImageChangeRisk(event)
		factors = append(factors, domain.ScoreFactor{
			Name:        "image_change",
			Impact:      imageRisk,
			Weight:      s.config.ImageChangeWeight,
			Description: fmt.Sprintf("Image changed from %s to %s", event.Metadata.OldImage, event.Metadata.NewImage),
		})
		totalScore += imageRisk * s.config.ImageChangeWeight
		totalWeight += s.config.ImageChangeWeight
	}

	// Factor 2: Scale change risk
	if event.HasScaleChange() {
		scaleRisk := s.calculateScaleChangeRisk(event)
		factors = append(factors, domain.ScoreFactor{
			Name:        "scale_change",
			Impact:      scaleRisk,
			Weight:      s.config.ScaleChangeWeight,
			Description: fmt.Sprintf("Scaling from %d to %d replicas", event.Metadata.OldReplicas, event.Metadata.NewReplicas),
		})
		totalScore += scaleRisk * s.config.ScaleChangeWeight
		totalWeight += s.config.ScaleChangeWeight
	}

	// Factor 3: Deployment frequency risk
	frequencyRisk := s.calculateFrequencyRisk(event)
	factors = append(factors, domain.ScoreFactor{
		Name:        "deployment_frequency",
		Impact:      frequencyRisk,
		Weight:      s.config.FrequencyWeight,
		Description: "Deployment frequency analysis",
	})
	totalScore += frequencyRisk * s.config.FrequencyWeight
	totalWeight += s.config.FrequencyWeight

	// Factor 4: Time of day risk
	timeRisk := s.calculateTimeOfDayRisk(event)
	factors = append(factors, domain.ScoreFactor{
		Name:        "time_of_day",
		Impact:      timeRisk,
		Weight:      s.config.TimeOfDayWeight,
		Description: fmt.Sprintf("Deployment at %s", event.Timestamp.Format("15:04")),
	})
	totalScore += timeRisk * s.config.TimeOfDayWeight
	totalWeight += s.config.TimeOfDayWeight

	// Factor 5: Historical failure risk
	historyRisk := s.calculateHistoricalRisk(event)
	factors = append(factors, domain.ScoreFactor{
		Name:        "historical_failures",
		Impact:      historyRisk,
		Weight:      s.config.HistoricalFailureWeight,
		Description: "Based on past deployment failures",
	})
	totalScore += historyRisk * s.config.HistoricalFailureWeight
	totalWeight += s.config.HistoricalFailureWeight

	// Calculate final score
	finalScore := 0.0
	if totalWeight > 0 {
		finalScore = totalScore / totalWeight
	}

	// Calculate confidence based on available data
	confidence := s.calculateConfidence(event)

	score := &domain.SafetyScore{
		Value:        finalScore,
		Factors:      factors,
		Confidence:   confidence,
		Timestamp:    time.Now(),
		DeploymentID: fmt.Sprintf("%s/%s", event.Namespace, event.Name),
	}

	// Validate the score
	if err := score.Validate(); err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		return nil, fmt.Errorf("invalid safety score: %w", err)
	}

	// Update history
	s.updateHistory(event)

	// Update metrics
	if s.scoresCalculated != nil {
		s.scoresCalculated.Add(ctx, 1, metric.WithAttributes(
			attribute.String("risk_level", string(score.GetRiskLevel())),
			attribute.String("namespace", event.Namespace),
		))
	}

	if s.averageScore != nil {
		s.averageScore.Record(ctx, finalScore)
	}

	span.SetAttributes(
		attribute.Float64("safety_score", finalScore),
		attribute.Float64("confidence", confidence),
		attribute.String("risk_level", string(score.GetRiskLevel())),
	)

	return score, nil
}

// calculateImageChangeRisk calculates risk for image changes
func (s *SafetyScorer) calculateImageChangeRisk(event *domain.DeploymentEvent) float64 {
	if !event.HasImageChange() {
		return 0.0
	}

	// Major version change = higher risk
	oldImage := event.Metadata.OldImage
	newImage := event.Metadata.NewImage

	// Simple heuristic: if tags are very different, it's riskier
	if oldImage == "" || newImage == "" {
		return 0.5 // Medium risk for unknown changes
	}

	// Check for latest tag (risky)
	if containsLatestTag(newImage) {
		return 0.8
	}

	// Check for major version changes
	if isMajorVersionChange(oldImage, newImage) {
		return 0.7
	}

	// Minor version change
	return 0.3
}

// calculateScaleChangeRisk calculates risk for scaling operations
func (s *SafetyScorer) calculateScaleChangeRisk(event *domain.DeploymentEvent) float64 {
	if !event.HasScaleChange() {
		return 0.0
	}

	oldReplicas := event.Metadata.OldReplicas
	newReplicas := event.Metadata.NewReplicas

	// Scaling to zero is very risky
	if newReplicas == 0 {
		return 0.9
	}

	// Large scale changes are risky
	scaleFactor := float64(newReplicas) / float64(max(oldReplicas, 1))

	if scaleFactor > 3.0 || scaleFactor < 0.33 {
		return 0.7 // Large scale change
	}

	if scaleFactor > 2.0 || scaleFactor < 0.5 {
		return 0.4 // Moderate scale change
	}

	return 0.2 // Small scale change
}

// calculateFrequencyRisk calculates risk based on deployment frequency
func (s *SafetyScorer) calculateFrequencyRisk(event *domain.DeploymentEvent) float64 {
	key := fmt.Sprintf("%s/%s", event.Namespace, event.Name)
	history, exists := s.deploymentHistory[key]

	if !exists || history.LastDeployment.IsZero() {
		return 0.1 // First deployment, low frequency risk
	}

	timeSinceLastDeployment := event.Timestamp.Sub(history.LastDeployment)

	if timeSinceLastDeployment < s.config.HighFrequencyThreshold {
		// Very frequent deployments are risky
		return 0.8
	}

	if timeSinceLastDeployment < s.config.HighFrequencyThreshold*2 {
		return 0.5
	}

	if timeSinceLastDeployment < s.config.HighFrequencyThreshold*4 {
		return 0.3
	}

	return 0.1 // Infrequent deployments are safer
}

// calculateTimeOfDayRisk calculates risk based on time of day
func (s *SafetyScorer) calculateTimeOfDayRisk(event *domain.DeploymentEvent) float64 {
	hour := event.Timestamp.Hour()

	// Risky hours (e.g., end of day)
	if hour >= s.config.RiskyHoursStart && hour <= s.config.RiskyHoursEnd {
		return 0.7
	}

	// Weekend deployments
	weekday := event.Timestamp.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return 0.6
	}

	// Night deployments (0-6 AM)
	if hour >= 0 && hour < 6 {
		return 0.5
	}

	// Business hours on weekdays
	if hour >= 9 && hour < 17 && weekday != time.Saturday && weekday != time.Sunday {
		return 0.1 // Safest time
	}

	return 0.3
}

// calculateHistoricalRisk calculates risk based on historical failures
func (s *SafetyScorer) calculateHistoricalRisk(event *domain.DeploymentEvent) float64 {
	key := fmt.Sprintf("%s/%s", event.Namespace, event.Name)
	history, exists := s.deploymentHistory[key]

	if !exists || history.DeploymentCount == 0 {
		return 0.5 // Unknown history, medium risk
	}

	failureRate := float64(history.FailureCount) / float64(history.DeploymentCount)

	// Recent failure increases risk
	if !history.LastFailure.IsZero() {
		timeSinceFailure := event.Timestamp.Sub(history.LastFailure)
		if timeSinceFailure < 24*time.Hour {
			failureRate = min(failureRate*1.5, 1.0)
		}
	}

	return failureRate
}

// calculateConfidence calculates confidence in the score
func (s *SafetyScorer) calculateConfidence(event *domain.DeploymentEvent) float64 {
	key := fmt.Sprintf("%s/%s", event.Namespace, event.Name)
	history, exists := s.deploymentHistory[key]

	confidence := 0.5 // Base confidence

	if exists && history.DeploymentCount > 0 {
		// More history = more confidence
		historyFactor := min(float64(history.DeploymentCount)/10.0, 1.0)
		confidence = 0.5 + (0.5 * historyFactor)
	}

	return confidence
}

// updateHistory updates deployment history
func (s *SafetyScorer) updateHistory(event *domain.DeploymentEvent) {
	key := fmt.Sprintf("%s/%s", event.Namespace, event.Name)

	history, exists := s.deploymentHistory[key]
	if !exists {
		history = &DeploymentHistory{}
		s.deploymentHistory[key] = history
	}

	// Update deployment count and timing
	if !history.LastDeployment.IsZero() {
		interval := event.Timestamp.Sub(history.LastDeployment)
		if history.AverageInterval == 0 {
			history.AverageInterval = interval
		} else {
			// Exponential moving average
			history.AverageInterval = time.Duration(float64(history.AverageInterval)*0.7 + float64(interval)*0.3)
		}
	}

	history.LastDeployment = event.Timestamp
	history.DeploymentCount++

	// Track image changes
	if event.Metadata.NewImage != "" {
		history.LastImages = append([]string{event.Metadata.NewImage}, history.LastImages...)
		if len(history.LastImages) > 5 {
			history.LastImages = history.LastImages[:5]
		}
	}

	// Track replica count
	history.LastReplicaCount = event.Metadata.NewReplicas

	// Clean up old history
	s.cleanupOldHistory()
}

// cleanupOldHistory removes old history entries
func (s *SafetyScorer) cleanupOldHistory() {
	now := time.Now()
	for key, history := range s.deploymentHistory {
		if now.Sub(history.LastDeployment) > s.config.HistoryRetentionPeriod {
			delete(s.deploymentHistory, key)
		}
	}

	// Limit total entries
	if len(s.deploymentHistory) > s.config.MaxHistoryEntries {
		// Remove oldest entries
		// This is a simple implementation; in production, use a proper LRU cache
		count := 0
		for key := range s.deploymentHistory {
			delete(s.deploymentHistory, key)
			count++
			if len(s.deploymentHistory) <= s.config.MaxHistoryEntries/2 {
				break
			}
		}
	}
}

// Scores returns the channel of calculated safety scores
func (s *SafetyScorer) Scores() <-chan *domain.SafetyScore {
	return s.safetyScores
}

// ProcessDeploymentEvent processes a deployment event and emits a safety score
func (s *SafetyScorer) ProcessDeploymentEvent(ctx context.Context, event *domain.DeploymentEvent) error {
	score, err := s.CalculateScore(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to calculate safety score: %w", err)
	}

	select {
	case s.safetyScores <- score:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		s.logger.Warn("Safety scores channel full, dropping score",
			zap.String("deployment", score.DeploymentID),
			zap.Float64("score", score.Value),
		)
		return nil
	}
}

// Helper functions

func containsLatestTag(image string) bool {
	return len(image) > 6 && image[len(image)-6:] == ":latest"
}

func isMajorVersionChange(oldImage, newImage string) bool {
	// Simple heuristic: check if first digit after : changes
	// This is a placeholder for more sophisticated version comparison
	oldTag := extractTag(oldImage)
	newTag := extractTag(newImage)

	if oldTag == "" || newTag == "" {
		return false
	}

	// Check if major version changed (first character of tag)
	if len(oldTag) > 0 && len(newTag) > 0 && oldTag[0] != newTag[0] {
		return true
	}

	return false
}

func extractTag(image string) string {
	lastColon := -1
	for i := len(image) - 1; i >= 0; i-- {
		if image[i] == ':' {
			lastColon = i
			break
		}
	}

	if lastColon == -1 || lastColon == len(image)-1 {
		return ""
	}

	return image[lastColon+1:]
}

func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
