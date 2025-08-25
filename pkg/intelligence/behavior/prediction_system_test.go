package behavior

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// TestSystemPatternMatching tests the complete pattern matching and prediction flow
func TestSystemPatternMatching(t *testing.T) {
	logger := zap.NewNop()

	// Create engine with all components
	engine, err := NewEngine(logger)
	require.NoError(t, err)
	defer engine.Stop()

	// Create and load test patterns
	patterns := createTestPatterns()
	engine.patternLoader = &PatternLoader{
		patterns: make(map[string]*domain.BehaviorPattern),
	}
	for _, p := range patterns {
		engine.patternLoader.patterns[p.ID] = p
	}

	// Initialize pattern matcher and predictor
	engine.patternMatcher = NewPatternMatcher(logger)
	engine.patternMatcher.UpdatePatterns(convertPatterns(patterns))
	engine.predictor = NewPredictor(logger)
	engine.predictor.patternLoader = engine.patternLoader

	// Initialize circuit breaker and backpressure
	engine.circuitBreaker = NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  3,
		ResetTimeout: 100 * time.Millisecond,
	})
	engine.backpressure = NewBackpressureManager(10)

	ctx := context.Background()

	tests := []struct {
		name          string
		event         *domain.ObservationEvent
		expectPattern string
		expectMinConf float64
		expectMaxConf float64
	}{
		{
			name: "high CPU pattern match",
			event: &domain.ObservationEvent{
				ID:        uuid.New().String(),
				Type:      "metrics.cpu",
				Source:    "prometheus",
				Timestamp: time.Now(),
				PodName:   stringPtr("api-server"),
				Data: map[string]string{
					"cpu_usage":    "85",
					"cpu_throttle": "true",
				},
			},
			expectPattern: "High CPU Usage",
			expectMinConf: 0.7,
			expectMaxConf: 0.95,
		},
		{
			name: "memory leak pattern match",
			event: &domain.ObservationEvent{
				ID:        uuid.New().String(),
				Type:      "metrics.memory",
				Source:    "prometheus",
				Timestamp: time.Now(),
				PodName:   stringPtr("database"),
				Data: map[string]string{
					"memory_usage": "92",
					"memory_trend": "increasing",
					"gc_frequency": "high",
				},
			},
			expectPattern: "Memory Leak",
			expectMinConf: 0.8,
			expectMaxConf: 1.0,
		},
		{
			name: "security incident pattern match",
			event: &domain.ObservationEvent{
				ID:        uuid.New().String(),
				Type:      "security.alert",
				Source:    "falco",
				Timestamp: time.Now(),
				PodName:   stringPtr("frontend"),
				Action:    stringPtr("file_write"),
				Target:    stringPtr("/etc/passwd"),
				Result:    stringPtr("denied"),
			},
			expectPattern: "Security Incident",
			expectMinConf: 0.85,
			expectMaxConf: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.Process(ctx, tt.event)
			require.NoError(t, err)

			if tt.expectPattern != "" {
				require.NotNil(t, result)
				require.NotNil(t, result.Prediction)

				assert.Equal(t, tt.expectPattern, result.Prediction.PatternName)
				assert.GreaterOrEqual(t, result.Prediction.Confidence, tt.expectMinConf)
				assert.LessOrEqual(t, result.Prediction.Confidence, tt.expectMaxConf)
				assert.NotEmpty(t, result.Prediction.ID)
				assert.NotEmpty(t, result.Prediction.Evidence)
				assert.Equal(t, domain.PredictionStatusActive, result.Prediction.Status)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

// TestSystemConfidenceScoring tests confidence calculation across different scenarios
func TestSystemConfidenceScoring(t *testing.T) {
	logger := zap.NewNop()

	// Create predictor with pattern loader
	predictor := NewPredictor(logger)
	patternLoader := &PatternLoader{
		patterns: make(map[string]*domain.BehaviorPattern),
	}

	// Create patterns with different confidence levels
	patterns := []*domain.BehaviorPattern{
		{
			ID:                 "high-conf",
			Name:               "High Confidence Pattern",
			BaseConfidence:     0.95,
			AdjustedConfidence: 0.98, // Positive feedback applied
			Enabled:            true,
			PredictionTemplate: createPredictionTemplate(domain.PredictionTypeAnomaly),
		},
		{
			ID:                 "medium-conf",
			Name:               "Medium Confidence Pattern",
			BaseConfidence:     0.7,
			AdjustedConfidence: 0.65, // Negative feedback applied
			Enabled:            true,
			PredictionTemplate: createPredictionTemplate(domain.PredictionTypeDegradation),
		},
		{
			ID:                 "low-conf",
			Name:               "Low Confidence Pattern",
			BaseConfidence:     0.4,
			AdjustedConfidence: 0.0, // No feedback yet
			Enabled:            true,
			PredictionTemplate: createPredictionTemplate(domain.PredictionTypeThresholdBreach),
		},
	}

	for _, p := range patterns {
		patternLoader.patterns[p.ID] = p
	}
	predictor.patternLoader = patternLoader

	ctx := context.Background()
	event := createTestObservationEvent()

	tests := []struct {
		name          string
		match         domain.PatternMatch
		expectedRange [2]float64 // [min, max]
	}{
		{
			name: "high confidence with complete evidence",
			match: domain.PatternMatch{
				PatternID:   "high-conf",
				PatternName: "High Confidence Pattern",
				Confidence:  0.9,
				Conditions: []domain.ConditionMatch{
					{Matched: true}, {Matched: true}, {Matched: true},
				},
				Evidence: []string{"e1", "e2", "e3"},
			},
			expectedRange: [2]float64{0.95, 1.0},
		},
		{
			name: "medium confidence with partial evidence",
			match: domain.PatternMatch{
				PatternID:   "medium-conf",
				PatternName: "Medium Confidence Pattern",
				Confidence:  0.6,
				Conditions: []domain.ConditionMatch{
					{Matched: true}, {Matched: true}, {Matched: false},
				},
				Evidence: []string{"e1"},
			},
			expectedRange: [2]float64{0.5, 0.75},
		},
		{
			name: "low confidence with minimal evidence",
			match: domain.PatternMatch{
				PatternID:   "low-conf",
				PatternName: "Low Confidence Pattern",
				Confidence:  0.3,
				Conditions: []domain.ConditionMatch{
					{Matched: true}, {Matched: false}, {Matched: false}, {Matched: false},
				},
				Evidence: []string{},
			},
			expectedRange: [2]float64{0.2, 0.4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prediction, err := predictor.GeneratePrediction(ctx, tt.match, event)
			require.NoError(t, err)
			require.NotNil(t, prediction)

			assert.GreaterOrEqual(t, prediction.Confidence, tt.expectedRange[0],
				"Confidence %f should be >= %f", prediction.Confidence, tt.expectedRange[0])
			assert.LessOrEqual(t, prediction.Confidence, tt.expectedRange[1],
				"Confidence %f should be <= %f", prediction.Confidence, tt.expectedRange[1])
		})
	}
}

// TestSystemMultiPredictorConsensus tests consensus between multiple predictors
func TestSystemMultiPredictorConsensus(t *testing.T) {
	logger := zap.NewNop()
	engine, err := NewEngine(logger)
	require.NoError(t, err)

	// Test consensus calculation with multiple matches
	tests := []struct {
		name              string
		matches           []BehaviorPatternMatch
		expectedConsensus float64
	}{
		{
			name: "all patterns agree",
			matches: []BehaviorPatternMatch{
				{PatternName: "CPU Spike", Confidence: 0.8},
				{PatternName: "CPU Spike", Confidence: 0.75},
				{PatternName: "CPU Spike", Confidence: 0.85},
			},
			expectedConsensus: 1.0, // 3/3 agree
		},
		{
			name: "majority agreement",
			matches: []BehaviorPatternMatch{
				{PatternName: "Memory Leak", Confidence: 0.8},
				{PatternName: "Memory Leak", Confidence: 0.75},
				{PatternName: "OOM Risk", Confidence: 0.6},
			},
			expectedConsensus: 0.67, // 2/3 agree
		},
		{
			name: "no consensus",
			matches: []BehaviorPatternMatch{
				{PatternName: "Pattern A", Confidence: 0.8},
				{PatternName: "Pattern B", Confidence: 0.75},
				{PatternName: "Pattern C", Confidence: 0.7},
			},
			expectedConsensus: 0.33, // 1/3 each
		},
		{
			name: "single match",
			matches: []BehaviorPatternMatch{
				{PatternName: "Single Pattern", Confidence: 0.9},
			},
			expectedConsensus: 0.0, // Need at least 2 for consensus
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consensus := engine.calculateConsensus(tt.matches)
			assert.InDelta(t, tt.expectedConsensus, consensus, 0.01)
		})
	}
}

// TestSystemBestMatchSelection tests the selection of best pattern match
func TestSystemBestMatchSelection(t *testing.T) {
	logger := zap.NewNop()
	engine, err := NewEngine(logger)
	require.NoError(t, err)

	now := time.Now()

	tests := []struct {
		name         string
		matches      []BehaviorPatternMatch
		expectedBest string
	}{
		{
			name: "highest confidence wins",
			matches: []BehaviorPatternMatch{
				{PatternName: "Pattern A", Confidence: 0.7, MatchedAt: now},
				{PatternName: "Pattern B", Confidence: 0.9, MatchedAt: now},
				{PatternName: "Pattern C", Confidence: 0.8, MatchedAt: now},
			},
			expectedBest: "Pattern B",
		},
		{
			name: "equal confidence - most recent wins",
			matches: []BehaviorPatternMatch{
				{PatternName: "Old Pattern", Confidence: 0.85, MatchedAt: now.Add(-1 * time.Minute)},
				{PatternName: "New Pattern", Confidence: 0.85, MatchedAt: now},
			},
			expectedBest: "New Pattern",
		},
		{
			name: "consensus boost applied",
			matches: []BehaviorPatternMatch{
				{PatternName: "Common Pattern", Confidence: 0.75, MatchedAt: now},
				{PatternName: "Common Pattern", Confidence: 0.7, MatchedAt: now},
				{PatternName: "Rare Pattern", Confidence: 0.78, MatchedAt: now},
			},
			expectedBest: "Common Pattern", // Gets consensus boost
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			best := engine.selectBestMatch(tt.matches)
			assert.Equal(t, tt.expectedBest, best.PatternName)
		})
	}
}

// TestSystemConcurrentPredictions tests concurrent prediction generation
func TestSystemConcurrentPredictions(t *testing.T) {
	logger := zap.NewNop()

	engine, err := NewEngine(logger)
	require.NoError(t, err)
	defer engine.Stop()

	// Setup engine with patterns
	patterns := createTestPatterns()
	engine.patternLoader = &PatternLoader{
		patterns: make(map[string]*domain.BehaviorPattern),
	}
	for _, p := range patterns {
		engine.patternLoader.patterns[p.ID] = p
	}

	engine.patternMatcher = NewPatternMatcher(logger)
	engine.patternMatcher.UpdatePatterns(convertPatterns(patterns))
	engine.predictor = NewPredictor(logger)
	engine.predictor.patternLoader = engine.patternLoader
	engine.circuitBreaker = NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  10,
		ResetTimeout: 100 * time.Millisecond,
	})
	engine.backpressure = NewBackpressureManager(100)

	ctx := context.Background()

	// Generate multiple events
	numEvents := 50
	events := make([]*domain.ObservationEvent, numEvents)
	for i := 0; i < numEvents; i++ {
		events[i] = &domain.ObservationEvent{
			ID:        uuid.New().String(),
			Type:      "metrics.cpu",
			Source:    "prometheus",
			Timestamp: time.Now(),
			PodName:   stringPtr("pod-" + string(rune(i))),
			Data: map[string]string{
				"cpu_usage": "85",
			},
		}
	}

	// Process events concurrently
	var wg sync.WaitGroup
	results := make([]*domain.PredictionResult, numEvents)
	errors := make([]error, numEvents)

	for i := 0; i < numEvents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errors[idx] = engine.Process(ctx, events[idx])
		}(i)
	}

	wg.Wait()

	// Verify results
	successCount := 0
	for i := 0; i < numEvents; i++ {
		if errors[i] == nil && results[i] != nil {
			successCount++
		}
	}

	// Most should succeed (allowing for some backpressure drops)
	assert.Greater(t, successCount, numEvents/2, "At least half should succeed")
}

