package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRiskLevel_Constants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		level    RiskLevel
		expected string
	}{
		{"Safe level", RiskLevelSafe, "safe"},
		{"Low level", RiskLevelLow, "low"},
		{"Medium level", RiskLevelMedium, "medium"},
		{"High level", RiskLevelHigh, "high"},
		{"Critical level", RiskLevelCritical, "critical"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, string(tt.level))
		})
	}
}

func TestScoreFactor_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		factor  ScoreFactor
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid score factor",
			factor: ScoreFactor{
				Name:        "image_change",
				Impact:      0.3,
				Description: "Deployment includes image change",
				Weight:      1.0,
			},
			wantErr: false,
		},
		{
			name: "Valid zero impact",
			factor: ScoreFactor{
				Name:        "no_change",
				Impact:      0.0,
				Description: "No significant changes",
				Weight:      0.5,
			},
			wantErr: false,
		},
		{
			name: "Invalid empty name",
			factor: ScoreFactor{
				Name:        "",
				Impact:      0.3,
				Description: "Valid description",
				Weight:      1.0,
			},
			wantErr: true,
			errMsg:  "name cannot be empty",
		},
		{
			name: "Invalid negative impact",
			factor: ScoreFactor{
				Name:        "test_factor",
				Impact:      -0.1,
				Description: "Valid description",
				Weight:      1.0,
			},
			wantErr: true,
			errMsg:  "impact must be between 0.0 and 1.0",
		},
		{
			name: "Invalid impact greater than 1",
			factor: ScoreFactor{
				Name:        "test_factor",
				Impact:      1.1,
				Description: "Valid description",
				Weight:      1.0,
			},
			wantErr: true,
			errMsg:  "impact must be between 0.0 and 1.0",
		},
		{
			name: "Invalid negative weight",
			factor: ScoreFactor{
				Name:        "test_factor",
				Impact:      0.5,
				Description: "Valid description",
				Weight:      -0.1,
			},
			wantErr: true,
			errMsg:  "weight must be between 0.0 and 1.0",
		},
		{
			name: "Invalid weight greater than 1",
			factor: ScoreFactor{
				Name:        "test_factor",
				Impact:      0.5,
				Description: "Valid description",
				Weight:      1.1,
			},
			wantErr: true,
			errMsg:  "weight must be between 0.0 and 1.0",
		},
		{
			name: "Valid zero weight",
			factor: ScoreFactor{
				Name:        "disabled_factor",
				Impact:      0.5,
				Description: "Disabled factor",
				Weight:      0.0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.factor.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSafetyScore_Validate(t *testing.T) {
	t.Parallel()

	validTime := time.Now()
	validFactors := []ScoreFactor{
		{
			Name:        "image_change",
			Impact:      0.3,
			Description: "Image change detected",
			Weight:      1.0,
		},
		{
			Name:        "scale_change",
			Impact:      0.2,
			Description: "Replica count changed",
			Weight:      0.8,
		},
	}

	tests := []struct {
		name    string
		score   SafetyScore
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid safety score",
			score: SafetyScore{
				Value:        0.5,
				Factors:      validFactors,
				Confidence:   0.85,
				Timestamp:    validTime,
				DeploymentID: "deployment-123",
			},
			wantErr: false,
		},
		{
			name: "Valid zero score",
			score: SafetyScore{
				Value:        0.0,
				Factors:      []ScoreFactor{},
				Confidence:   1.0,
				Timestamp:    validTime,
				DeploymentID: "deployment-safe",
			},
			wantErr: false,
		},
		{
			name: "Valid max score",
			score: SafetyScore{
				Value:        1.0,
				Factors:      validFactors,
				Confidence:   0.9,
				Timestamp:    validTime,
				DeploymentID: "deployment-risky",
			},
			wantErr: false,
		},
		{
			name: "Invalid negative score",
			score: SafetyScore{
				Value:        -0.1,
				Factors:      validFactors,
				Confidence:   0.85,
				Timestamp:    validTime,
				DeploymentID: "deployment-123",
			},
			wantErr: true,
			errMsg:  "value must be between 0.0 and 1.0",
		},
		{
			name: "Invalid score greater than 1",
			score: SafetyScore{
				Value:        1.1,
				Factors:      validFactors,
				Confidence:   0.85,
				Timestamp:    validTime,
				DeploymentID: "deployment-123",
			},
			wantErr: true,
			errMsg:  "value must be between 0.0 and 1.0",
		},
		{
			name: "Invalid negative confidence",
			score: SafetyScore{
				Value:        0.5,
				Factors:      validFactors,
				Confidence:   -0.1,
				Timestamp:    validTime,
				DeploymentID: "deployment-123",
			},
			wantErr: true,
			errMsg:  "confidence must be between 0.0 and 1.0",
		},
		{
			name: "Invalid confidence greater than 1",
			score: SafetyScore{
				Value:        0.5,
				Factors:      validFactors,
				Confidence:   1.1,
				Timestamp:    validTime,
				DeploymentID: "deployment-123",
			},
			wantErr: true,
			errMsg:  "confidence must be between 0.0 and 1.0",
		},
		{
			name: "Invalid zero timestamp",
			score: SafetyScore{
				Value:        0.5,
				Factors:      validFactors,
				Confidence:   0.85,
				Timestamp:    time.Time{},
				DeploymentID: "deployment-123",
			},
			wantErr: true,
			errMsg:  "timestamp cannot be zero",
		},
		{
			name: "Invalid empty deployment ID",
			score: SafetyScore{
				Value:        0.5,
				Factors:      validFactors,
				Confidence:   0.85,
				Timestamp:    validTime,
				DeploymentID: "",
			},
			wantErr: true,
			errMsg:  "deployment ID cannot be empty",
		},
		{
			name: "Invalid factor in list",
			score: SafetyScore{
				Value: 0.5,
				Factors: []ScoreFactor{
					{
						Name:        "valid_factor",
						Impact:      0.3,
						Description: "Valid factor",
						Weight:      1.0,
					},
					{
						Name:        "", // Invalid empty name
						Impact:      0.2,
						Description: "Invalid factor",
						Weight:      0.8,
					},
				},
				Confidence:   0.85,
				Timestamp:    validTime,
				DeploymentID: "deployment-123",
			},
			wantErr: true,
			errMsg:  "invalid factor at index 1",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.score.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSafetyScore_GetRiskLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    float64
		expected RiskLevel
	}{
		{"Safe score 0.0", 0.0, RiskLevelSafe},
		{"Safe score 0.1", 0.1, RiskLevelSafe},
		{"Safe score 0.19", 0.19, RiskLevelSafe},
		{"Low risk score 0.2", 0.2, RiskLevelLow},
		{"Low risk score 0.3", 0.3, RiskLevelLow},
		{"Low risk score 0.39", 0.39, RiskLevelLow},
		{"Medium risk score 0.4", 0.4, RiskLevelMedium},
		{"Medium risk score 0.5", 0.5, RiskLevelMedium},
		{"Medium risk score 0.59", 0.59, RiskLevelMedium},
		{"High risk score 0.6", 0.6, RiskLevelHigh},
		{"High risk score 0.7", 0.7, RiskLevelHigh},
		{"High risk score 0.79", 0.79, RiskLevelHigh},
		{"Critical risk score 0.8", 0.8, RiskLevelCritical},
		{"Critical risk score 0.9", 0.9, RiskLevelCritical},
		{"Critical risk score 1.0", 1.0, RiskLevelCritical},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			score := SafetyScore{Value: tt.value}
			assert.Equal(t, tt.expected, score.GetRiskLevel())
		})
	}
}

