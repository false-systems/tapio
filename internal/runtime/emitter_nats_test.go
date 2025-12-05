package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yairfalse/tapio/pkg/domain"
)

// startTestNATS starts an embedded NATS server for testing.
func startTestNATS(t *testing.T) *server.Server {
	t.Helper()
	opts := &server.Options{
		Host: "127.0.0.1",
		Port: -1, // Random available port
	}
	ns, err := server.NewServer(opts)
	require.NoError(t, err)
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server not ready")
	}
	return ns
}

func TestNATSEmitter_Emit(t *testing.T) {
	// Start embedded NATS
	ns := startTestNATS(t)
	defer ns.Shutdown()

	// Subscribe to verify event arrives
	received := make(chan *domain.ObserverEvent, 1)
	nc, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err)
	defer nc.Close()

	_, err = nc.Subscribe("tapio.events.>", func(m *nats.Msg) {
		var event domain.ObserverEvent
		if err := json.Unmarshal(m.Data, &event); err == nil {
			received <- &event
		}
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())

	// Create emitter
	emitter, err := NewNATSEmitter(ns.ClientURL())
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	})

	// Emit event
	event := &domain.ObserverEvent{
		ID:      "test-123",
		Type:    "deployment",
		Subtype: "rollout_stuck",
		Source:  "deployments-observer",
	}
	err = emitter.Emit(context.Background(), event)
	require.NoError(t, err)

	// Verify received
	select {
	case evt := <-received:
		assert.Equal(t, "test-123", evt.ID)
		assert.Equal(t, "deployment", evt.Type)
		assert.Equal(t, "rollout_stuck", evt.Subtype)
		assert.Equal(t, "deployments-observer", evt.Source)
	case <-time.After(2 * time.Second):
		t.Fatal("event not received on NATS")
	}
}

func TestNATSEmitter_EmitNilEvent(t *testing.T) {
	ns := startTestNATS(t)
	defer ns.Shutdown()

	emitter, err := NewNATSEmitter(ns.ClientURL())
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	})

	err = emitter.Emit(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestNATSEmitter_EmitContextCanceled(t *testing.T) {
	ns := startTestNATS(t)
	defer ns.Shutdown()

	emitter, err := NewNATSEmitter(ns.ClientURL())
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := emitter.Close(); err != nil {
			t.Logf("failed to close emitter: %v", err)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	event := &domain.ObserverEvent{Type: "test"}
	err = emitter.Emit(ctx, event)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestNATSEmitter_IsCritical(t *testing.T) {
	// NATS emitter is NOT critical - if NATS is down, OTLP should still work
	emitter := &NATSEmitter{}
	assert.False(t, emitter.IsCritical())
}

func TestNATSEmitter_Name(t *testing.T) {
	emitter := &NATSEmitter{}
	assert.Equal(t, "nats", emitter.Name())
}

func TestNATSEmitter_Close(t *testing.T) {
	ns := startTestNATS(t)
	defer ns.Shutdown()

	emitter, err := NewNATSEmitter(ns.ClientURL())
	require.NoError(t, err)

	// Close should not error
	err = emitter.Close()
	assert.NoError(t, err)

	// Double close should not error
	err = emitter.Close()
	assert.NoError(t, err)

	// Emit after close should error
	event := &domain.ObserverEvent{Type: "test"}
	err = emitter.Emit(context.Background(), event)
	assert.Error(t, err)
}

func TestNATSEmitter_ConnectionFailure(t *testing.T) {
	// Try to connect to non-existent NATS server
	_, err := NewNATSEmitter("nats://nonexistent:4222")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect")
}