// TestSystemPerformanceMetrics tests performance characteristics
func TestSystemPerformanceMetrics(t *testing.T) {
	logger := zap.NewNop()

	engine, err := NewEngine(logger)
	require.NoError(t, err)
	defer engine.Stop()

	// Setup
	setupTestEngine(engine, logger)

	ctx := context.Background()
	event := &domain.ObservationEvent{
		ID:        uuid.New().String(),
		Type:      "metrics.cpu",
		Source:    "prometheus",
		Timestamp: time.Now(),
		PodName:   stringPtr("test-pod"),
		Data: map[string]string{
			"cpu_usage": "75",
		},
	}

	// Measure processing time
	iterations := 100
	start := time.Now()

	for i := 0; i < iterations; i++ {
		_, _ = engine.Process(ctx, event)
	}

	elapsed := time.Since(start)
	avgTime := elapsed / time.Duration(iterations)

	// Performance assertions
	assert.Less(t, avgTime, 10*time.Millisecond, "Average processing time should be < 10ms")
	t.Logf("Average processing time: %v", avgTime)
}

// TestSystemCircuitBreakerIntegration tests circuit breaker behavior in the system
func TestSystemCircuitBreakerIntegration(t *testing.T) {
	logger := zap.NewNop()

	engine, err := NewEngine(logger)
	require.NoError(t, err)
	defer engine.Stop()

	// Configure circuit breaker with low threshold for testing
	engine.circuitBreaker = NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  2,
		ResetTimeout: 50 * time.Millisecond,
	})
	engine.backpressure = NewBackpressureManager(10)

	// Force failures by not setting up pattern loader properly
	engine.patternLoader = nil
	engine.patternMatcher = nil
	engine.predictor = nil

	ctx := context.Background()
	event := createTestObservationEvent()

	// First two calls should fail and open the circuit
	for i := 0; i < 2; i++ {
		result, err := engine.Process(ctx, event)
		assert.Error(t, err)
		assert.Nil(t, result)
	}

	// Circuit should be open now
	assert.Equal(t, "open", engine.circuitBreaker.State())

	// Next call should fail immediately
	result, err := engine.Process(ctx, event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
	assert.Nil(t, result)

	// Wait for reset timeout
	time.Sleep(100 * time.Millisecond)

	// Fix the engine
	setupTestEngine(engine, logger)

	// Should work now (circuit half-open then closed)
	result, err = engine.Process(ctx, event)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	assert.Equal(t, "closed", engine.circuitBreaker.State())
}

