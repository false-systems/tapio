package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEvent(t *testing.T) {
	event := NewEvent()
	
	assert.NotEmpty(t, event.Id, "Event ID should be generated")
	assert.NotNil(t, event.Timestamp, "Timestamp should be set")
	assert.NotNil(t, event.Metadata, "Metadata should be initialized")
	assert.NotNil(t, event.Source, "Source should be initialized")
	assert.NotNil(t, event.Entity, "Entity should be initialized")
	assert.NotNil(t, event.Correlation, "Correlation should be initialized")
	assert.NotNil(t, event.Quality, "Quality should be initialized")
	assert.Equal(t, float32(1.0), event.Quality.Confidence, "Default confidence should be 1.0")
	
	// Test pool statistics
	stats := GetEventStats()
	assert.Greater(t, stats.Created, uint64(0), "Created counter should increment")
	assert.Greater(t, stats.InFlight, uint64(0), "InFlight counter should increment")
	
	ReleaseEvent(event)
	
	// Test pool cleanup
	updatedStats := GetEventStats()
	assert.Greater(t, updatedStats.Released, stats.Released, "Released counter should increment")
}

func TestEventBuilder(t *testing.T) {
	builder := NewBuilder()
	
	event := builder.
		WithType("network.connection", EventCategory_CATEGORY_NETWORK).
		WithSeverity(EventSeverity_SEVERITY_INFO).
		WithSource("ebpf", "network-collector", "node-1").
		WithEntity(EntityType_ENTITY_PROCESS, "123", "nginx").
		WithProcess(1234, "nginx").
		WithContainer("container-123", "nginx-container", "nginx:latest").
		WithAttribute("bytes_sent", int64(1024)).
		WithAttribute("duration_ms", float64(150.5)).
		WithAttribute("success", true).
		WithLabel("environment", "production").
		WithCorrelation("correlation-123", "trace-456").
		Build()
	
	// Validate event structure
	assert.Equal(t, "network.connection", event.Metadata.Type)
	assert.Equal(t, EventCategory_CATEGORY_NETWORK, event.Metadata.Category)
	assert.Equal(t, EventSeverity_SEVERITY_INFO, event.Metadata.Severity)
	
	assert.Equal(t, "ebpf", event.Source.Type)
	assert.Equal(t, "network-collector", event.Source.Collector)
	assert.Equal(t, "node-1", event.Source.Node)
	
	assert.Equal(t, EntityType_ENTITY_PROCESS, event.Entity.Type)
	assert.Equal(t, "123", event.Entity.Id)
	assert.Equal(t, "nginx", event.Entity.Name)
	
	assert.Equal(t, uint32(1234), event.Entity.Process.Pid)
	assert.Equal(t, "nginx", event.Entity.Process.Comm)
	
	assert.Equal(t, "container-123", event.Entity.Container.Id)
	assert.Equal(t, "nginx-container", event.Entity.Container.Name)
	assert.Equal(t, "nginx:latest", event.Entity.Container.Image)
	
	// Test attributes
	bytes, ok := event.GetIntAttribute("bytes_sent")
	assert.True(t, ok, "Should find bytes_sent attribute")
	assert.Equal(t, int64(1024), bytes)
	
	// Test labels
	env, exists := event.Labels["environment"]
	assert.True(t, exists, "Should find environment label")
	assert.Equal(t, "production", env)
	
	// Test correlation
	assert.Equal(t, "correlation-123", event.Correlation.CorrelationId)
	assert.Equal(t, "trace-456", event.Correlation.TraceId)
	
	ReleaseEvent(event)
}

