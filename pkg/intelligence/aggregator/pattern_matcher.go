package aggregator

import (
	"fmt"
	"strings"
	"time"
)

// PatternMatcher matches causal chains against known patterns
type PatternMatcher struct {
	patterns []KnownPattern
}

// KnownPattern represents a pattern we've seen before
type KnownPattern struct {
	ID          string
	Name        string
	Description string
	Signature   PatternSignature
	Confidence  float64
	Occurrences int
	LastSeen    time.Time
	Resolution  string
}

// PatternSignature defines what to look for
type PatternSignature struct {
	RequiredTypes []string      // Finding types that must be present
	CausalOrder   []string      // Expected order of events
	TimeWindow    time.Duration // Max time span for pattern
	MinFindings   int           // Minimum findings required
}

// NewPatternMatcher creates a new pattern matcher
func NewPatternMatcher() *PatternMatcher {
	pm := &PatternMatcher{
		patterns: []KnownPattern{},
	}

	// Initialize with known patterns
	pm.initializeKnownPatterns()

	return pm
}

// FindMatches finds patterns that match the causal chain
func (pm *PatternMatcher) FindMatches(chain []CausalLink) []Pattern {
	matches := []Pattern{}

	// Extract finding types from chain
	findingTypes := pm.extractFindingTypes(chain)

	// Check each known pattern
	for _, knownPattern := range pm.patterns {
		if pm.matchesPattern(findingTypes, chain, knownPattern) {
			matches = append(matches, Pattern{
				ID:          knownPattern.ID,
				Name:        knownPattern.Name,
				Type:        "known_pattern",
				Confidence:  knownPattern.Confidence,
				Occurrences: knownPattern.Occurrences,
				LastSeen:    knownPattern.LastSeen.Format(time.RFC3339),
			})
		}
	}

	return matches
}

// matchesPattern checks if chain matches a known pattern
func (pm *PatternMatcher) matchesPattern(findingTypes []string, chain []CausalLink, pattern KnownPattern) bool {
	sig := pattern.Signature

	// Check minimum findings
	if len(findingTypes) < sig.MinFindings {
		return false
	}

	// Create index of finding types for O(1) lookup
	typeIndex := make(map[string]bool, len(findingTypes))
	for _, fType := range findingTypes {
		typeIndex[fType] = true
	}

	// Check required types using index
	for _, required := range sig.RequiredTypes {
		found := false
		// Check exact match first
		if typeIndex[required] {
			found = true
		} else {
			// Only do substring search if exact match fails
			for fType := range typeIndex {
				if strings.Contains(fType, required) {
					found = true
					break
				}
			}
		}
		if !found {
			return false
		}
	}

	// Check causal order if specified
	if len(sig.CausalOrder) > 0 {
		if !pm.checkCausalOrder(findingTypes, sig.CausalOrder) {
			return false
		}
	}

	// Check time window
	if sig.TimeWindow > 0 && len(chain) > 0 {
		// Get time span of chain
		earliest := chain[0].Timestamp
		latest := chain[len(chain)-1].Timestamp

		for _, link := range chain {
			if link.Timestamp.Before(earliest) {
				earliest = link.Timestamp
			}
			if link.Timestamp.After(latest) {
				latest = link.Timestamp
			}
		}

		if latest.Sub(earliest) > sig.TimeWindow {
			return false
		}
	}

	return true
}

// extractFindingTypes gets finding types from causal chain
func (pm *PatternMatcher) extractFindingTypes(chain []CausalLink) []string {
	types := []string{}
	seen := make(map[string]bool)

	for _, link := range chain {
		// Extract type from "type: message" format
		if parts := strings.SplitN(link.From, ":", 2); len(parts) > 0 {
			if !seen[parts[0]] {
				types = append(types, parts[0])
				seen[parts[0]] = true
			}
		}
		if parts := strings.SplitN(link.To, ":", 2); len(parts) > 0 {
			if !seen[parts[0]] {
				types = append(types, parts[0])
				seen[parts[0]] = true
			}
		}
	}

	return types
}

// checkCausalOrder verifies events happen in expected order
func (pm *PatternMatcher) checkCausalOrder(actual, expected []string) bool {
	// Simple check: expected order should appear in actual order
	expectedIndex := 0

	for _, actualType := range actual {
		if expectedIndex >= len(expected) {
			return true // Found all expected types
		}

		if strings.Contains(actualType, expected[expectedIndex]) {
			expectedIndex++
		}
	}

	return expectedIndex >= len(expected)
}

