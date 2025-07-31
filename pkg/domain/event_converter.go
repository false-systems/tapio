package domain

import (
	"fmt"
)

// EventConverter provides conversions between Event and UnifiedEvent
type EventConverter struct{}

// NewEventConverter creates a new event converter
func NewEventConverter() *EventConverter {
	return &EventConverter{}
}

// ToUnifiedEvent converts a legacy Event to UnifiedEvent
func (c *EventConverter) ToUnifiedEvent(event *Event) *UnifiedEvent {
	if event == nil {
		return nil
	}

	unified := &UnifiedEvent{
		ID:        string(event.ID),
		Timestamp: event.Timestamp,
		Type:      EventType(event.Type),
		Source:    string(event.Source),
		RawData:   nil, // Legacy events don't have raw data
	}

	// Extract trace context if available
	if event.Context.TraceID != "" {
		unified.TraceContext = &TraceContext{
			TraceID: event.Context.TraceID,
			SpanID:  event.Context.SpanID,
			// Other fields not available in current EventContext
		}
	}

	// Convert semantic information
	if len(event.Semantic) > 0 {
		unified.Semantic = &SemanticContext{
			Intent:     getStringFromMap(event.Semantic, "intent"),
			Category:   event.Category,
			Confidence: event.Confidence,
			Tags:       event.Tags,
			// Attributes field doesn't exist in SemanticContext
		}
	} else if event.Category != "" || event.Confidence > 0 {
		// Use classification fields
		unified.Semantic = &SemanticContext{
			Category:   event.Category,
			Confidence: event.Confidence,
			Tags:       event.Tags,
		}
	}

	// Convert entity information
	unified.Entity = &EntityContext{
		Type:      getStringFromMap(event.Data, "entity_type"),
		Name:      getStringFromMap(event.Data, "entity_name"),
		UID:       getStringFromMap(event.Data, "entity_uid"),
		Namespace: event.Context.Namespace,
		Labels:    convertToStringMap(event.Data["labels"]),
	}

	// Extract layer-specific data based on event type and source
	c.extractLayerData(event, unified)

	// Convert impact information
	unified.Impact = &ImpactContext{
		Severity:             string(event.Severity),
		InfrastructureImpact: getFloatFromMap(event.Data, "infrastructure_impact"),
		SystemCritical:       getBoolFromMap(event.Data, "system_critical"),
		CascadeRisk:          getBoolFromMap(event.Data, "cascade_risk"),
		SLOImpact:            getBoolFromMap(event.Data, "slo_impact"),
	}

	// Convert correlation context
	if event.Causality != nil {
		unified.Correlation = &CorrelationContext{
			// Map from causality to correlation
			CausalChain: event.Causality.CausalChain,
			// Other fields not directly mappable
		}
	}

	// Note: Original event.Data is not directly stored in UnifiedEvent
	// It's distributed across the specific context fields above

	return unified
}

// FromUnifiedEvent converts a UnifiedEvent to legacy Event
func (c *EventConverter) FromUnifiedEvent(unified *UnifiedEvent) *Event {
	if unified == nil {
		return nil
	}

	event := &Event{
		ID:        EventID(unified.ID),
		Timestamp: unified.Timestamp,
		Type:      EventType(unified.Type),
		Source:    SourceType(unified.Source),
		Data:      make(map[string]interface{}),
		Message:   "", // Will be set from layer-specific data
	}

	// Set severity
	if unified.Impact != nil {
		event.Severity = EventSeverity(unified.Impact.Severity)
	}

	// Set semantic fields
	if unified.Semantic != nil {
		event.Category = unified.Semantic.Category
		event.Confidence = unified.Semantic.Confidence
		event.Tags = unified.Semantic.Tags
		// Note: Attributes field not available in SemanticContext
		if event.Semantic == nil {
			event.Semantic = make(map[string]interface{})
		}
		event.Semantic["intent"] = unified.Semantic.Intent
	}

	// Set trace context
	if unified.TraceContext != nil {
		event.Context.TraceID = unified.TraceContext.TraceID
		event.Context.SpanID = unified.TraceContext.SpanID
		// Note: ParentSpanID, Sampled, Baggage not available in EventContext
	}

	// Set entity context
	if unified.Entity != nil {
		event.Context.Namespace = unified.Entity.Namespace
		event.Data["entity_type"] = unified.Entity.Type
		event.Data["entity_name"] = unified.Entity.Name
		event.Data["entity_uid"] = unified.Entity.UID
		if len(unified.Entity.Labels) > 0 {
			event.Data["labels"] = unified.Entity.Labels
		}
	}

	// Add layer-specific data
	c.addLayerData(unified, event)

	// Add impact data
	if unified.Impact != nil {
		event.Data["infrastructure_impact"] = unified.Impact.InfrastructureImpact
		event.Data["system_critical"] = unified.Impact.SystemCritical
		event.Data["cascade_risk"] = unified.Impact.CascadeRisk
		event.Data["slo_impact"] = unified.Impact.SLOImpact
		event.Data["affected_services"] = unified.Impact.AffectedServices
	}

	// Add causality
	if unified.Correlation != nil && len(unified.Correlation.CausalChain) > 0 {
		event.Causality = &CausalityContext{
			CausalChain: unified.Correlation.CausalChain,
			// Other fields not directly mappable
		}
	}

	// Note: UnifiedEvent doesn't have OriginalData field
	// Data is reconstructed from the specific context fields

	// Set appropriate payload based on event type
	event.Payload = c.createPayload(unified)

	return event
}

