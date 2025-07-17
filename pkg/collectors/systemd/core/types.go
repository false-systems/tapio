package core

import (
	"fmt"
	"time"
)

// CollectorError represents a systemd collector error
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
	ErrorTypeConnection     ErrorType = "connection"
	ErrorTypeDBus           ErrorType = "dbus"
	ErrorTypePermission     ErrorType = "permission"
	ErrorTypeWatch          ErrorType = "watch"
	ErrorTypeProcess        ErrorType = "process"
	ErrorTypeSystemd        ErrorType = "systemd"
	ErrorTypeUnsupported    ErrorType = "unsupported"
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

// ServiceFilter defines criteria for filtering services
type ServiceFilter struct {
	// Name patterns
	NamePatterns      []string `json:"name_patterns"`
	ExcludePatterns   []string `json:"exclude_patterns"`
	
	// State filters
	States            []string `json:"states"`            // active, inactive, failed, etc.
	SubStates         []string `json:"sub_states"`        // running, exited, dead, etc.
	LoadStates        []string `json:"load_states"`       // loaded, not-found, etc.
	
	// Unit type filters
	UnitTypes         []string `json:"unit_types"`        // service, socket, timer, etc.
	
	// Property filters
	HasProperties     map[string]string `json:"has_properties"`
	
	// Result filters (for failures)
	Results           []string `json:"results"`           // success, exit-code, signal, etc.
}

// ServiceMetrics tracks metrics for a specific service
type ServiceMetrics struct {
	ServiceName       string    `json:"service_name"`
	TotalStarts       uint64    `json:"total_starts"`
	TotalStops        uint64    `json:"total_stops"`
	TotalFailures     uint64    `json:"total_failures"`
	TotalRestarts     uint64    `json:"total_restarts"`
	LastStartTime     time.Time `json:"last_start_time"`
	LastStopTime      time.Time `json:"last_stop_time"`
	LastFailureTime   time.Time `json:"last_failure_time"`
	CurrentState      string    `json:"current_state"`
	CurrentSubState   string    `json:"current_sub_state"`
	MainPID           int32     `json:"main_pid"`
	MemoryCurrent     uint64    `json:"memory_current"`
	CPUUsageNSec      uint64    `json:"cpu_usage_nsec"`
}

// DBusSignal represents a D-Bus signal from systemd
type DBusSignal struct {
	Path      string        `json:"path"`
	Interface string        `json:"interface"`
	Member    string        `json:"member"`
	Body      []interface{} `json:"body"`
}

// JobInfo represents systemd job information
type JobInfo struct {
	ID       uint32 `json:"id"`
	Unit     string `json:"unit"`
	JobType  string `json:"job_type"`  // start, stop, restart, reload, etc.
	State    string `json:"state"`     // waiting, running, done, failed, etc.
}

// SystemdProperty represents a systemd unit property
type SystemdProperty struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
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

// ConnectionState represents the D-Bus connection state
type ConnectionState struct {
	Connected       bool      `json:"connected"`
	LastConnected   time.Time `json:"last_connected"`
	LastError       error     `json:"last_error,omitempty"`
	ReconnectCount  uint64    `json:"reconnect_count"`
	SystemdVersion  string    `json:"systemd_version"`
}

// Common systemd states
const (
	// Active states
	StateActive   = "active"
	StateInactive = "inactive"
	StateFailed   = "failed"
	StateActivating = "activating"
	StateDeactivating = "deactivating"
	
	// Sub states
	SubStateRunning = "running"
	SubStateExited  = "exited"
	SubStateDead    = "dead"
	SubStateFailed  = "failed"
	SubStateWaiting = "waiting"
	
	// Load states
	LoadStateLoaded   = "loaded"
	LoadStateNotFound = "not-found"
	LoadStateMasked   = "masked"
	LoadStateError    = "error"
	
	// Job states
	JobStateWaiting = "waiting"
	JobStateRunning = "running"
	JobStateDone    = "done"
	JobStateFailed  = "failed"
)