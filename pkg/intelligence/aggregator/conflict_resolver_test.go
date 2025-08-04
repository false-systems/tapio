package aggregator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewConflictResolver(t *testing.T) {
	resolver := NewConflictResolver(ConflictResolutionHighestConfidence)

	assert.NotNil(t, resolver)
	assert.Equal(t, ConflictResolutionHighestConfidence, resolver.strategy)
}

func TestResolve_NoConflicts(t *testing.T) {
	resolver := NewConflictResolver(ConflictResolutionHighestConfidence)

	findings := []Finding{
		{
			ID:     "f1",
			Type:   "memory_issue",
			Impact: Impact{Resources: []string{"pod-1"}},
		},
		{
			ID:     "f2",
			Type:   "network_issue",
			Impact: Impact{Resources: []string{"pod-2"}},
		},
	}

	resolved := resolver.Resolve(findings)

	assert.Len(t, resolved, 2)
}

func TestResolve_SingleFinding(t *testing.T) {
	resolver := NewConflictResolver(ConflictResolutionHighestConfidence)

	findings := []Finding{
		{
			ID:     "f1",
			Type:   "memory_issue",
			Impact: Impact{Resources: []string{"pod-1"}},
		},
	}

	resolved := resolver.Resolve(findings)

	assert.Len(t, resolved, 1)
	assert.Equal(t, "f1", resolved[0].ID)
}

func TestResolveByConfidence(t *testing.T) {
	resolver := NewConflictResolver(ConflictResolutionHighestConfidence)

	findings := []Finding{
		{
			ID:         "f1",
			Type:       "memory_issue",
			Confidence: 0.6,
			Impact:     Impact{Resources: []string{"pod-1"}},
		},
		{
			ID:         "f2",
			Type:       "cpu_issue",
			Confidence: 0.9,
			Impact:     Impact{Resources: []string{"pod-1"}},
		},
		{
			ID:         "f3",
			Type:       "network_issue",
			Confidence: 0.7,
			Impact:     Impact{Resources: []string{"pod-1"}},
		},
	}

	resolved := resolver.Resolve(findings)

	assert.Len(t, resolved, 1)
	assert.Equal(t, "f2", resolved[0].ID) // Highest confidence
}

func TestResolveBySpecificity(t *testing.T) {
	resolver := NewConflictResolver(ConflictResolutionMostSpecific)

	findings := []Finding{
		{
			ID:      "f1",
			Type:    "issue",
			Message: "Brief message",
			Evidence: Evidence{
				Events: make([]domain.UnifiedEvent, 1),
			},
			Impact: Impact{Resources: []string{"pod-1"}},
		},
		{
			ID:      "f2",
			Type:    "issue",
			Message: "Very detailed message with lots of specific information",
			Evidence: Evidence{
				Events:     make([]domain.UnifiedEvent, 3),
				Metrics:    make([]MetricPoint, 2),
				GraphPaths: make([]GraphPath, 1),
			},
			Impact: Impact{Resources: []string{"pod-1"}},
		},
	}

	resolved := resolver.Resolve(findings)

	assert.Len(t, resolved, 1)
	assert.Equal(t, "f2", resolved[0].ID) // More specific
}

func TestResolveByRecency(t *testing.T) {
	resolver := NewConflictResolver(ConflictResolutionMostRecent)

	now := time.Now()
	findings := []Finding{
		{
			ID:        "f1",
			Type:      "issue",
			Timestamp: now.Add(-5 * time.Minute),
			Impact:    Impact{Resources: []string{"pod-1"}},
		},
		{
			ID:        "f2",
			Type:      "issue",
			Timestamp: now,
			Impact:    Impact{Resources: []string{"pod-1"}},
		},
		{
			ID:        "f3",
			Type:      "issue",
			Timestamp: now.Add(-1 * time.Minute),
			Impact:    Impact{Resources: []string{"pod-1"}},
		},
	}

	resolved := resolver.Resolve(findings)

	assert.Len(t, resolved, 1)
	assert.Equal(t, "f2", resolved[0].ID) // Most recent
}

