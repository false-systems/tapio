package universal

import (
	"sync"
	"time"
)

// DataQuality represents the quality and reliability of data
type DataQuality struct {
	Confidence float64                `json:"confidence"` // 0.0 to 1.0
	Source     string                 `json:"source"`
	Version    string                 `json:"version"`
	Tags       map[string]string      `json:"tags,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// UniversalMetric represents a metric in the universal format
type UniversalMetric struct {
	// Core fields
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Target    Target    `json:"target"`

	// Metric data
	Name   string            `json:"name"`
	Value  float64           `json:"value"`
	Unit   string            `json:"unit"`
	Type   MetricType        `json:"type"`
	Labels map[string]string `json:"labels,omitempty"`

	// Quality and metadata
	Quality DataQuality `json:"quality"`

	// Resilience fields
	FallbackUsed bool   `json:"fallback_used,omitempty"`
	ErrorContext string `json:"error_context,omitempty"`
}

// MetricType represents the type of metric
type MetricType string

const (
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeCounter   MetricType = "counter"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeSummary   MetricType = "summary"
)

// Target identifies what the metric/event is about
type Target struct {
	Type      TargetType `json:"type"`
	Name      string     `json:"name"`
	Namespace string     `json:"namespace,omitempty"`
	PID       int32      `json:"pid,omitempty"`
	Container string     `json:"container,omitempty"`
	Pod       string     `json:"pod,omitempty"`
	Node      string     `json:"node,omitempty"`
	Cluster   string     `json:"cluster,omitempty"`
}

// TargetType represents the type of target
type TargetType string

const (
	TargetTypeProcess   TargetType = "process"
	TargetTypeContainer TargetType = "container"
	TargetTypePod       TargetType = "pod"
	TargetTypeNode      TargetType = "node"
	TargetTypeService   TargetType = "service"
	TargetTypeCluster   TargetType = "cluster"
)

// UniversalEvent represents an event in the universal format
type UniversalEvent struct {
	// Core fields
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Target    Target    `json:"target"`

	// Event data
	Type    EventType              `json:"type"`
	Level   EventLevel             `json:"level"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`

	// Quality and metadata
	Quality DataQuality `json:"quality"`

	// Correlation
	CorrelationID string   `json:"correlation_id,omitempty"`
	CausedBy      []string `json:"caused_by,omitempty"`
}

// EventType represents the type of event
type EventType string

const (
	EventTypeOOM            EventType = "oom"
	EventTypeOOMKill        EventType = "oom_kill"
	EventTypeMemoryPressure EventType = "memory_pressure"
	EventTypeCPUThrottle    EventType = "cpu_throttle"
	EventTypeDiskPressure   EventType = "disk_pressure"
	EventTypeNetworkError   EventType = "network_error"
	EventTypeRestart        EventType = "restart"
	EventTypeCrash          EventType = "crash"
	EventTypeCustom         EventType = "custom"
)

// EventLevel represents the severity level of an event
type EventLevel string

const (
	EventLevelDebug    EventLevel = "debug"
	EventLevelInfo     EventLevel = "info"
	EventLevelWarning  EventLevel = "warning"
	EventLevelError    EventLevel = "error"
	EventLevelCritical EventLevel = "critical"
)

// UniversalPrediction represents a prediction in the universal format
type UniversalPrediction struct {
	// Core fields
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Target    Target    `json:"target"`

	// Prediction data
	Type        PredictionType `json:"type"`
	TimeToEvent time.Duration  `json:"time_to_event"`
	Probability float64        `json:"probability"` // 0.0 to 1.0
	Impact      ImpactLevel    `json:"impact"`
	Description string         `json:"description"`

	// Evidence and reasoning
	Evidence []Evidence `json:"evidence"`
	Factors  []string   `json:"factors"`

	// Quality and metadata
	Quality DataQuality `json:"quality"`

	// Mitigation
	Mitigations []Mitigation `json:"mitigations,omitempty"`
}

// PredictionType represents the type of prediction
type PredictionType string

const (
	PredictionTypeOOM         PredictionType = "oom"
	PredictionTypeCrash       PredictionType = "crash"
	PredictionTypePerformance PredictionType = "performance"
	PredictionTypeDiskFull    PredictionType = "disk_full"
	PredictionTypeCustom      PredictionType = "custom"
)

// ImpactLevel represents the impact level of a predicted event
type ImpactLevel string

const (
	ImpactLevelLow      ImpactLevel = "low"
	ImpactLevelMedium   ImpactLevel = "medium"
	ImpactLevelHigh     ImpactLevel = "high"
	ImpactLevelCritical ImpactLevel = "critical"
)

// Evidence represents evidence supporting a prediction
type Evidence struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Data        map[string]interface{} `json:"data,omitempty"`
	Confidence  float64                `json:"confidence"`
	Source      string                 `json:"source"`
}

// Mitigation represents a suggested mitigation action
type Mitigation struct {
	Action      string   `json:"action"`
	Description string   `json:"description"`
	Commands    []string `json:"commands,omitempty"`
	Urgency     string   `json:"urgency"`
	Risk        string   `json:"risk"`
}

// UniversalDataset represents a collection of universal data
type UniversalDataset struct {
	// Identification
	ID        string    `json:"id"`
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`

	// Data collections
	Metrics     []UniversalMetric     `json:"metrics,omitempty"`
	Events      []UniversalEvent      `json:"events,omitempty"`
	Predictions []UniversalPrediction `json:"predictions,omitempty"`

	// Metadata
	Source      string                 `json:"source"`
	Duration    time.Duration          `json:"duration"`
	SampleCount int                    `json:"sample_count"`
	Tags        map[string]string      `json:"tags,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`

	// Quality
	OverallQuality DataQuality `json:"overall_quality"`
}

