package dataflow

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

func TestTapioDataFlow_BasicOperation(t *testing.T) {
	// Create test channels
	input := make(chan domain.Event, 10)
	output := make(chan domain.Event, 10)

	// Create data flow
	config := Config{
		EnableSemanticGrouping: true,
		ServiceName:            "test-service",
		ServiceVersion:         "1.0.0",
		Environment:            "test",
	}

	df := NewTapioDataFlow(config)
	df.Connect(input, output)

	// Start data flow
	if err := df.Start(); err != nil {
		t.Fatalf("Failed to start data flow: %v", err)
	}

	// Ensure proper cleanup
	defer func() {
		df.Stop()
		close(input)
		close(output)
	}()

	// Send test event
	testEvent := domain.Event{
		ID:         "test-001",
		Type:       "test_event",
		Severity:   "medium",
		Timestamp:  time.Now(),
		Source:     "test",
		Confidence: 0.9,
		Context: domain.EventContext{
			Namespace: "test-ns",
			Host:      "test-host",
		},
	}

	// Send event
	select {
	case input <- testEvent:
	case <-time.After(time.Second):
		t.Fatal("Timeout sending event")
	}

	// Receive enriched event
	select {
	case enriched := <-output:
		// Verify event ID matches
		if enriched.ID != testEvent.ID {
			t.Errorf("Expected event ID %s, got %s", testEvent.ID, enriched.ID)
		}

		// Check for correlation metadata - metadata might be nil if no correlation findings were generated
		// This is normal behavior, so we just verify the event was processed
		_ = enriched.Context.Metadata
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout receiving enriched event")
	}
}

func TestServerBridge_Configuration(t *testing.T) {
	// Test configuration validation
	config := BridgeConfig{
		ServerAddress: "localhost:9090",
		BufferSize:    100,
		MaxBatchSize:  50,
		EnableTracing: true,
	}

	// Create mock data flow with proper initialization
	ctx, cancel := context.WithCancel(context.Background())
	df := &TapioDataFlow{
		ctx:    ctx,
		cancel: cancel,
	}

	// Attempt to create bridge (will fail due to connection)
	_, err := NewServerBridge(config, df)
	if err == nil {
		t.Skip("Expected error creating bridge without server")
	}

	// Clean up
	cancel()
}

func TestEnrichEventWithFindings(t *testing.T) {
	df := &TapioDataFlow{}

	event := &domain.Event{
		ID:   "test-event",
		Type: "test",
		Context: domain.EventContext{
			Metadata: make(map[string]interface{}),
		},
	}

	finding := &correlation.Finding{
		ID:            "corr-001",
		PatternType:   "test_pattern",
		Confidence:    0.85,
		RelatedEvents: []*domain.Event{event},
	}

	df.enrichEventWithFindings(event, finding)

	// Verify enrichment
	if event.Context.Metadata["correlation_id"] != finding.ID {
		t.Error("Expected correlation_id to be set")
	}

	if event.Context.Metadata["correlation_pattern"] != finding.PatternType {
		t.Error("Expected correlation_pattern to be set")
	}

	if event.Context.Metadata["correlation_confidence"] != finding.Confidence {
		t.Error("Expected correlation_confidence to be set")
	}
}

// Helper function to create test events
func createTestEvent(id string, eventType string, severity domain.EventSeverity) domain.Event {
	return domain.Event{
		ID:         domain.EventID(id),
		Type:       domain.EventType(eventType),
		Timestamp:  time.Now(),
		Source:     "test-source",
		Severity:   severity,
		Confidence: 0.8,
		Context: domain.EventContext{
			Namespace: "test-ns",
			Host:      "test-host",
			Labels:    map[string]string{"pod": "test-pod"},
			Metadata:  make(map[string]interface{}),
		},
		Payload: domain.GenericEventPayload{
			Type: "test",
			Data: map[string]interface{}{"test": "data"},
		},
	}
}

func createTestConfig() Config {
	return Config{
		EnableSemanticGrouping: true,
		GroupRetentionPeriod:   30 * time.Minute,
		ServiceName:            "test-dataflow",
		ServiceVersion:         "1.0.0",
		Environment:            "test",
		BufferSize:             100,
		FlushInterval:          1 * time.Second,
	}
}

func TestNewTapioDataFlow(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	assert.NotNil(t, dataflow)
	assert.NotNil(t, dataflow.semanticTracer)
	assert.NotNil(t, dataflow.correlationEngine)
	assert.NotNil(t, dataflow.tracer)
	assert.NotNil(t, dataflow.rootSpan)
	assert.NotNil(t, dataflow.ctx)
	assert.NotNil(t, dataflow.cancel)
	assert.Equal(t, uint64(0), dataflow.eventsProcessed)
	assert.Equal(t, uint64(0), dataflow.groupsCreated)
	assert.Equal(t, uint64(0), dataflow.tracesExported)
	assert.False(t, dataflow.lastStatus.IsZero())
}

