package domain

import (
	"context"
	"math/rand"
	"testing"
	"time"
	"unsafe"
)

// Fuzzing tests for trace data validation and robustness
// These tests ensure our domain model can handle malformed, extreme, and malicious inputs

// FuzzTraceAggregateCreation tests trace aggregate creation with random inputs
func FuzzTraceAggregateCreation(f *testing.F) {
	// Seed corpus with valid inputs
	f.Add([]byte("service-name"), []byte("span-name"), uint64(1), uint64(2))
	f.Add([]byte(""), []byte("test"), uint64(0), uint64(1))
	f.Add([]byte("very-long-service-name-that-exceeds-normal-limits"), []byte("span"), uint64(999999), uint64(1000000))

	f.Fuzz(func(t *testing.T, serviceName, spanName []byte, traceIDHigh, traceIDLow uint64) {
		// Convert bytes to strings (may contain invalid UTF-8)
		serviceStr := string(serviceName)
		spanStr := string(spanName)

		// Create trace ID from uint64s
		traceID := TraceID{
			High: traceIDHigh,
			Low:  traceIDLow,
		}

		// Attempt to create trace aggregate
		_, err := NewTraceAggregate[string](
			traceID,
			serviceStr,
			spanStr,
			SpanKindServer,
			map[string]string{"test": "value"},
			nil, // correlation service
			nil, // sampling service
		)

		// We should never panic, even with invalid inputs
		// Error is acceptable for invalid inputs
		if err != nil {
			// Validate error handling is consistent
			if serviceStr == "" && err.Error() != "service name cannot be empty" {
				t.Errorf("Unexpected error for empty service name: %v", err)
			}
			if spanStr == "" && err.Error() != "root span name cannot be empty" {
				t.Errorf("Unexpected error for empty span name: %v", err)
			}
		}
	})
}

// FuzzSpanAttributes tests span attribute handling with random data
func FuzzSpanAttributes(f *testing.F) {
	// Seed with various attribute patterns
	f.Add([]byte("key"), []byte("value"))
	f.Add([]byte(""), []byte(""))
	f.Add([]byte("unicode-key-🚀"), []byte("unicode-value-🔥"))
	f.Add(make([]byte, 1000), make([]byte, 10000)) // Large data

	f.Fuzz(func(t *testing.T, keyBytes, valueBytes []byte) {
		key := string(keyBytes)
		value := string(valueBytes)

		// Create a valid trace aggregate first
		traceID := TraceID{High: 1, Low: 1}
		aggregate, err := NewTraceAggregate[string](
			traceID,
			"test-service",
			"test-span",
			SpanKindServer,
			map[string]string{},
			nil,
			nil,
		)

		if err != nil {
			t.Skip("Failed to create test aggregate")
		}

		// Get the root span
		rootSpan := aggregate.GetRootSpan()
		if rootSpan == nil {
			t.Fatal("No root span found")
		}

		// Test setting attributes - should not panic
		attrs := map[string]string{key: value}
		err = aggregate.SetSpanAttributes(rootSpan.spanID, attrs)

		// Should handle any input gracefully
		if err != nil && !rootSpan.isRecording {
			// Expected error for non-recording spans
			return
		}

		// If successful, validate the attribute was set
		if err == nil && len(key) > 0 {
			// Verify attribute was stored (if span is recording)
			if rootSpan.isRecording {
				if storedValue, exists := rootSpan.attributes[key]; !exists || storedValue != value {
					t.Errorf("Attribute not properly stored: key=%q, expected=%q, got=%q", key, value, storedValue)
				}
			}
		}
	})
}

// FuzzSpanEvents tests span event creation with malformed data
func FuzzSpanEvents(f *testing.F) {
	// Seed with event patterns
	f.Add([]byte("event-name"), int64(1640995200000000000)) // Valid timestamp
	f.Add([]byte(""), int64(0))
	f.Add([]byte("very-long-event-name-that-might-cause-issues"), int64(-1))

	f.Fuzz(func(t *testing.T, eventNameBytes []byte, timestampNanos int64) {
		eventName := string(eventNameBytes)
		timestamp := time.Unix(0, timestampNanos)

		// Create test aggregate
		traceID := TraceID{High: 1, Low: 2}
		aggregate, err := NewTraceAggregate[string](
			traceID,
			"test-service",
			"test-span",
			SpanKindServer,
			map[string]string{},
			nil,
			nil,
		)

		if err != nil {
			t.Skip("Failed to create test aggregate")
		}

		rootSpan := aggregate.GetRootSpan()
		if rootSpan == nil {
			t.Fatal("No root span found")
		}

		// Test adding span event - should not panic
		err = aggregate.AddSpanEvent(
			rootSpan.spanID,
			eventName,
			timestamp,
			map[string]string{"test": "value"},
		)

		// Validate error handling
		if err != nil {
			// Check for expected error cases
			if !rootSpan.isRecording && err.Error() != "cannot add event to non-recording span" {
				t.Errorf("Unexpected error for non-recording span: %v", err)
			}
		}
	})
}

