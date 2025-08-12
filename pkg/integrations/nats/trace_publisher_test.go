package nats

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestEventPublisher_TraceRouting(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	streamName := fmt.Sprintf("TEST_TRACE_%d", time.Now().UnixNano())
	config := &PublisherConfig{
		URL:        url,
		StreamName: streamName,
	}

	publisher, err := NewEventPublisher(config)
	require.NoError(t, err)
	defer publisher.Close()

	// Subscribe to trace subjects
	nc, err := natsgo.Connect(url)
	require.NoError(t, err)
	defer nc.Close()

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Subscribe to all trace subjects
	basePrefix := strings.ToLower(strings.ReplaceAll(streamName, "_", "."))
	consumerName := fmt.Sprintf("trace-consumer-%d", time.Now().UnixNano())
	traceSub, err := js.PullSubscribe(basePrefix+".traces.>", consumerName,
		natsgo.BindStream(streamName))
	require.NoError(t, err)

	// Test event with TraceID and SpanID
	traceID := "abc123def456"
	spanID := "span789"

	event := collectors.RawEvent{
		Type:      "kubeapi",
		TraceID:   traceID,
		SpanID:    spanID,
		Timestamp: time.Now(),
		Data:      []byte(`{"type": "Warning", "reason": "OOMKilling"}`),
		Metadata: map[string]string{
			"namespace": "production",
		},
	}

	// Publish event
	err = publisher.PublishRawEvent(context.Background(), event)
	require.NoError(t, err)

	// Should receive message on trace subject
	msgs, err := traceSub.Fetch(1, natsgo.MaxWait(2*time.Second))
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	msg := msgs[0]
	expectedTraceSubject := basePrefix + ".traces." + traceID
	assert.Equal(t, expectedTraceSubject, msg.Subject)

	// Check headers
	assert.Equal(t, traceID, msg.Header.Get("Trace-ID"))
	assert.Equal(t, spanID, msg.Header.Get("Span-ID"))
	assert.Equal(t, "kubeapi", msg.Header.Get("Collector-Type"))

	msg.Ack()
}

func TestEventPublisher_UnifiedEventTraceRouting(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	streamName := fmt.Sprintf("TEST_UNIFIED_TRACE_%d", time.Now().UnixNano())
	config := &PublisherConfig{
		URL:        url,
		StreamName: streamName,
	}

	publisher, err := NewEventPublisher(config)
	require.NoError(t, err)
	defer publisher.Close()

	// Subscribe to trace subjects
	nc, err := natsgo.Connect(url)
	require.NoError(t, err)
	defer nc.Close()

	js, err := nc.JetStream()
	require.NoError(t, err)

	basePrefix := strings.ToLower(strings.ReplaceAll(streamName, "_", "."))
	consumerName := fmt.Sprintf("unified-trace-consumer-%d", time.Now().UnixNano())
	traceSub, err := js.PullSubscribe(basePrefix+".traces.>", consumerName,
		natsgo.BindStream(streamName))
	require.NoError(t, err)

	// Create unified event with trace context
	traceID := "unified-trace-123"
	spanID := "unified-span-456"

	event := &domain.UnifiedEvent{
		ID:        "evt-123",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		Source:    "kubeapi",
		Category:  "orchestration",
		Severity:  domain.EventSeverityCritical,
		TraceContext: &domain.TraceContext{
			TraceID: traceID,
			SpanID:  spanID,
		},
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "api-server",
			Namespace: "production",
		},
	}

	// Publish unified event
	err = publisher.PublishUnifiedEvent(context.Background(), event)
	require.NoError(t, err)

	// Should receive message on trace subject
	msgs, err := traceSub.Fetch(1, natsgo.MaxWait(2*time.Second))
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	msg := msgs[0]
	expectedTraceSubject := basePrefix + ".traces." + traceID
	assert.Equal(t, expectedTraceSubject, msg.Subject)

	// Check headers
	assert.Equal(t, traceID, msg.Header.Get("Trace-ID"))
	assert.Equal(t, spanID, msg.Header.Get("Span-ID"))
	assert.Equal(t, "evt-123", msg.Header.Get("Event-ID"))

	msg.Ack()
}

