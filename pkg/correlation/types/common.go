package types

import (
	"time"
)

// Event represents a generic event for correlation
type Event struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Source      string                 `json:"source"`
	Type        string                 `json:"type"`
	Severity    Severity               `json:"severity"`
	Data        map[string]interface{} `json:"data,omitempty"`
	Entity      Entity                 `json:"entity"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	Fingerprint string                 `json:"fingerprint,omitempty"`
	Labels      map[string]string      `json:"labels,omitempty"`
}

// Severity levels for events
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// Category represents finding categories
type Category string

const (
	CategoryPerformance Category = "performance"
	CategorySecurity    Category = "security"
	CategoryReliability Category = "reliability"
	CategoryResource    Category = "resource"
	CategoryNetwork     Category = "network"
	CategoryCost        Category = "cost"
	CategoryCapacity    Category = "capacity"
)

// Entity represents an entity
type Entity struct {
	ID        string            `json:"id,omitempty"`
	Type      string            `json:"type"` // "pod", "node", "service", etc.
	Name      string            `json:"name"` // Resource name
	Namespace string            `json:"namespace,omitempty"`
	Node      string            `json:"node,omitempty"`
	Pod       string            `json:"pod,omitempty"`
	Container string            `json:"container,omitempty"`
	Process   string            `json:"process,omitempty"`
	UID       string            `json:"uid,omitempty"` // Unique identifier
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// MetricStatistics provides statistical analysis of a metric series
type MetricStatistics struct {
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
	Mean      float64 `json:"mean"`
	StdDev    float64 `json:"std_dev"`
	P50       float64 `json:"p50"`
	P95       float64 `json:"p95"`
	P99       float64 `json:"p99"`
	Count     int     `json:"count"`
	Trend     string  `json:"trend"`
	Anomalies int     `json:"anomalies"`
}

// MetricSeries represents a metric series
type MetricSeries struct {
	Name       string            `json:"name"`
	Values     []float64         `json:"values"`
	Times      []time.Time       `json:"times"`
	Labels     map[string]string `json:"labels,omitempty"`
	Unit       string            `json:"unit,omitempty"`
	Points     []MetricPoint     `json:"points,omitempty"`
	Aggregated bool              `json:"aggregated,omitempty"`
	Statistics *MetricStatistics `json:"statistics,omitempty"`
}

// MetricPoint represents a single metric data point
type MetricPoint struct {
	Timestamp time.Time         `json:"timestamp"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
}