package aggregator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewConfidenceCalculator(t *testing.T) {
	calc := NewConfidenceCalculator()

	assert.NotNil(t, calc)
	assert.Equal(t, 0.2, calc.agreementBoost)
	assert.Equal(t, 0.1, calc.patternMatchBoost)
	assert.Equal(t, 0.2, calc.missingDataPenalty)
	assert.Equal(t, 0.15, calc.conflictPenalty)
}

func TestCalculate_NoFindings(t *testing.T) {
	calc := NewConfidenceCalculator()

	confidence := calc.Calculate([]Finding{}, []Pattern{}, []*CorrelatorOutput{})

	assert.Equal(t, 0.0, confidence)
}

func TestCalculate_BaseConfidence(t *testing.T) {
	calc := NewConfidenceCalculator()

	findings := []Finding{
		{Confidence: 0.8},
		{Confidence: 0.6},
		{Confidence: 0.7},
	}

	outputs := []*CorrelatorOutput{
		{Findings: findings},
	}

	confidence := calc.Calculate(findings, []Pattern{}, outputs)

	// Average should be 0.7
	assert.InDelta(t, 0.7, confidence, 0.01)
}

func TestCalculate_WithAgreementBoost(t *testing.T) {
	calc := NewConfidenceCalculator()

	findings := []Finding{
		{Type: "memory_issue", Confidence: 0.7},
		{Type: "memory_issue", Confidence: 0.7},
	}

	outputs := []*CorrelatorOutput{
		{CorrelatorName: "A", Findings: []Finding{{Type: "memory_issue", Confidence: 0.7}}},
		{CorrelatorName: "B", Findings: []Finding{{Type: "memory_issue", Confidence: 0.7}}},
	}

	confidence := calc.Calculate(findings, []Pattern{}, outputs)

	// Base 0.7 * 1.2 (agreement boost) = 0.84
	assert.InDelta(t, 0.84, confidence, 0.01)
}

func TestCalculate_WithPatternBoost(t *testing.T) {
	calc := NewConfidenceCalculator()

	findings := []Finding{
		{Confidence: 0.7},
	}

	patterns := []Pattern{
		{ID: "pattern-1"},
	}

	outputs := []*CorrelatorOutput{
		{Findings: findings},
	}

	confidence := calc.Calculate(findings, patterns, outputs)

	// Base 0.7 * 1.1 (pattern boost) = 0.77
	assert.InDelta(t, 0.77, confidence, 0.01)
}

func TestCalculate_WithMissingDataPenalty(t *testing.T) {
	calc := NewConfidenceCalculator()

	findings := []Finding{
		{Confidence: 0.8},
	}

	outputs := []*CorrelatorOutput{
		{CorrelatorName: "A", Findings: findings},
		{CorrelatorName: "B", Findings: []Finding{}}, // Empty
		{CorrelatorName: "C", Findings: []Finding{}}, // Empty
	}

	confidence := calc.Calculate(findings, []Pattern{}, outputs)

	// Base 0.8 * 0.8 (missing data penalty) = 0.64
	assert.InDelta(t, 0.64, confidence, 0.01)
}

func TestCalculate_WithConflictPenalty(t *testing.T) {
	calc := NewConfidenceCalculator()

	findings := []Finding{
		{Type: "memory_issue", Confidence: 0.8, Impact: Impact{Resources: []string{"pod-1"}}},
		{Type: "network_issue", Confidence: 0.8, Impact: Impact{Resources: []string{"pod-1"}}},
	}

	outputs := []*CorrelatorOutput{
		{Findings: []Finding{findings[0]}},
		{Findings: []Finding{findings[1]}},
	}

	confidence := calc.Calculate(findings, []Pattern{}, outputs)

	// Base 0.8 * 0.85 (conflict penalty) = 0.68
	assert.InDelta(t, 0.68, confidence, 0.01)
}

