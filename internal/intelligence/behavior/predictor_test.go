package behavior

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// TestNewPredictor tests predictor creation
func TestNewPredictor(t *testing.T) {
	logger := zap.NewNop()
	predictor := NewPredictor(logger)

	assert.NotNil(t, predictor)
	assert.NotNil(t, predictor.logger)
	assert.Equal(t, 0.7, predictor.baseConfidenceWeight)
	assert.Equal(t, 0.3, predictor.feedbackWeight)
	assert.Equal(t, 0.2, predictor.agreementBoost)
	assert.Equal(t, 0.1, predictor.patternMatchBoost)
	assert.Equal(t, 0.2, predictor.missingDataPenalty)
	assert.Equal(t, 0.15, predictor.conflictPenalty)
	assert.NotNil(t, predictor.evidenceWeights)
	assert.NotNil(t, predictor.tracer)
}

// TestGeneratePrediction tests the main prediction generation flow
func TestGeneratePrediction(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func(*Predictor)
		match         domain.PatternMatch
		event         *domain.ObservationEvent
		expectError   bool
		errorContains string
		validate      func(*testing.T, *domain.Prediction)
	}{
		{
			name: "successful prediction generation",
			setupMock: func(p *Predictor) {
				p.patternLoader = createMockPatternLoader()
			},
			match: domain.PatternMatch{
				PatternID:   "test-pattern-1",
				PatternName: "Test Pattern",
				Confidence:  0.85,
				Conditions: []domain.ConditionMatch{
					{
						Matched:       true,
						ActualValue:   "high",
						ExpectedValue: "high",
						Message:       "CPU usage is high",
					},
				},
				Evidence: []string{"event1", "event2"},
			},
			event: createTestObservationEvent(),
			validate: func(t *testing.T, p *domain.Prediction) {
				assert.NotEmpty(t, p.ID)
				assert.Equal(t, "test-pattern-1", p.PatternID)
				assert.Equal(t, "Test Pattern", p.PatternName)
				assert.Equal(t, domain.PredictionTypeAnomaly, p.Type)
				assert.InDelta(t, 0.935, p.Confidence, 0.01) // 0.85 * 1.1 (pattern match boost)
				assert.Equal(t, 10*time.Minute, p.TimeHorizon)
				assert.Equal(t, "High CPU usage detected", p.Message)
				assert.Equal(t, "medium", p.Severity)
				assert.Equal(t, domain.PredictionStatusActive, p.Status)
				assert.NotEmpty(t, p.Evidence)
				assert.NotEmpty(t, p.Resources)
			},
		},
		{
			name: "pattern not found",
			setupMock: func(p *Predictor) {
				p.patternLoader = &PatternLoader{
					patterns: make(map[string]*domain.BehaviorPattern),
				}
			},
			match: domain.PatternMatch{
				PatternID:   "non-existent",
				PatternName: "Missing Pattern",
				Confidence:  0.5,
			},
			event:         createTestObservationEvent(),
			expectError:   true,
			errorContains: "pattern non-existent not found",
		},
		{
			name: "incomplete evidence penalty applied",
			setupMock: func(p *Predictor) {
				p.patternLoader = createMockPatternLoader()
			},
			match: domain.PatternMatch{
				PatternID:   "test-pattern-1",
				PatternName: "Test Pattern",
				Confidence:  0.8,
				Conditions: []domain.ConditionMatch{
					{Matched: true, Message: "Condition 1"},
					{Matched: false, Message: "Condition 2"},
					{Matched: false, Message: "Condition 3"},
					{Matched: false, Message: "Condition 4"},
					{Matched: false, Message: "Condition 5"},
				},
			},
			event: createTestObservationEvent(),
			validate: func(t *testing.T, p *domain.Prediction) {
				// Confidence should be reduced due to incomplete evidence (< 60% matched)
				// 0.8 * 1.1 (pattern boost) * 0.8 (missing data penalty) * quality
				assert.Less(t, p.Confidence, 0.8)
			},
		},
		{
			name: "with adjusted confidence from feedback",
			setupMock: func(p *Predictor) {
				loader := createMockPatternLoader()
				pattern, _ := loader.GetPattern("test-pattern-1")
				pattern.AdjustedConfidence = 0.95
				p.patternLoader = loader
			},
			match: domain.PatternMatch{
				PatternID:   "test-pattern-1",
				PatternName: "Test Pattern",
				Confidence:  0.7,
				Conditions: []domain.ConditionMatch{
					{Matched: true, Message: "All conditions met"},
				},
			},
			event: createTestObservationEvent(),
			validate: func(t *testing.T, p *domain.Prediction) {
				// Should use weighted combination of base and adjusted confidence
				// Base: 0.7 * 0.7 = 0.49
				// Adjusted: 0.95 * 0.3 = 0.285
				// Combined: 0.49 + 0.285 = 0.775
				// With pattern boost: 0.775 * 1.1 = 0.8525
				// With evidence quality (no evidence, default 0.8): 0.8525 * 0.8 = 0.682
				assert.InDelta(t, 0.682, p.Confidence, 0.01)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			predictor := NewPredictor(logger)

			if tt.setupMock != nil {
				tt.setupMock(predictor)
			}

			ctx := context.Background()
			prediction, err := predictor.GeneratePrediction(ctx, tt.match, tt.event)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, prediction)
			} else {
				require.NoError(t, err)
				require.NotNil(t, prediction)
				if tt.validate != nil {
					tt.validate(t, prediction)
				}
			}
		})
	}
}

