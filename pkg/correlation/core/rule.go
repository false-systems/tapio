package core

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/domain"
)

// SimpleRule is a basic implementation of the Rule interface
type SimpleRule struct {
	id             string
	name           string
	category       domain.Category
	minConfidence  float64
	cooldown       time.Duration
	enabled        bool
	evaluateFunc   func(*domain.Context) *domain.Result
}

// NewSimpleRule creates a new simple rule
func NewSimpleRule(
	id, name string,
	category domain.Category,
	evaluateFunc func(*domain.Context) *domain.Result,
) *SimpleRule {
	return &SimpleRule{
		id:            id,
		name:          name,
		category:      category,
		minConfidence: 0.7,
		cooldown:      5 * time.Minute,
		enabled:       true,
		evaluateFunc:  evaluateFunc,
	}
}

// ID returns the rule identifier
func (r *SimpleRule) ID() string {
	return r.id
}

// Name returns the rule name
func (r *SimpleRule) Name() string {
	return r.name
}

// Evaluate evaluates the rule against the context
func (r *SimpleRule) Evaluate(ctx *domain.Context) *domain.Result {
	if r.evaluateFunc == nil {
		return nil
	}
	
	return r.evaluateFunc(ctx)
}

// GetMinConfidence returns the minimum confidence threshold
func (r *SimpleRule) GetMinConfidence() float64 {
	return r.minConfidence
}

// GetCooldown returns the cooldown period
func (r *SimpleRule) GetCooldown() time.Duration {
	return r.cooldown
}

// IsEnabled returns whether the rule is enabled
func (r *SimpleRule) IsEnabled() bool {
	return r.enabled
}

// GetCategory returns the rule category
func (r *SimpleRule) GetCategory() domain.Category {
	return r.category
}

// SetEnabled enables or disables the rule
func (r *SimpleRule) SetEnabled(enabled bool) {
	r.enabled = enabled
}

// SetMinConfidence sets the minimum confidence threshold
func (r *SimpleRule) SetMinConfidence(confidence float64) {
	r.minConfidence = confidence
}

// SetCooldown sets the cooldown period
func (r *SimpleRule) SetCooldown(cooldown time.Duration) {
	r.cooldown = cooldown
}

// Common Rule Builders

// NewHighFrequencyRule creates a rule that detects high frequency events
func NewHighFrequencyRule(id, name string, threshold int, timeWindow time.Duration) *SimpleRule {
	return NewSimpleRule(id, name, domain.CategoryPerformance, func(ctx *domain.Context) *domain.Result {
		// Count events in the time window
		eventCount := len(ctx.Events)
		
		if eventCount >= threshold {
			return &domain.Result{
				ID:          fmt.Sprintf("result-%s-%d", id, time.Now().UnixNano()),
				Type:        "high_frequency",
				Confidence:  0.9,
				Description: fmt.Sprintf("High frequency events detected: %d events in %v", eventCount, timeWindow),
				Events:      ctx.Events,
			}
		}
		
		return nil
	})
}

// NewErrorSpikeRule creates a rule that detects error spikes
func NewErrorSpikeRule(id, name string) *SimpleRule {
	return NewSimpleRule(id, name, domain.CategoryReliability, func(ctx *domain.Context) *domain.Result {
		// Look for error events
		errorEvents := ctx.GetEvents(domain.Filter{
			Severity: domain.SeverityHigh,
		})
		
		if len(errorEvents) > 0 {
			// Check if errors are clustered in time
			if len(errorEvents) >= 3 {
				return &domain.Result{
					ID:          fmt.Sprintf("result-%s-%d", id, time.Now().UnixNano()),
					Type:        "error_spike",
					Confidence:  0.8,
					Description: fmt.Sprintf("Error spike detected: %d high-severity events", len(errorEvents)),
					Events:      errorEvents,
				}
			}
		}
		
		return nil
	})
}

// NewResourceExhaustionRule creates a rule that detects resource exhaustion
func NewResourceExhaustionRule(id, name string) *SimpleRule {
	return NewSimpleRule(id, name, domain.CategoryResource, func(ctx *domain.Context) *domain.Result {
		// Look for resource-related events
		resourceEvents := ctx.GetEvents(domain.Filter{
			Category: domain.CategoryResource,
			Severity: domain.SeverityCritical,
		})
		
		if len(resourceEvents) > 0 {
			return &domain.Result{
				ID:          fmt.Sprintf("result-%s-%d", id, time.Now().UnixNano()),
				Type:        "resource_exhaustion",
				Confidence:  0.85,
				Description: fmt.Sprintf("Resource exhaustion detected: %d critical resource events", len(resourceEvents)),
				Events:      resourceEvents,
			}
		}
		
		return nil
	})
}

// NewSequentialEventsRule creates a rule that detects sequential events
func NewSequentialEventsRule(id, name string, eventTypes []string, maxGap time.Duration) *SimpleRule {
	return NewSimpleRule(id, name, domain.CategoryReliability, func(ctx *domain.Context) *domain.Result {
		if len(eventTypes) < 2 {
			return nil
		}
		
		// Find events of each type
		var eventsByType [][]domain.Event
		for _, eventType := range eventTypes {
			events := ctx.GetEvents(domain.Filter{Type: eventType})
			if len(events) == 0 {
				return nil // Missing event type
			}
			eventsByType = append(eventsByType, events)
		}
		
		// Check if we have a sequence within the time gap
		// This is a simplified implementation - could be more sophisticated
		if len(eventsByType) >= 2 {
			firstType := eventsByType[0]
			secondType := eventsByType[1]
			
			for _, first := range firstType {
				for _, second := range secondType {
					if second.Timestamp.After(first.Timestamp) {
						gap := second.Timestamp.Sub(first.Timestamp)
						if gap <= maxGap {
							return &domain.Result{
								ID:          fmt.Sprintf("result-%s-%d", id, time.Now().UnixNano()),
								Type:        "sequential_events",
								Confidence:  0.75,
								Description: fmt.Sprintf("Sequential events detected: %s followed by %s within %v", first.Type, second.Type, gap),
								Events:      []domain.Event{first, second},
							}
						}
					}
				}
			}
		}
		
		return nil
	})
}

// NewCascadingFailureRule creates a rule that detects cascading failures
func NewCascadingFailureRule(id, name string) *SimpleRule {
	return NewSimpleRule(id, name, domain.CategoryReliability, func(ctx *domain.Context) *domain.Result {
		// Look for failure events across different entities
		failureEvents := ctx.GetEvents(domain.Filter{
			Severity: domain.SeverityHigh,
		})
		
		if len(failureEvents) < 2 {
			return nil
		}
		
		// Group by entity
		entityMap := make(map[string][]domain.Event)
		for _, event := range failureEvents {
			key := fmt.Sprintf("%s/%s", event.Entity.Type, event.Entity.Name)
			entityMap[key] = append(entityMap[key], event)
		}
		
		// Check if failures span multiple entities
		if len(entityMap) >= 2 {
			return &domain.Result{
				ID:          fmt.Sprintf("result-%s-%d", id, time.Now().UnixNano()),
				Type:        "cascading_failure",
				Confidence:  0.8,
				Description: fmt.Sprintf("Cascading failure detected: failures across %d entities", len(entityMap)),
				Events:      failureEvents,
			}
		}
		
		return nil
	})
}