func TestEventWithNetworkData(t *testing.T) {
	networkData := &NetworkEvent{
		Protocol:    "tcp",
		SrcIp:       "192.168.1.1",
		SrcPort:     8080,
		DstIp:       "192.168.1.2",
		DstPort:     80,
		BytesSent:   1024,
		BytesReceived: 2048,
		PacketsSent: 10,
		PacketsReceived: 15,
		LatencyNs:   1500000, // 1.5ms
		State:       "ESTABLISHED",
	}
	
	event := NewBuilder().
		WithType("network.connection", EventCategory_CATEGORY_NETWORK).
		WithNetworkData(networkData).
		Build()
	
	// Validate network data
	netEvent, ok := event.Data.(*UnifiedEvent_Network)
	assert.True(t, ok, "Should be a network event")
	
	net := netEvent.Network
	assert.Equal(t, "tcp", net.Protocol)
	assert.Equal(t, "192.168.1.1", net.SrcIp)
	assert.Equal(t, uint32(8080), net.SrcPort)
	assert.Equal(t, "192.168.1.2", net.DstIp)
	assert.Equal(t, uint32(80), net.DstPort)
	assert.Equal(t, uint64(1024), net.BytesSent)
	assert.Equal(t, uint64(2048), net.BytesReceived)
	
	ReleaseEvent(event)
}

func TestEventWithMemoryData(t *testing.T) {
	memoryData := &MemoryEvent{
		Operation:   "alloc",
		SizeBytes:   4096,
		Address:     0x7fff12345678,
		RssBytes:    1024 * 1024,
		VmsBytes:    2048 * 1024,
		SharedBytes: 512 * 1024,
		Allocator:   "malloc",
		StackTrace:  []string{"main", "allocate", "malloc"},
	}
	
	event := NewBuilder().
		WithType("memory.allocation", EventCategory_CATEGORY_MEMORY).
		WithMemoryData(memoryData).
		Build()
	
	// Validate memory data
	memEvent, ok := event.Data.(*UnifiedEvent_Memory)
	assert.True(t, ok, "Should be a memory event")
	
	mem := memEvent.Memory
	assert.Equal(t, "alloc", mem.Operation)
	assert.Equal(t, uint64(4096), mem.SizeBytes)
	assert.Equal(t, uint64(0x7fff12345678), mem.Address)
	assert.Equal(t, uint64(1024*1024), mem.RssBytes)
	assert.Equal(t, uint64(2048*1024), mem.VmsBytes)
	assert.Equal(t, "malloc", mem.Allocator)
	assert.Len(t, mem.StackTrace, 3)
	
	ReleaseEvent(event)
}

func TestEventSerialization(t *testing.T) {
	// Create a complex event
	event := NewBuilder().
		WithType("test.event", EventCategory_CATEGORY_APPLICATION).
		WithSeverity(EventSeverity_SEVERITY_WARNING).
		WithSource("test", "test-collector", "test-node").
		WithEntity(EntityType_ENTITY_CONTAINER, "container-123", "test-container").
		WithAttribute("key1", "value1").
		WithAttribute("key2", int64(42)).
		WithAttribute("key3", true).
		WithLabel("env", "test").
		Build()
	
	// Test fast serialization
	data, err := event.SerializeFast()
	require.NoError(t, err, "Serialization should succeed")
	assert.Greater(t, len(data), 0, "Serialized data should not be empty")
	
	// Test deserialization
	deserializedEvent, err := DeserializeFast(data)
	require.NoError(t, err, "Deserialization should succeed")
	
	// Validate deserialized data
	assert.Equal(t, event.Id, deserializedEvent.Id)
	assert.Equal(t, event.Metadata.Type, deserializedEvent.Metadata.Type)
	assert.Equal(t, event.Source.Type, deserializedEvent.Source.Type)
	assert.Equal(t, event.Entity.Id, deserializedEvent.Entity.Id)
	
	// Test attributes
	value, ok := deserializedEvent.GetStringAttribute("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", value)
	
	intValue, ok := deserializedEvent.GetIntAttribute("key2")
	assert.True(t, ok)
	assert.Equal(t, int64(42), intValue)
	
	ReleaseEvent(event)
	ReleaseEvent(deserializedEvent)
}

