package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
)

func TestNATSIntegration(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Connect to NATS
	nc, err := nats.Connect("nats://localhost:4222",
		nats.Name("test-client"),
		nats.Timeout(5*time.Second),
		nats.MaxReconnects(3),
	)
	require.NoError(t, err, "Failed to connect to NATS")
	defer nc.Close()

	// Get JetStream context
	js, err := nc.JetStream()
	require.NoError(t, err, "Failed to get JetStream context")

	t.Run("StreamManagement", func(t *testing.T) {
		// Create or update stream
		streamConfig := &nats.StreamConfig{
			Name:        "TEST_OBSERVATIONS",
			Subjects:    []string{"test.observations.>"},
			MaxAge:      time.Hour,
			Storage:     nats.FileStorage,
			Retention:   nats.LimitsPolicy,
			MaxMsgs:     10000,
			MaxBytes:    100 * 1024 * 1024, // 100MB
			Duplicates:  2 * time.Minute,
			NoAck:       false,
			MaxMsgSize:  1024 * 1024, // 1MB
			Replicas:    1,
			Description: "Test stream for observations",
		}

		stream, err := js.AddStream(streamConfig)
		require.NoError(t, err, "Failed to create stream")
		assert.Equal(t, "TEST_OBSERVATIONS", stream.Config.Name)

		// Verify stream info
		info, err := js.StreamInfo("TEST_OBSERVATIONS")
		require.NoError(t, err)
		assert.NotNil(t, info)
		logger.Info("Stream created",
			zap.String("name", info.Config.Name),
			zap.Uint64("messages", info.State.Msgs),
			zap.Uint64("bytes", info.State.Bytes))
	})

	t.Run("PublishAndSubscribe", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Test events
		events := []collectors.RawEvent{
			{
				Timestamp: time.Now(),
				Type:      "kernel",
				Data:      json.RawMessage(`{"PID": 1234, "Comm": "test-app", "Syscall": "open"}`),
				Metadata: map[string]string{
					"node":      "test-node",
					"collector": "kernel",
				},
			},
			{
				Timestamp: time.Now(),
				Type:      "dns",
				Data:      json.RawMessage(`{"QueryName": "example.com", "QueryType": "A"}`),
				Metadata: map[string]string{
					"node":      "test-node",
					"collector": "dns",
				},
			},
		}

		// Publish events
		publishedSeqs := make([]uint64, 0, len(events))
		for i, event := range events {
			data, err := json.Marshal(event)
			require.NoError(t, err)

			subject := fmt.Sprintf("test.observations.%s", event.Type)
			pubAck, err := js.Publish(subject, data)
			require.NoError(t, err)
			publishedSeqs = append(publishedSeqs, pubAck.Sequence)

			logger.Info("Published event",
				zap.Int("index", i),
				zap.String("type", event.Type),
				zap.Uint64("sequence", pubAck.Sequence))
		}

		// Create pull subscriber
		sub, err := js.PullSubscribe(
			"test.observations.>",
			"test-consumer",
			nats.ManualAck(),
			nats.AckExplicit(),
			nats.DeliverNew(),
		)
		require.NoError(t, err)

		// Fetch and verify messages
		msgs, err := sub.Fetch(len(events), nats.MaxWait(5*time.Second))
		require.NoError(t, err)
		require.Len(t, msgs, len(events), "Should receive all published events")

		for i, msg := range msgs {
			var receivedEvent collectors.RawEvent
			err := json.Unmarshal(msg.Data, &receivedEvent)
			require.NoError(t, err)

			// Verify event matches what was published
			assert.Equal(t, events[i].Type, receivedEvent.Type)
			assert.Equal(t, events[i].Metadata["node"], receivedEvent.Metadata["node"])

			// Acknowledge message
			err = msg.Ack()
			require.NoError(t, err)

			logger.Info("Received and acked message",
				zap.Int("index", i),
				zap.String("type", receivedEvent.Type))
		}
	})

	t.Run("ConsumerGroups", func(t *testing.T) {
		// Create durable consumer
		consumerConfig := &nats.ConsumerConfig{
			Durable:       "test-durable-consumer",
			DeliverPolicy: nats.DeliverAllPolicy,
			AckPolicy:     nats.AckExplicitPolicy,
			MaxDeliver:    3,
			AckWait:       10 * time.Second,
			MaxAckPending: 100,
			FilterSubject: "test.observations.kernel",
		}

		consumerInfo, err := js.AddConsumer("TEST_OBSERVATIONS", consumerConfig)
		require.NoError(t, err)
		assert.Equal(t, "test-durable-consumer", consumerInfo.Name)

		// Subscribe using durable consumer
		sub, err := js.PullSubscribe(
			"test.observations.kernel",
			"test-durable-consumer",
			nats.Bind("TEST_OBSERVATIONS", "test-durable-consumer"),
		)
		require.NoError(t, err)

		// Publish test message
		testEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kernel",
			Data:      json.RawMessage(`{"PID": 5678, "Comm": "consumer-test"}`),
		}
		data, _ := json.Marshal(testEvent)
		pubAck, err := js.Publish("test.observations.kernel", data)
		require.NoError(t, err)

		// Fetch from durable consumer
		msgs, err := sub.Fetch(1, nats.MaxWait(3*time.Second))
		require.NoError(t, err)
		require.Len(t, msgs, 1)

		// Verify message
		var received collectors.RawEvent
		err = json.Unmarshal(msgs[0].Data, &received)
		require.NoError(t, err)
		assert.Equal(t, "kernel", received.Type)

		// Ack message
		err = msgs[0].Ack()
		require.NoError(t, err)

		logger.Info("Durable consumer test passed",
			zap.Uint64("sequence", pubAck.Sequence))
	})

	t.Run("ErrorHandling", func(t *testing.T) {
		// Test message retry on NAK
		sub, err := js.PullSubscribe(
			"test.observations.error",
			"error-test-consumer",
			nats.ManualAck(),
			nats.MaxDeliver(3),
			nats.AckWait(2*time.Second),
		)
		require.NoError(t, err)

		// Publish error test event
		testEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "error",
			Data:      json.RawMessage(`{"error": "test-error"}`),
		}
		data, _ := json.Marshal(testEvent)
		_, err = js.Publish("test.observations.error", data)
		require.NoError(t, err)

		// Fetch and NAK message
		msgs, err := sub.Fetch(1, nats.MaxWait(3*time.Second))
		require.NoError(t, err)
		require.Len(t, msgs, 1)

		// NAK for retry
		err = msgs[0].Nak()
		require.NoError(t, err)

		// Should receive again after NAK
		msgs2, err := sub.Fetch(1, nats.MaxWait(3*time.Second))
		require.NoError(t, err)
		require.Len(t, msgs2, 1)

		// ACK this time
		err = msgs2[0].Ack()
		require.NoError(t, err)

		logger.Info("Error handling test passed")
	})

	t.Run("Performance", func(t *testing.T) {
		// Measure throughput
		numMessages := 1000
		startTime := time.Now()

		// Publish batch
		for i := 0; i < numMessages; i++ {
			event := collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "perf",
				Data:      json.RawMessage(fmt.Sprintf(`{"index": %d}`, i)),
			}
			data, _ := json.Marshal(event)
			_, err := js.PublishAsync("test.observations.perf", data)
			require.NoError(t, err)
		}

		// Wait for all publishes to complete
		select {
		case <-js.PublishAsyncComplete():
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for async publish")
		}

		publishDuration := time.Since(startTime)
		publishRate := float64(numMessages) / publishDuration.Seconds()

		// Subscribe and consume
		sub, err := js.PullSubscribe(
			"test.observations.perf",
			"perf-consumer",
			nats.ManualAck(),
		)
		require.NoError(t, err)

		consumeStart := time.Now()
		totalReceived := 0

		for totalReceived < numMessages {
			msgs, err := sub.Fetch(100, nats.MaxWait(1*time.Second))
			if err == nats.ErrTimeout {
				continue
			}
			require.NoError(t, err)

			for _, msg := range msgs {
				msg.Ack()
				totalReceived++
			}
		}

		consumeDuration := time.Since(consumeStart)
		consumeRate := float64(numMessages) / consumeDuration.Seconds()

		logger.Info("Performance test results",
			zap.Int("messages", numMessages),
			zap.Duration("publish_duration", publishDuration),
			zap.Float64("publish_rate", publishRate),
			zap.Duration("consume_duration", consumeDuration),
			zap.Float64("consume_rate", consumeRate))

		// Assert reasonable performance
		assert.Greater(t, publishRate, 100.0, "Should publish at least 100 msg/s")
		assert.Greater(t, consumeRate, 100.0, "Should consume at least 100 msg/s")
	})

	t.Run("Cleanup", func(t *testing.T) {
		// Clean up test stream
		err := js.DeleteStream("TEST_OBSERVATIONS")
		require.NoError(t, err)
		logger.Info("Test stream deleted")
	})
}