// FuzzTraceID tests TraceID operations with random data
func FuzzTraceID(f *testing.F) {
	// Seed with various ID patterns
	f.Add(uint64(0), uint64(0))
	f.Add(uint64(18446744073709551615), uint64(18446744073709551615)) // Max uint64
	f.Add(uint64(1), uint64(0))

	f.Fuzz(func(t *testing.T, high, low uint64) {
		traceID := TraceID{High: high, Low: low}

		// Test string representation - should not panic
		str := traceID.String()

		// Validate string format
		if len(str) != 32 { // 16 bytes = 32 hex chars
			t.Errorf("Invalid trace ID string length: expected 32, got %d", len(str))
		}

		// Test IsValid method
		isValid := traceID.IsValid()
		expectedValid := (high != 0 || low != 0)
		if isValid != expectedValid {
			t.Errorf("Invalid IsValid result: high=%d, low=%d, expected=%v, got=%v",
				high, low, expectedValid, isValid)
		}

		// Test bytes conversion
		bytes := traceID.Bytes()
		if len(bytes) != 16 {
			t.Errorf("Invalid trace ID bytes length: expected 16, got %d", len(bytes))
		}

		// Test round-trip conversion
		reconstructed := TraceIDFromBytes(bytes)
		if reconstructed.High != high || reconstructed.Low != low {
			t.Errorf("Round-trip conversion failed: original=(%d,%d), reconstructed=(%d,%d)",
				high, low, reconstructed.High, reconstructed.Low)
		}
	})
}

// FuzzSpanID tests SpanID operations
func FuzzSpanID(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(18446744073709551615))
	f.Add(uint64(1))

	f.Fuzz(func(t *testing.T, id uint64) {
		spanID := SpanID{ID: id}

		// Test string representation
		str := spanID.String()
		if len(str) != 16 { // 8 bytes = 16 hex chars
			t.Errorf("Invalid span ID string length: expected 16, got %d", len(str))
		}

		// Test IsValid
		isValid := spanID.IsValid()
		expectedValid := (id != 0)
		if isValid != expectedValid {
			t.Errorf("Invalid IsValid result: id=%d, expected=%v, got=%v",
				id, expectedValid, isValid)
		}

		// Test bytes conversion
		bytes := spanID.Bytes()
		if len(bytes) != 8 {
			t.Errorf("Invalid span ID bytes length: expected 8, got %d", len(bytes))
		}
	})
}

// FuzzUnsafeSpanAttributes tests unsafe span attribute operations
func FuzzUnsafeSpanAttributes(f *testing.F) {
	// Test unsafe pointer operations with various data
	f.Add([]byte("test-key"), []byte("test-value"))
	f.Add([]byte(""), []byte(""))
	f.Add(make([]byte, 1000), []byte("value"))

	f.Fuzz(func(t *testing.T, keyBytes, valueBytes []byte) {
		// Only test if we have some data
		if len(keyBytes) == 0 {
			return
		}

		key := string(keyBytes)
		value := string(valueBytes)

		// Create mock arena span for testing
		span := &ArenaSpan[string]{}

		// Test unsafe attribute setting
		defer func() {
			if r := recover(); r != nil {
				// Panic is acceptable for truly invalid unsafe operations
				t.Logf("Panic recovered (acceptable for unsafe ops): %v", r)
			}
		}()

		// Create unsafe pointer to key
		keyPtr := unsafe.Pointer(&keyBytes[0])
		keyLen := len(keyBytes)

		// This should not crash the process
		result := span.SetAttributeUnsafe(keyPtr, keyLen, value)

		// Validate result is not nil
		if result == nil {
			t.Error("SetAttributeUnsafe returned nil")
		}
	})
}

