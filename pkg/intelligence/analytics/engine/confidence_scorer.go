package engine

import (
	"math"
	"strings"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ConfidenceScorer calculates confidence scores for events
type ConfidenceScorer struct {
	threshold float64
}

// NewConfidenceScorer creates a new confidence scorer
func NewConfidenceScorer(threshold float64) *ConfidenceScorer {
	return &ConfidenceScorer{
		threshold: threshold,
	}
}

// Score calculates confidence score for an event
func (cs *ConfidenceScorer) Score(event *domain.UnifiedEvent) float64 {
	score := 0.5 // Base score

	// Boost score based on trace context completeness
	if event.HasTraceContext() {
		score += 0.1
		if event.TraceContext.ParentSpanID != "" {
			score += 0.05 // Has parent context
		}
		if event.TraceContext.Sampled {
			score += 0.05 // Is being sampled
		}
	}

	// Boost score based on semantic information
	if event.Semantic != nil {
		score += 0.1
		// Use semantic confidence if available
		if event.Semantic.Confidence > 0 {
			score = (score + event.Semantic.Confidence) / 2
		}
		// Known intents get higher scores
		if isKnownSemanticIntent(event.Semantic.Intent) {
			score += 0.1
		}
	}

	// Boost score based on entity context
	if event.Entity != nil && event.Entity.Type != "" && event.Entity.Name != "" {
		score += 0.1
		if event.Entity.UID != "" {
			score += 0.05 // Has unique identifier
		}
	}

	// Boost score based on impact assessment
	if event.Impact != nil {
		if event.Impact.SLOImpact || event.Impact.CustomerFacing || event.Impact.RevenueImpacting {
			score += 0.2 // High-impact events get higher confidence
		}
		// Critical severity
		if event.Impact.Severity == "critical" {
			score += 0.1
		}
	}

	// Boost score based on correlation context
	if event.Correlation != nil {
		score += 0.1
		if len(event.Correlation.CausalChain) > 0 {
			score += 0.05 // Part of causal chain
		}
		if event.Correlation.Pattern != "" {
			score += 0.05 // Matches known pattern
		}
	}

	// Layer-specific scoring
	if event.IsKernelEvent() && event.Kernel != nil {
		// Kernel events with stack traces are more reliable
		if len(event.Kernel.StackTrace) > 0 {
			score += 0.1
		}
		// Failed syscalls might indicate issues
		if event.Kernel.ReturnCode < 0 {
			score += 0.05
		}
	}

	if event.IsNetworkEvent() && event.Network != nil {
		// Network events with complete data
		if event.Network.SourceIP != "" && event.Network.DestIP != "" {
			score += 0.05
		}
		// Error status codes
		if event.Network.StatusCode >= 400 {
			score += 0.1
		}
		// High latency
		if event.Network.Latency > 1000000000 { // > 1 second
			score += 0.05
		}
	}

	if event.IsApplicationEvent() && event.Application != nil {
		// Error/critical logs
		if event.Application.Level == "error" || event.Application.Level == "critical" {
			score += 0.15
		}
		// Has stack trace
		if event.Application.StackTrace != "" {
			score += 0.1
		}
		// Has user/session context
		if event.Application.UserID != "" || event.Application.SessionID != "" {
			score += 0.05
		}
	}

	// Normalize score to 0-1 range
	score = math.Min(score, 1.0)
	score = math.Max(score, 0.0)

	return score
}

// isKnownSemanticIntent checks if the intent is a known pattern
func isKnownSemanticIntent(intent string) bool {
	knownIntents := []string{
		"user-login",
		"cache-miss",
		"oom-kill",
		"connection-timeout",
		"rate-limit",
		"authentication-failure",
		"resource-exhaustion",
		"service-degradation",
		"deployment-started",
		"scaling-triggered",
	}

	intent = strings.ToLower(intent)
	for _, known := range knownIntents {
		if strings.Contains(intent, known) {
			return true
		}
	}
	return false
}