func TestCalculate_MaxConfidence(t *testing.T) {
	calc := NewConfidenceCalculator()

	findings := []Finding{
		{Type: "issue", Confidence: 0.9},
		{Type: "issue", Confidence: 0.9},
	}

	patterns := []Pattern{{ID: "p1"}}

	outputs := []*CorrelatorOutput{
		{Findings: []Finding{{Type: "issue", Confidence: 0.9}}},
		{Findings: []Finding{{Type: "issue", Confidence: 0.9}}},
	}

	confidence := calc.Calculate(findings, patterns, outputs)

	// Should be capped at 1.0
	assert.LessOrEqual(t, confidence, 1.0)
}

func TestAverageConfidence(t *testing.T) {
	calc := NewConfidenceCalculator()

	tests := []struct {
		name     string
		findings []Finding
		expected float64
	}{
		{
			name:     "empty findings",
			findings: []Finding{},
			expected: 0.0,
		},
		{
			name:     "single finding",
			findings: []Finding{{Confidence: 0.8}},
			expected: 0.8,
		},
		{
			name:     "multiple findings",
			findings: []Finding{{Confidence: 0.6}, {Confidence: 0.8}, {Confidence: 0.7}},
			expected: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.averageConfidence(tt.findings)
			assert.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestHasAgreement(t *testing.T) {
	calc := NewConfidenceCalculator()

	tests := []struct {
		name     string
		outputs  []*CorrelatorOutput
		expected bool
	}{
		{
			name:     "single correlator",
			outputs:  []*CorrelatorOutput{{Findings: []Finding{{Type: "issue"}}}},
			expected: false,
		},
		{
			name: "agreement on type",
			outputs: []*CorrelatorOutput{
				{Findings: []Finding{{Type: "memory_issue"}}},
				{Findings: []Finding{{Type: "memory_issue"}}},
			},
			expected: true,
		},
		{
			name: "no agreement",
			outputs: []*CorrelatorOutput{
				{Findings: []Finding{{Type: "memory_issue"}}},
				{Findings: []Finding{{Type: "network_issue"}}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.hasAgreement(tt.outputs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasMissingData(t *testing.T) {
	calc := NewConfidenceCalculator()

	tests := []struct {
		name     string
		outputs  []*CorrelatorOutput
		expected bool
	}{
		{
			name: "all have findings",
			outputs: []*CorrelatorOutput{
				{Findings: []Finding{{}}},
				{Findings: []Finding{{}}},
			},
			expected: false,
		},
		{
			name: "majority empty",
			outputs: []*CorrelatorOutput{
				{Findings: []Finding{{}}},
				{Findings: []Finding{}},
				{Findings: []Finding{}},
			},
			expected: true,
		},
		{
			name: "half empty",
			outputs: []*CorrelatorOutput{
				{Findings: []Finding{{}}},
				{Findings: []Finding{}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.hasMissingData(tt.outputs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasConflicts(t *testing.T) {
	calc := NewConfidenceCalculator()

	tests := []struct {
		name     string
		outputs  []*CorrelatorOutput
		expected bool
	}{
		{
			name: "no conflicts",
			outputs: []*CorrelatorOutput{
				{Findings: []Finding{{Type: "issue", Impact: Impact{Resources: []string{"res1"}}}}},
				{Findings: []Finding{{Type: "issue", Impact: Impact{Resources: []string{"res2"}}}}},
			},
			expected: false,
		},
		{
			name: "conflict on same resource",
			outputs: []*CorrelatorOutput{
				{Findings: []Finding{{Type: "memory_issue", Impact: Impact{Resources: []string{"pod-1"}}}}},
				{Findings: []Finding{{Type: "network_issue", Impact: Impact{Resources: []string{"pod-1"}}}}},
			},
			expected: true,
		},
		{
			name: "same type on same resource",
			outputs: []*CorrelatorOutput{
				{Findings: []Finding{{Type: "memory_issue", Impact: Impact{Resources: []string{"pod-1"}}}}},
				{Findings: []Finding{{Type: "memory_issue", Impact: Impact{Resources: []string{"pod-1"}}}}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.hasConflicts(tt.outputs)
			assert.Equal(t, tt.expected, result)
		})
	}
}