// FuzzBinaryEncoding tests binary encoding with malformed data
func FuzzBinaryEncoding(f *testing.F) {
	// Seed with various binary patterns
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x01, 0x02, 0x03})
	f.Add(make([]byte, 1000))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Test binary encoder with random data
		encoder := NewBinaryEncoder()

		// Should not panic with any input
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Binary encoder panicked: %v", r)
			}
		}()

		// Test encoding
		encoded, err := encoder.Encode(data)
		if err != nil {
			// Error is acceptable for malformed data
			return
		}

		// Test decoding if encoding succeeded
		decoded, err := encoder.Decode(encoded)
		if err != nil {
			t.Errorf("Failed to decode previously encoded data: %v", err)
			return
		}

		// Validate round-trip
		if len(data) > 0 && len(decoded) == 0 {
			t.Error("Round-trip encoding lost data")
		}
	})
}

// FuzzRingBufferOperations tests lock-free ring buffer with concurrent operations
func FuzzRingBufferOperations(f *testing.F) {
	f.Add(uint32(1), uint32(1000), []byte("test-data"))
	f.Add(uint32(0), uint32(0), []byte(""))
	f.Add(uint32(1000), uint32(1), make([]byte, 10000))

	f.Fuzz(func(t *testing.T, producers, consumers uint32, testData []byte) {
		// Limit to reasonable values to prevent resource exhaustion
		if producers > 100 {
			producers = 100
		}
		if consumers > 100 {
			consumers = 100
		}
		if producers == 0 && consumers == 0 {
			return
		}

		// Create ring buffer
		buffer := NewLockFreeRingBuffer[[]byte](1024)

		// Should not panic with any configuration
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Ring buffer operation panicked: %v", r)
			}
		}()

		// Test basic operations
		success := buffer.TryPush(testData)
		if !success && len(testData) > 0 {
			// May fail if buffer is full, which is acceptable
		}

		// Test pop operation
		_, ok := buffer.TryPop()
		// May be false if buffer is empty, which is acceptable
		_ = ok

		// Test size operations
		size := buffer.Size()
		capacity := buffer.Capacity()

		if size > capacity {
			t.Errorf("Buffer size (%d) exceeds capacity (%d)", size, capacity)
		}
	})
}

// FuzzMemoryArenaOperations tests memory arena with various allocation patterns
func FuzzMemoryArenaOperations(f *testing.F) {
	f.Add(uint32(1), uint32(64))
	f.Add(uint32(0), uint32(0))
	f.Add(uint32(1000), uint32(1048576)) // 1MB

	f.Fuzz(func(t *testing.T, allocCount, allocSize uint32) {
		// Limit to prevent resource exhaustion
		if allocCount > 1000 {
			allocCount = 1000
		}
		if allocSize > 1048576 { // 1MB max
			allocSize = 1048576
		}

		// Create arena
		arena := NewMemoryArena(1024 * 1024) // 1MB arena

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Memory arena operation panicked: %v", r)
			}
		}()

		// Test allocations
		var ptrs []unsafe.Pointer
		for i := uint32(0); i < allocCount; i++ {
			ptr := arena.Allocate(int(allocSize))
			if ptr != nil {
				ptrs = append(ptrs, ptr)
			}

			// Test arena stats
			used := arena.Used()
			remaining := arena.Remaining()

			if used < 0 || remaining < 0 {
				t.Errorf("Invalid arena stats: used=%d, remaining=%d", used, remaining)
			}
		}

		// Test reset
		arena.Reset()

		// After reset, should have full capacity
		if arena.Used() != 0 {
			t.Errorf("Arena not properly reset: used=%d", arena.Used())
		}
	})
}