func TestSafetyScore_GetWeightedImpact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		factors  []ScoreFactor
		expected float64
	}{
		{
			name:     "Empty factors",
			factors:  []ScoreFactor{},
			expected: 0.0,
		},
		{
			name: "Single factor",
			factors: []ScoreFactor{
				{Impact: 0.5, Weight: 1.0},
			},
			expected: 0.5,
		},
		{
			name: "Multiple factors equal weight",
			factors: []ScoreFactor{
				{Impact: 0.4, Weight: 1.0},
				{Impact: 0.6, Weight: 1.0},
			},
			expected: 0.5, // (0.4*1.0 + 0.6*1.0) / (1.0 + 1.0)
		},
		{
			name: "Multiple factors different weights",
			factors: []ScoreFactor{
				{Impact: 0.8, Weight: 0.3}, // 0.24
				{Impact: 0.4, Weight: 0.7}, // 0.28
			},
			expected: 0.52, // (0.24 + 0.28) / (0.3 + 0.7)
		},
		{
			name: "Factor with zero weight ignored",
			factors: []ScoreFactor{
				{Impact: 0.5, Weight: 1.0}, // 0.5
				{Impact: 0.9, Weight: 0.0}, // ignored
			},
			expected: 0.5, // 0.5 / 1.0
		},
		{
			name: "All zero weights",
			factors: []ScoreFactor{
				{Impact: 0.5, Weight: 0.0},
				{Impact: 0.7, Weight: 0.0},
			},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			score := SafetyScore{Factors: tt.factors}
			result := score.GetWeightedImpact()
			assert.InDelta(t, tt.expected, result, 0.001, "Expected %.3f, got %.3f", tt.expected, result)
		})
	}
}

