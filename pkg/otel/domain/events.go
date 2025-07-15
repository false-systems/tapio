package domain

import (
	"crypto/rand"
	"fmt"
	"time"
)

// TraceEvent represents a domain event in the trace aggregate
type TraceEvent interface {
	GetEventID() EventID
	GetEventType() TraceEventType
	GetTimestamp() time.Time
	GetTraceID() TraceID
	GetVersion() int64
	GetPayload() any
}

// EventID represents a unique event identifier
type EventID struct {
	ID [16]byte
}

func (e EventID) String() string {
	return fmt.Sprintf("%x", e.ID)
}

// TraceEventType represents the type of trace event
type TraceEventType int

const (
	TraceEventTypeSpanStarted TraceEventType = iota
	TraceEventTypeSpanEnded
	TraceEventTypeSpanExported
	TraceEventTypeTraceCompleted
	TraceEventTypeError
)

// Base event implementation
type baseEvent struct {
	eventID   EventID
	eventType string
	timestamp time.Time
	version   int64
}

func newBaseEvent(eventType string) baseEvent {
	return baseEvent{
		eventID:   generateEventID(),
		eventType: eventType,
		timestamp: time.Now(),
		version:   1,
	}
}

func (e *baseEvent) GetEventID() EventID {
	return e.eventID
}

func (e *baseEvent) GetEventType() TraceEventType {
	switch e.eventType {
	case "span_started":
		return TraceEventTypeSpanStarted
	case "span_finished":
		return TraceEventTypeSpanEnded
	case "span_exported":
		return TraceEventTypeSpanExported
	case "trace_completed":
		return TraceEventTypeTraceCompleted
	default:
		return TraceEventTypeError
	}
}

func (e *baseEvent) GetTimestamp() time.Time {
	return e.timestamp
}

func (e *baseEvent) GetVersion() int64 {
	return e.version
}

// Specific event implementations

type traceStartedEvent struct {
	baseEvent
	traceID     TraceID
	serviceName string
	spanName    string
	attributes  map[string]string
}

func (e *traceStartedEvent) GetTraceID() TraceID {
	return e.traceID
}

func (e *traceStartedEvent) GetPayload() any {
	return map[string]any{
		"service_name": e.serviceName,
		"span_name":    e.spanName,
		"attributes":   e.attributes,
	}
}

type spanCreatedEvent struct {
	baseEvent
	traceID      TraceID
	spanID       SpanID
	parentSpanID SpanID
	spanName     string
}

func (e *spanCreatedEvent) GetTraceID() TraceID {
	return e.traceID
}

func (e *spanCreatedEvent) GetPayload() any {
	return map[string]any{
		"span_id":        e.spanID,
		"parent_span_id": e.parentSpanID,
		"span_name":      e.spanName,
	}
}

type spanFinishedEvent struct {
	baseEvent
	traceID  TraceID
	spanID   SpanID
	endTime  time.Time
	duration time.Duration
}

func (e *spanFinishedEvent) GetTraceID() TraceID {
	return e.traceID
}

func (e *spanFinishedEvent) GetPayload() any {
	return map[string]any{
		"span_id":  e.spanID,
		"end_time": e.endTime,
		"duration": e.duration,
	}
}

type spanEventAddedEvent struct {
	baseEvent
	traceID   TraceID
	spanID    SpanID
	eventName string
	timestamp time.Time
}

func (e *spanEventAddedEvent) GetTraceID() TraceID {
	return e.traceID
}

func (e *spanEventAddedEvent) GetPayload() any {
	return map[string]any{
		"span_id":    e.spanID,
		"event_name": e.eventName,
		"timestamp":  e.timestamp,
	}
}

type spanAttributesSetEvent struct {
	baseEvent
	traceID    TraceID
	spanID     SpanID
	attributes map[string]any
}

func (e *spanAttributesSetEvent) GetTraceID() TraceID {
	return e.traceID
}

func (e *spanAttributesSetEvent) GetPayload() any {
	return map[string]any{
		"span_id":    e.spanID,
		"attributes": e.attributes,
	}
}

type traceFinishedEvent struct {
	baseEvent
	traceID  TraceID
	endTime  time.Time
	duration time.Duration
}

func (e *traceFinishedEvent) GetTraceID() TraceID {
	return e.traceID
}

func (e *traceFinishedEvent) GetPayload() any {
	return map[string]any{
		"end_time": e.endTime,
		"duration": e.duration,
	}
}

// Event factory functions

func NewSpanEventAddedEvent(traceID TraceID, spanID SpanID, eventName string, timestamp time.Time) TraceEvent {
	return &spanEventAddedEvent{
		baseEvent: newBaseEvent("span_event_added"),
		traceID:   traceID,
		spanID:    spanID,
		eventName: eventName,
		timestamp: timestamp,
	}
}

func NewSpanAttributesSetEvent(traceID TraceID, spanID SpanID, attributes map[string]any) TraceEvent {
	return &spanAttributesSetEvent{
		baseEvent:  newBaseEvent("span_attributes_set"),
		traceID:    traceID,
		spanID:     spanID,
		attributes: attributes,
	}
}

