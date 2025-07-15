package correlation

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/events/opinionated"
)

// Helper methods for SemanticPatternEngine

func (e *SemanticPatternEngine) extractSemanticFeatures(event *opinionated.OpinionatedEvent) map[string]interface{} {
	features := make(map[string]interface{})
	
	// Extract text features
	features["message"] = strings.ToLower(event.Message)
	features["source"] = event.Source
	features["type"] = event.Type
	features["severity"] = event.Severity
	
	// Extract keywords
	keywords := e.extractKeywords(event.Message)
	features["keywords"] = keywords
	
	// Extract entities
	features["resource"] = event.ResourceID
	features["namespace"] = event.Namespace
	
	return features
}

func (e *SemanticPatternEngine) calculateSemanticSimilarity(features map[string]interface{}, pattern *SemanticPattern) float64 {
	score := 0.0
	
	// Keyword matching
	eventKeywords := features["keywords"].([]string)
	matchedKeywords := 0
	for _, keyword := range pattern.Keywords {
		for _, eventKeyword := range eventKeywords {
			if strings.Contains(eventKeyword, keyword) {
				matchedKeywords++
				break
			}
		}
	}
	
	if len(pattern.Keywords) > 0 {
		score = float64(matchedKeywords) / float64(len(pattern.Keywords))
	}
	
	return score
}

func (e *SemanticPatternEngine) findMatchingKeywords(event *opinionated.OpinionatedEvent, pattern *SemanticPattern) []string {
	var matched []string
	eventText := strings.ToLower(event.Message + " " + event.Type)
	
	for _, keyword := range pattern.Keywords {
		if strings.Contains(eventText, keyword) {
			matched = append(matched, keyword)
		}
	}
	
	return matched
}

func (e *SemanticPatternEngine) extractKeywords(text string) []string {
	// Simple keyword extraction
	words := strings.Fields(strings.ToLower(text))
	keywords := make([]string, 0)
	
	// Filter stop words and short words
	stopWords := map[string]bool{
		"the": true, "is": true, "at": true, "in": true, "on": true,
		"a": true, "an": true, "and": true, "or": true, "but": true,
	}
	
	for _, word := range words {
		if len(word) > 3 && !stopWords[word] {
			keywords = append(keywords, word)
		}
	}
	
	return keywords
}

// Helper methods for BehavioralPatternEngine

func (e *BehavioralPatternEngine) extractBehavioralMetrics(event *opinionated.OpinionatedEvent) map[string]float64 {
	metrics := make(map[string]float64)
	
	// Extract numeric metrics from event
	if event.Metrics != nil {
		for k, v := range event.Metrics {
			if fval, ok := v.(float64); ok {
				metrics[k] = fval
			} else if ival, ok := v.(int); ok {
				metrics[k] = float64(ival)
			}
		}
	}
	
	// Add derived metrics
	metrics["event_count"] = 1.0
	metrics["severity_score"] = e.severityToScore(event.Severity)
	
	return metrics
}

func (e *BehavioralPatternEngine) detectBehavioralAnomalies(current, baseline map[string]float64) []string {
	var anomalies []string
	
	for metric, value := range current {
		if baseValue, exists := baseline[metric]; exists {
			deviation := math.Abs(value-baseValue) / (baseValue + 1.0)
			if deviation > 0.5 { // 50% deviation threshold
				anomalies = append(anomalies, metric)
			}
		}
	}
	
	return anomalies
}

func (e *BehavioralPatternEngine) severityToScore(severity string) float64 {
	switch severity {
	case "critical":
		return 5.0
	case "error":
		return 4.0
	case "warning":
		return 3.0
	case "info":
		return 2.0
	default:
		return 1.0
	}
}

// Helper methods for TemporalPatternEngine

func (e *TemporalPatternEngine) sortEventsByTime(events []*opinionated.OpinionatedEvent) {
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
}

