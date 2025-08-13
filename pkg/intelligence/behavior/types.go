package behavior

import (
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// BehaviorPatternMatch represents a successful pattern match in the behavior engine
// This is specific to the behavior engine and doesn't conflict with domain.PatternMatch
type BehaviorPatternMatch struct {
	PatternID     string                   `json:"pattern_id"`
	PatternName   string                   `json:"pattern_name"`
	EventID       string                   `json:"event_id"`
	Confidence    float64                  `json:"confidence"`
	MatchedAt     time.Time                `json:"matched_at"`
	Conditions    []BehaviorConditionMatch `json:"conditions"`
	Context       map[string]interface{}   `json:"context"`
	MatchedEvents []*domain.UnifiedEvent   `json:"-"` // Events that contributed to match
}

// BehaviorConditionMatch represents a matched condition in the behavior engine
// This is specific to behavior matching and doesn't conflict with domain.ConditionMatch
type BehaviorConditionMatch struct {
	Condition   domain.Condition `json:"condition"`
	Matched     bool             `json:"matched"`
	ActualValue interface{}      `json:"actual_value"`
	Message     string           `json:"message"`
}

// extractContext extracts context from an event for pattern matching
func extractContext(event *domain.UnifiedEvent) map[string]interface{} {
	context := make(map[string]interface{})

	if event == nil {
		return context
	}

	context["source"] = event.Source
	context["type"] = string(event.Type)
	context["severity"] = string(event.Severity)

	// Add namespace from K8sContext if present
	if event.K8sContext != nil {
		context["namespace"] = event.K8sContext.Namespace
		context["resource_name"] = event.K8sContext.Name
		context["resource_kind"] = event.K8sContext.Kind
	}

	// Add entity context if present
	if event.Entity != nil {
		context["entity_type"] = event.Entity.Type
		context["entity_name"] = event.Entity.Name
		if event.Entity.Namespace != "" {
			context["entity_namespace"] = event.Entity.Namespace
		}
	}

	// Add attributes if present
	if event.Attributes != nil {
		for k, v := range event.Attributes {
			context["attr."+k] = v
		}
	}

	return context
}
