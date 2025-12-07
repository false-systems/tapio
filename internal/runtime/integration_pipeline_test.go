package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// TestFullPipeline_EnterpriseToNATS verifies the complete flow:
// Observer → TierConfig(Enterprise) → NATSEmitter → NATS → (Ahti subscriber)
func TestFullPipeline_EnterpriseToNATS(t *testing.T) {
	// 1. Start embedded NATS
	ns := startTestNATS(t)
	defer ns.Shutdown()

	// 2. Subscribe (simulating Ahti)
	received := make(chan *domain.ObserverEvent, 10)
	nc, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err)
	t.Cleanup(func() {
		nc.Close()
	})

	_, err = nc.Subscribe("tapio.events.>", func(m *nats.Msg) {
		var event domain.ObserverEvent
		if err := json.Unmarshal(m.Data, &event); err == nil {
			received <- &event
		}
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())

	// 3. Create runtime with ENTERPRISE tier config
	cfg := TierConfig{
		Tier:     TierEnterprise,
		OTLPURL:  "localhost:4317", // Required but won't actually connect
		Insecure: true,
		NATSURL:  ns.ClientURL(),
	}
	emitters, err := cfg.BuildEmitters()
	require.NoError(t, err)
	cleanupEmitters(t, emitters)

	// Find the NATS emitter for direct testing
	var natsEmitter Emitter
	for _, e := range emitters {
		if e.Name() == "nats" {
			natsEmitter = e
			break
		}
	}
	require.NotNil(t, natsEmitter, "NATS emitter should be created for ENTERPRISE tier")

	// 4. Emit event directly to NATS emitter (simulating observer)
	event := &domain.ObserverEvent{
		ID:      "test-pipeline-123",
		Type:    "network",
		Subtype: "connection_established",
		Source:  "network-observer",
		NetworkData: &domain.NetworkEventData{
			SrcIP:    "10.0.1.42",
			DstIP:    "10.0.2.100",
			DstPort:  443,
			Protocol: "tcp",
		},
	}
	err = natsEmitter.Emit(context.Background(), event)
	require.NoError(t, err)

	// 5. Verify Ahti would receive the event
	select {
	case evt := <-received:
		assert.Equal(t, "test-pipeline-123", evt.ID)
		assert.Equal(t, "network", evt.Type)
		assert.Equal(t, "connection_established", evt.Subtype)
		assert.Equal(t, "network-observer", evt.Source)
		// Verify network data preserved
		require.NotNil(t, evt.NetworkData)
		assert.Equal(t, "10.0.1.42", evt.NetworkData.SrcIP)
		assert.Equal(t, "10.0.2.100", evt.NetworkData.DstIP)
	case <-time.After(2 * time.Second):
		t.Fatal("event not received - pipeline broken")
	}
}

// TestFullPipeline_FreeTierNoNATS verifies FREE tier doesn't publish to NATS.
func TestFullPipeline_FreeTierNoNATS(t *testing.T) {
	// Start embedded NATS
	ns := startTestNATS(t)
	defer ns.Shutdown()

	// Subscribe to detect any messages
	received := make(chan bool, 1)
	nc, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err)
	t.Cleanup(func() {
		nc.Close()
	})

	_, err = nc.Subscribe("tapio.events.>", func(m *nats.Msg) {
		received <- true
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())

	// Create FREE tier config
	cfg := TierConfig{
		Tier:     TierFree,
		OTLPURL:  "localhost:4317",
		Insecure: true,
		NATSURL:  ns.ClientURL(), // Should be ignored for Free tier
	}
	emitters, err := cfg.BuildEmitters()
	require.NoError(t, err)
	cleanupEmitters(t, emitters)

	// Verify no NATS emitter
	for _, e := range emitters {
		assert.NotEqual(t, "nats", e.Name(), "FREE tier should not have NATS emitter")
	}

	// No message should be received (we can't even emit since no NATS emitter)
	select {
	case <-received:
		t.Fatal("FREE tier should not publish to NATS")
	case <-time.After(100 * time.Millisecond):
		// Good - no message received
	}
}

// TestFullPipeline_GracefulDegradation verifies pipeline works when NATS is down.
func TestFullPipeline_GracefulDegradation(t *testing.T) {
	// Create ENTERPRISE tier with bad NATS URL
	cfg := TierConfig{
		Tier:     TierEnterprise,
		OTLPURL:  "localhost:4317",
		Insecure: true,
		NATSURL:  "nats://nonexistent:4222", // Bad URL
	}

	emitters, err := cfg.BuildEmitters()

	// Should NOT fail - graceful degradation
	require.NoError(t, err)
	cleanupEmitters(t, emitters)

	// Should only have OTLP emitter
	assert.Len(t, emitters, 1)
	assert.Equal(t, "otlp", emitters[0].Name())

	// OTLP is critical, so pipeline can still work
	assert.True(t, emitters[0].IsCritical())
}
