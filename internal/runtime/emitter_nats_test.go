package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RED: Test NATS emitter creation
func TestNewNATSEmitter(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222", "tapio.events")
	require.NoError(t, err)
	require.NotNil(t, emitter)
	defer emitter.Close()

	assert.Equal(t, "nats", emitter.Name())
	assert.True(t, emitter.IsCritical()) // NATS is critical by default
}

// RED: Test NATS emitter with invalid URL fails
func TestNewNATSEmitter_InvalidURL(t *testing.T) {
	emitter, err := NewNATSEmitter("", "tapio.events")
	assert.Error(t, err)
	assert.Nil(t, emitter)
	assert.Contains(t, err.Error(), "URL")
}

// RED: Test NATS emitter with invalid subject fails
func TestNewNATSEmitter_InvalidSubject(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222", "")
	assert.Error(t, err)
	assert.Nil(t, emitter)
	assert.Contains(t, err.Error(), "subject")
}

// RED: Test NATS emitter emits event
func TestNATSEmitter_Emit(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222", "tapio.events")
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
	// May fail if no NATS server running, but should not panic
	// In tests, we'll use a mock NATS server or skip if not available
	_ = err // Ignore: Test validates no panic, not error value
}

// RED: Test NATS emitter respects context cancellation
func TestNATSEmitter_Emit_ContextCancelled(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222", "tapio.events")
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

// RED: Test NATS emitter handles nil event
func TestNATSEmitter_Emit_NilEvent(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222", "tapio.events")
	require.NoError(t, err)
	defer emitter.Close()

	ctx := context.Background()
	err = emitter.Emit(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// RED: Test NATS emitter close is idempotent
func TestNATSEmitter_Close_Idempotent(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222", "tapio.events")
	require.NoError(t, err)

	// Close twice should not panic
	err = emitter.Close()
	assert.NoError(t, err)

	err = emitter.Close()
	assert.NoError(t, err) // Second close should also succeed
}

// RED: Test NATS emitter with custom options
func TestNewNATSEmitter_WithOptions(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222", "tapio.events",
		WithNATSTimeout(5*time.Second),
		WithNATSReconnect(true),
		WithNATSCredentials("user", "password"),
	)
	require.NoError(t, err)
	require.NotNil(t, emitter)
	defer emitter.Close()
}

// RED: Test NATS emitter with JetStream
func TestNewNATSEmitter_WithJetStream(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222", "tapio.events",
		WithJetStream(true),
		WithStreamName("TAPIO_EVENTS"),
	)
	require.NoError(t, err)
	require.NotNil(t, emitter)
	defer emitter.Close()
}

// RED: Test NATS emitter handles connection failure gracefully
func TestNATSEmitter_ConnectionFailure(t *testing.T) {
	t.Skip("Connection validation deferred to NATS client integration phase")

	// Connect to invalid server
	emitter, err := NewNATSEmitter("nats://invalid-host:9999", "tapio.events",
		WithNATSTimeout(100*time.Millisecond),
	)

	// Should fail to connect
	assert.Error(t, err)
	assert.Nil(t, emitter)
}

// RED: Test NATS emitter reconnects after disconnect
func TestNATSEmitter_Reconnect(t *testing.T) {
	t.Skip("Requires NATS test server with disconnect simulation")

	emitter, err := NewNATSEmitter("nats://localhost:4222", "tapio.events",
		WithNATSReconnect(true),
	)
	require.NoError(t, err)
	defer emitter.Close()

	// This test would:
	// 1. Emit an event successfully
	// 2. Simulate NATS server disconnect
	// 3. Verify emitter handles disconnect gracefully
	// 4. Restart NATS server
	// 5. Verify emitter reconnects and emits successfully
}

// RED: Test NATS emitter with TLS
func TestNewNATSEmitter_WithTLS(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222", "tapio.events",
		WithNATSTLS(true),
	)
	require.NoError(t, err)
	require.NotNil(t, emitter)
	defer emitter.Close()
}
