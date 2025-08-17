package intelligence

import (
	"fmt"
	"strings"
	"time"
)

// EventContext provides strongly-typed context for pattern matching
// Replaces untyped maps context fields
type EventContext struct {
	// Event identification
	EventType string `json:"event_type"`
	Source    string `json:"source"`
	EventID   string `json:"event_id,omitempty"`

	// Kubernetes context
	PID         *uint32 `json:"pid,omitempty"`
	ContainerID *string `json:"container_id,omitempty"`
	PodName     *string `json:"pod_name,omitempty"`
	Namespace   *string `json:"namespace,omitempty"`
	ServiceName *string `json:"service_name,omitempty"`
	NodeName    *string `json:"node_name,omitempty"`

	// Action context
	Action *string `json:"action,omitempty"`
	Target *string `json:"target,omitempty"`
	Result *string `json:"result,omitempty"`
	Reason *string `json:"reason,omitempty"`

	// Metrics context
	DurationMS *uint64 `json:"duration_ms,omitempty"`
	SizeBytes  *uint64 `json:"size_bytes,omitempty"`
	Count      *uint64 `json:"count,omitempty"`

	// Correlation context
	CausedBy *string `json:"caused_by,omitempty"`
	ParentID *string `json:"parent_id,omitempty"`

	// Custom data - strongly typed common cases
	CustomData map[string]string `json:"custom_data,omitempty"`
}

// NewEventContextFromEvent creates EventContext from an observation event
// This replaces the extractContext function that returned untyped maps
func NewEventContextFromEvent(eventType, source, eventID string) *EventContext {
	return &EventContext{
		EventType:  eventType,
		Source:     source,
		EventID:    eventID,
		CustomData: make(map[string]string),
	}
}

// SetPID sets the process ID context
func (c *EventContext) SetPID(pid uint32) *EventContext {
	c.PID = &pid
	return c
}

// SetContainerID sets the container ID context
func (c *EventContext) SetContainerID(containerID string) *EventContext {
	c.ContainerID = &containerID
	return c
}

// SetPodName sets the pod name context
func (c *EventContext) SetPodName(podName string) *EventContext {
	c.PodName = &podName
	return c
}

// SetNamespace sets the namespace context
func (c *EventContext) SetNamespace(namespace string) *EventContext {
	c.Namespace = &namespace
	return c
}

// SetServiceName sets the service name context
func (c *EventContext) SetServiceName(serviceName string) *EventContext {
	c.ServiceName = &serviceName
	return c
}

// SetNodeName sets the node name context
func (c *EventContext) SetNodeName(nodeName string) *EventContext {
	c.NodeName = &nodeName
	return c
}

// SetAction sets the action context
func (c *EventContext) SetAction(action string) *EventContext {
	c.Action = &action
	return c
}

// SetTarget sets the target context
func (c *EventContext) SetTarget(target string) *EventContext {
	c.Target = &target
	return c
}

// SetResult sets the result context
func (c *EventContext) SetResult(result string) *EventContext {
	c.Result = &result
	return c
}

// SetReason sets the reason context
func (c *EventContext) SetReason(reason string) *EventContext {
	c.Reason = &reason
	return c
}

// SetDuration sets the duration context in milliseconds
func (c *EventContext) SetDuration(durationMS uint64) *EventContext {
	c.DurationMS = &durationMS
	return c
}

// SetSize sets the size context in bytes
func (c *EventContext) SetSize(sizeBytes uint64) *EventContext {
	c.SizeBytes = &sizeBytes
	return c
}

// SetCount sets the count context
func (c *EventContext) SetCount(count uint64) *EventContext {
	c.Count = &count
	return c
}

// SetCausedBy sets the causation context
func (c *EventContext) SetCausedBy(causedBy string) *EventContext {
	c.CausedBy = &causedBy
	return c
}

// SetParentID sets the parent ID context
func (c *EventContext) SetParentID(parentID string) *EventContext {
	c.ParentID = &parentID
	return c
}

// AddCustomData adds custom string data
func (c *EventContext) AddCustomData(key, value string) *EventContext {
	if c.CustomData == nil {
		c.CustomData = make(map[string]string)
	}
	c.CustomData[key] = value
	return c
}

// Interface implementation for domain.PredictionContext

// GetEventType returns the event type
func (c *EventContext) GetEventType() string {
	return c.EventType
}

// GetSource returns the event source
func (c *EventContext) GetSource() string {
	return c.Source
}

// GetEventID returns the event ID
func (c *EventContext) GetEventID() string {
	return c.EventID
}

