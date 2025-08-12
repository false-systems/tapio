package aggregator

import (
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"
)

// CausalityBuilder constructs causal chains from findings
type CausalityBuilder struct {
	logger   *zap.Logger
	maxDepth int
}

// NewCausalityBuilder creates a new causality builder
func NewCausalityBuilder(logger *zap.Logger) *CausalityBuilder {
	return &CausalityBuilder{
		logger:   logger,
		maxDepth: 5,
	}
}

// BuildChain builds a causal chain from findings
func (b *CausalityBuilder) BuildChain(findings []Finding) []CausalLink {
	if len(findings) == 0 {
		return []CausalLink{}
	}

	// Sort findings by timestamp (earliest first)
	sorted := make([]Finding, len(findings))
	copy(sorted, findings)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	// Build chain based on temporal and logical relationships
	chain := []CausalLink{}
	processed := make(map[string]bool)

	for i, finding := range sorted {
		if processed[finding.ID] {
			continue
		}

		// Look for related findings that could be caused by this one
		for j := i + 1; j < len(sorted); j++ {
			related := sorted[j]
			if processed[related.ID] {
				continue
			}

			// Check if there's a causal relationship
			if link := b.findCausalRelation(finding, related); link != nil {
				chain = append(chain, *link)
				processed[related.ID] = true
			}
		}

		processed[finding.ID] = true
	}

	// Sort chain by confidence
	sort.Slice(chain, func(i, j int) bool {
		return chain[i].Confidence > chain[j].Confidence
	})

	// Limit chain depth
	if len(chain) > b.maxDepth {
		chain = chain[:b.maxDepth]
	}

	return chain
}

// findCausalRelation determines if finding A caused finding B
func (b *CausalityBuilder) findCausalRelation(findingA, findingB Finding) *CausalLink {
	// Check temporal relationship (A must happen before B)
	if !findingA.Timestamp.Before(findingB.Timestamp) {
		return nil
	}

	// Time window check (events too far apart are unlikely to be related)
	timeDiff := findingB.Timestamp.Sub(findingA.Timestamp)
	if timeDiff > 30*time.Minute {
		return nil
	}

	confidence := 0.0
	relation := ""

	// Check for known causal patterns
	if pattern := b.checkCausalPatterns(findingA, findingB); pattern != nil {
		confidence = pattern.confidence
		relation = pattern.relation
	}

	// Check resource overlap
	if overlap := b.checkResourceOverlap(findingA, findingB); overlap > 0 {
		if confidence == 0 {
			confidence = overlap
			relation = "affects"
		} else {
			confidence = (confidence + overlap) / 2
		}
	}

	// Check if findings are in same scope
	if findingA.Impact.Scope == findingB.Impact.Scope && confidence == 0 {
		confidence = 0.3
		relation = "related_to"
	}

	// Only create link if we have some confidence
	if confidence > 0.2 {
		return &CausalLink{
			From:       fmt.Sprintf("%s: %s", findingA.Type, findingA.Message),
			To:         fmt.Sprintf("%s: %s", findingB.Type, findingB.Message),
			Relation:   relation,
			Confidence: confidence,
			Evidence: Evidence{
				Events: append(findingA.Evidence.Events, findingB.Evidence.Events...),
			},
			Timestamp: findingA.Timestamp,
		}
	}

	return nil
}

// causalPattern represents a known causal relationship
type causalPattern struct {
	relation   string
	confidence float64
}

// checkCausalPatterns checks for known causal patterns
func (b *CausalityBuilder) checkCausalPatterns(findingA, findingB Finding) *causalPattern {
	// Configuration change → Pod restart
	if findingA.Type == "config_change" && findingB.Type == "pod_restart" {
		return &causalPattern{
			relation:   "triggered",
			confidence: 0.9,
		}
	}

	// Memory exhaustion → OOM Kill
	if findingA.Type == "memory_exhaustion" && findingB.Type == "oom_kill" {
		return &causalPattern{
			relation:   "caused",
			confidence: 0.95,
		}
	}

	// Service down → Dependent service errors
	if findingA.Type == "service_unavailable" && findingB.Type == "connection_error" {
		return &causalPattern{
			relation:   "impacted",
			confidence: 0.85,
		}
	}

	// CPU throttling → Latency increase
	if findingA.Type == "cpu_throttling" && findingB.Type == "latency_spike" {
		return &causalPattern{
			relation:   "resulted_in",
			confidence: 0.8,
		}
	}

	// Deployment update → Rolling restart
	if findingA.Type == "deployment_update" && findingB.Type == "pod_restart" {
		return &causalPattern{
			relation:   "initiated",
			confidence: 0.9,
		}
	}

	// Network policy → Connection blocked
	if findingA.Type == "network_policy_change" && findingB.Type == "connection_blocked" {
		return &causalPattern{
			relation:   "enforced",
			confidence: 0.95,
		}
	}

	// Storage full → Write failures
	if findingA.Type == "storage_full" && findingB.Type == "write_error" {
		return &causalPattern{
			relation:   "caused",
			confidence: 0.9,
		}
	}

	return nil
}

// checkResourceOverlap calculates overlap between affected resources
func (b *CausalityBuilder) checkResourceOverlap(findingA, findingB Finding) float64 {
	if len(findingA.Impact.Resources) == 0 || len(findingB.Impact.Resources) == 0 {
		return 0.0
	}

	// Create sets for efficient lookup
	aResources := make(map[string]bool)
	for _, r := range findingA.Impact.Resources {
		aResources[r] = true
	}

	// Count overlapping resources
	overlap := 0
	for _, r := range findingB.Impact.Resources {
		if aResources[r] {
			overlap++
		}
	}

	// Calculate overlap ratio
	totalUnique := len(aResources) + len(findingB.Impact.Resources) - overlap
	if totalUnique == 0 {
		return 0.0
	}

	overlapRatio := float64(overlap) / float64(totalUnique)

	// Convert to confidence (0.5 = 50% overlap → 0.5 confidence)
	return overlapRatio
}

// SetMaxDepth sets the maximum depth for causal chains
func (b *CausalityBuilder) SetMaxDepth(depth int) {
	if depth > 0 {
		b.maxDepth = depth
	}
}
