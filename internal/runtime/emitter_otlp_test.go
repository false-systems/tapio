package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RED: Test OTLP emitter creation
func TestNewOTLPEmitter(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4317")
	require.NoError(t, err)
	require.NotNil(t, emitter)
	defer emitter.Close()

	assert.Equal(t, "otlp", emitter.Name())
	assert.True(t, emitter.IsCritical()) // OTLP should be critical by default
}

// RED: Test OTLP emitter with invalid endpoint fails
func TestNewOTLPEmitter_InvalidEndpoint(t *testing.T) {
	emitter, err := NewOTLPEmitter("")
	assert.Error(t, err)
	assert.Nil(t, emitter)
	assert.Contains(t, err.Error(), "endpoint")
}

// RED: Test OTLP emitter emits event
func TestOTLPEmitter_Emit(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4317")
	require.NoError(t, err)
	defer emitter.Close()

	event := &domain.ObserverEvent{
		Type:      "network",
		Subtype:   "dns_query",
		Timestamp: time.Now(),
		NetworkData: &domain.NetworkEventData{
			Protocol: "DNS",
			SrcIP:    "10.0.0.1",
			DstIP:    "8.8.8.8",
			SrcPort:  12345,
			DstPort:  53,
		},
	}

	ctx := context.Background()
	err = emitter.Emit(ctx, event)
	// May fail if no OTLP collector running, but should not panic
	// In tests, we'll use a mock collector or skip if not available
	_ = err // Ignore: Test validates no panic, not error value
}

// RED: Test OTLP emitter respects context cancellation
func TestOTLPEmitter_Emit_ContextCancelled(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4317")
	require.NoError(t, err)
	defer emitter.Close()

	event := &domain.ObserverEvent{
		Type:    "test",
		Subtype: "event",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = emitter.Emit(ctx, event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

// RED: Test OTLP emitter handles nil event
func TestOTLPEmitter_Emit_NilEvent(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4317")
	require.NoError(t, err)
	defer emitter.Close()

	ctx := context.Background()
	err = emitter.Emit(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// RED: Test OTLP emitter close is idempotent
func TestOTLPEmitter_Close_Idempotent(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4317")
	require.NoError(t, err)

	// Close twice should not panic
	err = emitter.Close()
	assert.NoError(t, err)

	err = emitter.Close()
	assert.NoError(t, err) // Second close should also succeed
}

// RED: Test OTLP emitter with custom options
func TestNewOTLPEmitter_WithOptions(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4317",
		WithInsecure(true),
		WithTimeout(5*time.Second),
		WithBatchSize(100),
	)
	require.NoError(t, err)
	require.NotNil(t, emitter)
	defer emitter.Close()
}

// RED: Test OTLP emitter batching
func TestOTLPEmitter_Batching(t *testing.T) {
	emitter, err := NewOTLPEmitter("localhost:4317", WithBatchSize(5))
	require.NoError(t, err)
	defer emitter.Close()

	ctx := context.Background()

	// Send 10 events - should batch into 2 batches of 5
	for i := 0; i < 10; i++ {
		event := &domain.ObserverEvent{
			Type:    "test",
			Subtype: "batch",
		}
		_ = emitter.Emit(ctx, event) // Ignore: Test batching, not error handling
	}

	// Flush remaining
	err = emitter.Close()
	assert.NoError(t, err)
}

// RED: Test OTLP emitter with headers
func TestNewOTLPEmitter_WithHeaders(t *testing.T) {
	headers := map[string]string{
		"X-API-Key": "secret",
		"X-Tenant":  "test",
	}

	emitter, err := NewOTLPEmitter("localhost:4317", WithHeaders(headers))
	require.NoError(t, err)
	require.NotNil(t, emitter)
	defer emitter.Close()
}