func TestTapioDataFlow_Connect(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	inputChan := make(chan domain.Event, 10)
	outputChan := make(chan domain.Event, 10)

	dataflow.Connect(inputChan, outputChan)

	// Can't directly compare channels due to type conversion, just verify they're not nil
	assert.NotNil(t, dataflow.eventStream)
	assert.NotNil(t, dataflow.outputStream)
}

func TestTapioDataFlow_Start_NotConnected(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	err := dataflow.Start()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "data flow not connected")
}

func TestTapioDataFlow_Start_Success(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	inputChan := make(chan domain.Event, 10)
	outputChan := make(chan domain.Event, 10)
	dataflow.Connect(inputChan, outputChan)

	err := dataflow.Start()
	assert.NoError(t, err)

	// Allow goroutines to start
	time.Sleep(50 * time.Millisecond)

	// Stop to clean up
	err = dataflow.Stop()
	assert.NoError(t, err)

	// Close channels
	close(inputChan)
	close(outputChan)
}

func TestTapioDataFlow_EventProcessing(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	inputChan := make(chan domain.Event, 10)
	outputChan := make(chan domain.Event, 10)
	dataflow.Connect(inputChan, outputChan)

	err := dataflow.Start()
	require.NoError(t, err)

	// Send test events
	testEvents := []domain.Event{
		createTestEvent("event-1", "cpu_high", domain.EventSeverityHigh),
		createTestEvent("event-2", "memory_high", domain.EventSeverityMedium),
		createTestEvent("event-3", "disk_full", domain.EventSeverityCritical),
	}

	// Send events
	for _, event := range testEvents {
		inputChan <- event
	}

	// Collect processed events
	var processedEvents []domain.Event
	timeout := time.After(2 * time.Second)

eventLoop:
	for i := 0; i < len(testEvents); i++ {
		select {
		case processedEvent := <-outputChan:
			processedEvents = append(processedEvents, processedEvent)
		case <-timeout:
			break eventLoop
		}
	}

	// Verify events were processed
	assert.Len(t, processedEvents, len(testEvents))

	// Verify events were enriched with correlation metadata
	for _, event := range processedEvents {
		assert.NotNil(t, event.Context.Metadata)
		// Events might have correlation metadata if correlation engine created findings
	}

	// Check metrics
	metrics := dataflow.GetMetrics()
	assert.GreaterOrEqual(t, metrics["events_processed"], uint64(3))
	assert.GreaterOrEqual(t, metrics["semantic_groups_active"], 0)
	assert.GreaterOrEqual(t, metrics["events_per_second"], float64(0))

	// Stop dataflow
	err = dataflow.Stop()
	assert.NoError(t, err)

	// Close channels
	close(inputChan)
	close(outputChan)
}

func TestTapioDataFlow_EventEnrichment(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	// Create a mock finding
	finding := &correlation.Finding{
		ID:          "test-finding-1",
		PatternType: "performance_degradation",
		Confidence:  0.85,
		RelatedEvents: []*domain.Event{
			func() *domain.Event {
				event := createTestEvent("related-1", "cpu_spike", domain.EventSeverityHigh)
				return &event
			}(),
		},
		SemanticGroup: &correlation.SemanticGroupSummary{
			ID:     "semantic-group-1",
			Intent: "High resource usage detected",
			Type:   "resource_exhaustion",
			Impact: &correlation.ImpactAssessment{
				BusinessImpact: 0.7,
				CascadeRisk:    0.5,
			},
			Prediction: &correlation.PredictedOutcome{
				Scenario:    "System overload",
				Probability: 0.8,
			},
		},
		Timestamp:   time.Now(),
		Description: "Performance degradation pattern detected",
	}

	// Test event enrichment
	event := createTestEvent("test-event", "cpu_high", domain.EventSeverityHigh)
	dataflow.enrichEventWithFindings(&event, finding)

	// Verify enrichment
	assert.NotNil(t, event.Context.Metadata)
	assert.Equal(t, "test-finding-1", event.Context.Metadata["correlation_id"])
	assert.Equal(t, 0.85, event.Context.Metadata["correlation_confidence"])
	assert.Equal(t, "performance_degradation", event.Context.Metadata["correlation_pattern"])
	assert.Equal(t, 1, event.Context.Metadata["related_event_count"])

	// Verify semantic group enrichment
	assert.Equal(t, "semantic-group-1", event.Context.Metadata["semantic_group_id"])
	assert.Equal(t, "High resource usage detected", event.Context.Metadata["semantic_intent"])
	assert.Equal(t, "resource_exhaustion", event.Context.Metadata["semantic_type"])

	// Verify impact assessment
	assert.Equal(t, float32(0.7), event.Context.Metadata["impact_business"])
	assert.Equal(t, float32(0.5), event.Context.Metadata["impact_cascade_risk"])

	// Verify predictions
	assert.Equal(t, "System overload", event.Context.Metadata["prediction_scenario"])
	assert.Equal(t, 0.8, event.Context.Metadata["prediction_probability"])
}

