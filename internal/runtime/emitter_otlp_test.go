package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RED: Test basic OTLP emitter creation and emission
func TestOTLPEmitter_BasicEmit(t *testing.T) {
	// Create OTLP emitter (this will fail - doesn't exist yet)
	emitter, err := NewOTLPEmitter("localhost:4317", true) // insecure=true for test
	require.NoError(t, err, "NewOTLPEmitter should succeed")
	require.NotNil(t, emitter, "emitter should not be nil")
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	// Create a basic domain event
	event := &domain.ObserverEvent{
		ID:        "test-123",
		Type:      string(domain.EventTypeNetwork),
		Subtype:   "connection_established",
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	// Emit the event
	ctx := context.Background()
	err = emitter.Emit(ctx, event)
	require.NoError(t, err, "Emit should succeed")
}

// RED: Test OTLP emitter implements Emitter interface
func TestOTLPEmitter_Interface(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4317", true)
	require.NoError(t, err)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	// Verify interface methods
	assert.Equal(t, "otlp", emitter.Name())
	assert.True(t, emitter.IsCritical(), "OTLP emitter should be critical")
}

// RED: Test OTLP emitter preserves trace context
func TestOTLPEmitter_TraceContext(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4317", true)
	require.NoError(t, err)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	// Event with trace context
	event := &domain.ObserverEvent{
		ID:         "test-456",
		Type:       string(domain.EventTypeNetwork),
		Subtype:    "dns_query",
		Source:     "network-observer",
		Timestamp:  time.Now(),
		TraceID:    "0123456789abcdef0123456789abcdef", // 32 hex chars
		SpanID:     "0123456789abcdef",                 // 16 hex chars
		TraceFlags: 0x01,                               // Sampled
	}

	ctx := context.Background()
	err = emitter.Emit(ctx, event)
	require.NoError(t, err, "Emit with trace context should succeed")
}

// RED: Test OTLP emitter respects context cancellation
func TestOTLPEmitter_ContextCancellation(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4317", true)
	require.NoError(t, err)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	event := &domain.ObserverEvent{
		ID:        "test-789",
		Type:      string(domain.EventTypeNetwork),
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Emit should fail fast due to cancelled context
	err = emitter.Emit(ctx, event)
	assert.Error(t, err, "Emit should fail with cancelled context")
}

// RED: Test OTLP emitter Close() cleans up resources
func TestOTLPEmitter_Close(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4317", true)
	require.NoError(t, err)

	// Close should succeed
	err = emitter.Close()
	require.NoError(t, err, "Close should succeed")

	// Multiple Close() calls should be safe
	err = emitter.Close()
	assert.NoError(t, err, "Multiple Close() calls should be safe")
}

// RED: Test OTLP emitter exports NetworkData as structured attributes
func TestOTLPEmitter_NetworkDataAttributes(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4318", true) // HTTP uses 4318
	require.NoError(t, err)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	event := &domain.ObserverEvent{
		ID:        "net-123",
		Type:      string(domain.EventTypeNetwork),
		Subtype:   "connection_established",
		Source:    "network-observer",
		Timestamp: time.Now(),
		NetworkData: &domain.NetworkEventData{
			SrcIP:    "10.0.0.1",
			DstIP:    "10.0.0.2",
			SrcPort:  54321,
			DstPort:  443,
			Protocol: "TCP",
		},
	}

	ctx := context.Background()
	err = emitter.Emit(ctx, event)
	require.NoError(t, err, "Emit with NetworkData should succeed")
}

// RED: Test OTLP emitter exports SchedulingData as structured attributes
func TestOTLPEmitter_SchedulingDataAttributes(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4318", true)
	require.NoError(t, err)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	event := &domain.ObserverEvent{
		ID:        "sched-456",
		Type:      string(domain.EventTypePod),
		Subtype:   "pod_scheduled",
		Source:    "scheduling-observer",
		Timestamp: time.Now(),
		SchedulingData: &domain.SchedulingEventData{
			PodUID:         "abc-123-def",
			Attempts:       2,
			NodesFailed:    3,
			NodesTotal:     5,
			FailureReasons: map[string]int{"InsufficientCPU": 2, "Taints": 1},
		},
	}

	ctx := context.Background()
	err = emitter.Emit(ctx, event)
	require.NoError(t, err, "Emit with SchedulingData should succeed")
}

// RED: Test OTLP emitter uses HTTP transport (not gRPC)
func TestOTLPEmitter_HTTPTransport(t *testing.T) {
	// HTTP endpoint uses port 4318 (not 4317 for gRPC)
	emitter, err := NewOTLPEmitter("localhost:4318", true)
	require.NoError(t, err)
	defer func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	}()

	event := &domain.ObserverEvent{
		ID:        "http-789",
		Type:      string(domain.EventTypeNetwork),
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err = emitter.Emit(ctx, event)
	require.NoError(t, err, "HTTP transport should work")
}
