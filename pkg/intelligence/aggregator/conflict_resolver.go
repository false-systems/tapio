package aggregator

import (
	"sort"
	"strings"
)

// ConflictResolver resolves conflicts between correlator findings
type ConflictResolver struct {
	strategy ConflictResolution
}

// NewConflictResolver creates a new conflict resolver
func NewConflictResolver(strategy ConflictResolution) *ConflictResolver {
	return &ConflictResolver{
		strategy: strategy,
	}
}

// Resolve resolves conflicts between findings
func (r *ConflictResolver) Resolve(findings []Finding) []Finding {
	if len(findings) <= 1 {
		return findings
	}

	// Group findings by resource/target
	groups := r.groupByTarget(findings)

	// Resolve conflicts within each group
	resolved := []Finding{}
	for _, group := range groups {
		if len(group) == 1 {
			resolved = append(resolved, group[0])
			continue
		}

		// Apply resolution strategy
		winner := r.resolveGroup(group)
		if winner != nil {
			resolved = append(resolved, *winner)
		}
	}

	return resolved
}

// groupByTarget groups findings by what they're explaining
func (r *ConflictResolver) groupByTarget(findings []Finding) map[string][]Finding {
	groups := make(map[string][]Finding)

	for _, finding := range findings {
		// Group by primary resource
		if len(finding.Impact.Resources) > 0 {
			key := finding.Impact.Resources[0]
			groups[key] = append(groups[key], finding)
		} else {
			// Group by type if no resources
			groups[finding.Type] = append(groups[finding.Type], finding)
		}
	}

	return groups
}

// resolveGroup resolves conflicts within a group
func (r *ConflictResolver) resolveGroup(group []Finding) *Finding {
	if len(group) == 0 {
		return nil
	}

	switch r.strategy {
	case ConflictResolutionHighestConfidence:
		return r.resolveByConfidence(group)
	case ConflictResolutionMostSpecific:
		return r.resolveBySpecificity(group)
	case ConflictResolutionMostRecent:
		return r.resolveByRecency(group)
	case ConflictResolutionConsensus:
		return r.resolveByConsensus(group)
	default:
		return &group[0]
	}
}

// resolveByConfidence picks the finding with highest confidence
func (r *ConflictResolver) resolveByConfidence(group []Finding) *Finding {
	sort.Slice(group, func(i, j int) bool {
		return group[i].Confidence > group[j].Confidence
	})
	return &group[0]
}

// resolveBySpecificity picks the most specific finding
func (r *ConflictResolver) resolveBySpecificity(group []Finding) *Finding {
	// Score each finding by specificity
	type scoredFinding struct {
		finding Finding
		score   int
	}

	scored := make([]scoredFinding, len(group))
	for i, finding := range group {
		score := 0

		// More evidence = more specific
		score += len(finding.Evidence.Events) * 2
		score += len(finding.Evidence.Metrics)
		score += len(finding.Evidence.Logs)
		score += len(finding.Evidence.GraphPaths) * 3

		// Longer, more detailed message = more specific
		score += len(strings.Fields(finding.Message))

		// More impacted resources = more specific
		score += len(finding.Impact.Resources) * 2

		scored[i] = scoredFinding{finding: finding, score: score}
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	return &scored[0].finding
}

// resolveByRecency picks the most recent finding
func (r *ConflictResolver) resolveByRecency(group []Finding) *Finding {
	sort.Slice(group, func(i, j int) bool {
		return group[i].Timestamp.After(group[j].Timestamp)
	})
	return &group[0]
}

// resolveByConsensus tries to find consensus or combine findings
func (r *ConflictResolver) resolveByConsensus(group []Finding) *Finding {
	// Count finding types
	typeCounts := make(map[string]int)
	for _, finding := range group {
		typeCounts[finding.Type]++
	}

	// Find most common type
	maxCount := 0
	mostCommonType := ""
	for findingType, count := range typeCounts {
		if count > maxCount {
			maxCount = count
			mostCommonType = findingType
		}
	}

	// If majority agrees on type, combine their evidence
	if maxCount > len(group)/2 {
		// Combine findings of the same type
		combined := Finding{
			Type:       mostCommonType,
			Confidence: 0.0,
			Evidence:   Evidence{},
		}

		count := 0
		for _, finding := range group {
			if finding.Type == mostCommonType {
				// Merge evidence
				combined.Evidence.Events = append(combined.Evidence.Events, finding.Evidence.Events...)
				combined.Evidence.Metrics = append(combined.Evidence.Metrics, finding.Evidence.Metrics...)
				combined.Evidence.Logs = append(combined.Evidence.Logs, finding.Evidence.Logs...)
				combined.Evidence.GraphPaths = append(combined.Evidence.GraphPaths, finding.Evidence.GraphPaths...)

				// Average confidence
				combined.Confidence += finding.Confidence
				count++

				// Use first matching finding's other fields
				if combined.ID == "" {
					combined.ID = finding.ID
					combined.Severity = finding.Severity
					combined.Message = finding.Message
					combined.Impact = finding.Impact
					combined.Timestamp = finding.Timestamp
				}
			}
		}

		combined.Confidence /= float64(count)
		return &combined
	}

	// No consensus, fall back to highest confidence
	return r.resolveByConfidence(group)
}