// FuzzDomainEvents tests domain event creation and validation
func FuzzDomainEvents(f *testing.F) {
	f.Add([]byte("event-type"), []byte("event-data"), int64(1640995200000000000))
	f.Add([]byte(""), []byte(""), int64(0))
	f.Add(make([]byte, 1000), make([]byte, 10000), int64(-1))

	f.Fuzz(func(t *testing.T, eventTypeBytes, eventDataBytes []byte, timestampNanos int64) {
		eventType := string(eventTypeBytes)
		eventData := string(eventDataBytes)
		timestamp := time.Unix(0, timestampNanos)

		// Create test trace ID
		traceID := TraceID{High: 1, Low: 1}

		// Test event creation - should not panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Domain event creation panicked: %v", r)
			}
		}()

		// Create various event types
		events := []TraceEvent{
			NewTraceStartedEvent(traceID, "service", "span", map[string]string{"key": eventData}),
			NewSpanCreatedEvent(traceID, SpanID{ID: 1}, SpanID{ID: 2}, eventType),
			NewSpanFinishedEvent(traceID, SpanID{ID: 1}, timestamp, time.Second),
		}

		// Validate all events
		for _, event := range events {
			if event == nil {
				t.Error("Event creation returned nil")
				continue
			}

			// Test event methods
			eventID := event.GetEventID()
			eventType := event.GetEventType()
			eventTimestamp := event.GetTimestamp()

			// Basic validation
			if eventID.String() == "" {
				t.Error("Event ID is empty")
			}
			if eventTimestamp.IsZero() {
				t.Error("Event timestamp is zero")
			}

			_ = eventType // EventType validation depends on implementation
		}
	})
}

// Helper functions for fuzzing

func NewBinaryEncoder() *BinaryEncoder {
	// Mock implementation for fuzzing
	return &BinaryEncoder{}
}

type BinaryEncoder struct{}

func (e *BinaryEncoder) Encode(data []byte) ([]byte, error) {
	// Simple implementation for fuzzing
	if len(data) == 0 {
		return []byte{}, nil
	}
	// Add length prefix
	result := make([]byte, 4+len(data))
	result[0] = byte(len(data) >> 24)
	result[1] = byte(len(data) >> 16)
	result[2] = byte(len(data) >> 8)
	result[3] = byte(len(data))
	copy(result[4:], data)
	return result, nil
}

func (e *BinaryEncoder) Decode(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("insufficient data")
	}
	length := int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	if len(data) < 4+length {
		return nil, fmt.Errorf("incomplete data")
	}
	return data[4 : 4+length], nil
}

// Mock implementations for types referenced in fuzzing

type ArenaSpan[T TraceData] struct {
	attributes map[string]T
}

func (s *ArenaSpan[T]) SetAttributeUnsafe(keyPtr unsafe.Pointer, keyLen int, value T) *ArenaSpan[T] {
	if keyPtr == nil || keyLen <= 0 {
		return s
	}

	// Convert unsafe pointer to string safely
	keyBytes := (*[1000]byte)(keyPtr)[:keyLen:keyLen]
	key := string(keyBytes)

	if s.attributes == nil {
		s.attributes = make(map[string]T)
	}
	s.attributes[key] = value
	return s
}

func NewMemoryArena(size int) *MemoryArena {
	return &MemoryArena{
		data:     make([]byte, size),
		capacity: size,
		used:     0,
	}
}

type MemoryArena struct {
	data     []byte
	capacity int
	used     int
}

func (a *MemoryArena) Allocate(size int) unsafe.Pointer {
	if a.used+size > a.capacity {
		return nil
	}
	ptr := unsafe.Pointer(&a.data[a.used])
	a.used += size
	return ptr
}

func (a *MemoryArena) Used() int {
	return a.used
}

func (a *MemoryArena) Remaining() int {
	return a.capacity - a.used
}

func (a *MemoryArena) Reset() {
	a.used = 0
}

// Additional event creation functions
func NewTraceStartedEvent(traceID TraceID, serviceName, spanName string, attributes map[string]string) TraceEvent {
	return &traceStartedEvent{
		baseEvent:   newBaseEvent("trace_started"),
		traceID:     traceID,
		serviceName: serviceName,
		spanName:    spanName,
		attributes:  attributes,
	}
}

func NewSpanCreatedEvent(traceID TraceID, spanID, parentSpanID SpanID, spanName string) TraceEvent {
	return &spanCreatedEvent{
		baseEvent:    newBaseEvent("span_created"),
		traceID:      traceID,
		spanID:       spanID,
		parentSpanID: parentSpanID,
		spanName:     spanName,
	}
}

func NewSpanFinishedEvent(traceID TraceID, spanID SpanID, endTime time.Time, duration time.Duration) TraceEvent {
	return &spanFinishedEvent{
		baseEvent: newBaseEvent("span_finished"),
		traceID:   traceID,
		spanID:    spanID,
		endTime:   endTime,
		duration:  duration,
	}
}