func TestSafetyScore_GetFactorByName(t *testing.T) {
	t.Parallel()

	factors := []ScoreFactor{
		{
			Name:        "image_change",
			Impact:      0.3,
			Description: "Image change detected",
			Weight:      1.0,
		},
		{
			Name:        "scale_change",
			Impact:      0.2,
			Description: "Replica count changed",
			Weight:      0.8,
		},
	}

	score := SafetyScore{Factors: factors}

	// Test finding existing factor
	factor, found := score.GetFactorByName("image_change")
	assert.True(t, found)
	assert.Equal(t, "image_change", factor.Name)
	assert.Equal(t, 0.3, factor.Impact)

	// Test finding another existing factor
	factor, found = score.GetFactorByName("scale_change")
	assert.True(t, found)
	assert.Equal(t, "scale_change", factor.Name)
	assert.Equal(t, 0.2, factor.Impact)

	// Test not finding non-existent factor
	factor, found = score.GetFactorByName("non_existent")
	assert.False(t, found)
	assert.Equal(t, ScoreFactor{}, factor)

	// Test empty name
	factor, found = score.GetFactorByName("")
	assert.False(t, found)
	assert.Equal(t, ScoreFactor{}, factor)
}

func TestSafetyScore_IsHighRisk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    float64
		expected bool
	}{
		{"Safe score", 0.0, false},
		{"Low risk score", 0.3, false},
		{"Medium risk score", 0.5, false},
		{"Boundary below high", 0.59, false},
		{"High risk boundary", 0.6, true},
		{"High risk score", 0.7, true},
		{"Critical risk score", 0.9, true},
		{"Maximum risk score", 1.0, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			score := SafetyScore{Value: tt.value}
			assert.Equal(t, tt.expected, score.IsHighRisk())
		})
	}
}

func TestSafetyScore_IsSafe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    float64
		expected bool
	}{
		{"Safe score 0.0", 0.0, true},
		{"Safe score 0.1", 0.1, true},
		{"Safe boundary", 0.19, true},
		{"Low risk boundary", 0.2, false},
		{"Medium risk score", 0.5, false},
		{"High risk score", 0.7, false},
		{"Critical risk score", 1.0, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			score := SafetyScore{Value: tt.value}
			assert.Equal(t, tt.expected, score.IsSafe())
		})
	}
}

func TestSafetyScore_GetFactorNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		factors  []ScoreFactor
		expected []string
	}{
		{
			name:     "Empty factors",
			factors:  []ScoreFactor{},
			expected: []string{},
		},
		{
			name: "Single factor",
			factors: []ScoreFactor{
				{Name: "image_change"},
			},
			expected: []string{"image_change"},
		},
		{
			name: "Multiple factors",
			factors: []ScoreFactor{
				{Name: "image_change"},
				{Name: "scale_change"},
				{Name: "strategy_change"},
			},
			expected: []string{"image_change", "scale_change", "strategy_change"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			score := SafetyScore{Factors: tt.factors}
			result := score.GetFactorNames()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Benchmarks for performance validation
func BenchmarkSafetyScore_Validate(b *testing.B) {
	score := SafetyScore{
		Value: 0.5,
		Factors: []ScoreFactor{
			{Name: "factor1", Impact: 0.3, Weight: 1.0, Description: "Test factor 1"},
			{Name: "factor2", Impact: 0.4, Weight: 0.8, Description: "Test factor 2"},
			{Name: "factor3", Impact: 0.6, Weight: 1.2, Description: "Test factor 3"},
		},
		Confidence:   0.85,
		Timestamp:    time.Now(),
		DeploymentID: "deployment-123",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = score.Validate()
	}
}

func BenchmarkSafetyScore_GetRiskLevel(b *testing.B) {
	score := SafetyScore{Value: 0.5}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = score.GetRiskLevel()
	}
}

func BenchmarkSafetyScore_GetWeightedImpact(b *testing.B) {
	score := SafetyScore{
		Factors: []ScoreFactor{
			{Impact: 0.3, Weight: 1.0},
			{Impact: 0.4, Weight: 0.8},
			{Impact: 0.6, Weight: 1.2},
			{Impact: 0.2, Weight: 0.5},
			{Impact: 0.7, Weight: 0.9},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = score.GetWeightedImpact()
	}
}

func BenchmarkSafetyScore_GetFactorByName(b *testing.B) {
	factors := make([]ScoreFactor, 10)
	for i := 0; i < 10; i++ {
		factors[i] = ScoreFactor{
			Name:   "factor_" + string(rune('0'+i)),
			Impact: float64(i) / 10.0,
			Weight: 1.0,
		}
	}

	score := SafetyScore{Factors: factors}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = score.GetFactorByName("factor_5")
	}
}
