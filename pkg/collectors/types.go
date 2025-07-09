package collectors

import (
	"time"
)

// Target represents a monitoring target
type Target struct {
	// Type indicates the target type (pod, container, process, etc.)
	Type string `json:"type"`

	// Name is the target identifier
	Name string `json:"name"`

	// Namespace for Kubernetes targets
	Namespace string `json:"namespace,omitempty"`

	// Labels for additional metadata
	Labels map[string]string `json:"labels,omitempty"`

	// Platform-specific identifiers
	PID        int    `json:"pid,omitempty"`
	CgroupPath string `json:"cgroup_path,omitempty"`
}

// DataSet represents collected monitoring data
type DataSet struct {
	// Timestamp when the data was collected
	Timestamp time.Time `json:"timestamp"`

	// Source that collected the data
	Source string `json:"source"`

	// Metrics contains the collected metrics
	Metrics []Metric `json:"metrics"`

	// Events contains system events
	Events []Event `json:"events"`

	// Errors contains any collection errors
	Errors []error `json:"errors,omitempty"`
}

// Metric represents a single metric measurement
type Metric struct {
	// Name of the metric
	Name string `json:"name"`

	// Value of the metric
	Value float64 `json:"value"`

	// Unit of measurement
	Unit string `json:"unit"`

	// Target that this metric relates to
	Target Target `json:"target"`

	// Timestamp when the metric was collected
	Timestamp time.Time `json:"timestamp"`

	// Labels for additional metadata
	Labels map[string]string `json:"labels,omitempty"`
}

// Event represents a system event
type Event struct {
	// Type of event (oom, crash, network, etc.)
	Type string `json:"type"`

	// Message describing the event
	Message string `json:"message"`

	// Target that triggered the event
	Target Target `json:"target"`

	// Timestamp when the event occurred
	Timestamp time.Time `json:"timestamp"`

	// Severity level
	Severity string `json:"severity"`

	// Additional event data
	Data map[string]interface{} `json:"data,omitempty"`
}
