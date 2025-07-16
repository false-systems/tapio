package relay

import (
	"crypto/md5"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/api"
)

// TimeWindowAggregator aggregates events within time windows
type TimeWindowAggregator struct {
	window        time.Duration
	aggregations  map[string]*AggregatedEvent
	mu            sync.RWMutex
	lastCleanup   time.Time
}

// NewTimeWindowAggregator creates a new time-based aggregator
func NewTimeWindowAggregator(window time.Duration) *TimeWindowAggregator {
	return &TimeWindowAggregator{
		window:       window,
		aggregations: make(map[string]*AggregatedEvent),
		lastCleanup:  time.Now(),
	}
}

// ShouldAggregate determines if events should be aggregated
func (twa *TimeWindowAggregator) ShouldAggregate(events []*api.Event) bool {
	// Aggregate if we have multiple similar events
	if len(events) < 2 {
		return false
	}
	
	// Check if events are similar enough to aggregate
	firstType := events[0].Type
	for _, event := range events[1:] {
		if event.Type != firstType {
			return false
		}
	}
	
	return true
}

// Aggregate combines multiple events
func (twa *TimeWindowAggregator) Aggregate(events []*api.Event) (*AggregatedEvent, error) {
	if len(events) == 0 {
		return nil, fmt.Errorf("no events to aggregate")
	}
	
	// Generate aggregation key
	key := twa.generateAggregationKey(events[0])
	
	twa.mu.Lock()
	defer twa.mu.Unlock()
	
	// Clean up old aggregations periodically
	if time.Since(twa.lastCleanup) > twa.window {
		twa.cleanup()
		twa.lastCleanup = time.Now()
	}
	
	// Get or create aggregation
	agg, exists := twa.aggregations[key]
	if !exists {
		agg = &AggregatedEvent{
			ID:        fmt.Sprintf("agg-%s-%d", key, time.Now().Unix()),
			Type:      events[0].Type,
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
			Sources:   make([]string, 0),
			Events:    make([]*api.Event, 0),
		}
		twa.aggregations[key] = agg
	}
	
	// Update aggregation
	for _, event := range events {
		agg.Count++
		agg.LastSeen = time.Now()
		agg.Events = append(agg.Events, event)
		
		// Track unique sources
		source := fmt.Sprintf("%s/%s", event.Namespace, event.PodName)
		if !contains(agg.Sources, source) {
			agg.Sources = append(agg.Sources, source)
		}
	}
	
	// Calculate significance
	agg.Significance = twa.calculateSignificance(agg)
	
	// Detect pattern
	agg.Pattern = twa.detectPattern(agg)
	
	return agg, nil
}

// GetWindow returns the aggregation window duration
func (twa *TimeWindowAggregator) GetWindow() time.Duration {
	return twa.window
}

// generateAggregationKey creates a key for grouping similar events
func (twa *TimeWindowAggregator) generateAggregationKey(event *api.Event) string {
	// Create key based on event characteristics
	h := md5.New()
	h.Write([]byte(event.Type))
	h.Write([]byte(event.Namespace))
	h.Write([]byte(event.Level))
	
	// Include pattern-specific fields
	if event.ContainerName != "" {
		h.Write([]byte(event.ContainerName))
	}
	
	return fmt.Sprintf("%x", h.Sum(nil))[:8]
}

// calculateSignificance determines how significant an aggregation is
func (twa *TimeWindowAggregator) calculateSignificance(agg *AggregatedEvent) float64 {
	// Factors that increase significance:
	// - High event count
	// - Multiple sources affected
	// - Short time window
	// - Critical severity
	
	significance := 0.0
	
	// Event frequency
	duration := agg.LastSeen.Sub(agg.FirstSeen)
	if duration > 0 {
		eventsPerMinute := float64(agg.Count) / duration.Minutes()
		significance += eventsPerMinute * 0.3
	}
	
	// Number of affected sources
	significance += float64(len(agg.Sources)) * 0.2
	
	// Event type criticality
	if agg.Type == "pod_oom_killed" || agg.Type == "pod_crash" {
		significance += 0.5
	}
	
	// Normalize to 0-1 range
	if significance > 1.0 {
		significance = 1.0
	}
	
	return significance
}

// detectPattern identifies patterns in aggregated events
func (twa *TimeWindowAggregator) detectPattern(agg *AggregatedEvent) string {
	// Simple pattern detection based on event characteristics
	
	if agg.Count > 10 && len(agg.Sources) == 1 {
		return "single_source_storm"
	}
	
	if agg.Count > 5 && len(agg.Sources) > 3 {
		return "distributed_issue"
	}
	
	duration := agg.LastSeen.Sub(agg.FirstSeen)
	if duration < time.Minute && agg.Count > 5 {
		return "rapid_burst"
	}
	
	if agg.Type == "pod_restart" && agg.Count > 3 {
		return "restart_loop"
	}
	
	return "normal_frequency"
}

// cleanup removes old aggregations
func (twa *TimeWindowAggregator) cleanup() {
	cutoff := time.Now().Add(-twa.window)
	
	for key, agg := range twa.aggregations {
		if agg.LastSeen.Before(cutoff) {
			delete(twa.aggregations, key)
		}
	}
}

// contains checks if a string slice contains a value
func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}