func TestEventValidation(t *testing.T) {
	// Test valid event
	validEvent := NewBuilder().
		WithType("test.valid", EventCategory_CATEGORY_APPLICATION).
		WithSource("test", "test-collector", "test-node").
		Build()
	
	err := validEvent.Validate()
	assert.NoError(t, err, "Valid event should pass validation")
	
	// Test invalid event (missing ID)
	invalidEvent := NewEvent()
	invalidEvent.Id = ""
	
	err = invalidEvent.Validate()
	assert.Error(t, err, "Event without ID should fail validation")
	assert.Contains(t, err.Error(), "event ID is required")
	
	// Test invalid event (missing type)
	invalidEvent2 := NewEvent()
	invalidEvent2.Metadata.Type = ""
	
	err = invalidEvent2.Validate()
	assert.Error(t, err, "Event without type should fail validation")
	
	ReleaseEvent(validEvent)
	ReleaseEvent(invalidEvent)
	ReleaseEvent(invalidEvent2)
}

func TestEventClone(t *testing.T) {
	original := NewBuilder().
		WithType("test.clone", EventCategory_CATEGORY_APPLICATION).
		WithSource("test", "test-collector", "test-node").
		WithAttribute("key1", "value1").
		WithLabel("env", "test").
		Build()
	
	clone := original.Clone()
	require.NotNil(t, clone, "Clone should not be nil")
	
	// Verify clone has same data
	assert.Equal(t, original.Id, clone.Id)
	assert.Equal(t, original.Metadata.Type, clone.Metadata.Type)
	assert.Equal(t, original.Source.Type, clone.Source.Type)
	
	// Verify they are separate objects
	assert.NotSame(t, original, clone, "Clone should be a different object")
	
	// Modify clone and verify original is unchanged
	clone.Metadata.Type = "modified.type"
	assert.NotEqual(t, original.Metadata.Type, clone.Metadata.Type)
	
	ReleaseEvent(original)
	ReleaseEvent(clone)
}

func TestEventSize(t *testing.T) {
	event := NewBuilder().
		WithType("test.size", EventCategory_CATEGORY_APPLICATION).
		WithSource("test", "test-collector", "test-node").
		WithAttribute("small", "x").
		WithAttribute("large", string(make([]byte, 1024))).
		WithLabel("env", "test").
		Build()
	
	size := event.Size()
	assert.Greater(t, size, 1000, "Event with large attribute should have significant size")
	
	ReleaseEvent(event)
}

func TestEventHighPriority(t *testing.T) {
	// Test high priority event
	highPriorityEvent := NewBuilder().
		WithType("test.priority", EventCategory_CATEGORY_SECURITY).
		WithSeverity(EventSeverity_SEVERITY_CRITICAL).
		Build()
	
	assert.True(t, highPriorityEvent.IsHighPriority(), "Security critical event should be high priority")
	
	// Test normal priority event
	normalEvent := NewBuilder().
		WithType("test.normal", EventCategory_CATEGORY_APPLICATION).
		WithSeverity(EventSeverity_SEVERITY_INFO).
		Build()
	
	assert.False(t, normalEvent.IsHighPriority(), "Info application event should not be high priority")
	
	ReleaseEvent(highPriorityEvent)
	ReleaseEvent(normalEvent)
}

func TestBatchBuilder(t *testing.T) {
	builder := NewBatchBuilder()
	
	// Add events to batch
	for i := 0; i < 5; i++ {
		event := NewBuilder().
			WithType("test.batch", EventCategory_CATEGORY_APPLICATION).
			WithAttribute("index", int64(i)).
			Build()
		
		err := builder.Add(event)
		assert.NoError(t, err, "Adding event to batch should succeed")
	}
	
	assert.Equal(t, 5, builder.Size(), "Batch should contain 5 events")
	
	batch := builder.Build()
	assert.NotNil(t, batch, "Batch should not be nil")
	assert.Len(t, batch.Events, 5, "Batch should contain 5 events")
	assert.NotEmpty(t, batch.BatchId, "Batch should have an ID")
	assert.NotNil(t, batch.CreatedAt, "Batch should have creation time")
	
	// Clean up events
	for _, event := range batch.Events {
		ReleaseEvent(event)
	}
}

