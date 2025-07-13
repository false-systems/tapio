package foundation

import (
	"fmt"
	"time"
)

// Foundation types - The bedrock of all correlation functionality
// This package has ZERO dependencies on other correlation packages
// All other packages depend on this, creating a clean dependency tree

// ============================================================================
// CORE DATA TYPES
// ============================================================================

// Event represents a normalized event from any source
// This is the fundamental unit of correlation analysis
type Event struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Source      SourceType             `json:"source"`
	Type        string                 `json:"type"`
	Entity      Entity                 `json:"entity"`
	Attributes  map[string]interface{} `json:"attributes"`
	Fingerprint string                 `json:"fingerprint"`
	Labels      map[string]string      `json:"labels,omitempty"`
}

// Entity represents the target of an event (pod, node, service, etc.)
type Entity struct {
	Type      string            `json:"type"`      // "pod", "node", "service", etc.
	Name      string            `json:"name"`      // Resource name
	Namespace string            `json:"namespace,omitempty"`
	Node      string            `json:"node,omitempty"`
	Pod       string            `json:"pod,omitempty"`
	Container string            `json:"container,omitempty"`
	Process   string            `json:"process,omitempty"`
	UID       string            `json:"uid"`       // Unique identifier
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// String returns human-readable entity representation
func (e Entity) String() string {
	if e.Namespace != "" {
		return fmt.Sprintf("%s/%s", e.Namespace, e.Name)
	}
	return e.Name
}

// TimeWindow represents a time range for correlation analysis
type TimeWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Duration returns the duration of the time window
func (tw TimeWindow) Duration() time.Duration {
	return tw.End.Sub(tw.Start)
}

// Contains checks if a timestamp falls within the window
func (tw TimeWindow) Contains(t time.Time) bool {
	return !t.Before(tw.Start) && !t.After(tw.End)
}

// ============================================================================
// SEVERITY AND CATEGORIZATION
// ============================================================================

// Severity represents the severity level of findings
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityError
	SeverityCritical
)

// String returns string representation of severity
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Category represents the category of correlation rules or findings
type Category string

const (
	CategoryPerformance Category = "performance"
	CategorySecurity    Category = "security"
	CategoryReliability Category = "reliability"
	CategoryCost        Category = "cost"
	CategoryCapacity    Category = "capacity"
	CategoryNetwork     Category = "network"
)

// SourceType identifies the source system that generated an event
type SourceType string

const (
	SourceEBPF       SourceType = "ebpf"
	SourceKubernetes SourceType = "kubernetes"
	SourceSystemd    SourceType = "systemd"
	SourceJournald   SourceType = "journald"
	SourceMetrics    SourceType = "metrics"
	SourcePrometheus SourceType = "prometheus"
	SourceOTEL       SourceType = "otel"
)

// ============================================================================
// CONFIDENCE AND EVIDENCE
// ============================================================================

// ConfidenceLevel represents confidence in correlation results
type ConfidenceLevel int

const (
	ConfidenceLow ConfidenceLevel = iota
	ConfidenceMedium
	ConfidenceHigh
	ConfidenceVeryHigh
)

// String returns string representation of confidence level
func (c ConfidenceLevel) String() string {
	switch c {
	case ConfidenceLow:
		return "low"
	case ConfidenceMedium:
		return "medium"
	case ConfidenceHigh:
		return "high"
	case ConfidenceVeryHigh:
		return "very_high"
	default:
		return "unknown"
	}
}

// Evidence represents supporting evidence for correlations
type Evidence struct {
	Type        string                 `json:"type"`
	Source      SourceType             `json:"source"`
	Description string                 `json:"description"`
	Data        map[string]interface{} `json:"data"`
	Timestamp   time.Time              `json:"timestamp"`
	Confidence  float64                `json:"confidence"` // 0.0 to 1.0
}

// ============================================================================
// METRICS AND TIME SERIES
// ============================================================================

// MetricPoint represents a single metric data point
type MetricPoint struct {
	Timestamp time.Time         `json:"timestamp"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// MetricSeries represents a time series of metric data
type MetricSeries struct {
	Name     string        `json:"name"`
	Points   []MetricPoint `json:"points"`
	Unit     string        `json:"unit,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
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
	if len(ms.Points) == 1 {
		return mean, 0
	}

	variance := 0.0
	for _, point := range ms.Points {
		diff := point.Value - mean
		variance += diff * diff
	}
	variance /= float64(len(ms.Points) - 1)
	stddev = variance // sqrt would need math import, keep it simple

	return mean, stddev
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

// ============================================================================
// FILTERING AND QUERYING
// ============================================================================

// Filter defines criteria for selecting events
type Filter struct {
	Source       SourceType        `json:"source,omitempty"`
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

// ============================================================================
// PREDICTION AND FORECASTING
// ============================================================================

// Prediction represents a time-based prediction about future events
type Prediction struct {
	Event       string        `json:"event"`        // What will happen
	Description string        `json:"description"`  // Human description
	TimeToEvent time.Duration `json:"time_to_event"` // When it will happen
	Probability float64       `json:"probability"`  // 0.0 to 1.0
	Confidence  float64       `json:"confidence"`   // 0.0 to 1.0
	Factors     []string      `json:"factors"`      // Contributing factors
	Mitigation  []string      `json:"mitigation"`   // Suggested actions
	UpdatedAt   time.Time     `json:"updated_at"`
}

// ============================================================================
// RESOURCE REFERENCES
// ============================================================================

// ResourceReference represents a reference to a Kubernetes resource
type ResourceReference struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	UID       string `json:"uid,omitempty"`
}

// ResourceInfo represents resource information with Type field for compatibility
type ResourceInfo struct {
	Type      string            `json:"type"` // Resource type (e.g., "pod", "service")
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

// NewTimeWindow creates a time window from start and duration
func NewTimeWindow(start time.Time, duration time.Duration) TimeWindow {
	return TimeWindow{
		Start: start,
		End:   start.Add(duration),
	}
}

// NewTimeWindowFromRange creates a time window from start and end times
func NewTimeWindowFromRange(start, end time.Time) TimeWindow {
	return TimeWindow{
		Start: start,
		End:   end,
	}
}

// EventsByTimestamp sorts events by timestamp (for external use)
type EventsByTimestamp []Event

func (e EventsByTimestamp) Len() int           { return len(e) }
func (e EventsByTimestamp) Swap(i, j int)      { e[i], e[j] = e[j], e[i] }
func (e EventsByTimestamp) Less(i, j int) bool { return e[i].Timestamp.Before(e[j].Timestamp) }