// TestCalculateConfidence tests confidence calculation with various scenarios
func TestCalculateConfidence(t *testing.T) {
	tests := []struct {
		name        string
		match       domain.PatternMatch
		pattern     *domain.BehaviorPattern
		expectedMin float64
		expectedMax float64
	}{
		{
			name: "base confidence without adjustments",
			match: domain.PatternMatch{
				Confidence: 0.75,
				Conditions: []domain.ConditionMatch{
					{Matched: true},
					{Matched: true},
				},
			},
			pattern: &domain.BehaviorPattern{
				BaseConfidence: 0.8,
			},
			expectedMin: 0.65,
			expectedMax: 0.67,
		},
		{
			name: "with adjusted confidence from feedback",
			match: domain.PatternMatch{
				Confidence: 0.6,
				Conditions: []domain.ConditionMatch{
					{Matched: true},
					{Matched: true},
				},
			},
			pattern: &domain.BehaviorPattern{
				BaseConfidence:     0.7,
				AdjustedConfidence: 0.9,
			},
			expectedMin: 0.6,
			expectedMax: 0.8,
		},
		{
			name: "incomplete evidence penalty",
			match: domain.PatternMatch{
				Confidence: 0.9,
				Conditions: []domain.ConditionMatch{
					{Matched: true},
					{Matched: false},
					{Matched: false},
					{Matched: false},
					{Matched: false},
				},
			},
			pattern: &domain.BehaviorPattern{
				BaseConfidence: 0.85,
			},
			expectedMin: 0.6,
			expectedMax: 0.8,
		},
		{
			name: "confidence capped at 1.0",
			match: domain.PatternMatch{
				Confidence: 0.95,
				Conditions: []domain.ConditionMatch{
					{Matched: true},
					{Matched: true},
				},
				Evidence: []string{"e1", "e2", "e3", "e4", "e5"},
			},
			pattern: &domain.BehaviorPattern{
				BaseConfidence:     0.9,
				AdjustedConfidence: 0.95,
			},
			expectedMin: 0.95,
			expectedMax: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			predictor := NewPredictor(logger)

			confidence := predictor.calculateConfidence(tt.match, tt.pattern)

			assert.GreaterOrEqual(t, confidence, tt.expectedMin,
				"Confidence %f should be >= %f", confidence, tt.expectedMin)
			assert.LessOrEqual(t, confidence, tt.expectedMax,
				"Confidence %f should be <= %f", confidence, tt.expectedMax)
			assert.GreaterOrEqual(t, confidence, 0.0)
			assert.LessOrEqual(t, confidence, 1.0)
		})
	}
}