func (e *TemporalPatternEngine) matchKnownSequence(window []*opinionated.OpinionatedEvent) *TemporalSequence {
	// Check for known sequences
	eventTypes := make([]string, len(window))
	for i, event := range window {
		eventTypes[i] = event.Type
	}
	
	// Example: OOM followed by restart
	if len(window) >= 2 {
		if eventTypes[0] == "oom_killed" && eventTypes[1] == "container_restart" {
			return &TemporalSequence{
				ID:       "oom_restart_sequence",
				Events:   []string{window[0].ID, window[1].ID},
				Duration: window[1].Timestamp.Sub(window[0].Timestamp),
				Pattern:  "OOM-Restart Cascade",
			}
		}
	}
	
	return nil
}

func (e *TemporalPatternEngine) detectPeriodicPattern(window []*opinionated.OpinionatedEvent) *TemporalSequence {
	// Detect if events occur at regular intervals
	if len(window) < 3 {
		return nil
	}
	
	intervals := make([]time.Duration, len(window)-1)
	for i := 1; i < len(window); i++ {
		intervals[i-1] = window[i].Timestamp.Sub(window[i-1].Timestamp)
	}
	
	// Check if intervals are consistent
	avgInterval := time.Duration(0)
	for _, interval := range intervals {
		avgInterval += interval
	}
	avgInterval /= time.Duration(len(intervals))
	
	// Check variance
	variance := 0.0
	for _, interval := range intervals {
		diff := float64(interval - avgInterval)
		variance += diff * diff
	}
	variance /= float64(len(intervals))
	
	// If variance is low, we have a periodic pattern
	if variance < float64(avgInterval*avgInterval)/10 {
		eventIDs := make([]string, len(window))
		for i, event := range window {
			eventIDs[i] = event.ID
		}
		
		return &TemporalSequence{
			ID:       "periodic_pattern",
			Events:   eventIDs,
			Duration: window[len(window)-1].Timestamp.Sub(window[0].Timestamp),
			Pattern:  "Periodic Event",
			Period:   avgInterval,
		}
	}
	
	return nil
}

func (e *TemporalPatternEngine) detectCascade(window []*opinionated.OpinionatedEvent) *TemporalSequence {
	// Detect cascading failures
	if len(window) < 3 {
		return nil
	}
	
	// Check if severity escalates
	severityEscalating := true
	for i := 1; i < len(window); i++ {
		if e.severityLevel(window[i].Severity) < e.severityLevel(window[i-1].Severity) {
			severityEscalating = false
			break
		}
	}
	
	if severityEscalating {
		eventIDs := make([]string, len(window))
		for i, event := range window {
			eventIDs[i] = event.ID
		}
		
		return &TemporalSequence{
			ID:       "cascading_failure",
			Events:   eventIDs,
			Duration: window[len(window)-1].Timestamp.Sub(window[0].Timestamp),
			Pattern:  "Cascading Failure",
		}
	}
	
	return nil
}

func (e *TemporalPatternEngine) severityLevel(severity string) int {
	levels := map[string]int{
		"info":     1,
		"warning":  2,
		"error":    3,
		"critical": 4,
	}
	return levels[severity]
}

// Helper methods for CausalityPatternEngine

func (e *CausalityPatternEngine) buildCausalityGraph(event *opinionated.OpinionatedEvent) map[string][]string {
	graph := make(map[string][]string)
	
	// Simple causality rules
	switch event.Type {
	case "container_restart":
		graph[event.ID] = []string{"oom_killed", "crash_loop", "health_check_failed"}
	case "service_unavailable":
		graph[event.ID] = []string{"pod_not_ready", "network_failure", "dependency_down"}
	case "high_latency":
		graph[event.ID] = []string{"cpu_throttling", "memory_pressure", "network_congestion"}
	}
	
	return graph
}

func (e *CausalityPatternEngine) isRelated(event *opinionated.OpinionatedEvent, causeID string) bool {
	// Check if events are related by resource or namespace
	for _, chain := range e.causalChains {
		if chain.Effect == event.ID || contains(chain.Causes, causeID) {
			return true
		}
	}
	return false
}