// TestSystemBackpressureHandling tests backpressure management
func TestSystemBackpressureHandling(t *testing.T) {
	logger := zap.NewNop()

	engine, err := NewEngine(logger)
	require.NoError(t, err)
	defer engine.Stop()

	// Setup with very limited capacity
	engine.backpressure = NewBackpressureManager(3)
	setupTestEngine(engine, logger)

	ctx := context.Background()
	event := createTestObservationEvent()

	// Fill up the backpressure capacity
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond) // Simulate slow processing
			_, _ = engine.Process(ctx, event)
		}()
	}

	// Give goroutines time to acquire slots
	time.Sleep(10 * time.Millisecond)

	// This should be rejected due to backpressure
	result, err := engine.Process(ctx, event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "system overloaded")
	assert.Nil(t, result)

	// Wait for goroutines to finish
	wg.Wait()

	// Should work again now
	result, err = engine.Process(ctx, event)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// Helper functions

func createTestPatterns() []*domain.BehaviorPattern {
	return []*domain.BehaviorPattern{
		{
			ID:             "cpu-high",
			Name:           "High CPU Usage",
			Description:    "Detects high CPU usage",
			BaseConfidence: 0.8,
			Enabled:        true,
			Conditions: []domain.Condition{
				{
					EventType: "metrics.cpu",
					Match: domain.MatchCriteria{
						Field:     "cpu_usage",
						Type:      "threshold",
						Threshold: 80,
						Operator:  ">",
					},
					Required: true,
				},
			},
			PredictionTemplate: createPredictionTemplate(domain.PredictionTypeAnomaly),
		},
		{
			ID:             "memory-leak",
			Name:           "Memory Leak",
			Description:    "Detects potential memory leaks",
			BaseConfidence: 0.85,
			Enabled:        true,
			Conditions: []domain.Condition{
				{
					EventType: "metrics.memory",
					Match: domain.MatchCriteria{
						Field:     "memory_usage",
						Type:      "threshold",
						Threshold: 90,
						Operator:  ">",
					},
					Required: true,
				},
			},
			PredictionTemplate: createPredictionTemplate(domain.PredictionTypeDegradation),
		},
		{
			ID:             "security-incident",
			Name:           "Security Incident",
			Description:    "Detects security incidents",
			BaseConfidence: 0.9,
			Enabled:        true,
			Conditions: []domain.Condition{
				{
					EventType: "security.alert",
					Match: domain.MatchCriteria{
						Field: "action",
						Type:  "exact",
						Value: "file_write",
					},
					Required: true,
				},
			},
			PredictionTemplate: createPredictionTemplate(domain.PredictionTypeDegradation),
		},
	}
}