// TestBuildEvidence tests evidence building from matches and events
func TestBuildEvidence(t *testing.T) {
	logger := zap.NewNop()
	predictor := NewPredictor(logger)

	match := domain.PatternMatch{
		PatternName: "Test Pattern",
		Conditions: []domain.ConditionMatch{
			{
				Matched:       true,
				ActualValue:   "80%",
				ExpectedValue: ">70%",
				Message:       "CPU usage above threshold",
			},
			{
				Matched:       false,
				ActualValue:   "500MB",
				ExpectedValue: ">1GB",
				Message:       "Memory usage below threshold",
			},
			{
				Matched:       true,
				ActualValue:   "10ms",
				ExpectedValue: "<20ms",
				Message:       "Latency within limits",
			},
		},
	}

	event := createTestObservationEvent()
	evidence := predictor.buildEvidence(match, event)

	// Should have 3 pieces of evidence: 2 matched conditions + 1 observation
	assert.Len(t, evidence, 3)

	// Check matched conditions are included
	conditionEvidence := 0
	observationEvidence := 0

	for _, e := range evidence {
		if e.Type == "condition" {
			conditionEvidence++
			assert.Equal(t, "Test Pattern", e.Source)
			assert.NotEmpty(t, e.Description)
			assert.NotNil(t, e.Data)
			assert.Equal(t, 1.0, e.Data.Metrics["confidence"])
		} else if e.Type == "observation" {
			observationEvidence++
			assert.Equal(t, "test-collector", e.Source)
			assert.Contains(t, e.Description, "Observation:")
			assert.NotNil(t, e.Data)
			assert.Equal(t, 0.9, e.Data.Metrics["confidence"])
		}
	}

	assert.Equal(t, 2, conditionEvidence) // Only matched conditions
	assert.Equal(t, 1, observationEvidence)
}

// TestExtractResources tests resource extraction from observation events
func TestExtractResources(t *testing.T) {
	tests := []struct {
		name     string
		event    *domain.ObservationEvent
		expected []domain.ResourceRef
	}{
		{
			name: "extract all resource types",
			event: &domain.ObservationEvent{
				PodName:     stringPtr("test-pod"),
				ServiceName: stringPtr("test-service"),
				NodeName:    stringPtr("test-node"),
				Namespace:   stringPtr("test-namespace"),
			},
			expected: []domain.ResourceRef{
				{
					Kind:      "Pod",
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				{
					Kind:      "Service",
					Name:      "test-service",
					Namespace: "test-namespace",
				},
				{
					Kind: "Node",
					Name: "test-node",
				},
			},
		},
		{
			name: "partial resources",
			event: &domain.ObservationEvent{
				PodName:   stringPtr("pod-only"),
				Namespace: stringPtr("default"),
			},
			expected: []domain.ResourceRef{
				{
					Kind:      "Pod",
					Name:      "pod-only",
					Namespace: "default",
				},
			},
		},
		{
			name:     "no resources",
			event:    &domain.ObservationEvent{},
			expected: []domain.ResourceRef{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			predictor := NewPredictor(logger)

			resources := predictor.extractResources(tt.event)

			assert.Equal(t, len(tt.expected), len(resources))
			for i, expectedRes := range tt.expected {
				assert.Equal(t, expectedRes.Kind, resources[i].Kind)
				assert.Equal(t, expectedRes.Name, resources[i].Name)
				assert.Equal(t, expectedRes.Namespace, resources[i].Namespace)
			}
		})
	}
}

// TestHasIncompleteEvidence tests incomplete evidence detection
func TestHasIncompleteEvidence(t *testing.T) {
	tests := []struct {
		name       string
		match      domain.PatternMatch
		incomplete bool
	}{
		{
			name: "all conditions matched - complete",
			match: domain.PatternMatch{
				Conditions: []domain.ConditionMatch{
					{Matched: true},
					{Matched: true},
					{Matched: true},
				},
			},
			incomplete: false,
		},
		{
			name: "60% matched - complete",
			match: domain.PatternMatch{
				Conditions: []domain.ConditionMatch{
					{Matched: true},
					{Matched: true},
					{Matched: true},
					{Matched: false},
					{Matched: false},
				},
			},
			incomplete: false,
		},
		{
			name: "less than 60% matched - incomplete",
			match: domain.PatternMatch{
				Conditions: []domain.ConditionMatch{
					{Matched: true},
					{Matched: false},
					{Matched: false},
					{Matched: false},
				},
			},
			incomplete: true,
		},
		{
			name: "no conditions matched - incomplete",
			match: domain.PatternMatch{
				Conditions: []domain.ConditionMatch{
					{Matched: false},
					{Matched: false},
				},
			},
			incomplete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			predictor := NewPredictor(logger)

			result := predictor.hasIncompleteEvidence(tt.match)
			assert.Equal(t, tt.incomplete, result)
		})
	}
}

