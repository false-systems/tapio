package intelligence

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewSafetyScorer(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultScoringConfig()

	scorer, err := NewSafetyScorer(logger, config)
	require.NoError(t, err)
	require.NotNil(t, scorer)
	assert.NotNil(t, scorer.safetyScores)
	assert.NotNil(t, scorer.deploymentHistory)
}

func TestCalculateScore_FirstDeployment(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultScoringConfig()
	scorer, err := NewSafetyScorer(logger, config)
	require.NoError(t, err)

	event := &domain.DeploymentEvent{
		Timestamp: time.Now(),
		Namespace: "default",
		Name:      "test-app",
		Action:    domain.DeploymentCreated,
		Metadata: domain.DeploymentMetadata{
			NewImage:    "nginx:1.19",
			NewReplicas: 3,
		},
	}

	score, err := scorer.CalculateScore(context.Background(), event)
	require.NoError(t, err)
	require.NotNil(t, score)

	// First deployment should have relatively low risk
	assert.True(t, score.Value < 0.5)
	assert.True(t, score.Confidence >= 0.5)
	assert.Equal(t, domain.RiskLevelLow, score.GetRiskLevel())
}

func TestCalculateScore_ImageChangeRisk(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultScoringConfig()
	scorer, err := NewSafetyScorer(logger, config)
	require.NoError(t, err)

	tests := []struct {
		name        string
		oldImage    string
		newImage    string
		expectedMin float64
		expectedMax float64
	}{
		{
			name:        "latest tag",
			oldImage:    "nginx:1.19",
			newImage:    "nginx:latest",
			expectedMin: 0.5,
			expectedMax: 1.0,
		},
		{
			name:        "major version change",
			oldImage:    "nginx:1.19",
			newImage:    "nginx:2.0",
			expectedMin: 0.4,
			expectedMax: 0.8,
		},
		{
			name:        "minor version change",
			oldImage:    "nginx:1.19",
			newImage:    "nginx:1.20",
			expectedMin: 0.1,
			expectedMax: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &domain.DeploymentEvent{
				Timestamp: time.Now(),
				Namespace: "default",
				Name:      "test-app",
				Action:    domain.DeploymentUpdated,
				Metadata: domain.DeploymentMetadata{
					OldImage:    tt.oldImage,
					NewImage:    tt.newImage,
					OldReplicas: 3,
					NewReplicas: 3,
				},
			}

			score, err := scorer.CalculateScore(context.Background(), event)
			require.NoError(t, err)
			assert.True(t, score.Value >= tt.expectedMin && score.Value <= tt.expectedMax,
				"Score %f not in range [%f, %f]", score.Value, tt.expectedMin, tt.expectedMax)

			// Check that image change factor is present
			hasImageFactor := false
			for _, factor := range score.Factors {
				if factor.Name == "image_change" {
					hasImageFactor = true
					break
				}
			}
			assert.True(t, hasImageFactor)
		})
	}
}

func TestCalculateScore_ScaleChangeRisk(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultScoringConfig()
	scorer, err := NewSafetyScorer(logger, config)
	require.NoError(t, err)

	tests := []struct {
		name        string
		oldReplicas int32
		newReplicas int32
		expectedMin float64
		expectedMax float64
		riskLevel   domain.RiskLevel
	}{
		{
			name:        "scale to zero",
			oldReplicas: 3,
			newReplicas: 0,
			expectedMin: 0.5,
			expectedMax: 1.0,
			riskLevel:   domain.RiskLevelHigh,
		},
		{
			name:        "large scale up",
			oldReplicas: 2,
			newReplicas: 10,
			expectedMin: 0.3,
			expectedMax: 0.8,
			riskLevel:   domain.RiskLevelMedium,
		},
		{
			name:        "small scale change",
			oldReplicas: 3,
			newReplicas: 4,
			expectedMin: 0.0,
			expectedMax: 0.4,
			riskLevel:   domain.RiskLevelLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &domain.DeploymentEvent{
				Timestamp: time.Now(),
				Namespace: "default",
				Name:      "test-app",
				Action:    domain.DeploymentScaled,
				Metadata: domain.DeploymentMetadata{
					OldReplicas: tt.oldReplicas,
					NewReplicas: tt.newReplicas,
				},
			}

			score, err := scorer.CalculateScore(context.Background(), event)
			require.NoError(t, err)
			assert.True(t, score.Value >= tt.expectedMin && score.Value <= tt.expectedMax,
				"Score %f not in range [%f, %f]", score.Value, tt.expectedMin, tt.expectedMax)

			// Check risk level
			if tt.newReplicas == 0 {
				// Scaling to zero should always be high risk
				assert.True(t, score.GetRiskLevel() >= domain.RiskLevelMedium)
			}
		})
	}
}

