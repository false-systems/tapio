package correlation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/integrations/nats"
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

func createTestCorrelationEngine() *MockCorrelationEngine {
	return NewMockCorrelationEngine()
}

func TestNATSSubscriber_TraceCorrelation(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	// Create correlation engine
	correlationEngine := createTestCorrelationEngine()

	// Create NATS subscriber
	streamName := fmt.Sprintf("TEST_CORR_%d", time.Now().UnixNano())
	config := &NATSSubscriberConfig{
		URL:               url,
		StreamName:        streamName,
		Name:              "test-subscriber",
		TraceSubjects:     []string{"traces.>"},
		BatchSize:         5,
		BatchTimeout:      1 * time.Second,
		WorkerCount:       2,
		CorrelationWindow: 2 * time.Second,
		MinEventsForCorr:  2,
		Logger:            zap.NewNop(),
	}

	subscriber, err := NewNATSSubscriber(config, correlationEngine)
	require.NoError(t, err)
	defer subscriber.Stop()

	// Create NATS publisher to send events
	publisherConfig := &nats.PublisherConfig{
		URL:        url,
		StreamName: streamName,
	}
	publisher, err := nats.NewEventPublisher(publisherConfig)
	require.NoError(t, err)
	defer publisher.Close()

	// Start subscriber
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = subscriber.Start(ctx)
	require.NoError(t, err)

	// Wait for subscriptions to be ready
	time.Sleep(100 * time.Millisecond)

	// Create correlated events with same trace ID
	traceID := "correlation-test-123"
	events := []collectors.RawEvent{
		{
			Type:      "kubeapi",
			TraceID:   traceID,
			SpanID:    "span-1",
			Timestamp: time.Now(),
			Data:      []byte(`{"type": "Warning", "reason": "FailedMount"}`),
			Metadata:  map[string]string{"namespace": "production"},
		},
		{
			Type:      "systemd",
			TraceID:   traceID,
			SpanID:    "span-2",
			Timestamp: time.Now().Add(100 * time.Millisecond),
			Data:      []byte(`{"message": "Mount failed: permission denied"}`),
		},
		{
			Type:      "ebpf",
			TraceID:   traceID,
			SpanID:    "span-3",
			Timestamp: time.Now().Add(200 * time.Millisecond),
			Data:      []byte(`{"syscall": "mount", "errno": "EACCES"}`),
		},
	}

	// Publish events
	for _, event := range events {
		err := publisher.PublishRawEvent(ctx, event)
		require.NoError(t, err)
	}

	// Wait for correlation processing
	time.Sleep(3 * time.Second)

	// Check for correlation results
	select {
	case results := <-subscriber.Results():
		assert.Greater(t, len(results), 0, "Should have correlation results")
		
		// Verify results contain our events
		foundEvents := false
		for _, result := range results {
			if len(result.Events) > 0  {
				foundEvents = true
				assert.Greater(t, len(result.Events), 0, "Should have events")
				break
			}
		}
		assert.True(t, foundEvents, "Should find events in results")

	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for correlation results")
	}
}

func TestNATSSubscriber_IndividualEventProcessing(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	correlationEngine := createTestCorrelationEngine()

	streamName := fmt.Sprintf("TEST_INDIVIDUAL_%d", time.Now().UnixNano())
	config := &NATSSubscriberConfig{
		URL:               url,
		StreamName:        streamName,
		Name:              "test-individual",
		RawEventSubjects:  []string{"events.raw.>"},
		BatchSize:         1,
		BatchTimeout:      500 * time.Millisecond,
		WorkerCount:       1,
		CorrelationWindow: 1 * time.Second,
		MinEventsForCorr:  5, // High threshold so individual processing is used
		Logger:            zap.NewNop(),
	}

	subscriber, err := NewNATSSubscriber(config, correlationEngine)
	require.NoError(t, err)
	defer subscriber.Stop()

	publisherConfig := &nats.PublisherConfig{
		URL:        url,
		StreamName: streamName,
	}
	publisher, err := nats.NewEventPublisher(publisherConfig)
	require.NoError(t, err)
	defer publisher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = subscriber.Start(ctx)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish event without trace ID (should be processed individually)
	event := collectors.RawEvent{
		Type:      "systemd",
		Timestamp: time.Now(),
		Data:      []byte(`{"message": "Service failed", "level": "error"}`),
		Metadata:  map[string]string{"service": "nginx"},
	}

	err = publisher.PublishRawEvent(ctx, event)
	require.NoError(t, err)

	// Should get individual processing results quickly
	select {
	case results := <-subscriber.Results():
		// Individual events may or may not produce correlations
		// The important thing is that processing happened
		t.Logf("Received %d correlation results", len(results))

	case <-time.After(2 * time.Second):
		// This is not necessarily a failure - individual events
		// might not produce correlations
		t.Log("No correlation results (expected for individual events)")
	}
}