// BatchToUnified converts multiple Events to UnifiedEvents
func (c *EventConverter) BatchToUnified(events []Event) []*UnifiedEvent {
	unified := make([]*UnifiedEvent, len(events))
	for i, event := range events {
		unified[i] = c.ToUnifiedEvent(&event)
	}
	return unified
}

// BatchFromUnified converts multiple UnifiedEvents to Events
func (c *EventConverter) BatchFromUnified(unified []*UnifiedEvent) []Event {
	events := make([]Event, len(unified))
	for i, u := range unified {
		if e := c.FromUnifiedEvent(u); e != nil {
			events[i] = *e
		}
	}
	return events
}

// extractLayerData extracts layer-specific data from legacy event
func (c *EventConverter) extractLayerData(event *Event, unified *UnifiedEvent) {
	switch event.Source {
	case "kernel", "ebpf", "syscall":
		unified.Kernel = &KernelData{
			Syscall:    getStringFromMap(event.Data, "syscall"),
			PID:        uint32(getIntFromMap(event.Data, "pid")),
			TID:        uint32(getIntFromMap(event.Data, "tid")),
			Comm:       getStringFromMap(event.Data, "comm"),
			UID:        uint32(getIntFromMap(event.Data, "uid")),
			GID:        uint32(getIntFromMap(event.Data, "gid")),
			ReturnCode: int32(getIntFromMap(event.Data, "return_code")),
			StackTrace: getStringSliceFromMap(event.Data, "stack_trace"),
		}

	case "network", "tcp", "http":
		unified.Network = &NetworkData{
			Protocol:   getStringFromMap(event.Data, "protocol"),
			SourceIP:   getStringFromMap(event.Data, "source_ip"),
			SourcePort: uint16(getIntFromMap(event.Data, "source_port")),
			DestIP:     getStringFromMap(event.Data, "dest_ip"),
			DestPort:   uint16(getIntFromMap(event.Data, "dest_port")),
			Direction:  getStringFromMap(event.Data, "direction"),
			BytesSent:  uint64(getInt64FromMap(event.Data, "bytes_sent")),
			BytesRecv:  uint64(getInt64FromMap(event.Data, "bytes_recv")),
			Latency:    getInt64FromMap(event.Data, "latency"),
			StatusCode: getIntFromMap(event.Data, "status_code"),
			Method:     getStringFromMap(event.Data, "method"),
			Path:       getStringFromMap(event.Data, "path"),
			Headers:    convertToStringMap(event.Data["headers"]),
		}

	case "app", "application", "log":
		unified.Application = &ApplicationData{
			Level:      getStringFromMap(event.Data, "level"),
			Logger:     getStringFromMap(event.Data, "logger"),
			Message:    event.Message,
			StackTrace: getStringFromMap(event.Data, "stack_trace"),
			ErrorType:  getStringFromMap(event.Data, "error_type"),
			UserID:     getStringFromMap(event.Data, "user_id"),
			SessionID:  getStringFromMap(event.Data, "session_id"),
			RequestID:  getStringFromMap(event.Data, "request_id"),
			// Version field not available in ApplicationData
			Custom: event.Data,
		}

	case "k8s", "kubernetes":
		unified.Kubernetes = &KubernetesData{
			ObjectKind:      getStringFromMap(event.Data, "kind"),
			Object:          getStringFromMap(event.Data, "name"),
			APIVersion:      getStringFromMap(event.Data, "api_version"),
			EventType:       getStringFromMap(event.Data, "event_type"),
			Reason:          getStringFromMap(event.Data, "reason"),
			Message:         event.Message,
			ResourceVersion: getStringFromMap(event.Data, "resource_version"),
			Labels:          convertToStringMap(event.Data["labels"]),
			Annotations:     convertToStringMap(event.Data["annotations"]),
		}
	}
}

