// Package events provides the unified message format for all Tapio events.
// This package implements efficient event handling with zero-copy operations,
// object pooling, and high-performance serialization.
package events

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative ../../proto/events.proto

// EventPool provides zero-allocation event creation through object pooling
var EventPool = &sync.Pool{
	New: func() interface{} {
		return &UnifiedEvent{
			Attributes: make(map[string]*AttributeValue),
			Labels:     make(map[string]string),
		}
	},
}

// AttributeValuePool for efficient attribute management
var AttributeValuePool = &sync.Pool{
	New: func() interface{} {
		return &AttributeValue{}
	},
}

// Global event statistics for monitoring
var (
	eventsCreated   uint64
	eventsReleased  uint64
	eventsInFlight  uint64
	totalEventSize  uint64
	serializeErrors uint64
)

// NewEvent creates a new unified event with zero-allocation from pool
func NewEvent() *UnifiedEvent {
	event := EventPool.Get().(*UnifiedEvent)
	event.Reset()

	// Set defaults
	event.Id = generateEventID()
	event.Timestamp = timestamppb.Now()

	// Initialize maps
	if event.Attributes == nil {
		event.Attributes = make(map[string]*AttributeValue)
	}
	if event.Labels == nil {
		event.Labels = make(map[string]string)
	}

	// Initialize nested structures if nil
	if event.Metadata == nil {
		event.Metadata = &EventMetadata{}
	}
	if event.Source == nil {
		event.Source = &EventSource{}
	}
	if event.Entity == nil {
		event.Entity = &EntityContext{}
	}
	if event.Correlation == nil {
		event.Correlation = &CorrelationContext{}
	}
	if event.Quality == nil {
		event.Quality = &QualityMetadata{
			Confidence: 1.0,
		}
	}

	atomic.AddUint64(&eventsCreated, 1)
	atomic.AddUint64(&eventsInFlight, 1)

	return event
}

// ReleaseEvent returns an event to the pool for reuse
func ReleaseEvent(event *UnifiedEvent) {
	if event == nil {
		return
	}

	// Clear sensitive data but keep allocated maps
	event.Reset()

	// Preserve map allocations
	if event.Attributes == nil {
		event.Attributes = make(map[string]*AttributeValue)
	} else {
		for k := range event.Attributes {
			delete(event.Attributes, k)
		}
	}

	if event.Labels == nil {
		event.Labels = make(map[string]string)
	} else {
		for k := range event.Labels {
			delete(event.Labels, k)
		}
	}

	EventPool.Put(event)
	atomic.AddUint64(&eventsReleased, 1)
	atomic.AddUint64(&eventsInFlight, ^uint64(0))
}

// Builder provides a fluent interface for event construction
type Builder struct {
	event *UnifiedEvent
}

// NewBuilder creates a new event builder
func NewBuilder() *Builder {
	return &Builder{
		event: NewEvent(),
	}
}

// WithType sets the event type and category
func (b *Builder) WithType(eventType string, category EventCategory) *Builder {
	b.event.Metadata.Type = eventType
	b.event.Metadata.Category = category
	return b
}

// WithSeverity sets the event severity
func (b *Builder) WithSeverity(severity EventSeverity) *Builder {
	b.event.Metadata.Severity = severity
	return b
}

// WithSource sets the event source information
func (b *Builder) WithSource(sourceType, collector, node string) *Builder {
	b.event.Source.Type = sourceType
	b.event.Source.Collector = collector
	b.event.Source.Node = node
	return b
}

// WithEntity sets the entity context
func (b *Builder) WithEntity(entityType EntityType, id, name string) *Builder {
	b.event.Entity.Type = entityType
	b.event.Entity.Id = id
	b.event.Entity.Name = name
	return b
}

// WithProcess sets process information
func (b *Builder) WithProcess(pid uint32, comm string) *Builder {
	if b.event.Entity.Process == nil {
		b.event.Entity.Process = &ProcessInfo{}
	}
	b.event.Entity.Process.Pid = pid
	b.event.Entity.Process.Comm = comm
	return b
}

// WithContainer sets container information
func (b *Builder) WithContainer(id, name, image string) *Builder {
	if b.event.Entity.Container == nil {
		b.event.Entity.Container = &ContainerInfo{}
	}
	b.event.Entity.Container.Id = id
	b.event.Entity.Container.Name = name
	b.event.Entity.Container.Image = image
	return b
}

// WithPod sets Kubernetes pod information
func (b *Builder) WithPod(uid, name, namespace string) *Builder {
	if b.event.Entity.Pod == nil {
		b.event.Entity.Pod = &PodInfo{}
	}
	b.event.Entity.Pod.Uid = uid
	b.event.Entity.Pod.Name = name
	b.event.Entity.Pod.Namespace = namespace
	return b
}