func TestEventStats(t *testing.T) {
	initialStats := GetEventStats()
	
	// Create and release some events
	for i := 0; i < 10; i++ {
		event := NewEvent()
		ReleaseEvent(event)
	}
	
	finalStats := GetEventStats()
	
	assert.Equal(t, initialStats.Created+10, finalStats.Created, "Created count should increase by 10")
	assert.Equal(t, initialStats.Released+10, finalStats.Released, "Released count should increase by 10")
}

func TestAttributeTypes(t *testing.T) {
	event := NewBuilder().
		WithType("test.attributes", EventCategory_CATEGORY_APPLICATION).
		WithAttribute("string", "test").
		WithAttribute("int", int64(42)).
		WithAttribute("float", float64(3.14)).
		WithAttribute("bool", true).
		WithAttribute("bytes", []byte("test")).
		WithAttribute("time", time.Now()).
		Build()
	
	// Test string attribute
	strVal, ok := event.GetStringAttribute("string")
	assert.True(t, ok)
	assert.Equal(t, "test", strVal)
	
	// Test int attribute
	intVal, ok := event.GetIntAttribute("int")
	assert.True(t, ok)
	assert.Equal(t, int64(42), intVal)
	
	// Test missing attribute
	_, ok = event.GetStringAttribute("missing")
	assert.False(t, ok, "Missing attribute should return false")
	
	ReleaseEvent(event)
}

func BenchmarkEventCreation(b *testing.B) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		event := NewEvent()
		ReleaseEvent(event)
	}
}

func BenchmarkEventBuilder(b *testing.B) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		event := NewBuilder().
			WithType("benchmark.test", EventCategory_CATEGORY_APPLICATION).
			WithSeverity(EventSeverity_SEVERITY_INFO).
			WithSource("benchmark", "test-collector", "test-node").
			WithEntity(EntityType_ENTITY_PROCESS, "123", "test").
			WithAttribute("key1", "value1").
			WithAttribute("key2", int64(42)).
			WithLabel("env", "test").
			Build()
		
		ReleaseEvent(event)
	}
}

func BenchmarkEventSerialization(b *testing.B) {
	event := NewBuilder().
		WithType("benchmark.serialization", EventCategory_CATEGORY_APPLICATION).
		WithSeverity(EventSeverity_SEVERITY_INFO).
		WithSource("benchmark", "test-collector", "test-node").
		WithEntity(EntityType_ENTITY_PROCESS, "123", "test").
		WithAttribute("key1", "value1").
		WithAttribute("key2", int64(42)).
		WithLabel("env", "test").
		Build()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		data, err := event.SerializeFast()
		if err != nil {
			b.Fatal(err)
		}
		_ = data
	}
	
	ReleaseEvent(event)
}

func BenchmarkEventDeserialization(b *testing.B) {
	event := NewBuilder().
		WithType("benchmark.deserialization", EventCategory_CATEGORY_APPLICATION).
		WithSeverity(EventSeverity_SEVERITY_INFO).
		WithSource("benchmark", "test-collector", "test-node").
		WithEntity(EntityType_ENTITY_PROCESS, "123", "test").
		WithAttribute("key1", "value1").
		WithAttribute("key2", int64(42)).
		WithLabel("env", "test").
		Build()
	
	data, err := event.SerializeFast()
	if err != nil {
		b.Fatal(err)
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		deserializedEvent, err := DeserializeFast(data)
		if err != nil {
			b.Fatal(err)
		}
		ReleaseEvent(deserializedEvent)
	}
	
	ReleaseEvent(event)
}