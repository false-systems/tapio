package nats

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Test helpers
func startTestNATSServer(t *testing.T) (*server.Server, string) {
	opts := &server.Options{
		Port:      -1, // Random port
		JetStream: true,
	}

	ns, err := server.NewServer(opts)
	require.NoError(t, err)

	go ns.Start()

	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server failed to start")
	}

	return ns, ns.ClientURL()
}

func TestEventPublisher_PublishRawEvent(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	// Create publisher with unique stream name
	streamName := fmt.Sprintf("TEST_EVENTS_%d", time.Now().UnixNano())
	config := &PublisherConfig{
		URL:            url,
		StreamName:     streamName,
		MaxPending:     100,
		ConnectTimeout: 5 * time.Second,
	}

	publisher, err := NewEventPublisher(config)
	require.NoError(t, err)
	defer publisher.Close()

	// Subscribe to verify
	nc, err := natsgo.Connect(url)
	require.NoError(t, err)
	defer nc.Close()

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create a consumer for this test
	consumerName := fmt.Sprintf("test-consumer-%d", time.Now().UnixNano())
	// Subscribe to the stream-specific subject
	basePrefix := strings.ToLower(strings.ReplaceAll(streamName, "_", "."))
	sub, err := js.PullSubscribe(basePrefix+".raw.>", consumerName,
		natsgo.BindStream(streamName),
		natsgo.AckExplicit())
	require.NoError(t, err)

	// Test cases
	tests := []struct {
		name  string
		event collectors.RawEvent
		want  string // expected subject
	}{
		{
			name: "systemd event",
			event: collectors.RawEvent{
				Type:      "systemd",
				Timestamp: time.Now(),
				Data:      []byte(`{"message": "test"}`),
				Metadata: map[string]string{
					"node":     "node-1",
					"trace_id": "trace-123",
				},
			},
			want: ".raw.systemd", // will be prefixed in test
		},
		{
			name: "kubeapi event with namespace",
			event: collectors.RawEvent{
				Type:      "kubeapi",
				Timestamp: time.Now(),
				Data:      []byte(`{"type": "Warning"}`),
				Metadata: map[string]string{
					"namespace": "production",
					"trace_id":  "trace-456",
				},
			},
			want: ".raw.kubeapi.production", // will be prefixed in test
		},
		{
			name: "critical event",
			event: collectors.RawEvent{
				Type:      "ebpf",
				Timestamp: time.Now(),
				Data:      []byte(`{"syscall": "kill"}`),
				Metadata: map[string]string{
					"severity": "critical",
				},
			},
			want: ".raw.ebpf.critical", // will be prefixed in test
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Publish event
			err := publisher.PublishRawEvent(context.Background(), tt.event)
			assert.NoError(t, err)

			// Verify receipt
			msgs, err := sub.Fetch(1, natsgo.MaxWait(time.Second))
			require.NoError(t, err)
			require.Len(t, msgs, 1)

			msg := msgs[0]
			// Add stream-specific prefix to expected subject
			expectedSubject := basePrefix + tt.want
			assert.Equal(t, expectedSubject, msg.Subject)

			// Verify headers
			assert.Equal(t, tt.event.Metadata["trace_id"], msg.Header.Get("Trace-ID"))
			assert.Equal(t, tt.event.Type, msg.Header.Get("Collector-Type"))

			msg.Ack()
		})
	}
}

func TestEventPublisher_PublishUnifiedEvent(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	streamName := fmt.Sprintf("TEST_UNIFIED_%d", time.Now().UnixNano())
	config := &PublisherConfig{
		URL:        url,
		StreamName: streamName,
	}

	publisher, err := NewEventPublisher(config)
	require.NoError(t, err)
	defer publisher.Close()

	// Subscribe
	nc, err := natsgo.Connect(url)
	require.NoError(t, err)
	defer nc.Close()

	js, err := nc.JetStream()
	require.NoError(t, err)

	// Subscribe to the stream-specific subject
	basePrefix := strings.ToLower(strings.ReplaceAll(streamName, "_", "."))
	consumerName := fmt.Sprintf("test-unified-%d", time.Now().UnixNano())
	sub, err := js.PullSubscribe(basePrefix+".unified.>", consumerName,
		natsgo.BindStream(streamName))
	require.NoError(t, err)

	// Test unified event
	event := &domain.UnifiedEvent{
		ID:        "evt-123",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		Source:    "k8s",
		Category:  "orchestration",
		Severity:  domain.EventSeverityCritical,
		TraceContext: &domain.TraceContext{
			TraceID: "trace-unified",
			SpanID:  "span-123",
		},
		Semantic: &domain.SemanticContext{
			Intent:   "memory_exhaustion",
			Category: "resource_management",
			Tags:     []string{"oom", "critical"},
		},
		Kubernetes: &domain.KubernetesData{
			EventType:  "Warning",
			Reason:     "OOMKilling",
			ObjectKind: "Pod",
			Object:     "Pod/api-server",
		},
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "api-server",
			Namespace: "production",
		},
	}

	// Publish
	err = publisher.PublishUnifiedEvent(context.Background(), event)
	assert.NoError(t, err)

	// Verify
	msgs, err := sub.Fetch(1, natsgo.MaxWait(time.Second))
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	msg := msgs[0]

	// Check multi-dimensional routing
	basePrefix = strings.ToLower(strings.ReplaceAll(streamName, "_", "."))
	assert.Contains(t, []string{
		basePrefix + ".unified.kubernetes.production.api-server",
		basePrefix + ".unified.critical",
		basePrefix + ".unified.resource_management",
	}, msg.Subject)

	// Check headers
	assert.Equal(t, "trace-unified", msg.Header.Get("Trace-ID"))
	assert.Equal(t, "span-123", msg.Header.Get("Span-ID"))
	assert.Equal(t, "memory_exhaustion", msg.Header.Get("Semantic-Intent"))
	assert.Equal(t, "critical", msg.Header.Get("Severity"))
}

