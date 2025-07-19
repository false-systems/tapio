package core

import (
	"time"
)

// Metric represents a metric to be exported
type Metric struct {
	Name       string            `json:"name"`
	Value      float64           `json:"value"`
	Labels     map[string]string `json:"labels"`
	Timestamp  time.Time         `json:"timestamp"`
	MetricType MetricType        `json:"type"`
}

// MetricType represents the type of metric
type MetricType string

const (
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeCounter   MetricType = "counter"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeSummary   MetricType = "summary"
)

// Trace represents a distributed trace
type Trace struct {
	TraceID    string                 `json:"trace_id"`
	SpanID     string                 `json:"span_id"`
	ParentID   string                 `json:"parent_id,omitempty"`
	Name       string                 `json:"name"`
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time"`
	Attributes map[string]interface{} `json:"attributes"`
	Events     []TraceEvent           `json:"events,omitempty"`
	Status     TraceStatus            `json:"status"`
}

// TraceEvent represents an event within a trace span
type TraceEvent struct {
	Name       string                 `json:"name"`
	Timestamp  time.Time              `json:"timestamp"`
	Attributes map[string]interface{} `json:"attributes"`
}

// TraceStatus represents the status of a trace span
type TraceStatus struct {
	Code    StatusCode `json:"code"`
	Message string     `json:"message,omitempty"`
}

// StatusCode represents trace status codes
type StatusCode int

const (
	StatusCodeUnset StatusCode = iota
	StatusCodeOK
	StatusCodeError
)

// Webhook represents a webhook payload
type Webhook struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    interface{}       `json:"body"`
	Timeout time.Duration     `json:"timeout"`
}

// IntegrationError represents an integration-specific error
type IntegrationError struct {
	Integration string
	Operation   string
	Err         error
}

func (e IntegrationError) Error() string {
	return e.Integration + " integration failed during " + e.Operation + ": " + e.Err.Error()
}