// addLayerData adds layer-specific data to legacy event
func (c *EventConverter) addLayerData(unified *UnifiedEvent, event *Event) {
	if unified.Kernel != nil {
		event.Data["syscall"] = unified.Kernel.Syscall
		event.Data["pid"] = unified.Kernel.PID
		event.Data["tid"] = unified.Kernel.TID
		event.Data["comm"] = unified.Kernel.Comm
		event.Data["uid"] = unified.Kernel.UID
		event.Data["gid"] = unified.Kernel.GID
		event.Data["return_code"] = unified.Kernel.ReturnCode
		if len(unified.Kernel.StackTrace) > 0 {
			event.Data["stack_trace"] = unified.Kernel.StackTrace
		}
	}

	if unified.Network != nil {
		event.Data["protocol"] = unified.Network.Protocol
		event.Data["source_ip"] = unified.Network.SourceIP
		event.Data["source_port"] = unified.Network.SourcePort
		event.Data["dest_ip"] = unified.Network.DestIP
		event.Data["dest_port"] = unified.Network.DestPort
		event.Data["direction"] = unified.Network.Direction
		event.Data["bytes_sent"] = unified.Network.BytesSent
		event.Data["bytes_recv"] = unified.Network.BytesRecv
		event.Data["latency"] = unified.Network.Latency
		event.Data["status_code"] = unified.Network.StatusCode
		event.Data["method"] = unified.Network.Method
		event.Data["path"] = unified.Network.Path
		if len(unified.Network.Headers) > 0 {
			event.Data["headers"] = unified.Network.Headers
		}
	}

	if unified.Application != nil {
		event.Data["level"] = unified.Application.Level
		event.Data["logger"] = unified.Application.Logger
		event.Message = unified.Application.Message
		event.Data["stack_trace"] = unified.Application.StackTrace
		event.Data["error_type"] = unified.Application.ErrorType
		event.Data["user_id"] = unified.Application.UserID
		event.Data["session_id"] = unified.Application.SessionID
		event.Data["request_id"] = unified.Application.RequestID
		// Version field not available in ApplicationData
	}

	if unified.Kubernetes != nil {
		event.Data["kind"] = unified.Kubernetes.ObjectKind
		event.Data["name"] = unified.Kubernetes.Object
		// Namespace not directly available in KubernetesData
		// UID not directly available in KubernetesData
		event.Data["api_version"] = unified.Kubernetes.APIVersion
		event.Data["event_type"] = unified.Kubernetes.EventType
		event.Data["reason"] = unified.Kubernetes.Reason
		event.Message = unified.Kubernetes.Message
		if len(unified.Kubernetes.Labels) > 0 {
			event.Data["labels"] = unified.Kubernetes.Labels
		}
		if len(unified.Kubernetes.Annotations) > 0 {
			event.Data["annotations"] = unified.Kubernetes.Annotations
		}
	}
}

// createPayload creates appropriate payload based on event type
func (c *EventConverter) createPayload(unified *UnifiedEvent) EventPayload {
	// For now, return a generic payload
	// In future, create specific payload types based on event type
	return &GenericEventPayload{
		Type: string(unified.Type),
		Data: make(map[string]interface{}), // No original data available
	}
}

// Helper functions

func getStringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func getIntFromMap(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch i := v.(type) {
		case int:
			return i
		case int64:
			return int(i)
		case float64:
			return int(i)
		}
	}
	return 0
}

func getInt64FromMap(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch i := v.(type) {
		case int64:
			return i
		case int:
			return int64(i)
		case float64:
			return int64(i)
		}
	}
	return 0
}

func getFloatFromMap(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch f := v.(type) {
		case float64:
			return f
		case float32:
			return float64(f)
		case int:
			return float64(f)
		case int64:
			return float64(f)
		}
	}
	return 0.0
}

func getBoolFromMap(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getStringSliceFromMap(m map[string]interface{}, key string) []string {
	if v, ok := m[key]; ok {
		switch s := v.(type) {
		case []string:
			return s
		case []interface{}:
			result := make([]string, len(s))
			for i, item := range s {
				result[i] = fmt.Sprintf("%v", item)
			}
			return result
		}
	}
	return nil
}

func convertToStringMap(v interface{}) map[string]string {
	result := make(map[string]string)

	switch m := v.(type) {
	case map[string]string:
		return m
	case map[string]interface{}:
		for k, v := range m {
			result[k] = fmt.Sprintf("%v", v)
		}
	case map[interface{}]interface{}:
		for k, v := range m {
			result[fmt.Sprintf("%v", k)] = fmt.Sprintf("%v", v)
		}
	}

	return result
}
