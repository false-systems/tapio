package relay

import (
	"strings"
	"sync"

	"github.com/yairfalse/tapio/pkg/api"
)

// SmartRouter implements intelligent event routing
// Routes based on event type, severity, and patterns
type SmartRouter struct {
	rules []RoutingRule
	mu    sync.RWMutex
}

// NewSmartRouter creates a router with default rules
func NewSmartRouter() *SmartRouter {
	return &SmartRouter{
		rules: defaultRoutingRules(),
	}
}

// Route determines where an event should go
func (sr *SmartRouter) Route(event *api.Event) []Destination {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	
	destinations := make([]Destination, 0, 2)
	
	// Always send to engine for correlation
	destinations = append(destinations, Destination{
		Type:     DestinationEngine,
		Endpoint: "engine",
		Priority: 1,
	})
	
	// Apply routing rules
	for _, rule := range sr.rules {
		if sr.matchesRule(event, rule) {
			destinations = append(destinations, rule.Destination)
		}
	}
	
	// Deduplicate destinations
	return sr.deduplicateDestinations(destinations)
}

// UpdatePolicy updates routing rules dynamically
func (sr *SmartRouter) UpdatePolicy(rules []RoutingRule) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	
	sr.rules = rules
	return nil
}

// matchesRule checks if an event matches a routing rule
func (sr *SmartRouter) matchesRule(event *api.Event, rule RoutingRule) bool {
	// Simple condition matching for now
	// Real implementation would use CEL or similar
	
	switch rule.Condition {
	case "all":
		return true
		
	case "errors":
		return event.Level == "ERROR" || event.Level == "CRITICAL"
		
	case "high_priority":
		return event.Level == "CRITICAL" || 
			strings.Contains(event.Type, "oom") ||
			strings.Contains(event.Type, "crash")
			
	case "metrics":
		return event.Type == "metric" || strings.HasPrefix(event.Type, "stat_")
		
	case "security":
		return strings.Contains(event.Type, "security") ||
			strings.Contains(event.Type, "auth") ||
			strings.Contains(event.Type, "rbac")
			
	default:
		// Check if condition matches event type
		return strings.Contains(event.Type, rule.Condition)
	}
}

// deduplicateDestinations removes duplicate destinations
func (sr *SmartRouter) deduplicateDestinations(destinations []Destination) []Destination {
	seen := make(map[string]bool)
	result := make([]Destination, 0, len(destinations))
	
	for _, dest := range destinations {
		key := string(dest.Type) + ":" + dest.Endpoint
		if !seen[key] {
			seen[key] = true
			result = append(result, dest)
		}
	}
	
	return result
}

// defaultRoutingRules returns production-ready routing rules
func defaultRoutingRules() []RoutingRule {
	return []RoutingRule{
		{
			Name:      "otel_all_events",
			Priority:  10,
			Condition: "all",
			Destination: Destination{
				Type:     DestinationOTEL,
				Endpoint: "otel",
				Priority: 2,
			},
		},
		{
			Name:      "critical_to_webhook",
			Priority:  5,
			Condition: "high_priority",
			Destination: Destination{
				Type:     DestinationWebhook,
				Endpoint: "webhook",
				Priority: 3,
			},
		},
		{
			Name:      "metrics_to_prometheus",
			Priority:  8,
			Condition: "metrics",
			Destination: Destination{
				Type:     DestinationPrometheus,
				Endpoint: "prometheus",
				Priority: 2,
			},
		},
	}
}