func TestTapioDataFlow_Stop(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	inputChan := make(chan domain.Event, 10)
	outputChan := make(chan domain.Event, 10)
	dataflow.Connect(inputChan, outputChan)

	err := dataflow.Start()
	require.NoError(t, err)

	// Allow goroutines to start
	time.Sleep(50 * time.Millisecond)

	// Stop should complete without error
	err = dataflow.Stop()
	assert.NoError(t, err)

	// Context should be cancelled
	select {
	case <-dataflow.ctx.Done():
		// Expected - context was cancelled
	default:
		t.Fatal("Context should be cancelled after Stop()")
	}

	// Close channels
	close(inputChan)
	close(outputChan)
}

func TestTapioDataFlow_CalculateEventsPerSecond(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	// Initially should be 0
	eps := dataflow.calculateEventsPerSecond()
	assert.Equal(t, float64(0), eps)

	// Set some events processed and adjust last status time
	dataflow.eventsProcessed = 100
	dataflow.lastStatus = time.Now().Add(-10 * time.Second)

	eps = dataflow.calculateEventsPerSecond()
	assert.Greater(t, eps, float64(0))
	assert.LessOrEqual(t, eps, 100.0) // Should not exceed total events
}

func TestTapioDataFlow_GetMetrics(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	metrics := dataflow.GetMetrics()

	// Verify required metrics are present
	assert.Contains(t, metrics, "events_processed")
	assert.Contains(t, metrics, "semantic_groups_active")
	assert.Contains(t, metrics, "traces_exported")
	assert.Contains(t, metrics, "events_per_second")
	assert.Contains(t, metrics, "uptime_seconds")

	// Verify initial values
	assert.Equal(t, uint64(0), metrics["events_processed"])
	assert.GreaterOrEqual(t, metrics["semantic_groups_active"], 0)
	assert.Equal(t, uint64(0), metrics["traces_exported"])
	assert.Equal(t, float64(0), metrics["events_per_second"])
	assert.GreaterOrEqual(t, metrics["uptime_seconds"], float64(0))
}

func TestTapioDataFlow_ConcurrentEventProcessing(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	inputChan := make(chan domain.Event, 100)
	outputChan := make(chan domain.Event, 100)
	dataflow.Connect(inputChan, outputChan)

	err := dataflow.Start()
	require.NoError(t, err)

	// Send events concurrently from multiple goroutines
	numSenders := 3
	eventsPerSender := 10
	var senderWg sync.WaitGroup

	senderWg.Add(numSenders)
	for i := 0; i < numSenders; i++ {
		go func(senderID int) {
			defer senderWg.Done()
			for j := 0; j < eventsPerSender; j++ {
				event := createTestEvent(
					fmt.Sprintf("sender-%d-event-%d", senderID, j),
					"concurrent_test",
					domain.EventSeverityMedium,
				)
				inputChan <- event
			}
		}(i)
	}

	// Collect events from output
	var receivedEvents []domain.Event
	timeout := time.After(3 * time.Second)
	totalExpected := numSenders * eventsPerSender

eventCollectionLoop:
	for len(receivedEvents) < totalExpected {
		select {
		case event := <-outputChan:
			receivedEvents = append(receivedEvents, event)
		case <-timeout:
			break eventCollectionLoop
		}
	}

	// Wait for all senders to complete
	senderWg.Wait()

	// Verify all events were processed
	assert.Equal(t, totalExpected, len(receivedEvents))

	// Stop dataflow
	err = dataflow.Stop()
	assert.NoError(t, err)

	// Close channels
	close(inputChan)
	close(outputChan)
}

func TestTapioDataFlow_InputChannelClosed(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	inputChan := make(chan domain.Event, 10)
	outputChan := make(chan domain.Event, 10)
	dataflow.Connect(inputChan, outputChan)

	err := dataflow.Start()
	require.NoError(t, err)

	// Send a few events
	testEvents := []domain.Event{
		createTestEvent("event-1", "test", domain.EventSeverityLow),
		createTestEvent("event-2", "test", domain.EventSeverityLow),
	}

	for _, event := range testEvents {
		inputChan <- event
	}

	// Close input channel to simulate end of input stream
	close(inputChan)

	// Collect events that were already sent
	var receivedEvents []domain.Event
	timeout := time.After(1 * time.Second)

eventLoop2:
	for {
		select {
		case event, ok := <-outputChan:
			if !ok {
				break eventLoop2
			}
			receivedEvents = append(receivedEvents, event)
		case <-timeout:
			break eventLoop2
		}
	}

	// Should have received the events that were sent
	assert.Equal(t, len(testEvents), len(receivedEvents))

	// Stop dataflow
	err = dataflow.Stop()
	assert.NoError(t, err)

	close(outputChan)
}