func TestCalculateScore_FrequencyRisk(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultScoringConfig()
	config.HighFrequencyThreshold = 5 * time.Minute
	scorer, err := NewSafetyScorer(logger, config)
	require.NoError(t, err)

	// First deployment
	event1 := &domain.DeploymentEvent{
		Timestamp: time.Now(),
		Namespace: "default",
		Name:      "test-app",
		Action:    domain.DeploymentCreated,
		Metadata: domain.DeploymentMetadata{
			NewImage:    "nginx:1.19",
			NewReplicas: 3,
		},
	}

	score1, err := scorer.CalculateScore(context.Background(), event1)
	require.NoError(t, err)
	assert.True(t, score1.Value < 0.5) // First deployment, low risk

	// Second deployment very quickly (high frequency)
	event2 := &domain.DeploymentEvent{
		Timestamp: time.Now().Add(2 * time.Minute),
		Namespace: "default",
		Name:      "test-app",
		Action:    domain.DeploymentUpdated,
		Metadata: domain.DeploymentMetadata{
			OldImage:    "nginx:1.19",
			NewImage:    "nginx:1.20",
			OldReplicas: 3,
			NewReplicas: 3,
		},
	}

	score2, err := scorer.CalculateScore(context.Background(), event2)
	require.NoError(t, err)
	assert.True(t, score2.Value > score1.Value) // Higher risk due to frequency

	// Check that frequency factor is present
	hasFrequencyFactor := false
	for _, factor := range score2.Factors {
		if factor.Name == "deployment_frequency" {
			hasFrequencyFactor = true
			assert.True(t, factor.Impact > 0.5) // High frequency risk
			break
		}
	}
	assert.True(t, hasFrequencyFactor)
}

func TestCalculateScore_TimeOfDayRisk(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultScoringConfig()
	config.RiskyHoursStart = 17
	config.RiskyHoursEnd = 20
	scorer, err := NewSafetyScorer(logger, config)
	require.NoError(t, err)

	// Test risky hour deployment
	riskyTime := time.Date(2024, 1, 15, 18, 30, 0, 0, time.UTC) // 6:30 PM
	riskyEvent := &domain.DeploymentEvent{
		Timestamp: riskyTime,
		Namespace: "default",
		Name:      "test-app",
		Action:    domain.DeploymentUpdated,
		Metadata: domain.DeploymentMetadata{
			OldImage:    "nginx:1.19",
			NewImage:    "nginx:1.20",
			OldReplicas: 3,
			NewReplicas: 3,
		},
	}

	riskyScore, err := scorer.CalculateScore(context.Background(), riskyEvent)
	require.NoError(t, err)

	// Test safe hour deployment
	safeTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC) // 10:30 AM on weekday
	safeEvent := &domain.DeploymentEvent{
		Timestamp: safeTime,
		Namespace: "default",
		Name:      "test-app-2",
		Action:    domain.DeploymentUpdated,
		Metadata: domain.DeploymentMetadata{
			OldImage:    "nginx:1.19",
			NewImage:    "nginx:1.20",
			OldReplicas: 3,
			NewReplicas: 3,
		},
	}

	safeScore, err := scorer.CalculateScore(context.Background(), safeEvent)
	require.NoError(t, err)

	// Risky time should have higher score
	assert.True(t, riskyScore.Value > safeScore.Value)
}

