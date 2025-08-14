package domain

import (
	"fmt"
	"time"
)

// ObservationEvent represents a parsed, structured event for correlation analysis.
// This is the canonical format used by the correlation engine.
// Fields are pointers to allow nil/empty values based on event source.
type ObservationEvent struct {
	// CORE IDENTITY (never nil)
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"` // "kernel", "kubeapi", "dns"
	Type      string    `json:"type"`   // "syscall", "pod_created", "dns_query"

	// CORRELATION KEYS (at least one required)
	PID         *int32  `json:"pid,omitempty"`
	ContainerID *string `json:"container_id,omitempty"`
	PodName     *string `json:"pod_name,omitempty"`
	Namespace   *string `json:"namespace,omitempty"`
	ServiceName *string `json:"service_name,omitempty"`
	NodeName    *string `json:"node_name,omitempty"`

	// EVENT DATA
	Action *string `json:"action,omitempty"`
	Target *string `json:"target,omitempty"`
	Result *string `json:"result,omitempty"`
	Reason *string `json:"reason,omitempty"`

	// METRICS
	Duration *int64 `json:"duration_ms,omitempty"` // ms
	Size     *int64 `json:"size_bytes,omitempty"`  // bytes
	Count    *int32 `json:"count,omitempty"`

	// SIMPLE DATA - NO interface{}
	Data map[string]string `json:"data,omitempty"`

	// RELATIONSHIPS
	CausedBy *string `json:"caused_by,omitempty"`
	ParentID *string `json:"parent_id,omitempty"`
}

// HasCorrelationKey returns true if at least one correlation key is present
func (o *ObservationEvent) HasCorrelationKey() bool {
	return o.PID != nil ||
		o.ContainerID != nil ||
		o.PodName != nil ||
		o.Namespace != nil ||
		o.ServiceName != nil ||
		o.NodeName != nil
}

// GetCorrelationKeys returns all non-nil correlation keys as a map
func (o *ObservationEvent) GetCorrelationKeys() map[string]string {
	keys := make(map[string]string)

	if o.PID != nil {
		keys["pid"] = fmt.Sprintf("%d", *o.PID)
	}
	if o.ContainerID != nil {
		keys["container_id"] = *o.ContainerID
	}
	if o.PodName != nil {
		keys["pod_name"] = *o.PodName
	}
	if o.Namespace != nil {
		keys["namespace"] = *o.Namespace
	}
	if o.ServiceName != nil {
		keys["service_name"] = *o.ServiceName
	}
	if o.NodeName != nil {
		keys["node_name"] = *o.NodeName
	}

	return keys
}

// Validate ensures the observation event meets basic requirements
func (o *ObservationEvent) Validate() error {
	if o.ID == "" {
		return NewValidationError("ID", o.ID, "cannot be empty")
	}
	if o.Timestamp.IsZero() {
		return NewValidationError("Timestamp", o.Timestamp, "cannot be zero")
	}
	if o.Source == "" {
		return NewValidationError("Source", o.Source, "cannot be empty")
	}
	if o.Type == "" {
		return NewValidationError("Type", o.Type, "cannot be empty")
	}
	if !o.HasCorrelationKey() {
		return NewValidationError("CorrelationKeys", nil, "at least one correlation key required")
	}

	return nil
}

// ValidationError represents a validation error with context
type ValidationError struct {
	Field   string
	Value   interface{}
	Rule    string
	wrapped error
}

// NewValidationError creates a new validation error
func NewValidationError(field string, value interface{}, rule string) *ValidationError {
	return &ValidationError{
		Field: field,
		Value: value,
		Rule:  rule,
	}
}

func (e *ValidationError) Error() string {
	return "validation failed for field " + e.Field + ": " + e.Rule
}

func (e *ValidationError) Unwrap() error {
	return e.wrapped
}