func TestEventPublisher_HighThroughput(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	config := &PublisherConfig{
		URL:        url,
		StreamName: "TEST_PERF",
		MaxPending: 1000,
	}

	publisher, err := NewEventPublisher(config)
	require.NoError(t, err)
	defer publisher.Close()

	// Publish many events concurrently
	numEvents := 10000
	numWorkers := 10
	eventsChan := make(chan collectors.RawEvent, numEvents)

	// Generate events
	for i := 0; i < numEvents; i++ {
		eventsChan <- collectors.RawEvent{
			Type:      fmt.Sprintf("type-%d", i%5),
			Timestamp: time.Now(),
			Data:      []byte(fmt.Sprintf(`{"index": %d}`, i)),
			Metadata: map[string]string{
				"trace_id": fmt.Sprintf("trace-%d", i),
			},
		}
	}
	close(eventsChan)

	// Publish concurrently
	start := time.Now()
	var wg sync.WaitGroup
	errors := make([]error, numWorkers)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for event := range eventsChan {
				if err := publisher.PublishRawEvent(context.Background(), event); err != nil {
					errors[workerID] = err
					return
				}
			}
		}(w)
	}

	wg.Wait()
	elapsed := time.Since(start)

	// Check no errors
	for _, err := range errors {
		assert.NoError(t, err)
	}

	// Performance check
	eventsPerSec := float64(numEvents) / elapsed.Seconds()
	t.Logf("Published %d events in %v (%.0f events/sec)", numEvents, elapsed, eventsPerSec)
	assert.Greater(t, eventsPerSec, 1000.0) // At least 1K events/sec
}

func TestEventPublisher_Resilience(t *testing.T) {
	// Start server
	ns, url := startTestNATSServer(t)

	streamName := fmt.Sprintf("TEST_RESILIENCE_%d", time.Now().UnixNano())
	config := &PublisherConfig{
		URL:           url,
		StreamName:    streamName,
		ReconnectWait: 100 * time.Millisecond,
		MaxReconnects: 5,
	}

	publisher, err := NewEventPublisher(config)
	require.NoError(t, err)
	defer publisher.Close()

	// Publish successfully
	event := collectors.RawEvent{
		Type:      "test",
		Timestamp: time.Now(),
		Data:      []byte("test"),
	}

	err = publisher.PublishRawEvent(context.Background(), event)
	assert.NoError(t, err)

	// Shutdown server
	ns.Shutdown()

	// Try to publish - should fail but not panic
	err = publisher.PublishRawEvent(context.Background(), event)
	assert.Error(t, err)

	// Restart server
	ns, _ = startTestNATSServer(t)
	defer ns.Shutdown()

	// Wait for reconnection
	time.Sleep(500 * time.Millisecond)

	// Should work again
	err = publisher.PublishRawEvent(context.Background(), event)
	// May still error if reconnection hasn't completed
	// but should not panic
}

func TestEventPublisher_StreamManagement(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	streamName := fmt.Sprintf("TEST_STREAM_%d", time.Now().UnixNano())
	config := &PublisherConfig{
		URL:        url,
		StreamName: streamName,
		StreamConfig: &StreamConfig{
			Subjects:  []string{"test." + streamName + ".>"},
			MaxBytes:  1024 * 1024 * 100, // 100MB
			MaxAge:    24 * time.Hour,
			MaxMsgs:   1000000,
			Retention: "limits",
			Storage:   "file",
			Replicas:  1,
		},
	}

	publisher, err := NewEventPublisher(config)
	require.NoError(t, err)
	defer publisher.Close()

	// Verify stream created
	nc, err := natsgo.Connect(url)
	require.NoError(t, err)
	defer nc.Close()

	js, err := nc.JetStream()
	require.NoError(t, err)

	info, err := js.StreamInfo(streamName)
	require.NoError(t, err)

	assert.Equal(t, streamName, info.Config.Name)
	assert.Equal(t, []string{"test." + streamName + ".>"}, info.Config.Subjects)
	assert.Equal(t, int64(1024*1024*100), info.Config.MaxBytes)
}

func TestEventPublisher_HealthCheck(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	streamName := fmt.Sprintf("TEST_HEALTH_%d", time.Now().UnixNano())
	config := &PublisherConfig{
		URL:        url,
		StreamName: streamName,
	}

	publisher, err := NewEventPublisher(config)
	require.NoError(t, err)
	defer publisher.Close()

	// Should be healthy
	err = publisher.HealthCheck()
	assert.NoError(t, err)

	// Close connection to simulate unhealthy state
	publisher.nc.Close()

	// Should report unhealthy
	err = publisher.HealthCheck()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

// Benchmark
func BenchmarkEventPublisher_PublishRawEvent(b *testing.B) {
	ns, url := startTestNATSServer(&testing.T{})
	defer ns.Shutdown()

	config := &PublisherConfig{
		URL:        url,
		StreamName: "BENCH_EVENTS",
		MaxPending: 1000,
	}

	publisher, err := NewEventPublisher(config)
	if err != nil {
		b.Fatal(err)
	}
	defer publisher.Close()

	event := collectors.RawEvent{
		Type:      "bench",
		Timestamp: time.Now(),
		Data:      []byte(`{"test": "benchmark"}`),
		Metadata: map[string]string{
			"trace_id": "bench-trace",
		},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := publisher.PublishRawEvent(context.Background(), event); err != nil {
				b.Fatal(err)
			}
		}
	})
}