func TestCalculateScore_HistoricalRisk(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultScoringConfig()
	scorer, err := NewSafetyScorer(logger, config)
	require.NoError(t, err)

	// Simulate deployment history
	for i := 0; i < 5; i++ {
		event := &domain.DeploymentEvent{
			Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
			Namespace: "default",
			Name:      "test-app",
			Action:    domain.DeploymentUpdated,
			Metadata: domain.DeploymentMetadata{
				NewImage:    fmt.Sprintf("nginx:1.%d", 19+i),
				NewReplicas: 3,
			},
		}
		scorer.updateHistory(event)
	}

	// Simulate some failures
	scorer.deploymentHistory["default/test-app"].FailureCount = 2

	// New deployment should consider history
	event := &domain.DeploymentEvent{
		Timestamp: time.Now().Add(6 * time.Hour),
		Namespace: "default",
		Name:      "test-app",
		Action:    domain.DeploymentUpdated,
		Metadata: domain.DeploymentMetadata{
			OldImage:    "nginx:1.23",
			NewImage:    "nginx:1.24",
			OldReplicas: 3,
			NewReplicas: 3,
		},
	}

	score, err := scorer.CalculateScore(context.Background(), event)
	require.NoError(t, err)

	// Should have historical failure factor
	hasHistoryFactor := false
	for _, factor := range score.Factors {
		if factor.Name == "historical_failures" {
			hasHistoryFactor = true
			assert.True(t, factor.Impact > 0) // Some risk from failures
			break
		}
	}
	assert.True(t, hasHistoryFactor)

	// Confidence should be higher with more history
	assert.True(t, score.Confidence > 0.7)
}

func TestProcessDeploymentEvent(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultScoringConfig()
	scorer, err := NewSafetyScorer(logger, config)
	require.NoError(t, err)

	event := &domain.DeploymentEvent{
		Timestamp: time.Now(),
		Namespace: "default",
		Name:      "test-app",
		Action:    domain.DeploymentCreated,
		Metadata: domain.DeploymentMetadata{
			NewImage:    "nginx:1.19",
			NewReplicas: 3,
		},
	}

	ctx := context.Background()
	err = scorer.ProcessDeploymentEvent(ctx, event)
	require.NoError(t, err)

	// Check that score was emitted
	select {
	case score := <-scorer.Scores():
		require.NotNil(t, score)
		assert.Equal(t, "default/test-app", score.DeploymentID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for safety score")
	}
}

func TestHistoryCleanup(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultScoringConfig()
	config.MaxHistoryEntries = 3
	config.HistoryRetentionPeriod = 1 * time.Hour
	scorer, err := NewSafetyScorer(logger, config)
	require.NoError(t, err)

	// Add multiple deployments
	for i := 0; i < 5; i++ {
		event := &domain.DeploymentEvent{
			Timestamp: time.Now().Add(time.Duration(i) * 10 * time.Minute),
			Namespace: "default",
			Name:      fmt.Sprintf("app-%d", i),
			Action:    domain.DeploymentCreated,
			Metadata: domain.DeploymentMetadata{
				NewReplicas: 1,
			},
		}
		scorer.updateHistory(event)
	}

	// History should be limited
	assert.LessOrEqual(t, len(scorer.deploymentHistory), config.MaxHistoryEntries)

	// Add old deployment that should be cleaned up
	oldEvent := &domain.DeploymentEvent{
		Timestamp: time.Now().Add(-2 * time.Hour),
		Namespace: "default",
		Name:      "old-app",
		Action:    domain.DeploymentCreated,
		Metadata: domain.DeploymentMetadata{
			NewReplicas: 1,
		},
	}
	scorer.updateHistory(oldEvent)
	scorer.cleanupOldHistory()

	// Old deployment should not be in history
	_, exists := scorer.deploymentHistory["default/old-app"]
	assert.False(t, exists)
}
