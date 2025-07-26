package correlation

import (
	"math"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// ConfidenceScorer calculates and tracks correlation confidence
type ConfidenceScorer struct {
	logger *zap.Logger

	// Historical performance tracking
	history *PerformanceHistory

	// Feature weights for scoring
	weights FeatureWeights

	// Configuration
	config ScorerConfig

	mu sync.RWMutex
}

// ScorerConfig configures the confidence scorer
type ScorerConfig struct {
	MinSampleSize    int           // Minimum observations before scoring
	DecayHalfLife    time.Duration // How fast old observations decay
	FeedbackWeight   float64       // Weight of user feedback
	AdaptiveLearning bool          // Enable weight adaptation
}

// DefaultScorerConfig returns sensible defaults
func DefaultScorerConfig() ScorerConfig {
	return ScorerConfig{
		MinSampleSize:    5,
		DecayHalfLife:    7 * 24 * time.Hour, // 1 week
		FeedbackWeight:   0.3,
		AdaptiveLearning: true,
	}
}

// FeatureWeights defines importance of different features
type FeatureWeights struct {
	Structural   float64 // K8s relationships
	Temporal     float64 // Time-based patterns
	Statistical  float64 // Co-occurrence stats
	Semantic     float64 // Content similarity
	UserFeedback float64 // Explicit/implicit feedback
}

// DefaultFeatureWeights returns balanced weights
func DefaultFeatureWeights() FeatureWeights {
	return FeatureWeights{
		Structural:   0.35, // K8s structure is most reliable
		Temporal:     0.25,
		Statistical:  0.20,
		Semantic:     0.10,
		UserFeedback: 0.10,
	}
}

// PerformanceHistory tracks correlation accuracy over time
type PerformanceHistory struct {
	correlations map[string]*CorrelationPerformance
	mu           sync.RWMutex
}

// CorrelationPerformance tracks a specific correlation's performance
type CorrelationPerformance struct {
	ID             string
	TruePositives  int
	FalsePositives int
	Observations   []Observation
	LastUpdated    time.Time
}

// Observation represents a single correlation observation
type Observation struct {
	Timestamp  time.Time
	Correct    bool
	Confidence float64
	Features   CorrelationFeatures
}

// CorrelationFeatures are inputs to confidence calculation
type CorrelationFeatures struct {
	// Structural features
	HasOwnerReference bool
	HasSelector       bool
	HasLabelMatch     bool
	K8sDistance       int // Hops in K8s graph

	// Temporal features
	TimeDelta       time.Duration
	TimeConsistency float64 // Std dev of time deltas
	Occurrences     int

	// Statistical features
	CoOccurrenceRate  float64
	ConditionalProb   float64 // P(B|A)
	MutualInformation float64

	// Semantic features
	MessageSimilarity float64
	EntitySimilarity  float64

	// Context features
	EventSeverity string
	SystemLoad    float64
	TimeOfDay     int
}

// NewConfidenceScorer creates a new confidence scorer
func NewConfidenceScorer(logger *zap.Logger, config ScorerConfig) *ConfidenceScorer {
	return &ConfidenceScorer{
		logger: logger,
		history: &PerformanceHistory{
			correlations: make(map[string]*CorrelationPerformance),
		},
		weights: DefaultFeatureWeights(),
		config:  config,
	}
}

// ScoreCorrelation calculates confidence for a correlation
func (s *ConfidenceScorer) ScoreCorrelation(
	sourceEvent *domain.UnifiedEvent,
	targetEvent *domain.UnifiedEvent,
	correlationType string,
	features CorrelationFeatures,
) float64 {
	// Calculate base scores for each feature category
	structuralScore := s.calculateStructuralScore(features)
	temporalScore := s.calculateTemporalScore(features)
	statisticalScore := s.calculateStatisticalScore(features)
	semanticScore := s.calculateSemanticScore(features)

	// Get historical performance score
	correlationID := s.getCorrelationID(sourceEvent, targetEvent, correlationType)
	historicalScore := s.getHistoricalScore(correlationID)

	// Weighted combination
	baseScore := s.weights.Structural*structuralScore +
		s.weights.Temporal*temporalScore +
		s.weights.Statistical*statisticalScore +
		s.weights.Semantic*semanticScore +
		s.weights.UserFeedback*historicalScore

	// Apply modifiers
	score := s.applyModifiers(baseScore, features)

	// Record observation
	s.recordObservation(correlationID, score, features)

	return score
}

// calculateStructuralScore scores K8s structural relationships
func (s *ConfidenceScorer) calculateStructuralScore(features CorrelationFeatures) float64 {
	score := 0.0

	// Owner reference is strongest signal
	if features.HasOwnerReference {
		score = 1.0
	} else if features.HasSelector {
		score = 0.9
	} else if features.HasLabelMatch {
		score = 0.7
	}

	// Adjust for K8s distance
	if features.K8sDistance > 0 && features.K8sDistance <= 3 {
		distancePenalty := 1.0 / float64(features.K8sDistance)
		score *= (0.5 + 0.5*distancePenalty)
	}

	return score
}

// calculateTemporalScore scores time-based relationships
func (s *ConfidenceScorer) calculateTemporalScore(features CorrelationFeatures) float64 {
	// Time proximity score
	proximityScore := 1.0
	if features.TimeDelta > 0 {
		// Exponential decay based on time delta
		decayRate := 1.0 / (5 * time.Minute).Seconds()
		proximityScore = math.Exp(-decayRate * features.TimeDelta.Seconds())
	}

	// Consistency score
	consistencyScore := features.TimeConsistency

	// Occurrence score
	occurrenceScore := math.Min(float64(features.Occurrences)/10.0, 1.0)

	// Weighted combination
	return 0.4*proximityScore + 0.4*consistencyScore + 0.2*occurrenceScore
}

// calculateStatisticalScore scores statistical relationships
func (s *ConfidenceScorer) calculateStatisticalScore(features CorrelationFeatures) float64 {
	// Co-occurrence is primary signal
	coOccScore := features.CoOccurrenceRate

	// Conditional probability adds directionality
	condProbScore := features.ConditionalProb

	// Mutual information captures dependency
	miScore := math.Min(features.MutualInformation/2.0, 1.0) // Normalize MI

	// Weighted combination
	return 0.5*coOccScore + 0.3*condProbScore + 0.2*miScore
}

// calculateSemanticScore scores content similarity
func (s *ConfidenceScorer) calculateSemanticScore(features CorrelationFeatures) float64 {
	// Average of message and entity similarity
	return (features.MessageSimilarity + features.EntitySimilarity) / 2.0
}

// getHistoricalScore returns performance-based score
func (s *ConfidenceScorer) getHistoricalScore(correlationID string) float64 {
	s.history.mu.RLock()
	perf, exists := s.history.correlations[correlationID]
	s.history.mu.RUnlock()

	if !exists || perf.TruePositives+perf.FalsePositives < s.config.MinSampleSize {
		return 0.5 // Neutral score for new correlations
	}

	// Calculate precision with time decay
	precision := s.calculateDecayedPrecision(perf)

	// Boost for recent activity
	recencyBoost := s.calculateRecencyBoost(perf.LastUpdated)

	return precision * (0.8 + 0.2*recencyBoost)
}

// calculateDecayedPrecision calculates precision with time decay
func (s *ConfidenceScorer) calculateDecayedPrecision(perf *CorrelationPerformance) float64 {
	if len(perf.Observations) == 0 {
		return 0.5
	}

	now := time.Now()
	weightedTP := 0.0
	weightedTotal := 0.0

	for _, obs := range perf.Observations {
		// Calculate decay weight
		age := now.Sub(obs.Timestamp)
		decayFactor := math.Exp(-math.Ln2 * age.Seconds() / s.config.DecayHalfLife.Seconds())

		weightedTotal += decayFactor
		if obs.Correct {
			weightedTP += decayFactor
		}
	}

	if weightedTotal == 0 {
		return 0.5
	}

	return weightedTP / weightedTotal
}

// calculateRecencyBoost rewards recent observations
func (s *ConfidenceScorer) calculateRecencyBoost(lastUpdated time.Time) float64 {
	hoursSinceUpdate := time.Since(lastUpdated).Hours()
	return math.Exp(-hoursSinceUpdate / 168.0) // Decay over a week
}

// applyModifiers applies contextual adjustments
func (s *ConfidenceScorer) applyModifiers(baseScore float64, features CorrelationFeatures) float64 {
	score := baseScore

	// Severity modifier - critical events get boost
	if features.EventSeverity == string(domain.EventSeverityCritical) {
		score *= 1.1
	}

	// System load modifier - high load increases correlation likelihood
	if features.SystemLoad > 0.8 {
		score *= 1.05
	}

	// Time of day modifier - some patterns are time-specific
	if s.isBusinessHours(features.TimeOfDay) {
		score *= 1.02
	}

	// Ensure score stays in [0, 1]
	return math.Min(math.Max(score, 0.0), 1.0)
}

// UpdateFeedback updates scoring based on user feedback
func (s *ConfidenceScorer) UpdateFeedback(
	correlationID string,
	isCorrect bool,
	confidence float64,
) {
	s.history.mu.Lock()
	defer s.history.mu.Unlock()

	perf, exists := s.history.correlations[correlationID]
	if !exists {
		perf = &CorrelationPerformance{
			ID:           correlationID,
			Observations: make([]Observation, 0),
		}
		s.history.correlations[correlationID] = perf
	}

	// Update counters
	if isCorrect {
		perf.TruePositives++
	} else {
		perf.FalsePositives++
	}
	perf.LastUpdated = time.Now()

	// Adaptive learning - adjust weights if enabled
	if s.config.AdaptiveLearning {
		s.adaptWeights(correlationID, isCorrect, confidence)
	}
}

// adaptWeights adjusts feature weights based on performance
func (s *ConfidenceScorer) adaptWeights(correlationID string, isCorrect bool, confidence float64) {
	// Simple gradient update
	learningRate := 0.01

	if isCorrect && confidence < 0.7 {
		// Under-confident correct prediction - increase weights
		s.adjustWeights(learningRate)
	} else if !isCorrect && confidence > 0.7 {
		// Over-confident incorrect prediction - decrease weights
		s.adjustWeights(-learningRate)
	}
}

// GetCorrelationStats returns performance statistics
func (s *ConfidenceScorer) GetCorrelationStats(correlationID string) *CorrelationStats {
	s.history.mu.RLock()
	perf, exists := s.history.correlations[correlationID]
	s.history.mu.RUnlock()

	if !exists {
		return nil
	}

	precision := float64(perf.TruePositives) / float64(perf.TruePositives+perf.FalsePositives)

	return &CorrelationStats{
		CorrelationID: correlationID,
		Precision:     precision,
		Observations:  perf.TruePositives + perf.FalsePositives,
		LastSeen:      perf.LastUpdated,
		Confidence:    s.getHistoricalScore(correlationID),
	}
}

// CorrelationStats contains correlation performance metrics
type CorrelationStats struct {
	CorrelationID string
	Precision     float64
	Observations  int
	LastSeen      time.Time
	Confidence    float64
}

// Helper methods

func (s *ConfidenceScorer) getCorrelationID(source, target *domain.UnifiedEvent, corrType string) string {
	sourceType := "unknown"
	targetType := "unknown"

	if source.Entity != nil {
		sourceType = source.Entity.Type
	}
	if target.Entity != nil {
		targetType = target.Entity.Type
	}

	return sourceType + "_" + targetType + "_" + corrType
}

func (s *ConfidenceScorer) recordObservation(correlationID string, confidence float64, features CorrelationFeatures) {
	obs := Observation{
		Timestamp:  time.Now(),
		Confidence: confidence,
		Features:   features,
	}

	s.history.mu.Lock()
	defer s.history.mu.Unlock()

	perf, exists := s.history.correlations[correlationID]
	if !exists {
		perf = &CorrelationPerformance{
			ID:           correlationID,
			Observations: make([]Observation, 0),
		}
		s.history.correlations[correlationID] = perf
	}

	perf.Observations = append(perf.Observations, obs)

	// Keep only recent observations
	maxObservations := 1000
	if len(perf.Observations) > maxObservations {
		perf.Observations = perf.Observations[len(perf.Observations)-maxObservations:]
	}
}

func (s *ConfidenceScorer) isBusinessHours(hour int) bool {
	return hour >= 8 && hour <= 18
}

func (s *ConfidenceScorer) adjustWeights(delta float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Simple uniform adjustment for now
	s.weights.Structural += delta
	s.weights.Temporal += delta
	s.weights.Statistical += delta
	s.weights.Semantic += delta

	// Normalize weights to sum to 1
	total := s.weights.Structural + s.weights.Temporal +
		s.weights.Statistical + s.weights.Semantic + s.weights.UserFeedback

	s.weights.Structural /= total
	s.weights.Temporal /= total
	s.weights.Statistical /= total
	s.weights.Semantic /= total
	s.weights.UserFeedback /= total
}
