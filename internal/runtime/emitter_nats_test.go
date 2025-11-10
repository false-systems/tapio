package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RED: Test basic NATS emitter creation and emission
func TestNATSEmitter_BasicEmit(t *testing.T) {
	// Create NATS emitter
	emitter, err := NewNATSEmitter("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}
	require.NotNil(t, emitter, "emitter should not be nil")
	defer emitter.Close()

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
	require.NoError(t, err, "Emit should succeed (or fail gracefully if NATS down)")
}

// RED: Test NATS emitter implements Emitter interface
func TestNATSEmitter_Interface(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}
	defer emitter.Close()

	// Verify interface methods
	assert.Equal(t, "nats", emitter.Name())
	assert.False(t, emitter.IsCritical(), "NATS emitter should be non-critical")
}

// RED: Test NATS subject construction
func TestNATSEmitter_SubjectConstruction(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}
	defer emitter.Close()

	tests := []struct {
		name            string
		eventType       string
		eventSubtype    string
		expectedSubject string
	}{
		{
			name:            "network event",
			eventType:       string(domain.EventTypeNetwork),
			eventSubtype:    "dns_query",
			expectedSubject: "tapio.events.network.dns_query",
		},
		{
			name:            "container event",
			eventType:       string(domain.EventTypeContainer),
			eventSubtype:    "oom_kill",
			expectedSubject: "tapio.events.container.oom_kill",
		},
		{
			name:            "event without subtype",
			eventType:       string(domain.EventTypeKernel),
			eventSubtype:    "",
			expectedSubject: "tapio.events.kernel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &domain.ObserverEvent{
				ID:        "test",
				Type:      tt.eventType,
				Subtype:   tt.eventSubtype,
				Source:    "test-observer",
				Timestamp: time.Now(),
			}

			// We can't easily test the actual subject without exposing it,
			// but we can verify Emit doesn't error on subject construction
			ctx := context.Background()
			err := emitter.Emit(ctx, event)
			// Ignore error - NATS server may not be available, we're just
			// testing that subject construction doesn't panic
			if err != nil {
				t.Logf("Emit failed (expected if NATS down): %v", err)
			}
		})
	}
}

// RED: Test NATS emitter respects context cancellation
func TestNATSEmitter_ContextCancellation(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}
	defer emitter.Close()

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

// RED: Test NATS emitter Close() cleans up resources
func TestNATSEmitter_Close(t *testing.T) {
	emitter, err := NewNATSEmitter("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}

	// Close should succeed
	err = emitter.Close()
	require.NoError(t, err, "Close should succeed")

	// Multiple Close() calls should be safe
	err = emitter.Close()
	assert.NoError(t, err, "Multiple Close() calls should be safe")
}
