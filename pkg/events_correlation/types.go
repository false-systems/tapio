package events_correlation

import (
	"fmt"
	"time"
)

// Severity levels for correlation results
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Category defines the type of correlation rule
type Category string

const (
	CategoryResource    Category = "resource"
	CategoryNetwork     Category = "network"
	CategoryPerformance Category = "performance"
	CategorySecurity    Category = "security"
	CategoryReliability Category = "reliability"
)

// EventSource identifies the source of an event
type EventSource string

const (
	SourceEBPF       EventSource = "ebpf"
	SourceKubernetes EventSource = "kubernetes"
	SourceSystemd    EventSource = "systemd"
	SourceJournald   EventSource = "journald"
	SourceMetrics    EventSource = "metrics"
)

// Event represents a normalized event from any source
type Event struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Source      EventSource            `json:"source"`
	Type        string                 `json:"type"`
	Entity      Entity                 `json:"entity"`
	Attributes  map[string]interface{} `json:"attributes"`
	Fingerprint string                 `json:"fingerprint"`
	Labels      map[string]string      `json:"labels,omitempty"`
}

// Entity represents the entity associated with an event
type Entity struct {
	Type      string            `json:"type"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Node      string            `json:"node,omitempty"`
	Pod       string            `json:"pod,omitempty"`
	Container string            `json:"container,omitempty"`
	Process   string            `json:"process,omitempty"`
	UID       string            `json:"uid"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// String returns a human-readable representation of the entity
func (e Entity) String() string {
	if e.Namespace != "" {
		return fmt.Sprintf("%s/%s", e.Namespace, e.Name)
	}
	return e.Name
}

// TimeWindow represents a time range for correlation
type TimeWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Duration returns the duration of the time window
func (tw TimeWindow) Duration() time.Duration {
	return tw.End.Sub(tw.Start)
}

// Contains checks if a timestamp is within the window
func (tw TimeWindow) Contains(t time.Time) bool {
	return !t.Before(tw.Start) && !t.After(tw.End)
}

// Filter defines criteria for selecting events
type Filter struct {
	Source       EventSource       `json:"source,omitempty"`
	Type         string            `json:"type,omitempty"`
	EntityType   string            `json:"entity_type,omitempty"`
	EntityName   string            `json:"entity_name,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	Node         string            `json:"node,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Since        time.Time         `json:"since,omitempty"`
	Until        time.Time         `json:"until,omitempty"`
	Limit        int               `json:"limit,omitempty"`
	AttributeHas string            `json:"attribute_has,omitempty"`
	AttributeEq  map[string]string `json:"attribute_eq,omitempty"`
}

// Matches checks if an event matches the filter criteria
func (f Filter) Matches(event Event) bool {
	if f.Source != "" && event.Source != f.Source {
		return false
	}

	if f.Type != "" && event.Type != f.Type {
		return false
	}

	if f.EntityType != "" && event.Entity.Type != f.EntityType {
		return false
	}

	if f.EntityName != "" && event.Entity.Name != f.EntityName {
		return false
	}

	if f.Namespace != "" && event.Entity.Namespace != f.Namespace {
		return false
	}

	if f.Node != "" && event.Entity.Node != f.Node {
		return false
	}

	if !f.Since.IsZero() && event.Timestamp.Before(f.Since) {
		return false
	}

	if !f.Until.IsZero() && event.Timestamp.After(f.Until) {
		return false
	}

	// Check labels
	for key, value := range f.Labels {
		if eventValue, exists := event.Labels[key]; !exists || eventValue != value {
			return false
		}
	}

	// Check attributes
	if f.AttributeHas != "" {
		if _, exists := event.Attributes[f.AttributeHas]; !exists {
			return false
		}
	}

	for key, value := range f.AttributeEq {
		if eventValue, exists := event.Attributes[key]; !exists || fmt.Sprintf("%v", eventValue) != value {
			return false
		}
	}

	return true
}

// Result represents the output of a correlation rule
type Result struct {
	RuleID          string            `json:"rule_id"`
	RuleName        string            `json:"rule_name"`
	Timestamp       time.Time         `json:"timestamp"`
	Confidence      float64           `json:"confidence"`
	Severity        Severity          `json:"severity"`
	Category        Category          `json:"category"`
	Title           string            `json:"title"`
	Description     string            `json:"description"`
	Impact          string            `json:"impact,omitempty"`
	Evidence        Evidence          `json:"evidence"`
	Recommendations []string          `json:"recommendations,omitempty"`
	Actions         []Action          `json:"actions,omitempty"`
	TTL             time.Duration     `json:"ttl,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// Evidence contains the supporting data for a correlation result
type Evidence struct {
	Events   []Event            `json:"events"`
	Metrics  map[string]float64 `json:"metrics,omitempty"`
	Patterns []string           `json:"patterns,omitempty"`
	Timeline []TimelineEntry    `json:"timeline,omitempty"`
	Entities []Entity           `json:"entities,omitempty"`
}

// TimelineEntry represents a significant event in the correlation timeline
type TimelineEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	Description string    `json:"description"`
	EventID     string    `json:"event_id,omitempty"`
	Source      string    `json:"source"`
}

// Action represents an automated response to a correlation result
type Action struct {
	Type       string            `json:"type"`
	Target     string            `json:"target"`
	Priority   string            `json:"priority,omitempty"`
	Parameters map[string]string `json:"parameters,omitempty"`
	Condition  string            `json:"condition,omitempty"`
	Delay      time.Duration     `json:"delay,omitempty"`
	TTL        time.Duration     `json:"ttl,omitempty"`
}

// MetricPoint represents a time-series data point
type MetricPoint struct {
	Timestamp time.Time         `json:"timestamp"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// MetricSeries represents a collection of metric points
type MetricSeries struct {
	Name   string        `json:"name"`
	Points []MetricPoint `json:"points"`
	Unit   string        `json:"unit,omitempty"`
}

// Statistics calculates basic statistics for the metric series
func (ms MetricSeries) Statistics() (mean, stddev float64) {
	if len(ms.Points) == 0 {
		return 0, 0
	}

	// Calculate mean
	sum := 0.0
	for _, point := range ms.Points {
		sum += point.Value
	}
	mean = sum / float64(len(ms.Points))

	// Calculate standard deviation
	variance := 0.0
	for _, point := range ms.Points {
		variance += (point.Value - mean) * (point.Value - mean)
	}
	variance /= float64(len(ms.Points))

	return mean, variance
}

// Last returns points from the last duration
func (ms MetricSeries) Last(duration time.Duration) []MetricPoint {
	if len(ms.Points) == 0 {
		return nil
	}

	cutoff := ms.Points[len(ms.Points)-1].Timestamp.Add(-duration)

	var result []MetricPoint
	for _, point := range ms.Points {
		if point.Timestamp.After(cutoff) {
			result = append(result, point)
		}
	}

	return result
}

// Stats contains runtime statistics for the correlation engine
type Stats struct {
	RulesRegistered   int                      `json:"rules_registered"`
	EventsProcessed   uint64                   `json:"events_processed"`
	CorrelationsFound uint64                   `json:"correlations_found"`
	ProcessingLatency time.Duration            `json:"processing_latency"`
	RuleExecutionTime map[string]time.Duration `json:"rule_execution_time"`
	LastProcessedAt   time.Time                `json:"last_processed_at"`
	MemoryUsage       uint64                   `json:"memory_usage"`
}