// WithCorrelation sets correlation context
func (b *Builder) WithCorrelation(correlationID, traceID string) *Builder {
	b.event.Correlation.CorrelationId = correlationID
	b.event.Correlation.TraceId = traceID
	return b
}

// WithAttribute adds a typed attribute
func (b *Builder) WithAttribute(key string, value interface{}) *Builder {
	attr := AttributeValuePool.Get().(*AttributeValue)

	switch v := value.(type) {
	case string:
		attr.Value = &AttributeValue_StringValue{StringValue: v}
	case int, int32, int64:
		attr.Value = &AttributeValue_IntValue{IntValue: toInt64(v)}
	case float32, float64:
		attr.Value = &AttributeValue_DoubleValue{DoubleValue: toFloat64(v)}
	case bool:
		attr.Value = &AttributeValue_BoolValue{BoolValue: v}
	case []byte:
		attr.Value = &AttributeValue_BytesValue{BytesValue: v}
	case time.Time:
		attr.Value = &AttributeValue_TimestampValue{TimestampValue: timestamppb.New(v)}
	default:
		// Try to convert to struct
		if structVal, err := structpb.NewValue(v); err == nil {
			if structData, ok := structVal.AsInterface().(map[string]interface{}); ok {
				if s, err := structpb.NewStruct(structData); err == nil {
					attr.Value = &AttributeValue_StructValue{StructValue: s}
				}
			}
		}
	}

	b.event.Attributes[key] = attr
	return b
}

// WithLabel adds a label
func (b *Builder) WithLabel(key, value string) *Builder {
	b.event.Labels[key] = value
	return b
}

// WithNetworkData sets network event data
func (b *Builder) WithNetworkData(data *NetworkEvent) *Builder {
	b.event.Data = &UnifiedEvent_Network{Network: data}
	return b
}

// WithMemoryData sets memory event data
func (b *Builder) WithMemoryData(data *MemoryEvent) *Builder {
	b.event.Data = &UnifiedEvent_Memory{Memory: data}
	return b
}

// WithGenericData sets generic event data
func (b *Builder) WithGenericData(data map[string]interface{}) *Builder {
	if structData, err := structpb.NewStruct(data); err == nil {
		b.event.Data = &UnifiedEvent_Generic{Generic: structData}
	}
	return b
}

// Build returns the constructed event
func (b *Builder) Build() *UnifiedEvent {
	// Set final timestamp if not already set
	if b.event.Timestamp == nil {
		b.event.Timestamp = timestamppb.Now()
	}

	// Calculate processing latency
	if b.event.Quality != nil && b.event.Quality.ProcessingLatencyUs == 0 {
		created := b.event.Timestamp.AsTime()
		b.event.Quality.ProcessingLatencyUs = time.Since(created).Microseconds()
	}

	return b.event
}

// Efficient conversion helpers

// SerializeFast performs efficient serialization with optional compression
func (e *UnifiedEvent) SerializeFast() ([]byte, error) {
	// Use protobuf's efficient serialization
	data, err := proto.Marshal(e)
	if err != nil {
		atomic.AddUint64(&serializeErrors, 1)
		return nil, fmt.Errorf("failed to serialize event: %w", err)
	}

	atomic.AddUint64(&totalEventSize, uint64(len(data)))
	return data, nil
}

// DeserializeFast performs efficient deserialization
func DeserializeFast(data []byte) (*UnifiedEvent, error) {
	event := NewEvent()
	if err := proto.Unmarshal(data, event); err != nil {
		ReleaseEvent(event)
		return nil, fmt.Errorf("failed to deserialize event: %w", err)
	}
	return event, nil
}

// Zero-copy operations for performance

// GetStringAttribute returns a string attribute without allocation
func (e *UnifiedEvent) GetStringAttribute(key string) (string, bool) {
	if attr, ok := e.Attributes[key]; ok {
		if sv, ok := attr.Value.(*AttributeValue_StringValue); ok {
			return sv.StringValue, true
		}
	}
	return "", false
}

// GetIntAttribute returns an int attribute
func (e *UnifiedEvent) GetIntAttribute(key string) (int64, bool) {
	if attr, ok := e.Attributes[key]; ok {
		if iv, ok := attr.Value.(*AttributeValue_IntValue); ok {
			return iv.IntValue, true
		}
	}
	return 0, false
}

// Performance monitoring

// EventStats returns current event pool statistics
type EventStats struct {
	Created   uint64
	Released  uint64
	InFlight  uint64
	TotalSize uint64
	SerErrors uint64
}