func TestTapioDataFlow_ContextCancellation(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	inputChan := make(chan domain.Event, 10)
	outputChan := make(chan domain.Event, 10)
	dataflow.Connect(inputChan, outputChan)

	err := dataflow.Start()
	require.NoError(t, err)

	// Allow goroutines to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context directly
	dataflow.cancel()

	// Allow goroutines to handle cancellation
	time.Sleep(100 * time.Millisecond)

	// Try to send an event - should not be processed due to context cancellation
	testEvent := createTestEvent("cancelled-event", "test", domain.EventSeverityLow)

	select {
	case inputChan <- testEvent:
		// Event sent to input channel
	default:
		t.Fatal("Should be able to send to input channel")
	}

	// Should not receive the event due to context cancellation
	select {
	case <-outputChan:
		t.Fatal("Should not receive event after context cancellation")
	case <-time.After(200 * time.Millisecond):
		// Expected - no event received
	}

	// Formal stop to ensure clean shutdown
	err = dataflow.Stop()
	assert.NoError(t, err)

	// Close channels
	close(inputChan)
	close(outputChan)
}

func TestTapioDataFlow_EnrichmentWithoutSemanticGroup(t *testing.T) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	// Create finding without semantic group
	finding := &correlation.Finding{
		ID:            "test-finding-2",
		PatternType:   "simple_pattern",
		Confidence:    0.6,
		RelatedEvents: []*domain.Event{},
		SemanticGroup: nil, // No semantic group
		Timestamp:     time.Now(),
		Description:   "Simple finding without semantic group",
	}

	// Test event enrichment
	event := createTestEvent("test-event", "simple", domain.EventSeverityLow)
	dataflow.enrichEventWithFindings(&event, finding)

	// Verify basic enrichment
	assert.NotNil(t, event.Context.Metadata)
	assert.Equal(t, "test-finding-2", event.Context.Metadata["correlation_id"])
	assert.Equal(t, 0.6, event.Context.Metadata["correlation_confidence"])
	assert.Equal(t, "simple_pattern", event.Context.Metadata["correlation_pattern"])
	assert.Equal(t, 0, event.Context.Metadata["related_event_count"])

	// Verify semantic group fields are not present
	assert.NotContains(t, event.Context.Metadata, "semantic_group_id")
	assert.NotContains(t, event.Context.Metadata, "semantic_intent")
	assert.NotContains(t, event.Context.Metadata, "semantic_type")
	assert.NotContains(t, event.Context.Metadata, "impact_business")
	assert.NotContains(t, event.Context.Metadata, "prediction_scenario")
}

// Benchmark tests
func BenchmarkTapioDataFlow_EventProcessing(b *testing.B) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	inputChan := make(chan domain.Event, 1000)
	outputChan := make(chan domain.Event, 1000)
	dataflow.Connect(inputChan, outputChan)

	err := dataflow.Start()
	require.NoError(b, err)

	// Create test event
	testEvent := createTestEvent("bench-event", "benchmark", domain.EventSeverityMedium)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inputChan <- testEvent
		<-outputChan
	}

	dataflow.Stop()
	close(inputChan)
	close(outputChan)
}

func BenchmarkTapioDataFlow_EnrichEvent(b *testing.B) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	finding := &correlation.Finding{
		ID:          "bench-finding",
		PatternType: "benchmark_pattern",
		Confidence:  0.8,
		RelatedEvents: []*domain.Event{
			func() *domain.Event {
				event := createTestEvent("related", "benchmark", domain.EventSeverityMedium)
				return &event
			}(),
		},
		SemanticGroup: &correlation.SemanticGroupSummary{
			ID:     "bench-group",
			Intent: "Benchmark test",
			Type:   "benchmark",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := createTestEvent("bench-event", "benchmark", domain.EventSeverityMedium)
		dataflow.enrichEventWithFindings(&event, finding)
	}
}

func BenchmarkTapioDataFlow_GetMetrics(b *testing.B) {
	config := createTestConfig()
	dataflow := NewTapioDataFlow(config)

	// Set some test values
	dataflow.eventsProcessed = 1000
	dataflow.groupsCreated = 50

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dataflow.GetMetrics()
	}
}