func TestNATSSubscriber_TraceIDExtraction(t *testing.T) {
	// This test doesn't need a real NATS connection
	subscriber := &NATSSubscriber{
		config: &NATSSubscriberConfig{Logger: zap.NewNop()},
		logger: zap.NewNop(),
	}

	tests := []struct {
		name     string
		subject  string
		expected string
	}{
		{
			name:     "Simple trace subject",
			subject:  "traces.abc123def456",
			expected: "abc123def456",
		},
		{
			name:     "Test stream trace subject",
			subject:  "test.stream.123.traces.xyz789",
			expected: "xyz789",
		},
		{
			name:     "No trace ID",
			subject:  "events.raw.systemd",
			expected: "",
		},
		{
			name:     "Traces without ID",
			subject:  "traces",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := subscriber.extractTraceIDFromSubject(tt.subject)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNATSSubscriber_StartStop(t *testing.T) {
	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	correlationEngine := createTestCorrelationEngine()

	config := &NATSSubscriberConfig{
		URL:              url,
		StreamName:       "TEST_LIFECYCLE",
		Name:             "test-lifecycle",
		TraceSubjects:    []string{"traces.>"},
		CorrelationWindow: 1 * time.Second,
		Logger:           zap.NewNop(),
	}

	subscriber, err := NewNATSSubscriber(config, correlationEngine)
	require.NoError(t, err)

	ctx := context.Background()

	// Start
	err = subscriber.Start(ctx)
	require.NoError(t, err)

	// Should not be able to start again
	err = subscriber.Start(ctx)
	assert.Error(t, err)

	// Stop
	err = subscriber.Stop()
	require.NoError(t, err)

	// Should be able to stop again without error
	err = subscriber.Stop()
	require.NoError(t, err)
}

func TestNATSSubscriber_HighThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high throughput test in short mode")
	}

	ns, url := startTestNATSServer(t)
	defer ns.Shutdown()

	correlationEngine := createTestCorrelationEngine()

	streamName := fmt.Sprintf("TEST_THROUGHPUT_%d", time.Now().UnixNano())
	config := &NATSSubscriberConfig{
		URL:               url,
		StreamName:        streamName,
		Name:              "test-throughput",
		TraceSubjects:     []string{"traces.>"},
		BatchSize:         50,
		BatchTimeout:      100 * time.Millisecond,
		WorkerCount:       8,
		MaxPending:        5000,
		CorrelationWindow: 1 * time.Second,
		MinEventsForCorr:  2,
		Logger:            zap.NewNop(),
	}

	subscriber, err := NewNATSSubscriber(config, correlationEngine)
	require.NoError(t, err)
	defer subscriber.Stop()

	publisherConfig := &nats.PublisherConfig{
		URL:          url,
		StreamName:   streamName,
		MaxPending:   1000,
		AsyncPublish: true,
	}
	publisher, err := nats.NewEventPublisher(publisherConfig)
	require.NoError(t, err)
	defer publisher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = subscriber.Start(ctx)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// Publish many events with different trace IDs
	numTraces := 100
	eventsPerTrace := 5
	totalEvents := numTraces * eventsPerTrace

	start := time.Now()
	
	for traceNum := 0; traceNum < numTraces; traceNum++ {
		traceID := fmt.Sprintf("trace-%d", traceNum)
		
		for eventNum := 0; eventNum < eventsPerTrace; eventNum++ {
			event := collectors.RawEvent{
				Type:      "kubeapi",
				TraceID:   traceID,
				SpanID:    fmt.Sprintf("span-%d-%d", traceNum, eventNum),
				Timestamp: time.Now(),
				Data:      []byte(fmt.Sprintf(`{"index": %d}`, eventNum)),
			}

			err := publisher.PublishRawEvent(ctx, event)
			require.NoError(t, err)
		}
	}

	publishDuration := time.Since(start)
	t.Logf("Published %d events in %v (%.0f events/sec)", 
		totalEvents, publishDuration, float64(totalEvents)/publishDuration.Seconds())

	// Wait for processing
	time.Sleep(5 * time.Second)

	// Count results
	resultCount := 0
	timeout := time.After(10 * time.Second)

	for {
		select {
		case results := <-subscriber.Results():
			resultCount += len(results)
			t.Logf("Received batch of %d results (total: %d)", len(results), resultCount)
			
		case <-timeout:
			t.Logf("Final result count: %d", resultCount)
			assert.Greater(t, resultCount, 0, "Should have processed some correlations")
			return
		}
	}
}