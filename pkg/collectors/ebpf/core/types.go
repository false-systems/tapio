package core

import (
	"fmt"
	"time"
)

// CollectorError represents an eBPF collector error
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
	ErrorTypeLoad       ErrorType = "load"
	ErrorTypeAttach     ErrorType = "attach"
	ErrorTypeRead       ErrorType = "read"
	ErrorTypeProcess    ErrorType = "process"
	ErrorTypePermission ErrorType = "permission"
	ErrorTypeResource   ErrorType = "resource"
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

// EventFilter defines criteria for filtering events
type EventFilter struct {
	// Process filters
	PIDs       []uint32 `json:"pids,omitempty"`
	UIDs       []uint32 `json:"uids,omitempty"`
	GIDs       []uint32 `json:"gids,omitempty"`
	Comms      []string `json:"comms,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
	
	// Event type filters
	EventTypes []string `json:"event_types,omitempty"`
	Syscalls   []string `json:"syscalls,omitempty"`
	
	// Network filters
	SourceIPs []string `json:"source_ips,omitempty"`
	DestIPs   []string `json:"dest_ips,omitempty"`
	Ports     []uint16 `json:"ports,omitempty"`
	Protocols []string `json:"protocols,omitempty"`
	
	// File filters
	Paths        []string `json:"paths,omitempty"`
	PathPrefixes []string `json:"path_prefixes,omitempty"`
	
	// Rate limiting
	MaxEventsPerSecond int `json:"max_events_per_second,omitempty"`
	SampleRate         float64 `json:"sample_rate,omitempty"`
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