package domain

import (
	"fmt"
	"time"
)

// RiskLevel represents the risk level of a deployment
type RiskLevel string

const (
	RiskLevelSafe     RiskLevel = "safe"     // 0.0 - 0.19
	RiskLevelLow      RiskLevel = "low"      // 0.2 - 0.39
	RiskLevelMedium   RiskLevel = "medium"   // 0.4 - 0.59
	RiskLevelHigh     RiskLevel = "high"     // 0.6 - 0.79
	RiskLevelCritical RiskLevel = "critical" // 0.8 - 1.0
)

// Risk level thresholds
const (
	riskLevelLowThreshold      = 0.2
	riskLevelMediumThreshold   = 0.4
	riskLevelHighThreshold     = 0.6
	riskLevelCriticalThreshold = 0.8
)

// ScoreFactor represents an individual risk factor contributing to the safety score
type ScoreFactor struct {
	Name        string  `json:"name"`
	Impact      float64 `json:"impact"` // 0.0 to 1.0
	Description string  `json:"description"`
	Weight      float64 `json:"weight"` // 0.0 to 1.0
}

// Validate validates the score factor
func (sf *ScoreFactor) Validate() error {
	if sf.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}

	if sf.Impact < 0.0 || sf.Impact > 1.0 {
		return fmt.Errorf("impact must be between 0.0 and 1.0, got: %f", sf.Impact)
	}

	if sf.Weight < 0.0 || sf.Weight > 1.0 {
		return fmt.Errorf("weight must be between 0.0 and 1.0, got: %f", sf.Weight)
	}

	return nil
}

// SafetyScore represents a deployment risk assessment
type SafetyScore struct {
	Value        float64       `json:"value"`         // 0.0 (safe) to 1.0 (risky)
	Factors      []ScoreFactor `json:"factors"`       // Contributing risk factors
	Confidence   float64       `json:"confidence"`    // 0.0 to 1.0
	Timestamp    time.Time     `json:"timestamp"`     // When the score was calculated
	DeploymentID string        `json:"deployment_id"` // Deployment identifier
}

// Validate validates the safety score
func (ss *SafetyScore) Validate() error {
	if ss.Value < 0.0 || ss.Value > 1.0 {
		return fmt.Errorf("value must be between 0.0 and 1.0, got: %f", ss.Value)
	}

	if ss.Confidence < 0.0 || ss.Confidence > 1.0 {
		return fmt.Errorf("confidence must be between 0.0 and 1.0, got: %f", ss.Confidence)
	}

	if ss.Timestamp.IsZero() {
		return fmt.Errorf("timestamp cannot be zero")
	}

	if ss.DeploymentID == "" {
		return fmt.Errorf("deployment ID cannot be empty")
	}

	// Validate all factors
	for i, factor := range ss.Factors {
		if err := factor.Validate(); err != nil {
			return fmt.Errorf("invalid factor at index %d: %w", i, err)
		}
	}

	return nil
}

// GetRiskLevel returns the risk level based on the safety score value
func (ss *SafetyScore) GetRiskLevel() RiskLevel {
	switch {
	case ss.Value < riskLevelLowThreshold:
		return RiskLevelSafe
	case ss.Value < riskLevelMediumThreshold:
		return RiskLevelLow
	case ss.Value < riskLevelHighThreshold:
		return RiskLevelMedium
	case ss.Value < riskLevelCriticalThreshold:
		return RiskLevelHigh
	default:
		return RiskLevelCritical
	}
}

// GetWeightedImpact calculates the weighted average impact of all factors
func (ss *SafetyScore) GetWeightedImpact() float64 {
	if len(ss.Factors) == 0 {
		return 0.0
	}

	var totalWeightedImpact float64
	var totalWeight float64

	for _, factor := range ss.Factors {
		if factor.Weight > 0 {
			totalWeightedImpact += factor.Impact * factor.Weight
			totalWeight += factor.Weight
		}
	}

	if totalWeight == 0 {
		return 0.0
	}

	return totalWeightedImpact / totalWeight
}

// GetFactorByName returns the factor with the given name, or false if not found
func (ss *SafetyScore) GetFactorByName(name string) (ScoreFactor, bool) {
	if name == "" {
		return ScoreFactor{}, false
	}

	for _, factor := range ss.Factors {
		if factor.Name == name {
			return factor, true
		}
	}

	return ScoreFactor{}, false
}

// IsHighRisk returns true if the deployment is considered high risk (score >= 0.6)
func (ss *SafetyScore) IsHighRisk() bool {
	return ss.Value >= riskLevelHighThreshold
}

// IsSafe returns true if the deployment is considered safe (score < 0.2)
func (ss *SafetyScore) IsSafe() bool {
	return ss.Value < riskLevelLowThreshold
}

// GetFactorNames returns the names of all factors contributing to the score
func (ss *SafetyScore) GetFactorNames() []string {
	names := make([]string, len(ss.Factors))
	for i, factor := range ss.Factors {
		names[i] = factor.Name
	}
	return names
}
