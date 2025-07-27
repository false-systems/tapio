package patterns

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestK8sPatternLibrary(t *testing.T) {
	library := NewK8sPatternLibrary()

	t.Run("Library initialization", func(t *testing.T) {
		// Check that patterns are loaded
		assert.True(t, len(library.patterns) > 0, "Library should have patterns")

		// Check categories
		assert.True(t, len(library.byCategory[CategoryDeployment]) > 0)
		assert.True(t, len(library.byCategory[CategoryNetwork]) > 0)
		assert.True(t, len(library.byCategory[CategorySecurity]) > 0)
	})

	t.Run("Pattern retrieval", func(t *testing.T) {
		// Get specific pattern
		pattern, exists := library.GetPattern("rolling-update-failure")
		assert.True(t, exists)
		assert.NotNil(t, pattern)
		assert.Equal(t, "Rolling Update Failure", pattern.Name)
		assert.Equal(t, CategoryDeployment, pattern.Category)

		// Get non-existent pattern
		_, exists = library.GetPattern("non-existent")
		assert.False(t, exists)
	})

	t.Run("Pattern matching - Rolling Update Failure", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test-1",
			Timestamp: time.Now(),
			Type:      domain.EventTypeKubernetes,
			Source:    "k8s",
			Severity:  domain.EventSeverityError,
			Kubernetes: &domain.KubernetesData{
				Object:  "pod/api-v2-abc",
				Reason:  "CrashLoopBackOff",
				Message: "Back-off restarting failed container",
			},
			Entity: &domain.EntityContext{
				Type:      "pod",
				Name:      "api-v2-abc",
				Namespace: "production",
			},
		}

		matches := library.MatchEvent(event)
		require.Len(t, matches, 1)
		assert.Equal(t, "rolling-update-failure", matches[0].Pattern.ID)
		assert.True(t, matches[0].Confidence > 0)
	})

	t.Run("Pattern matching - Network Policy Block", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test-2",
			Timestamp: time.Now(),
			Type:      domain.EventTypeNetwork,
			Source:    "cni",
			Network: &domain.NetworkData{
				Protocol: "TCP",
				SourceIP: "10.244.1.10",
				DestIP:   "10.244.2.20",
			},
			// This would need actual network drop indicators
		}

		matches := library.MatchEvent(event)
		// Should not match without proper indicators
		assert.Len(t, matches, 0)
	})

	t.Run("Pattern matching - Security Alert", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test-3",
			Timestamp: time.Now(),
			Type:      domain.EventTypeSystem,
			Source:    "ebpf",
			Severity:  domain.EventSeverityCritical,
			Kernel: &domain.KernelData{
				Syscall: "setuid",
				Comm:    "malicious",
			},
		}

		matches := library.MatchEvent(event)
		hasSecurityMatch := false
		for _, match := range matches {
			if match.Pattern.Category == CategorySecurity {
				hasSecurityMatch = true
				break
			}
		}
		assert.True(t, hasSecurityMatch, "Should match security pattern")
	})

	t.Run("Related patterns", func(t *testing.T) {
		// Pod eviction storm has correlation with node resource exhaustion
		related := library.GetRelatedPatterns("pod-eviction-storm")
		assert.Len(t, related, 1)
		assert.Equal(t, "node-resource-exhaustion", related[0].ID)

		// Check bidirectional correlation
		related = library.GetRelatedPatterns("node-resource-exhaustion")
		assert.Len(t, related, 1)
		assert.Equal(t, "pod-eviction-storm", related[0].ID)
	})

	t.Run("Pattern categories", func(t *testing.T) {
		deploymentPatterns := library.GetPatternsByCategory(CategoryDeployment)
		assert.True(t, len(deploymentPatterns) > 0)

		for _, pattern := range deploymentPatterns {
			assert.Equal(t, CategoryDeployment, pattern.Category)
		}

		securityPatterns := library.GetPatternsByCategory(CategorySecurity)
		assert.True(t, len(securityPatterns) > 0)
	})
}

func TestPatternIndicators(t *testing.T) {
	library := NewK8sPatternLibrary()

	t.Run("Event indicator matching", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			Kubernetes: &domain.KubernetesData{
				Reason:  "FailedScheduling",
				Message: "0/3 nodes are available",
			},
		}

		indicator := PatternIndicator{
			Type:      IndicatorEvent,
			Field:     "reason",
			Condition: "equals",
			Value:     "FailedScheduling",
		}

		matched := library.checkIndicator(event, indicator)
		assert.True(t, matched)

		// Test non-matching
		indicator.Value = "DifferentReason"
		matched = library.checkIndicator(event, indicator)
		assert.False(t, matched)
	})

	t.Run("State indicator matching", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			State: &domain.StateInfo{
				Current:    "Failed",
				Previous:   "Running",
				Transition: "Running->Failed",
			},
		}

		indicator := PatternIndicator{
			Type:      IndicatorState,
			Field:     "state.current",
			Condition: "equals",
			Value:     "Failed",
		}

		matched := library.checkIndicator(event, indicator)
		assert.True(t, matched)
	})

	t.Run("Condition checking", func(t *testing.T) {
		// Test equals
		assert.True(t, library.checkCondition("test", "equals", "test"))
		assert.False(t, library.checkCondition("test", "equals", "other"))

		// Test contains
		assert.True(t, library.checkCondition("test message", "contains", "message"))
		assert.False(t, library.checkCondition("test", "contains", "xyz"))

		// Test in
		assert.True(t, library.checkCondition("curl", "in", []string{"curl", "wget", "nc"}))
		assert.False(t, library.checkCondition("ls", "in", []string{"curl", "wget", "nc"}))
	})
}

func TestPatternMatching(t *testing.T) {
	library := NewK8sPatternLibrary()

	t.Run("Multiple indicator matching", func(t *testing.T) {
		// Create event that matches multiple indicators
		event := &domain.UnifiedEvent{
			ID:        "multi-1",
			Timestamp: time.Now(),
			Type:      domain.EventTypeKubernetes,
			Kubernetes: &domain.KubernetesData{
				Reason:  "CrashLoopBackOff",
				Message: "Back-off restarting failed container",
			},
			Entity: &domain.EntityContext{
				Type:      "pod",
				Name:      "api-v2-xyz",
				Namespace: "production",
			},
		}

		matches := library.MatchEvent(event)
		require.True(t, len(matches) > 0)

		// Check confidence calculation
		match := matches[0]
		assert.True(t, match.Confidence > 0 && match.Confidence <= 1.0)
		assert.True(t, len(match.MatchedIndicators) > 0)
	})

	t.Run("Pattern context extraction", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			Timestamp: time.Now(),
			Severity:  domain.EventSeverityCritical,
			Entity: &domain.EntityContext{
				Type:      "pod",
				Name:      "test-pod",
				Namespace: "test-ns",
			},
		}

		pattern := &K8sPattern{ID: "test-pattern"}
		context := library.extractContext(event, pattern)

		assert.Equal(t, "test-pod", context["entity_name"])
		assert.Equal(t, "test-ns", context["namespace"])
		assert.Equal(t, "pod", context["entity_type"])
		assert.Equal(t, domain.EventSeverityCritical, context["severity"])
	})
}

func BenchmarkPatternMatching(b *testing.B) {
	library := NewK8sPatternLibrary()
	event := CreateExamplePatternEvent("rolling-update-failure")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = library.MatchEvent(event)
	}
}

func BenchmarkPatternLibraryLookup(b *testing.B) {
	library := NewK8sPatternLibrary()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = library.GetPattern("rolling-update-failure")
	}
}