func NewTraceFinishedEvent(traceID TraceID, endTime time.Time, duration time.Duration) TraceEvent {
	return &traceFinishedEvent{
		baseEvent: newBaseEvent("trace_finished"),
		traceID:   traceID,
		endTime:   endTime,
		duration:  duration,
	}
}

// Helper functions

func generateEventID() EventID {
	var id EventID
	_, err := rand.Read(id.ID[:])
	if err != nil {
		// Fallback to timestamp-based ID if random fails
		now := time.Now().UnixNano()
		for i := 0; i < 8; i++ {
			id.ID[i] = byte(now >> (i * 8))
		}
		for i := 8; i < 16; i++ {
			id.ID[i] = byte(now >> ((i - 8) * 8))
		}
	}
	return id
}

// Event validation

func ValidateEvent(event TraceEvent) error {
	if event == nil {
		return fmt.Errorf("event cannot be nil")
	}

	if event.GetEventID().String() == "" {
		return fmt.Errorf("event ID cannot be empty")
	}

	if event.GetTimestamp().IsZero() {
		return fmt.Errorf("event timestamp cannot be zero")
	}

	if event.GetTraceID() == (TraceID{}) {
		return fmt.Errorf("trace ID cannot be empty")
	}

	return nil
}

// Event serialization for persistence

type SerializedEvent struct {
	EventID   string                 `json:"event_id"`
	EventType string                 `json:"event_type"`
	TraceID   string                 `json:"trace_id"`
	Timestamp time.Time              `json:"timestamp"`
	Version   int64                  `json:"version"`
	Payload   map[string]interface{} `json:"payload"`
}

func SerializeEvent(event TraceEvent) (*SerializedEvent, error) {
	if err := ValidateEvent(event); err != nil {
		return nil, fmt.Errorf("invalid event: %w", err)
	}

	payload, ok := event.GetPayload().(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid event payload type")
	}

	return &SerializedEvent{
		EventID:   event.GetEventID().String(),
		EventType: getEventTypeString(event.GetEventType()),
		TraceID:   event.GetTraceID().String(),
		Timestamp: event.GetTimestamp(),
		Version:   event.GetVersion(),
		Payload:   payload,
	}, nil
}

func getEventTypeString(eventType TraceEventType) string {
	switch eventType {
	case TraceEventTypeSpanStarted:
		return "span_started"
	case TraceEventTypeSpanEnded:
		return "span_ended"
	case TraceEventTypeSpanExported:
		return "span_exported"
	case TraceEventTypeTraceCompleted:
		return "trace_completed"
	case TraceEventTypeError:
		return "error"
	default:
		return "unknown"
	}
}

// Event aggregation for analytics

type EventAggregate struct {
	TraceID    TraceID
	EventCount int64
	Events     []TraceEvent
	StartTime  time.Time
	EndTime    time.Time
	Duration   time.Duration
}

func AggregateEvents(events []TraceEvent) map[TraceID]*EventAggregate {
	aggregates := make(map[TraceID]*EventAggregate)

	for _, event := range events {
		traceID := event.GetTraceID()

		aggregate, exists := aggregates[traceID]
		if !exists {
			aggregate = &EventAggregate{
				TraceID:   traceID,
				Events:    make([]TraceEvent, 0),
				StartTime: event.GetTimestamp(),
				EndTime:   event.GetTimestamp(),
			}
			aggregates[traceID] = aggregate
		}

		aggregate.Events = append(aggregate.Events, event)
		aggregate.EventCount++

		// Update time range
		eventTime := event.GetTimestamp()
		if eventTime.Before(aggregate.StartTime) {
			aggregate.StartTime = eventTime
		}
		if eventTime.After(aggregate.EndTime) {
			aggregate.EndTime = eventTime
		}

		aggregate.Duration = aggregate.EndTime.Sub(aggregate.StartTime)
	}

	return aggregates
}

// Event filtering and querying

type EventFilter struct {
	TraceIDs   []TraceID
	EventTypes []TraceEventType
	StartTime  *time.Time
	EndTime    *time.Time
	Limit      int
}

func FilterEvents(events []TraceEvent, filter EventFilter) []TraceEvent {
	var filtered []TraceEvent

	for _, event := range events {
		if !matchesFilter(event, filter) {
			continue
		}

		filtered = append(filtered, event)

		// Apply limit if specified
		if filter.Limit > 0 && len(filtered) >= filter.Limit {
			break
		}
	}

	return filtered
}

func matchesFilter(event TraceEvent, filter EventFilter) bool {
	// Check trace ID filter
	if len(filter.TraceIDs) > 0 {
		found := false
		for _, traceID := range filter.TraceIDs {
			if event.GetTraceID() == traceID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check event type filter
	if len(filter.EventTypes) > 0 {
		found := false
		for _, eventType := range filter.EventTypes {
			if event.GetEventType() == eventType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check time range filter
	eventTime := event.GetTimestamp()
	if filter.StartTime != nil && eventTime.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && eventTime.After(*filter.EndTime) {
		return false
	}

	return true
}
