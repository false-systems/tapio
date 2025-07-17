package core

import (
	"fmt"
	"time"
)

// CollectorError represents a Kubernetes collector error
type CollectorError struct {
	Type      ErrorType
	Message   string
	Cause     error
	Timestamp time.Time
	Context   map[string]interface{}
}

// ErrorType categorizes collector errors
type ErrorType string

const (
	ErrorTypeConnection    ErrorType = "connection"
	ErrorTypeAuthentication ErrorType = "authentication"
	ErrorTypePermission    ErrorType = "permission"
	ErrorTypeWatch         ErrorType = "watch"
	ErrorTypeProcess       ErrorType = "process"
	ErrorTypeRateLimit     ErrorType = "rate_limit"
	ErrorTypeResource      ErrorType = "resource"
)

func (e CollectorError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s error: %s (caused by: %v)", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s error: %s", e.Type, e.Message)
}

func (e CollectorError) Unwrap() error {
	return e.Cause
}

// NewCollectorError creates a new collector error
func NewCollectorError(errType ErrorType, message string, cause error) CollectorError {
	return CollectorError{
		Type:      errType,
		Message:   message,
		Cause:     cause,
		Timestamp: time.Now(),
	}
}

// ResourceFilter defines criteria for filtering Kubernetes resources
type ResourceFilter struct {
	// Namespace filters
	Namespaces      []string          `json:"namespaces,omitempty"`
	ExcludeNamespaces []string        `json:"exclude_namespaces,omitempty"`
	
	// Label filters
	Labels          map[string]string `json:"labels,omitempty"`
	LabelSelector   string            `json:"label_selector,omitempty"`
	
	// Field filters
	FieldSelector   string            `json:"field_selector,omitempty"`
	
	// Name filters
	Names           []string          `json:"names,omitempty"`
	NamePrefix      string            `json:"name_prefix,omitempty"`
	NameSuffix      string            `json:"name_suffix,omitempty"`
	
	// Event filters
	EventTypes      []string          `json:"event_types,omitempty"`
	EventReasons    []string          `json:"event_reasons,omitempty"`
	
	// Rate limiting
	MaxEventsPerSecond int            `json:"max_events_per_second,omitempty"`
}

// WatchOptions configures resource watching behavior
type WatchOptions struct {
	// Resource version to start watching from
	ResourceVersion string
	
	// Whether to list existing resources first
	ListFirst bool
	
	// Timeout for watch operations
	WatchTimeout time.Duration
	
	// Retry configuration
	MaxRetries     int
	RetryBackoff   time.Duration
}

// MetricType represents different metric types
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
)

// Metric represents a collector metric
type Metric struct {
	Name      string                 `json:"name"`
	Type      MetricType             `json:"type"`
	Value     float64                `json:"value"`
	Labels    map[string]string      `json:"labels,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Unit      string                 `json:"unit,omitempty"`
	Help      string                 `json:"help,omitempty"`
}

// ResourceMetrics tracks metrics for a specific resource type
type ResourceMetrics struct {
	ResourceType    string    `json:"resource_type"`
	TotalCount      int       `json:"total_count"`
	EventsReceived  uint64    `json:"events_received"`
	LastEventTime   time.Time `json:"last_event_time"`
	ErrorCount      uint64    `json:"error_count"`
}

// ConnectionState represents the K8s API connection state
type ConnectionState struct {
	Connected       bool      `json:"connected"`
	LastConnected   time.Time `json:"last_connected"`
	LastError       error     `json:"last_error,omitempty"`
	ReconnectCount  uint64    `json:"reconnect_count"`
	APIVersion      string    `json:"api_version"`
}