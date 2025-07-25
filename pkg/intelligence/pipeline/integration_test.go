package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// MockCorrelationStore for testing
type MockCorrelationStore struct {
	stored []*CorrelationOutput
}

func (m *MockCorrelationStore) Store(output *CorrelationOutput) error {
	m.stored = append(m.stored, output)
	return nil
}

func (m *MockCorrelationStore) StoreBatch(outputs []*CorrelationOutput) error {
	m.stored = append(m.stored, outputs...)
	return nil
}

func (m *MockCorrelationStore) Query(filters map[string]interface{}) ([]*CorrelationOutput, error) {
	return m.stored, nil
}

func (m *MockCorrelationStore) GetSimilar(embedding []float32, threshold float64) ([]*CorrelationOutput, error) {
	return m.stored, nil
}

func TestPipelineIntegrationCompleteFlow(t *testing.T) {
	// Create mock storage
	mockStore := &MockCorrelationStore{}

	// Create ring buffer pipeline
	pipeline, err := NewRingBufferPipeline()
	if err != nil {
		t.Fatalf("Failed to create pipeline: %v", err)
	}

	// Create pipeline integration
	config := PipelineIntegrationConfig{
		Pipeline:         pipeline,
		CorrelationStore: mockStore,
		BatchSize:        10,
		FlushInterval:    100 * time.Millisecond,
		CacheSize:        100,
		BufferSize:       1024,
	}

	integration, err := NewPipelineIntegration(config)
	if err != nil {
		t.Fatalf("Failed to create integration: %v", err)
	}

	// Start the integration
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = integration.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start integration: %v", err)
	}
	defer integration.Stop()

	// Create test events with correlation potential
	testEvents := []*domain.UnifiedEvent{
		{
			ID:        "test-event-1",
			Type:      "network",
			Source:    "ebpf",
			Timestamp: time.Now(),
			Metadata: map[string]string{
				"correlation_id":         "test-correlation-1",
				"correlation_confidence": "0.85",
				"correlation_pattern":    "anomaly",
			},
			Data: map[string]interface{}{
				"tcp_syn_flood": true,
				"source_ip":     "192.168.1.100",
			},
		},
		{
			ID:        "test-event-2",
			Type:      "security",
			Source:    "k8s",
			Timestamp: time.Now(),
			Metadata: map[string]string{
				"correlation_id":         "test-correlation-2",
				"correlation_confidence": "0.92",
				"correlation_pattern":    "analytics",
			},
			Data: map[string]interface{}{
				"pod_failure": true,
				"namespace":   "production",
			},
		},
	}

	// Process events through the complete flow
	for _, event := range testEvents {
		err = integration.ProcessEvent(event)
		if err != nil {
			t.Errorf("Failed to process event %s: %v", event.ID, err)
		}
	}

	// Wait for processing and storage
	time.Sleep(500 * time.Millisecond)

	// Verify events were stored
	if len(mockStore.stored) == 0 {
		t.Error("Expected correlation outputs to be stored, but none were found")
	} else {
		t.Logf("Successfully stored %d correlation outputs", len(mockStore.stored))

		// Verify the first stored output
		output := mockStore.stored[0]
		if output.ProcessingStage != "correlation" {
			t.Errorf("Expected processing stage 'correlation', got '%s'", output.ProcessingStage)
		}

		if output.Confidence < 0.7 {
			t.Errorf("Expected confidence >= 0.7, got %f", output.Confidence)
		}

		if output.ResultType != CorrelationTypeCorrelation {
			t.Errorf("Expected result type 'correlation', got '%s'", output.ResultType)
		}
	}

	// Check integration metrics
	metrics := integration.GetMetrics()
	if metrics.OutputsStored == 0 {
		t.Error("Expected outputs to be stored, but metrics show 0")
	}

	t.Logf("Integration metrics: Stored=%d, Dropped=%d, CacheHits=%d, CacheMisses=%d",
		metrics.OutputsStored, metrics.OutputsDropped, metrics.CacheHits, metrics.CacheMisses)
}

func TestCorrelationCache(t *testing.T) {
	// Create pipeline integration with small cache
	mockStore := &MockCorrelationStore{}
	pipeline, _ := NewRingBufferPipeline()

	config := PipelineIntegrationConfig{
		Pipeline:         pipeline,
		CorrelationStore: mockStore,
		CacheSize:        2, // Small cache for testing eviction
		BufferSize:       1024,
	}

	integration, err := NewPipelineIntegration(config)
	if err != nil {
		t.Fatalf("Failed to create integration: %v", err)
	}

	// Create test correlation outputs
	output1 := &CorrelationOutput{
		OriginalEvent: &domain.UnifiedEvent{ID: "event-1", Source: "test", Type: "test"},
		ProcessedAt:   time.Now(),
		Confidence:    0.8,
		ResultType:    CorrelationTypeCorrelation,
	}

	output2 := &CorrelationOutput{
		OriginalEvent: &domain.UnifiedEvent{ID: "event-2", Source: "test", Type: "test"},
		ProcessedAt:   time.Now(),
		Confidence:    0.9,
		ResultType:    CorrelationTypeCorrelation,
	}

	output3 := &CorrelationOutput{
		OriginalEvent: &domain.UnifiedEvent{ID: "event-3", Source: "test", Type: "test"},
		ProcessedAt:   time.Now(),
		Confidence:    0.85,
		ResultType:    CorrelationTypeCorrelation,
	}

	// Add to cache
	integration.updateCorrelationCache(output1)
	integration.updateCorrelationCache(output2)

	// Verify cache contents
	key1 := integration.generateCacheKey(output1)
	key2 := integration.generateCacheKey(output2)

	if cached, exists := integration.GetCachedCorrelation(key1); !exists {
		t.Error("Expected output1 to be in cache")
	} else if cached.OriginalEvent.ID != "event-1" {
		t.Error("Cached output1 has wrong event ID")
	}

	if cached, exists := integration.GetCachedCorrelation(key2); !exists {
		t.Error("Expected output2 to be in cache")
	} else if cached.OriginalEvent.ID != "event-2" {
		t.Error("Cached output2 has wrong event ID")
	}

	// Add third output (should evict oldest)
	integration.updateCorrelationCache(output3)

	// Verify eviction (output1 should be evicted, output2 and output3 should remain)
	if _, exists := integration.GetCachedCorrelation(key1); exists {
		t.Error("Expected output1 to be evicted from cache")
	}

	key3 := integration.generateCacheKey(output3)
	if cached, exists := integration.GetCachedCorrelation(key3); !exists {
		t.Error("Expected output3 to be in cache")
	} else if cached.OriginalEvent.ID != "event-3" {
		t.Error("Cached output3 has wrong event ID")
	}

	// Check cache metrics
	metrics := integration.GetMetrics()
	if metrics.CacheHits == 0 {
		t.Error("Expected cache hits > 0")
	}
}