// initializeKnownPatterns sets up common patterns
func (pm *PatternMatcher) initializeKnownPatterns() {
	pm.patterns = []KnownPattern{
		{
			ID:          "config-cascade-oom",
			Name:        "Configuration Change OOM Cascade",
			Description: "Configuration change leads to increased memory usage causing OOM kills",
			Signature: PatternSignature{
				RequiredTypes: []string{"config_change", "memory_exhaustion", "oom_kill"},
				CausalOrder:   []string{"config_change", "memory_exhaustion", "oom_kill"},
				TimeWindow:    30 * time.Minute,
				MinFindings:   3,
			},
			Confidence:  0.9,
			Occurrences: 0,
			LastSeen:    time.Now(),
			Resolution:  "Revert configuration change or increase memory limits",
		},
		{
			ID:          "deployment-restart-cascade",
			Name:        "Deployment Rolling Restart Cascade",
			Description: "Deployment update triggers rolling restarts affecting dependent services",
			Signature: PatternSignature{
				RequiredTypes: []string{"deployment_update", "pod_restart", "service_unavailable"},
				CausalOrder:   []string{"deployment_update", "pod_restart"},
				TimeWindow:    15 * time.Minute,
				MinFindings:   3,
			},
			Confidence:  0.85,
			Occurrences: 0,
			LastSeen:    time.Now(),
			Resolution:  "Use proper readiness probes and rolling update strategy",
		},
		{
			ID:          "resource-exhaustion-cascade",
			Name:        "Resource Exhaustion Cascade",
			Description: "CPU/Memory exhaustion leads to throttling and service degradation",
			Signature: PatternSignature{
				RequiredTypes: []string{"cpu_throttling", "latency_spike", "timeout"},
				CausalOrder:   []string{"cpu_throttling", "latency_spike"},
				TimeWindow:    10 * time.Minute,
				MinFindings:   3,
			},
			Confidence:  0.88,
			Occurrences: 0,
			LastSeen:    time.Now(),
			Resolution:  "Increase resource limits or optimize application",
		},
		{
			ID:          "network-policy-block",
			Name:        "Network Policy Blocking",
			Description: "Network policy change blocks legitimate traffic",
			Signature: PatternSignature{
				RequiredTypes: []string{"network_policy_change", "connection_blocked", "service_unavailable"},
				CausalOrder:   []string{"network_policy_change", "connection_blocked"},
				TimeWindow:    5 * time.Minute,
				MinFindings:   2,
			},
			Confidence:  0.92,
			Occurrences: 0,
			LastSeen:    time.Now(),
			Resolution:  "Review and fix network policy rules",
		},
		{
			ID:          "storage-pressure-eviction",
			Name:        "Storage Pressure Pod Eviction",
			Description: "Node storage pressure causes pod evictions",
			Signature: PatternSignature{
				RequiredTypes: []string{"storage_full", "pod_evicted", "service_unavailable"},
				CausalOrder:   []string{"storage_full", "pod_evicted"},
				TimeWindow:    20 * time.Minute,
				MinFindings:   3,
			},
			Confidence:  0.87,
			Occurrences: 0,
			LastSeen:    time.Now(),
			Resolution:  "Clean up disk space or add storage capacity",
		},
	}
}

// AddPattern adds a new pattern to the matcher
func (pm *PatternMatcher) AddPattern(pattern KnownPattern) {
	pm.patterns = append(pm.patterns, pattern)
}

// UpdatePatternOccurrence updates pattern statistics when matched
func (pm *PatternMatcher) UpdatePatternOccurrence(patternID string) {
	for i := range pm.patterns {
		if pm.patterns[i].ID == patternID {
			pm.patterns[i].Occurrences++
			pm.patterns[i].LastSeen = time.Now()
			break
		}
	}
}

// GetPattern retrieves a specific pattern by ID
func (pm *PatternMatcher) GetPattern(patternID string) (*KnownPattern, error) {
	for _, pattern := range pm.patterns {
		if pattern.ID == patternID {
			return &pattern, nil
		}
	}
	return nil, fmt.Errorf("pattern not found: %s", patternID)
}