func (e *CausalityPatternEngine) calculateCausalConfidence(causes []string, effect *opinionated.OpinionatedEvent) float64 {
	// Simple confidence calculation based on number of causes and timing
	baseConfidence := 0.5
	causeBonus := float64(len(causes)) * 0.1
	
	confidence := baseConfidence + causeBonus
	if confidence > 1.0 {
		confidence = 1.0
	}
	
	return confidence
}

func (e *CausalityPatternEngine) applyTemporalCausality(chains []*CausalChain, event *opinionated.OpinionatedEvent) []*CausalChain {
	// Filter chains based on temporal constraints
	filtered := make([]*CausalChain, 0)
	
	for _, chain := range chains {
		// Causes should occur before effects
		valid := true
		for _, causeID := range chain.Causes {
			// In real implementation, would check timestamps
			if causeID == event.ID {
				valid = false
				break
			}
		}
		
		if valid {
			filtered = append(filtered, chain)
		}
	}
	
	return filtered
}

// Helper methods for AnomalyPatternEngine

func (e *AnomalyPatternEngine) extractMetrics(event *opinionated.OpinionatedEvent) map[string]float64 {
	metrics := make(map[string]float64)
	
	// Extract all numeric values from event
	if event.Metrics != nil {
		for k, v := range event.Metrics {
			switch val := v.(type) {
			case float64:
				metrics[k] = val
			case int:
				metrics[k] = float64(val)
			case int64:
				metrics[k] = float64(val)
			}
		}
	}
	
	return metrics
}

func (e *AnomalyPatternEngine) updateStatistics(profile *AnomalyProfile, metric string, value float64) {
	// Update running statistics
	if _, exists := profile.Metrics["mean_"+metric]; !exists {
		profile.Metrics["mean_"+metric] = value
		profile.Metrics["variance_"+metric] = 0.0
		profile.Metrics["count_"+metric] = 1.0
	} else {
		count := profile.Metrics["count_"+metric]
		mean := profile.Metrics["mean_"+metric]
		variance := profile.Metrics["variance_"+metric]
		
		// Welford's online algorithm for variance
		count++
		delta := value - mean
		mean += delta / count
		delta2 := value - mean
		variance += delta * delta2
		
		profile.Metrics["mean_"+metric] = mean
		profile.Metrics["variance_"+metric] = variance
		profile.Metrics["count_"+metric] = count
		profile.Metrics["stddev_"+metric] = math.Sqrt(variance / count)
	}
}

func (e *AnomalyPatternEngine) isAnomaly(profile *AnomalyProfile, value float64) bool {
	// Z-score based anomaly detection
	for metric, stats := range profile.Metrics {
		if strings.HasPrefix(metric, "mean_") {
			metricName := strings.TrimPrefix(metric, "mean_")
			mean := stats
			stddev := profile.Metrics["stddev_"+metricName]
			
			if stddev > 0 {
				zScore := math.Abs(value-mean) / stddev
				if zScore > profile.Threshold {
					return true
				}
			}
		}
	}
	
	return false
}

func (e *AnomalyPatternEngine) calculateDeviation(profile *AnomalyProfile, value float64) float64 {
	maxDeviation := 0.0
	
	for metric, stats := range profile.Metrics {
		if strings.HasPrefix(metric, "mean_") {
			metricName := strings.TrimPrefix(metric, "mean_")
			mean := stats
			stddev := profile.Metrics["stddev_"+metricName]
			
			if stddev > 0 {
				deviation := math.Abs(value-mean) / stddev
				if deviation > maxDeviation {
					maxDeviation = deviation
				}
			}
		}
	}
	
	return maxDeviation
}

func (e *AnomalyPatternEngine) calculateSeverity(profile *AnomalyProfile, value float64) string {
	deviation := e.calculateDeviation(profile, value)
	
	switch {
	case deviation > 5.0:
		return "critical"
	case deviation > 4.0:
		return "high"
	case deviation > 3.0:
		return "medium"
	default:
		return "low"
	}
}

// Utility functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}