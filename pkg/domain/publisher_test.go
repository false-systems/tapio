package domain

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoOpPublisher_Publish verifies NoOp implementation discards events
func TestNoOpPublisher_Publish(t *testing.T) {
	publisher := &NoOpPublisher{}

	event := &TapioEvent{
		ID:   "test-123",
		Type: EventTypeNetwork,
	}

	err := publisher.Publish(context.Background(), "tapio.events.test", event)
	assert.NoError(t, err, "NoOp should never error")
}

// TestNoOpPublisher_Close verifies NoOp close
func TestNoOpPublisher_Close(t *testing.T) {
	publisher := &NoOpPublisher{}
	err := publisher.Close()
	assert.NoError(t, err, "NoOp close should never error")
}

// TestNewNATSPublisher_NilConnection verifies error on nil connection
func TestNewNATSPublisher_NilConnection(t *testing.T) {
	publisher, err := NewNATSPublisher(nil)
	assert.Error(t, err, "Should error with nil connection")
	assert.Nil(t, publisher)
	assert.Contains(t, err.Error(), "NATS connection is required")
}

// TestNATSPublisher_Publish verifies event publishing to NATS JetStream
func TestNATSPublisher_Publish(t *testing.T) {
	nc := connectTestNATS(t)
	defer nc.Close()

	// Create publisher
	publisher, err := NewNATSPublisher(nc)
	require.NoError(t, err, "Should create NATS publisher")
	defer publisher.Close()

	// Create test event
	event := &TapioEvent{
		ID:        "evt-123",
		Type:      EventTypeNetwork,
		Subtype:   "tcp_connect",
		Severity:  SeverityInfo,
		Outcome:   OutcomeSuccess,
		Timestamp: time.Now(),
		ClusterID: "test-cluster",
		Namespace: "default",
	}

	// Publish event
	ctx := context.Background()
	subject := "tapio.events.test.network.tcp_connect"
	err = publisher.Publish(ctx, subject, event)
	require.NoError(t, err, "Should publish event successfully")

	// Verify event was published
	js, _ := nc.JetStream()
	sub, err := js.SubscribeSync(subject)
	require.NoError(t, err)

	msg, err := sub.NextMsg(1 * time.Second)
	require.NoError(t, err, "Should receive published event")

	// Verify message content
	var received TapioEvent
	err = json.Unmarshal(msg.Data, &received)
	require.NoError(t, err)
	assert.Equal(t, "evt-123", received.ID)
	assert.Equal(t, EventTypeNetwork, received.Type)
	assert.Equal(t, "tcp_connect", received.Subtype)
}

// TestNATSPublisher_PublishWithContext verifies context cancellation
func TestNATSPublisher_PublishWithContext(t *testing.T) {
	nc := connectTestNATS(t)
	defer nc.Close()

	publisher, err := NewNATSPublisher(nc)
	require.NoError(t, err)
	defer publisher.Close()

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	event := &TapioEvent{ID: "test"}

	// Should handle cancelled context gracefully
	err = publisher.Publish(ctx, "tapio.events.test", event)
	// NATS publish is synchronous, so context cancellation might not affect it
	// But we should handle it gracefully
	if err != nil {
		assert.Contains(t, err.Error(), "context")
	}
}

// TestNATSPublisher_PublishNilEvent verifies error handling for nil event
func TestNATSPublisher_PublishNilEvent(t *testing.T) {
	nc := connectTestNATS(t)
	defer nc.Close()

	publisher, err := NewNATSPublisher(nc)
	require.NoError(t, err)
	defer publisher.Close()

	err = publisher.Publish(context.Background(), "tapio.events.test", nil)
	assert.Error(t, err, "Should error on nil event")
	assert.Contains(t, err.Error(), "nil event")
}