func createPredictionTemplate(predType domain.PredictionType) domain.PredictionTemplate {
	return domain.PredictionTemplate{
		Type:        predType,
		TimeHorizon: "10m",
		Message:     "Pattern detected",
		Impact:      "Service may be affected",
		Severity:    "medium",
		PotentialImpacts: []string{
			"Performance degradation",
			"Service disruption",
		},
	}
}

func convertPatterns(patterns []*domain.BehaviorPattern) []domain.BehaviorPattern {
	result := make([]domain.BehaviorPattern, len(patterns))
	for i, p := range patterns {
		result[i] = *p
	}
	return result
}

func setupTestEngine(engine *Engine, logger *zap.Logger) {
	patterns := createTestPatterns()
	engine.patternLoader = &PatternLoader{
		patterns: make(map[string]*domain.BehaviorPattern),
	}
	for _, p := range patterns {
		engine.patternLoader.patterns[p.ID] = p
	}

	engine.patternMatcher = NewPatternMatcher(logger)
	engine.patternMatcher.UpdatePatterns(convertPatterns(patterns))
	engine.predictor = NewPredictor(logger)
	engine.predictor.patternLoader = engine.patternLoader

	if engine.circuitBreaker == nil {
		engine.circuitBreaker = NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:  3,
			ResetTimeout: 100 * time.Millisecond,
		})
	}

	if engine.backpressure == nil {
		engine.backpressure = NewBackpressureManager(10)
	}
}