func TestEventPublisher_MultipleEventsTraceCorrelation(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	streamName := fmt.Sprintf("TEST_CORRELATION_%d", time.Now().UnixNano())
	config := &PublisherConfig{
		URL:        url,
		StreamName: streamName,
	}

	publisher, err := NewEventPublisher(config)
	require.NoError(t, err)
	defer publisher.Close()

	// Subscribe to trace subjects
	nc, err := natsgo.Connect(url)
	require.NoError(t, err)
	defer nc.Close()

	js, err := nc.JetStream()
	require.NoError(t, err)

	basePrefix := strings.ToLower(strings.ReplaceAll(streamName, "_", "."))
	consumerName := fmt.Sprintf("correlation-consumer-%d", time.Now().UnixNano())
	traceSub, err := js.PullSubscribe(basePrefix+".traces.>", consumerName,
		natsgo.BindStream(streamName))
	require.NoError(t, err)

	// Same trace ID for correlation
	traceID := "correlation-trace-999"

	// Publish multiple events with same trace ID
	events := []collectors.RawEvent{
		{
			Type:    "kubeapi",
			TraceID: traceID,
			SpanID:  "span-api-1",
			Data:    []byte(`{"type": "Normal", "reason": "Started"}`),
		},
		{
			Type:    "systemd",
			TraceID: traceID,
			SpanID:  "span-systemd-2",
			Data:    []byte(`{"message": "Service started"}`),
		},
		{
			Type:    "ebpf",
			TraceID: traceID,
			SpanID:  "span-ebpf-3",
			Data:    []byte(`{"syscall": "openat"}`),
		},
	}

	// Publish all events
	for _, event := range events {
		event.Timestamp = time.Now()
		err = publisher.PublishRawEvent(context.Background(), event)
		require.NoError(t, err)
	}

	// Should receive all messages on same trace subject
	msgs, err := traceSub.Fetch(3, natsgo.MaxWait(3*time.Second))
	require.NoError(t, err)
	require.Len(t, msgs, 3)

	expectedTraceSubject := basePrefix + ".traces." + traceID
	collectorTypes := make([]string, 0, 3)

	for _, msg := range msgs {
		assert.Equal(t, expectedTraceSubject, msg.Subject)
		assert.Equal(t, traceID, msg.Header.Get("Trace-ID"))
		collectorTypes = append(collectorTypes, msg.Header.Get("Collector-Type"))
		msg.Ack()
	}

	// Should have received events from all collectors
	assert.Contains(t, collectorTypes, "kubeapi")
	assert.Contains(t, collectorTypes, "systemd")
	assert.Contains(t, collectorTypes, "ebpf")
}

func TestEventPublisher_NoTraceIDSkipsTraceRouting(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	streamName := fmt.Sprintf("TEST_NO_TRACE_%d", time.Now().UnixNano())
	config := &PublisherConfig{
		URL:        url,
		StreamName: streamName,
	}

	publisher, err := NewEventPublisher(config)
	require.NoError(t, err)
	defer publisher.Close()

	// Subscribe to trace subjects
	nc, err := natsgo.Connect(url)
	require.NoError(t, err)
	defer nc.Close()

	js, err := nc.JetStream()
	require.NoError(t, err)

	basePrefix := strings.ToLower(strings.ReplaceAll(streamName, "_", "."))
	consumerName := fmt.Sprintf("no-trace-consumer-%d", time.Now().UnixNano())
	traceSub, err := js.PullSubscribe(basePrefix+".traces.>", consumerName,
		natsgo.BindStream(streamName))
	require.NoError(t, err)

	// Event without TraceID
	event := collectors.RawEvent{
		Type:      "systemd",
		Timestamp: time.Now(),
		Data:      []byte(`{"message": "No trace"}`),
		Metadata: map[string]string{
			"node": "worker-1",
		},
	}

	// Publish event
	err = publisher.PublishRawEvent(context.Background(), event)
	require.NoError(t, err)

	// Should NOT receive message on trace subject
	msgs, err := traceSub.Fetch(1, natsgo.MaxWait(1*time.Second))
	assert.Error(t, err) // Should timeout
	assert.Len(t, msgs, 0)
}