// GetEventStats returns current statistics
func GetEventStats() EventStats {
	return EventStats{
		Created:   atomic.LoadUint64(&eventsCreated),
		Released:  atomic.LoadUint64(&eventsReleased),
		InFlight:  atomic.LoadUint64(&eventsInFlight),
		TotalSize: atomic.LoadUint64(&totalEventSize),
		SerErrors: atomic.LoadUint64(&serializeErrors),
	}
}

// Helper functions

func generateEventID() string {
	return uuid.New().String()
}

func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	default:
		return 0
	}
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float32:
		return float64(val)
	case float64:
		return val
	default:
		return 0
	}
}

// IsHighPriority returns true if the event should be processed with priority
func (e *UnifiedEvent) IsHighPriority() bool {
	return e.Metadata.Priority > 5 ||
		e.Metadata.Severity >= EventSeverity_SEVERITY_ERROR ||
		e.Metadata.Category == EventCategory_CATEGORY_SECURITY
}

// Size returns the approximate size of the event in bytes
func (e *UnifiedEvent) Size() int {
	// This is an approximation for performance
	size := int(unsafe.Sizeof(*e))
	size += len(e.Id)

	if e.Metadata != nil {
		size += len(e.Metadata.Type) + len(e.Metadata.SchemaVersion)
	}

	if e.Source != nil {
		size += len(e.Source.Type) + len(e.Source.Collector) + len(e.Source.Node)
	}

	// Add attribute sizes
	for k, v := range e.Attributes {
		size += len(k) + v.Size()
	}

	// Add label sizes
	for k, v := range e.Labels {
		size += len(k) + len(v)
	}

	return size
}

// Size returns the approximate size of an attribute value
func (av *AttributeValue) Size() int {
	switch v := av.Value.(type) {
	case *AttributeValue_StringValue:
		return len(v.StringValue)
	case *AttributeValue_BytesValue:
		return len(v.BytesValue)
	default:
		return int(unsafe.Sizeof(av))
	}
}

// Validate checks if the event has required fields
func (e *UnifiedEvent) Validate() error {
	if e.Id == "" {
		return fmt.Errorf("event ID is required")
	}

	if e.Timestamp == nil {
		return fmt.Errorf("event timestamp is required")
	}

	if e.Metadata == nil || e.Metadata.Type == "" {
		return fmt.Errorf("event type is required")
	}

	if e.Source == nil || e.Source.Type == "" {
		return fmt.Errorf("event source type is required")
	}

	return nil
}

// Clone creates a deep copy of the event
func (e *UnifiedEvent) Clone() *UnifiedEvent {
	data, err := proto.Marshal(e)
	if err != nil {
		return nil
	}

	clone := NewEvent()
	if err := proto.Unmarshal(data, clone); err != nil {
		ReleaseEvent(clone)
		return nil
	}

	return clone
}

// Context support for event processing

type eventContextKey struct{}

// WithEvent adds an event to the context
func WithEvent(ctx context.Context, event *UnifiedEvent) context.Context {
	return context.WithValue(ctx, eventContextKey{}, event)
}

// EventFromContext retrieves an event from context
func EventFromContext(ctx context.Context) (*UnifiedEvent, bool) {
	event, ok := ctx.Value(eventContextKey{}).(*UnifiedEvent)
	return event, ok
}

// BatchBuilder helps construct event batches efficiently
type BatchBuilder struct {
	batch     *EventBatch
	sizeLimit int
	maxEvents int
}

// NewBatchBuilder creates a new batch builder
func NewBatchBuilder() *BatchBuilder {
	return &BatchBuilder{
		batch: &EventBatch{
			BatchId:   uuid.New().String(),
			CreatedAt: timestamppb.Now(),
			Events:    make([]*UnifiedEvent, 0, 100), // Pre-allocate for efficiency
		},
		sizeLimit: 1024 * 1024, // 1MB default
		maxEvents: 1000,
	}
}

// Add adds an event to the batch
func (bb *BatchBuilder) Add(event *UnifiedEvent) error {
	if len(bb.batch.Events) >= bb.maxEvents {
		return fmt.Errorf("batch is full")
	}

	bb.batch.Events = append(bb.batch.Events, event)
	return nil
}

// Build returns the constructed batch
func (bb *BatchBuilder) Build() *EventBatch {
	return bb.batch
}

// Size returns the current batch size
func (bb *BatchBuilder) Size() int {
	return len(bb.batch.Events)
}

// Reset clears the batch builder for reuse
func (bb *BatchBuilder) Reset() {
	bb.batch.Events = bb.batch.Events[:0]
	bb.batch.BatchId = uuid.New().String()
	bb.batch.CreatedAt = timestamppb.Now()
	bb.batch.DroppedEvents = 0
}