func TestNATSReconnection(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	reconnectCount := 0
	disconnectCount := 0

	// Connect with reconnect handlers
	nc, err := nats.Connect("nats://localhost:4222",
		nats.MaxReconnects(3),
		nats.ReconnectWait(1*time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			disconnectCount++
			logger.Warn("Disconnected from NATS",
				zap.Error(err),
				zap.Int("count", disconnectCount))
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			reconnectCount++
			logger.Info("Reconnected to NATS",
				zap.Int("count", reconnectCount),
				zap.String("url", nc.ConnectedUrl()))
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			logger.Error("NATS connection closed")
		}),
	)
	require.NoError(t, err)
	defer nc.Close()

	// Verify connection
	assert.True(t, nc.IsConnected())
	logger.Info("Connected to NATS",
		zap.String("url", nc.ConnectedUrl()),
		zap.String("id", nc.ConnectedServerId()))
}

func TestStreamMonitoring(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	nc, err := nats.Connect("nats://localhost:4222")
	require.NoError(t, err)
	defer nc.Close()

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create monitoring stream
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "MONITOR_TEST",
		Subjects: []string{"monitor.>"},
		MaxAge:   time.Hour,
	})
	require.NoError(t, err)
	defer js.DeleteStream("MONITOR_TEST")

	// Publish some test data
	for i := 0; i < 10; i++ {
		data := fmt.Sprintf(`{"index": %d}`, i)
		_, err := js.Publish("monitor.test", []byte(data))
		require.NoError(t, err)
	}

	// Get stream info
	info, err := js.StreamInfo("MONITOR_TEST")
	require.NoError(t, err)

	logger.Info("Stream statistics",
		zap.String("name", info.Config.Name),
		zap.Uint64("messages", info.State.Msgs),
		zap.Uint64("bytes", info.State.Bytes),
		zap.Uint64("first_seq", info.State.FirstSeq),
		zap.Uint64("last_seq", info.State.LastSeq),
		zap.Int("consumer_count", info.State.Consumers))

	// Verify stats
	assert.Equal(t, uint64(10), info.State.Msgs)
	assert.Greater(t, info.State.Bytes, uint64(0))
}