// TestCalculateEvidenceQuality tests evidence quality scoring
func TestCalculateEvidenceQuality(t *testing.T) {
	tests := []struct {
		name        string
		match       domain.PatternMatch
		expectedMin float64
		expectedMax float64
	}{
		{
			name: "no evidence - default quality",
			match: domain.PatternMatch{
				Evidence: []string{},
			},
			expectedMin: 0.8,
			expectedMax: 0.8,
		},
		{
			name: "single evidence",
			match: domain.PatternMatch{
				Evidence: []string{"event1"},
			},
			expectedMin: 0.9,
			expectedMax: 1.1,
		},
		{
			name: "multiple evidence",
			match: domain.PatternMatch{
				Evidence: []string{"event1", "event2", "event3"},
			},
			expectedMin: 0.9,
			expectedMax: 1.2,
		},
		{
			name: "many evidence - capped at 1.2",
			match: domain.PatternMatch{
				Evidence: []string{"e1", "e2", "e3", "e4", "e5", "e6", "e7", "e8"},
			},
			expectedMin: 1.0,
			expectedMax: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			predictor := NewPredictor(logger)

			quality := predictor.calculateEvidenceQuality(tt.match)

			assert.GreaterOrEqual(t, quality, tt.expectedMin)
			assert.LessOrEqual(t, quality, tt.expectedMax)
		})
	}
}

// TestAdjustConfidenceFromFeedback tests confidence adjustment based on user feedback
func TestAdjustConfidenceFromFeedback(t *testing.T) {
	tests := []struct {
		name     string
		feedback *domain.UserFeedback
	}{
		{
			name: "positive feedback with thumbs up",
			feedback: &domain.UserFeedback{
				PatternID: "pattern-1",
				Accurate:  true,
				Rating:    domain.FeedbackRating(1),
			},
		},
		{
			name: "negative feedback with thumbs down",
			feedback: &domain.UserFeedback{
				PatternID: "pattern-2",
				Accurate:  false,
				Rating:    domain.FeedbackRating(-1),
			},
		},
		{
			name: "neutral feedback",
			feedback: &domain.UserFeedback{
				PatternID: "pattern-3",
				Accurate:  true,
				Rating:    domain.FeedbackRating(0),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			predictor := NewPredictor(logger)

			ctx := context.Background()
			err := predictor.AdjustConfidenceFromFeedback(ctx, tt.feedback)

			assert.NoError(t, err)
		})
	}
}

// Helper functions

func createMockPatternLoader() *PatternLoader {
	pattern := &domain.BehaviorPattern{
		ID:             "test-pattern-1",
		Name:           "Test Pattern",
		Description:    "Test pattern for unit tests",
		BaseConfidence: 0.8,
		Enabled:        true,
		PredictionTemplate: domain.PredictionTemplate{
			Type:        domain.PredictionTypeAnomaly,
			TimeHorizon: "10m",
			Message:     "High CPU usage detected",
			Impact:      "Service may become unresponsive",
			Severity:    "medium",
			PotentialImpacts: []string{
				"Service degradation",
				"Increased latency",
			},
		},
		Remediation: &domain.RemediationActions{
			AutoRemediation: true,
			ManualSteps: []string{
				"Scale up replicas to 3",
				"Monitor resource usage",
			},
			PreventativeSteps: []string{
				"Set up autoscaling",
			},
		},
	}

	return &PatternLoader{
		patterns: map[string]*domain.BehaviorPattern{
			"test-pattern-1": pattern,
		},
	}
}

func createTestObservationEvent() *domain.ObservationEvent {
	return &domain.ObservationEvent{
		ID:          uuid.New().String(),
		Type:        "cpu.high",
		Source:      "test-collector",
		Timestamp:   time.Now(),
		PodName:     stringPtr("test-pod"),
		ServiceName: stringPtr("test-service"),
		NodeName:    stringPtr("test-node"),
		Namespace:   stringPtr("default"),
		Action:      stringPtr("alert"),
		Target:      stringPtr("/metrics"),
		Result:      stringPtr("triggered"),
		Data: map[string]string{
			"cpu_usage": "85%",
			"duration":  "5m",
		},
	}
}

// stringPtr is defined in integration_test.go - removing duplicate
