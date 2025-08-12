package aggregator

import (
	"math"
)

// ConfidenceCalculator calculates aggregated confidence scores
type ConfidenceCalculator struct {
	// Configuration for confidence calculation
	agreementBoost     float64
	patternMatchBoost  float64
	missingDataPenalty float64
	conflictPenalty    float64
}

// NewConfidenceCalculator creates a new confidence calculator
func NewConfidenceCalculator() *ConfidenceCalculator {
	return &ConfidenceCalculator{
		agreementBoost:     0.2,  // 20% boost for agreement
		patternMatchBoost:  0.1,  // 10% boost for pattern match
		missingDataPenalty: 0.2,  // 20% penalty for missing data
		conflictPenalty:    0.15, // 15% penalty for conflicts
	}
}

// Calculate computes the final confidence score
func (c *ConfidenceCalculator) Calculate(findings []Finding, patterns []Pattern, outputs []*CorrelatorOutput) float64 {
	if len(findings) == 0 {
		return 0.0
	}

	// Start with average confidence of findings
	baseConfidence := c.averageConfidence(findings)

	// Apply boosts and penalties
	if c.hasAgreement(outputs) {
		baseConfidence *= (1 + c.agreementBoost)
	}

	if len(patterns) > 0 {
		baseConfidence *= (1 + c.patternMatchBoost)
	}

	if c.hasMissingData(outputs) {
		baseConfidence *= (1 - c.missingDataPenalty)
	}

	if c.hasConflicts(outputs) {
		baseConfidence *= (1 - c.conflictPenalty)
	}

	// Weight by correlator accuracy
	weightedConfidence := c.applyCorrelatorWeights(baseConfidence, outputs)

	// Ensure confidence is between 0 and 1
	return math.Min(math.Max(weightedConfidence, 0.0), 1.0)
}

// averageConfidence calculates the average confidence of findings
func (c *ConfidenceCalculator) averageConfidence(findings []Finding) float64 {
	if len(findings) == 0 {
		return 0.0
	}

	sum := 0.0
	for _, finding := range findings {
		sum += finding.Confidence
	}
	return sum / float64(len(findings))
}

// hasAgreement checks if multiple correlators agree
func (c *ConfidenceCalculator) hasAgreement(outputs []*CorrelatorOutput) bool {
	if len(outputs) < 2 {
		return false
	}

	// Check if findings have similar types or messages
	findingTypes := make(map[string]int)
	for _, output := range outputs {
		for _, finding := range output.Findings {
			findingTypes[finding.Type]++
		}
	}

	// Agreement if any finding type appears in multiple correlators
	for _, count := range findingTypes {
		if count > 1 {
			return true
		}
	}

	return false
}

// hasMissingData checks if we're missing critical data
func (c *ConfidenceCalculator) hasMissingData(outputs []*CorrelatorOutput) bool {
	// Check if any correlator returned no findings
	emptyCount := 0
	for _, output := range outputs {
		if len(output.Findings) == 0 {
			emptyCount++
		}
	}

	// Consider data missing if more than half of correlators have no findings
	return emptyCount > len(outputs)/2
}

// hasConflicts checks if correlators have conflicting findings
func (c *ConfidenceCalculator) hasConflicts(outputs []*CorrelatorOutput) bool {
	// Simple conflict detection: different root causes for same resource
	resources := make(map[string][]string)

	for _, output := range outputs {
		for _, finding := range output.Findings {
			for _, resource := range finding.Impact.Resources {
				resources[resource] = append(resources[resource], finding.Type)
			}
		}
	}

	// Check if same resource has different finding types
	for _, types := range resources {
		if len(types) > 1 {
			// Check if types are actually different
			firstType := types[0]
			for _, t := range types[1:] {
				if t != firstType {
					return true
				}
			}
		}
	}

	return false
}

// applyCorrelatorWeights adjusts confidence based on correlator accuracy
func (c *ConfidenceCalculator) applyCorrelatorWeights(baseConfidence float64, outputs []*CorrelatorOutput) float64 {
	// For now, return base confidence
	// In real implementation, this would use correlator accuracy scores
	return baseConfidence
}

// Pattern represents a known correlation pattern
type Pattern struct {
	ID          string
	Name        string
	Type        string
	Confidence  float64
	Occurrences int
	LastSeen    string
}