// ToMap converts the context to a map for compatibility
func (c *EventContext) ToMap() map[string]string {
	result := map[string]string{
		"event_type": c.EventType,
		"source":     c.Source,
	}

	if c.EventID != "" {
		result["event_id"] = c.EventID
	}

	// Add optional fields if they exist
	if c.PID != nil {
		result["pid"] = fmt.Sprintf("%d", *c.PID)
	}
	if c.ContainerID != nil {
		result["container_id"] = *c.ContainerID
	}
	if c.PodName != nil {
		result["pod_name"] = *c.PodName
	}
	if c.Namespace != nil {
		result["namespace"] = *c.Namespace
	}
	if c.ServiceName != nil {
		result["service_name"] = *c.ServiceName
	}
	if c.NodeName != nil {
		result["node_name"] = *c.NodeName
	}
	if c.Action != nil {
		result["action"] = *c.Action
	}
	if c.Target != nil {
		result["target"] = *c.Target
	}
	if c.Result != nil {
		result["result"] = *c.Result
	}
	if c.Reason != nil {
		result["reason"] = *c.Reason
	}
	if c.DurationMS != nil {
		result["duration_ms"] = fmt.Sprintf("%d", *c.DurationMS)
	}
	if c.SizeBytes != nil {
		result["size_bytes"] = fmt.Sprintf("%d", *c.SizeBytes)
	}
	if c.Count != nil {
		result["count"] = fmt.Sprintf("%d", *c.Count)
	}
	if c.CausedBy != nil {
		result["caused_by"] = *c.CausedBy
	}
	if c.ParentID != nil {
		result["parent_id"] = *c.ParentID
	}

	// Add custom data
	for k, v := range c.CustomData {
		result[k] = v
	}

	return result
}

// HealthDetails provides strongly-typed health status information
// Replaces untyped maps health details
type HealthDetails struct {
	PatternsLoaded  int       `json:"patterns_loaded"`
	CircuitBreaker  string    `json:"circuit_breaker"`
	QueueUsage      float64   `json:"queue_usage"`
	ComponentStatus string    `json:"component_status"`
	LastHealthCheck time.Time `json:"last_health_check"`
	ErrorCount      int64     `json:"error_count,omitempty"`
	ProcessingRate  float64   `json:"processing_rate,omitempty"`
	MemoryUsageMB   float64   `json:"memory_usage_mb,omitempty"`
}

// IsHealthy returns true if all health indicators are good
func (h *HealthDetails) IsHealthy() bool {
	return h.CircuitBreaker != "open" && h.QueueUsage < 0.9
}

// ConditionValue represents a strongly-typed value for condition matching
// Replaces interface{} in condition evaluation
type ConditionValue struct {
	Type        ConditionValueType `json:"type"`
	StringValue *string            `json:"string_value,omitempty"`
	IntValue    *int64             `json:"int_value,omitempty"`
	FloatValue  *float64           `json:"float_value,omitempty"`
	BoolValue   *bool              `json:"bool_value,omitempty"`
	ListValue   []string           `json:"list_value,omitempty"`
}

// ConditionValueType represents the type of a condition value
type ConditionValueType string

const (
	ConditionValueTypeString ConditionValueType = "string"
	ConditionValueTypeInt    ConditionValueType = "int"
	ConditionValueTypeFloat  ConditionValueType = "float"
	ConditionValueTypeBool   ConditionValueType = "bool"
	ConditionValueTypeList   ConditionValueType = "list"
	ConditionValueTypeNil    ConditionValueType = "nil"
)

// Strongly-typed constructors for common cases
func NewStringConditionValue(value string) *ConditionValue {
	return &ConditionValue{
		Type:        ConditionValueTypeString,
		StringValue: &value,
	}
}

func NewIntConditionValue(value int64) *ConditionValue {
	return &ConditionValue{
		Type:     ConditionValueTypeInt,
		IntValue: &value,
	}
}

func NewFloatConditionValue(value float64) *ConditionValue {
	return &ConditionValue{
		Type:       ConditionValueTypeFloat,
		FloatValue: &value,
	}
}

func NewBoolConditionValue(value bool) *ConditionValue {
	return &ConditionValue{
		Type:      ConditionValueTypeBool,
		BoolValue: &value,
	}
}

func NewListConditionValue(value []string) *ConditionValue {
	return &ConditionValue{
		Type:      ConditionValueTypeList,
		ListValue: value,
	}
}

func NewNilConditionValue() *ConditionValue {
	return &ConditionValue{
		Type: ConditionValueTypeNil,
	}
}

