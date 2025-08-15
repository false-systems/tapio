package behavior

import (
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// BehaviorPatternMatch represents a successful pattern match in the behavior engine
// This is specific to the behavior engine and doesn't conflict with domain.PatternMatch
type BehaviorPatternMatch struct {
	PatternID     string                     `json:"pattern_id"`
	PatternName   string                     `json:"pattern_name"`
	EventID       string                     `json:"event_id"`
	Confidence    float64                    `json:"confidence"`
	MatchedAt     time.Time                  `json:"matched_at"`
	Conditions    []BehaviorConditionMatch   `json:"conditions"`
	Context       map[string]interface{}     `json:"context"`
	MatchedEvents []*domain.ObservationEvent `json:"-"` // Events that contributed to match
}

// BehaviorConditionMatch represents a matched condition in the behavior engine
// This is specific to behavior matching and doesn't conflict with domain.ConditionMatch
type BehaviorConditionMatch struct {
	Condition   domain.Condition `json:"condition"`
	Matched     bool             `json:"matched"`
	ActualValue interface{}      `json:"actual_value"`
	Message     string           `json:"message"`
}

// extractContext extracts context from an observation event for pattern matching
func extractContext(event *domain.ObservationEvent) map[string]interface{} {
	context := make(map[string]interface{})

	if event == nil {
		return context
	}

	context["source"] = event.Source
	context["type"] = event.Type

	// Add correlation keys if present
	if event.PID != nil {
		context["pid"] = *event.PID
	}
	if event.ContainerID != nil {
		context["container_id"] = *event.ContainerID
	}
	if event.PodName != nil {
		context["pod_name"] = *event.PodName
	}
	if event.Namespace != nil {
		context["namespace"] = *event.Namespace
	}
	if event.ServiceName != nil {
		context["service_name"] = *event.ServiceName
	}
	if event.NodeName != nil {
		context["node_name"] = *event.NodeName
	}

	// Add event data
	if event.Action != nil {
		context["action"] = *event.Action
	}
	if event.Target != nil {
		context["target"] = *event.Target
	}
	if event.Result != nil {
		context["result"] = *event.Result
	}

	// Add metrics if present
	if event.Duration != nil {
		context["duration_ms"] = *event.Duration
	}
	if event.Size != nil {
		context["size_bytes"] = *event.Size
	}
	if event.Count != nil {
		context["count"] = *event.Count
	}

	// Add custom data
	for k, v := range event.Data {
		context["data."+k] = v
	}

	return context
}