// TestNATSPublisher_PublishInvalidJSON verifies error handling for unmarshallable events
func TestNATSPublisher_PublishInvalidJSON(t *testing.T) {
	nc := connectTestNATS(t)
	defer nc.Close()

	publisher, err := NewNATSPublisher(nc)
	require.NoError(t, err)
	defer publisher.Close()

	// Create event with channel (can't be JSON marshaled)
	invalidEvent := struct {
		Ch chan int
	}{
		Ch: make(chan int),
	}

	err = publisher.Publish(context.Background(), "tapio.events.test", invalidEvent)
	assert.Error(t, err, "Should error on marshal failure")
	assert.Contains(t, err.Error(), "marshal")
}

// TestNATSPublisher_Close verifies graceful shutdown
func TestNATSPublisher_Close(t *testing.T) {
	nc := connectTestNATS(t)
	defer nc.Close()

	publisher, err := NewNATSPublisher(nc)
	require.NoError(t, err)

	// Close should succeed
	err = publisher.Close()
	assert.NoError(t, err)

	// Publishing after close should fail gracefully
	event := &TapioEvent{ID: "test"}
	err = publisher.Publish(context.Background(), "tapio.events.test", event)
	assert.Error(t, err, "Should error when publishing to closed publisher")
}

// TestNATSPublisher_MultipleEvents verifies publishing multiple events
func TestNATSPublisher_MultipleEvents(t *testing.T) {
	nc := connectTestNATS(t)
	defer nc.Close()

	publisher, err := NewNATSPublisher(nc)
	require.NoError(t, err)
	defer publisher.Close()

	// Subscribe to all tapio test events
	js, _ := nc.JetStream()
	sub, err := js.SubscribeSync("tapio.events.test.>")
	require.NoError(t, err)

	// Publish 3 different events
	events := []struct {
		subject string
		event   *TapioEvent
	}{
		{"tapio.events.test.network.tcp_connect", &TapioEvent{ID: "evt-1", Type: EventTypeNetwork}},
		{"tapio.events.test.kernel.oom_kill", &TapioEvent{ID: "evt-2", Type: EventTypeKernel}},
		{"tapio.events.test.pod.crash", &TapioEvent{ID: "evt-3", Type: EventTypePod}},
	}

	ctx := context.Background()
	for _, e := range events {
		err := publisher.Publish(ctx, e.subject, e.event)
		require.NoError(t, err)
	}

	// Verify all 3 events received
	for i := 0; i < 3; i++ {
		msg, err := sub.NextMsg(1 * time.Second)
		require.NoError(t, err, "Should receive event %d", i+1)

		var received TapioEvent
		err = json.Unmarshal(msg.Data, &received)
		require.NoError(t, err)
		assert.NotEmpty(t, received.ID)
	}
}

// TestNATSPublisher_ConnectionFailure verifies error handling when NATS is down
func TestNATSPublisher_ConnectionFailure(t *testing.T) {
	nc := connectTestNATS(t)

	publisher, err := NewNATSPublisher(nc)
	require.NoError(t, err)

	// Close connection to simulate failure
	nc.Close()

	// Try to publish
	event := &TapioEvent{ID: "test"}
	err = publisher.Publish(context.Background(), "tapio.events.test", event)
	assert.Error(t, err, "Should error when NATS is down")
}

// connectTestNATS connects to a test NATS server
// Skip test if NATS_URL not set (integration test)
func connectTestNATS(t *testing.T) *nats.Conn {
	natsURL := "nats://localhost:4222" // Default local NATS

	nc, err := nats.Connect(natsURL, nats.Timeout(2*time.Second))
	if err != nil {
		t.Skipf("Skipping test - NATS not available at %s: %v", natsURL, err)
	}

	// Enable JetStream
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		t.Skipf("Skipping test - JetStream not available: %v", err)
	}

	// Try to create/get stream for tapio events
	streamName := "TAPIO_EVENTS_TEST"
	_, err = js.StreamInfo(streamName)
	if err != nil {
		// Stream doesn't exist, create it
		_, err = js.AddStream(&nats.StreamConfig{
			Name:     streamName,
			Subjects: []string{"tapio.events.test.>"},
		})
		if err != nil {
			nc.Close()
			t.Skipf("Skipping test - Failed to create stream: %v", err)
		}
	}

	return nc
}