// NewConditionValue creates a new ConditionValue from various types
// NOTE: Prefer the strongly-typed constructors above when possible
// DEPRECATED: This function uses interface{} for backwards compatibility.
// Use strongly-typed constructors like NewStringConditionValue, NewIntConditionValue, etc.
func NewConditionValue(value any) *ConditionValue {
	cv := &ConditionValue{}

	switch v := value.(type) {
	case string:
		cv.Type = ConditionValueTypeString
		cv.StringValue = &v
	case int:
		val := int64(v)
		cv.Type = ConditionValueTypeInt
		cv.IntValue = &val
	case int32:
		val := int64(v)
		cv.Type = ConditionValueTypeInt
		cv.IntValue = &val
	case int64:
		cv.Type = ConditionValueTypeInt
		cv.IntValue = &v
	case uint32:
		val := int64(v)
		cv.Type = ConditionValueTypeInt
		cv.IntValue = &val
	case uint64:
		val := int64(v)
		cv.Type = ConditionValueTypeInt
		cv.IntValue = &val
	case float32:
		val := float64(v)
		cv.Type = ConditionValueTypeFloat
		cv.FloatValue = &val
	case float64:
		cv.Type = ConditionValueTypeFloat
		cv.FloatValue = &v
	case bool:
		cv.Type = ConditionValueTypeBool
		cv.BoolValue = &v
	case []string:
		cv.Type = ConditionValueTypeList
		cv.ListValue = v
	case []any:
		// Convert any slice to string slice for compatibility
		// This handles cases where data comes from JSON unmarshaling
		stringList := make([]string, len(v))
		for i, item := range v {
			stringList[i] = toString(item)
		}
		cv.Type = ConditionValueTypeList
		cv.ListValue = stringList
	case nil:
		cv.Type = ConditionValueTypeNil
	default:
		// Fallback to string representation
		str := toString(v)
		cv.Type = ConditionValueTypeString
		cv.StringValue = &str
	}

	return cv
}

// ToString returns the string representation of the value
func (cv *ConditionValue) ToString() string {
	switch cv.Type {
	case ConditionValueTypeString:
		if cv.StringValue != nil {
			return *cv.StringValue
		}
	case ConditionValueTypeInt:
		if cv.IntValue != nil {
			return toString(*cv.IntValue)
		}
	case ConditionValueTypeFloat:
		if cv.FloatValue != nil {
			return toString(*cv.FloatValue)
		}
	case ConditionValueTypeBool:
		if cv.BoolValue != nil {
			return toString(*cv.BoolValue)
		}
	case ConditionValueTypeList:
		return toString(cv.ListValue)
	case ConditionValueTypeNil:
		return ""
	}
	return ""
}

// ToFloat64 returns the float64 representation if possible
func (cv *ConditionValue) ToFloat64() (float64, bool) {
	switch cv.Type {
	case ConditionValueTypeFloat:
		if cv.FloatValue != nil {
			return *cv.FloatValue, true
		}
	case ConditionValueTypeInt:
		if cv.IntValue != nil {
			return float64(*cv.IntValue), true
		}
	case ConditionValueTypeString:
		if cv.StringValue != nil {
			// Try to parse string as float
			var f float64
			if n, err := fmt.Sscanf(*cv.StringValue, "%f", &f); n == 1 && err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

// IsNil returns true if the value is nil
func (cv *ConditionValue) IsNil() bool {
	return cv.Type == ConditionValueTypeNil
}

// Equals compares two ConditionValues for equality
func (cv *ConditionValue) Equals(other *ConditionValue) bool {
	if cv.Type != other.Type {
		return false
	}

	switch cv.Type {
	case ConditionValueTypeString:
		return cv.StringValue != nil && other.StringValue != nil && *cv.StringValue == *other.StringValue
	case ConditionValueTypeInt:
		return cv.IntValue != nil && other.IntValue != nil && *cv.IntValue == *other.IntValue
	case ConditionValueTypeFloat:
		return cv.FloatValue != nil && other.FloatValue != nil && *cv.FloatValue == *other.FloatValue
	case ConditionValueTypeBool:
		return cv.BoolValue != nil && other.BoolValue != nil && *cv.BoolValue == *other.BoolValue
	case ConditionValueTypeNil:
		return true
	case ConditionValueTypeList:
		if len(cv.ListValue) != len(other.ListValue) {
			return false
		}
		for i, v := range cv.ListValue {
			if v != other.ListValue[i] {
				return false
			}
		}
		return true
	}
	return false
}

// Contains checks if this value contains the other value
func (cv *ConditionValue) Contains(other *ConditionValue) bool {
	switch cv.Type {
	case ConditionValueTypeString:
		if cv.StringValue != nil && other.StringValue != nil {
			return strings.Contains(*cv.StringValue, *other.StringValue)
		}
	case ConditionValueTypeList:
		otherStr := other.ToString()
		for _, item := range cv.ListValue {
			if item == otherStr {
				return true
			}
		}
	}
	return false
}

// Helper function to convert any value to string
func toString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}