// Object pools for zero-allocation
var (
	metricPool = sync.Pool{
		New: func() interface{} {
			return &UniversalMetric{
				Labels: make(map[string]string),
				Quality: DataQuality{
					Tags:     make(map[string]string),
					Metadata: make(map[string]interface{}),
				},
			}
		},
	}

	eventPool = sync.Pool{
		New: func() interface{} {
			return &UniversalEvent{
				Details: make(map[string]interface{}),
				Quality: DataQuality{
					Tags:     make(map[string]string),
					Metadata: make(map[string]interface{}),
				},
			}
		},
	}

	predictionPool = sync.Pool{
		New: func() interface{} {
			return &UniversalPrediction{
				Evidence:    make([]Evidence, 0, 5),
				Factors:     make([]string, 0, 10),
				Mitigations: make([]Mitigation, 0, 3),
				Quality: DataQuality{
					Tags:     make(map[string]string),
					Metadata: make(map[string]interface{}),
				},
			}
		},
	}
)

// GetMetric retrieves a metric from the pool
func GetMetric() *UniversalMetric {
	m := metricPool.Get().(*UniversalMetric)
	m.reset()
	return m
}

// PutMetric returns a metric to the pool
func PutMetric(m *UniversalMetric) {
	if m != nil {
		metricPool.Put(m)
	}
}

// GetEvent retrieves an event from the pool
func GetEvent() *UniversalEvent {
	e := eventPool.Get().(*UniversalEvent)
	e.reset()
	return e
}

// PutEvent returns an event to the pool
func PutEvent(e *UniversalEvent) {
	if e != nil {
		eventPool.Put(e)
	}
}

// GetPrediction retrieves a prediction from the pool
func GetPrediction() *UniversalPrediction {
	p := predictionPool.Get().(*UniversalPrediction)
	p.reset()
	return p
}

// PutPrediction returns a prediction to the pool
func PutPrediction(p *UniversalPrediction) {
	if p != nil {
		predictionPool.Put(p)
	}
}

// reset clears the metric for reuse
func (m *UniversalMetric) reset() {
	m.ID = ""
	m.Timestamp = time.Time{}
	m.Target = Target{}
	m.Name = ""
	m.Value = 0
	m.Unit = ""
	m.Type = ""

	// Clear maps
	for k := range m.Labels {
		delete(m.Labels, k)
	}
	for k := range m.Quality.Tags {
		delete(m.Quality.Tags, k)
	}
	for k := range m.Quality.Metadata {
		delete(m.Quality.Metadata, k)
	}

	m.Quality.Confidence = 0
	m.Quality.Source = ""
	m.Quality.Version = ""
	m.FallbackUsed = false
	m.ErrorContext = ""
}

// reset clears the event for reuse
func (e *UniversalEvent) reset() {
	e.ID = ""
	e.Timestamp = time.Time{}
	e.Target = Target{}
	e.Type = ""
	e.Level = ""
	e.Message = ""

	// Clear maps
	for k := range e.Details {
		delete(e.Details, k)
	}
	for k := range e.Quality.Tags {
		delete(e.Quality.Tags, k)
	}
	for k := range e.Quality.Metadata {
		delete(e.Quality.Metadata, k)
	}

	e.Quality.Confidence = 0
	e.Quality.Source = ""
	e.Quality.Version = ""
	e.CorrelationID = ""
	e.CausedBy = e.CausedBy[:0]
}

// reset clears the prediction for reuse
func (p *UniversalPrediction) reset() {
	p.ID = ""
	p.Timestamp = time.Time{}
	p.Target = Target{}
	p.Type = ""
	p.TimeToEvent = 0
	p.Probability = 0
	p.Impact = ""
	p.Description = ""

	// Clear slices
	p.Evidence = p.Evidence[:0]
	p.Factors = p.Factors[:0]
	p.Mitigations = p.Mitigations[:0]

	// Clear maps
	for k := range p.Quality.Tags {
		delete(p.Quality.Tags, k)
	}
	for k := range p.Quality.Metadata {
		delete(p.Quality.Metadata, k)
	}

	p.Quality.Confidence = 0
	p.Quality.Source = ""
	p.Quality.Version = ""
}

// Clone creates a deep copy of the metric
func (m *UniversalMetric) Clone() *UniversalMetric {
	clone := GetMetric()
	clone.ID = m.ID
	clone.Timestamp = m.Timestamp
	clone.Target = m.Target
	clone.Name = m.Name
	clone.Value = m.Value
	clone.Unit = m.Unit
	clone.Type = m.Type

	// Copy maps
	for k, v := range m.Labels {
		clone.Labels[k] = v
	}
	for k, v := range m.Quality.Tags {
		clone.Quality.Tags[k] = v
	}
	for k, v := range m.Quality.Metadata {
		clone.Quality.Metadata[k] = v
	}

	clone.Quality.Confidence = m.Quality.Confidence
	clone.Quality.Source = m.Quality.Source
	clone.Quality.Version = m.Quality.Version
	clone.FallbackUsed = m.FallbackUsed
	clone.ErrorContext = m.ErrorContext

	return clone
}
