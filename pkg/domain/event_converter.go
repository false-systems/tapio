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
	if event.Semantic != nil {
		unified.Semantic = &SemanticContext{
			Intent:     event.Semantic.Intent,
			Category:   event.Category,
			Confidence: event.Confidence,
			Tags:       event.Tags,
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
	var entityType, entityName, entityUID string
	var labels map[string]string

	if event.Data != nil {
		if event.Data.Resource != nil {
			entityType = "resource"
			entityName = event.Data.Resource.Name
			entityUID = event.Data.Resource.UID
		}
		if event.Data.Dimensions != nil {
			if t, ok := event.Data.Dimensions["entity_type"]; ok {
				entityType = t
			}
			if n, ok := event.Data.Dimensions["entity_name"]; ok {
				entityName = n
			}
			if u, ok := event.Data.Dimensions["entity_uid"]; ok {
				entityUID = u
			}
		}
	}

	unified.Entity = &EntityContext{
		Type:      entityType,
		Name:      entityName,
		UID:       entityUID,
		Namespace: event.Context.Namespace,
		Labels:    labels,
	}

	// Extract layer-specific data based on event type and source
	c.extractLayerData(event, unified)

	// Convert impact information
	var infrastructureImpact float64
	var systemCritical, cascadeRisk, sloImpact bool

	if event.Data != nil && event.Data.Metrics != nil {
		if impact, ok := event.Data.Metrics["infrastructure_impact"]; ok {
			infrastructureImpact = impact
		}
	}

	unified.Impact = &ImpactContext{
		Severity:             string(event.Severity),
		InfrastructureImpact: infrastructureImpact,
		SystemCritical:       systemCritical,
		CascadeRisk:          cascadeRisk,
		SLOImpact:            sloImpact,
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
		Data:      &EventData{},
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
		// Set semantic data
		if event.Semantic == nil {
			event.Semantic = &SemanticData{}
		}
		event.Semantic.Intent = unified.Semantic.Intent
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
		if event.Data == nil {
			event.Data = &EventData{}
		}
		if event.Data.Dimensions == nil {
			event.Data.Dimensions = make(map[string]string)
		}
		event.Data.Dimensions["entity_type"] = unified.Entity.Type
		event.Data.Dimensions["entity_name"] = unified.Entity.Name
		event.Data.Dimensions["entity_uid"] = unified.Entity.UID
		if unified.Entity.Labels != nil {
			if event.Data.Resource == nil {
				event.Data.Resource = &ResourceInfo{}
			}
			// Store labels in resource context - this is a compromise for the conversion
		}
	}

	// Add layer-specific data
	c.addLayerData(unified, event)

	// Add impact data
	if unified.Impact != nil {
		if event.Data == nil {
			event.Data = &EventData{}
		}
		if event.Data.Metrics == nil {
			event.Data.Metrics = make(map[string]float64)
		}
		event.Data.Metrics["infrastructure_impact"] = unified.Impact.InfrastructureImpact
		// Store boolean values as float64 (1.0 for true, 0.0 for false)
		if unified.Impact.SystemCritical {
			event.Data.Metrics["system_critical"] = 1.0
		}
		if unified.Impact.CascadeRisk {
			event.Data.Metrics["cascade_risk"] = 1.0
		}
		if unified.Impact.SLOImpact {
			event.Data.Metrics["slo_impact"] = 1.0
		}
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

// Helper function to get data from EventData structure
func (c *EventConverter) getEventDataString(data *EventData, key string) string {
	if data == nil {
		return ""
	}
	if data.Dimensions != nil {
		if v, ok := data.Dimensions[key]; ok {
			return v
		}
	}
	if data.CustomData != nil {
		if m, ok := data.CustomData.(map[string]interface{}); ok {
			return getStringFromMap(m, key)
		}
	}
	return ""
}

func (c *EventConverter) getEventDataInt(data *EventData, key string) int {
	if data == nil {
		return 0
	}
	if data.CustomData != nil {
		if m, ok := data.CustomData.(map[string]interface{}); ok {
			return getIntFromMap(m, key)
		}
	}
	return 0
}

func (c *EventConverter) getEventDataInt64(data *EventData, key string) int64 {
	if data == nil {
		return 0
	}
	if data.CustomData != nil {
		if m, ok := data.CustomData.(map[string]interface{}); ok {
			return getInt64FromMap(m, key)
		}
	}
	return 0
}

// extractLayerData extracts layer-specific data from legacy event
func (c *EventConverter) extractLayerData(event *Event, unified *UnifiedEvent) {
	switch event.Source {
	case "kernel", "ebpf", "syscall":
		unified.Kernel = &KernelData{
			Syscall:    c.getEventDataString(event.Data, "syscall"),
			PID:        uint32(c.getEventDataInt(event.Data, "pid")),
			TID:        uint32(c.getEventDataInt(event.Data, "tid")),
			Comm:       c.getEventDataString(event.Data, "comm"),
			UID:        uint32(c.getEventDataInt(event.Data, "uid")),
			GID:        uint32(c.getEventDataInt(event.Data, "gid")),
			ReturnCode: int32(c.getEventDataInt(event.Data, "return_code")),
			// Note: Stack trace handling simplified for now
		}

	case "network", "tcp", "http":
		unified.Network = &NetworkData{
			Protocol:   c.getEventDataString(event.Data, "protocol"),
			SourceIP:   c.getEventDataString(event.Data, "source_ip"),
			SourcePort: uint16(c.getEventDataInt(event.Data, "source_port")),
			DestIP:     c.getEventDataString(event.Data, "dest_ip"),
			DestPort:   uint16(c.getEventDataInt(event.Data, "dest_port")),
			Direction:  c.getEventDataString(event.Data, "direction"),
			BytesSent:  uint64(c.getEventDataInt64(event.Data, "bytes_sent")),
			BytesRecv:  uint64(c.getEventDataInt64(event.Data, "bytes_recv")),
			Latency:    c.getEventDataInt64(event.Data, "latency"),
			StatusCode: c.getEventDataInt(event.Data, "status_code"),
			Method:     c.getEventDataString(event.Data, "method"),
			Path:       c.getEventDataString(event.Data, "path"),
			// Note: Headers handling simplified for now
		}

	case "app", "application", "log":
		unified.Application = &ApplicationData{
			Level:      c.getEventDataString(event.Data, "level"),
			Logger:     c.getEventDataString(event.Data, "logger"),
			Message:    event.Message,
			StackTrace: c.getEventDataString(event.Data, "stack_trace"),
			ErrorType:  c.getEventDataString(event.Data, "error_type"),
			UserID:     c.getEventDataString(event.Data, "user_id"),
			SessionID:  c.getEventDataString(event.Data, "session_id"),
			RequestID:  c.getEventDataString(event.Data, "request_id"),
			// Custom data is handled by the event data structure now
		}

	case "kubeapi", "kubernetes":
		unified.Kubernetes = &KubernetesData{
			ObjectKind:      c.getEventDataString(event.Data, "kind"),
			Object:          c.getEventDataString(event.Data, "name"),
			APIVersion:      c.getEventDataString(event.Data, "api_version"),
			EventType:       c.getEventDataString(event.Data, "event_type"),
			Reason:          c.getEventDataString(event.Data, "reason"),
			Message:         event.Message,
			ResourceVersion: c.getEventDataString(event.Data, "resource_version"),
			// Note: Labels and annotations handling simplified for now
		}
	}
}

// addLayerData adds layer-specific data to legacy event
func (c *EventConverter) addLayerData(unified *UnifiedEvent, event *Event) {
	if event.Data == nil {
		event.Data = &EventData{}
	}

	// Store layer-specific data in the CustomData field as interface{}
	layerData := make(map[string]interface{})

	if unified.Kernel != nil {
		layerData["syscall"] = unified.Kernel.Syscall
		layerData["pid"] = unified.Kernel.PID
		layerData["tid"] = unified.Kernel.TID
		layerData["comm"] = unified.Kernel.Comm
		layerData["uid"] = unified.Kernel.UID
		layerData["gid"] = unified.Kernel.GID
		layerData["return_code"] = unified.Kernel.ReturnCode
		if len(unified.Kernel.StackTrace) > 0 {
			layerData["stack_trace"] = unified.Kernel.StackTrace
		}
	}

	if unified.Network != nil {
		layerData["protocol"] = unified.Network.Protocol
		layerData["source_ip"] = unified.Network.SourceIP
		layerData["source_port"] = unified.Network.SourcePort
		layerData["dest_ip"] = unified.Network.DestIP
		layerData["dest_port"] = unified.Network.DestPort
		layerData["direction"] = unified.Network.Direction
		layerData["bytes_sent"] = unified.Network.BytesSent
		layerData["bytes_recv"] = unified.Network.BytesRecv
		layerData["latency"] = unified.Network.Latency
		layerData["status_code"] = unified.Network.StatusCode
		layerData["method"] = unified.Network.Method
		layerData["path"] = unified.Network.Path
		if len(unified.Network.Headers) > 0 {
			layerData["headers"] = unified.Network.Headers
		}
	}

	if unified.Application != nil {
		layerData["level"] = unified.Application.Level
		layerData["logger"] = unified.Application.Logger
		event.Message = unified.Application.Message
		layerData["stack_trace"] = unified.Application.StackTrace
		layerData["error_type"] = unified.Application.ErrorType
		layerData["user_id"] = unified.Application.UserID
		layerData["session_id"] = unified.Application.SessionID
		layerData["request_id"] = unified.Application.RequestID
	}

	if unified.Kubernetes != nil {
		layerData["kind"] = unified.Kubernetes.ObjectKind
		layerData["name"] = unified.Kubernetes.Object
		layerData["api_version"] = unified.Kubernetes.APIVersion
		layerData["event_type"] = unified.Kubernetes.EventType
		layerData["reason"] = unified.Kubernetes.Reason
		event.Message = unified.Kubernetes.Message
		if len(unified.Kubernetes.Labels) > 0 {
			layerData["labels"] = unified.Kubernetes.Labels
		}
		if len(unified.Kubernetes.Annotations) > 0 {
			layerData["annotations"] = unified.Kubernetes.Annotations
		}
	}

	// Store all layer data in CustomData
	if len(layerData) > 0 {
		event.Data.CustomData = layerData
	}
}

// createPayload creates appropriate payload based on event type
func (c *EventConverter) createPayload(unified *UnifiedEvent) EventPayload {
	// For now, return a generic payload
	// In future, create specific payload types based on event type
	return &GenericEventPayload{
		Type: string(unified.Type),
		Data: nil, // No original data available
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