func TestResolveByConsensus(t *testing.T) {
	resolver := NewConflictResolver(ConflictResolutionConsensus)

	findings := []Finding{
		{
			ID:         "f1",
			Type:       "memory_issue",
			Confidence: 0.7,
			Message:    "Memory problem",
			Severity:   SeverityHigh,
			Impact:     Impact{Resources: []string{"pod-1"}},
			Evidence:   Evidence{Events: make([]domain.UnifiedEvent, 1)},
		},
		{
			ID:         "f2",
			Type:       "memory_issue",
			Confidence: 0.8,
			Message:    "Memory leak detected",
			Severity:   SeverityHigh,
			Impact:     Impact{Resources: []string{"pod-1"}},
			Evidence:   Evidence{Events: make([]domain.UnifiedEvent, 2)},
		},
		{
			ID:         "f3",
			Type:       "network_issue",
			Confidence: 0.9,
			Impact:     Impact{Resources: []string{"pod-1"}},
		},
	}

	resolved := resolver.Resolve(findings)

	assert.Len(t, resolved, 1)
	// Should combine memory issues (majority)
	assert.Equal(t, "memory_issue", resolved[0].Type)
	// Combined confidence should be average
	assert.InDelta(t, 0.75, resolved[0].Confidence, 0.01)
	// Should have combined evidence
	assert.Len(t, resolved[0].Evidence.Events, 3)
}

func TestResolveByConsensus_NoMajority(t *testing.T) {
	resolver := NewConflictResolver(ConflictResolutionConsensus)

	findings := []Finding{
		{
			ID:         "f1",
			Type:       "memory_issue",
			Confidence: 0.6,
			Impact:     Impact{Resources: []string{"pod-1"}},
		},
		{
			ID:         "f2",
			Type:       "cpu_issue",
			Confidence: 0.9,
			Impact:     Impact{Resources: []string{"pod-1"}},
		},
		{
			ID:         "f3",
			Type:       "network_issue",
			Confidence: 0.7,
			Impact:     Impact{Resources: []string{"pod-1"}},
		},
	}

	resolved := resolver.Resolve(findings)

	assert.Len(t, resolved, 1)
	// Should fall back to highest confidence
	assert.Equal(t, "f2", resolved[0].ID)
}

func TestGroupByTarget(t *testing.T) {
	resolver := NewConflictResolver(ConflictResolutionHighestConfidence)

	findings := []Finding{
		{
			ID:     "f1",
			Type:   "memory_issue",
			Impact: Impact{Resources: []string{"pod-1", "pod-2"}},
		},
		{
			ID:     "f2",
			Type:   "cpu_issue",
			Impact: Impact{Resources: []string{"pod-1"}},
		},
		{
			ID:     "f3",
			Type:   "network_issue",
			Impact: Impact{Resources: []string{"pod-3"}},
		},
		{
			ID:     "f4",
			Type:   "orphan_issue",
			Impact: Impact{Resources: []string{}}, // No resources
		},
	}

	groups := resolver.groupByTarget(findings)

	assert.Len(t, groups, 3)
	assert.Len(t, groups["pod-1"], 2)
	assert.Len(t, groups["pod-3"], 1)
	assert.Len(t, groups["orphan_issue"], 1) // Grouped by type
}

func TestResolveGroup_EmptyGroup(t *testing.T) {
	resolver := NewConflictResolver(ConflictResolutionHighestConfidence)

	result := resolver.resolveGroup([]Finding{})

	assert.Nil(t, result)
}

func TestResolveGroup_UnknownStrategy(t *testing.T) {
	resolver := NewConflictResolver(ConflictResolution("unknown"))

	findings := []Finding{
		{ID: "f1"},
		{ID: "f2"},
	}

	result := resolver.resolveGroup(findings)

	assert.NotNil(t, result)
	assert.Equal(t, "f1", result.ID) // Falls back to first
}
