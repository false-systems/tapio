package behavior

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/internal/intelligence"
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
	Context       *intelligence.EventContext `json:"context"`
	MatchedEvents []*domain.ObservationEvent `json:"-"` // Events that contributed to match
}

// BehaviorConditionMatch represents a matched condition in the behavior engine
// This is specific to behavior matching and doesn't conflict with domain.ConditionMatch
type BehaviorConditionMatch struct {
	Condition   domain.Condition             `json:"condition"`
	Matched     bool                         `json:"matched"`
	ActualValue *intelligence.ConditionValue `json:"actual_value"`
	Message     string                       `json:"message"`
}

// extractContext extracts context from an observation event for pattern matching
func extractContext(event *domain.ObservationEvent) *intelligence.EventContext {
	if event == nil {
		return intelligence.NewEventContextFromEvent("", "", "")
	}

	context := intelligence.NewEventContextFromEvent(event.Type, event.Source, event.ID)

	// Add correlation keys if present
	if event.PID != nil {
		context.SetPID(uint32(*event.PID))
	}
	if event.ContainerID != nil {
		context.SetContainerID(*event.ContainerID)
	}
	if event.PodName != nil {
		context.SetPodName(*event.PodName)
	}
	if event.Namespace != nil {
		context.SetNamespace(*event.Namespace)
	}
	if event.ServiceName != nil {
		context.SetServiceName(*event.ServiceName)
	}
	if event.NodeName != nil {
		context.SetNodeName(*event.NodeName)
	}

	// Add event data
	if event.Action != nil {
		context.SetAction(*event.Action)
	}
	if event.Target != nil {
		context.SetTarget(*event.Target)
	}
	if event.Result != nil {
		context.SetResult(*event.Result)
	}
	if event.Reason != nil {
		context.SetReason(*event.Reason)
	}

	// Add metrics if present
	if event.Duration != nil {
		context.SetDuration(uint64(*event.Duration))
	}
	if event.Size != nil {
		context.SetSize(uint64(*event.Size))
	}
	if event.Count != nil {
		context.SetCount(uint64(*event.Count))
	}

	// Add correlation context
	if event.CausedBy != nil {
		context.SetCausedBy(*event.CausedBy)
	}
	if event.ParentID != nil {
		context.SetParentID(*event.ParentID)
	}

	// Add custom data as string values
	for k, v := range event.Data {
		context.AddCustomData(k, fmt.Sprintf("%v", v))
	}

	return context
}
