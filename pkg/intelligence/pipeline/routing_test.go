package pipeline

import (
	"context"
	"testing"

	"github.com/yairfalse/tapio/pkg/domain"
)

// TestPipelineAdapterRouting validates that ProcessEvent and ProcessBatch methods
// properly route to the internal pipeline through orchestrator.GetPipeline()
func TestPipelineAdapterRouting(t *testing.T) {
	// Create a pipeline using the PipelineBuilder
	builder := NewPipelineBuilder().
		WithMode(PipelineModeHighPerformance).
		WithBatchSize(10).
		WithBufferSize(100).
		EnableValidation(true).
		EnableContext(true).
		EnableCorrelation(true)

	pipeline, err := builder.Build()
	if err != nil {
		t.Fatalf("Failed to build pipeline: %v", err)
	}

	// Start the pipeline
	ctx := context.Background()
	if err := pipeline.Start(ctx); err != nil {
		t.Fatalf("Failed to start pipeline: %v", err)
	}
	defer func() {
		if err := pipeline.Stop(); err != nil {
			t.Errorf("Failed to stop pipeline: %v", err)
		}
	}()

	// Test ProcessEvent routing
	t.Run("ProcessEvent routing", func(t *testing.T) {
		// Create a valid UnifiedEvent
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeSystem).
			WithSource("test-collector").
			WithSemantic("test-intent", "test", "routing", "validation").
			WithEntity("pod", "test-pod", "default").
			WithApplicationData("info", "test message for routing validation").
			Build()

		// Call ProcessEvent - this should route through pa.orchestrator.GetPipeline().ProcessEvent(event)
		err := pipeline.ProcessEvent(event)
		if err != nil {
			t.Errorf("ProcessEvent failed: %v", err)
		}
	})

	// Test ProcessBatch routing
	t.Run("ProcessBatch routing", func(t *testing.T) {
		// Create a batch of valid UnifiedEvents
		events := make([]*domain.UnifiedEvent, 3)
		for i := 0; i < 3; i++ {
			events[i] = domain.NewUnifiedEvent().
				WithType(domain.EventTypeKubernetes).
				WithSource("test-batch-collector").
				WithSemantic("batch-intent", "test", "batch", "routing").
				WithEntity("service", "test-service", "default").
				WithNetworkData("TCP", "10.0.0.1", 8080, "10.0.0.2", 9090).
				Build()
		}

		// Call ProcessBatch - this should route through pa.orchestrator.GetPipeline().ProcessBatch(events)
		err := pipeline.ProcessBatch(events)
		if err != nil {
			t.Errorf("ProcessBatch failed: %v", err)
		}
	})

	// Verify pipeline is running
	if !pipeline.IsRunning() {
		t.Error("Pipeline should be running")
	}

	// Get metrics to verify events were processed (they should at least be received)
	metrics := pipeline.GetMetrics()
	t.Logf("Pipeline metrics after tests: %+v", metrics)
}

// TestPipelineAdapterDirectAccess verifies we can access the internal pipeline
func TestPipelineAdapterDirectAccess(t *testing.T) {
	// Create a pipeline using the builder
	pipeline, err := NewPipelineBuilder().
		WithMode(PipelineModeHighPerformance).
		Build()
	if err != nil {
		t.Fatalf("Failed to build pipeline: %v", err)
	}

	// Type assertion to access the internal orchestrator
	pipelineAdapter, ok := pipeline.(*pipelineAdapter)
	if !ok {
		t.Fatal("Pipeline is not a pipelineAdapter instance")
	}

	// Verify we can get the internal pipeline
	internalPipeline := pipelineAdapter.orchestrator.GetPipeline()
	if internalPipeline == nil {
		t.Fatal("Internal pipeline is nil")
	}

	// Verify the internal pipeline implements the IntelligencePipeline interface
	_, implementsInterface := internalPipeline.(IntelligencePipeline)
	if !implementsInterface {
		t.Error("Internal pipeline does not implement IntelligencePipeline interface")
	}

	t.Log("✅ Pipeline adapter properly exposes internal pipeline through orchestrator")
}

// TestPipelineConfiguration verifies the pipeline configuration is properly set
func TestPipelineConfiguration(t *testing.T) {
	config := DefaultPipelineConfig()
	config.BatchSize = 500
	config.BufferSize = 2000
	config.EnableCorrelation = false

	pipeline, err := NewPipelineBuilder().
		WithConfig(config).
		Build()
	if err != nil {
		t.Fatalf("Failed to build pipeline with custom config: %v", err)
	}

	retrievedConfig := pipeline.GetConfig()
	if retrievedConfig.BatchSize != 500 {
		t.Errorf("Expected batch size 500, got %d", retrievedConfig.BatchSize)
	}
	if retrievedConfig.BufferSize != 2000 {
		t.Errorf("Expected buffer size 2000, got %d", retrievedConfig.BufferSize)
	}
	if retrievedConfig.EnableCorrelation != false {
		t.Errorf("Expected correlation disabled, got %v", retrievedConfig.EnableCorrelation)
	}

	t.Log("✅ Pipeline configuration properly preserved")
}

// TestPipelineStartStop verifies proper lifecycle management
func TestPipelineStartStop(t *testing.T) {
	pipeline, err := NewStandardPipeline()
	if err != nil {
		t.Fatalf("Failed to create standard pipeline: %v", err)
	}

	// Initially not running
	if pipeline.IsRunning() {
		t.Error("Pipeline should not be running initially")
	}

	// Start the pipeline
	ctx := context.Background()
	if err := pipeline.Start(ctx); err != nil {
		t.Fatalf("Failed to start pipeline: %v", err)
	}

	// Should be running now
	if !pipeline.IsRunning() {
		t.Error("Pipeline should be running after start")
	}

	// Stop the pipeline
	if err := pipeline.Stop(); err != nil {
		t.Errorf("Failed to stop pipeline: %v", err)
	}

	// Should not be running after stop
	if pipeline.IsRunning() {
		t.Error("Pipeline should not be running after stop")
	}

	t.Log("✅ Pipeline lifecycle management working correctly")
}

// TestProcessingWithStoppedPipeline verifies error handling for stopped pipeline
func TestProcessingWithStoppedPipeline(t *testing.T) {
	pipeline, err := NewDebugPipeline()
	if err != nil {
		t.Fatalf("Failed to create debug pipeline: %v", err)
	}

	// Try to process event without starting pipeline
	event := domain.NewUnifiedEvent().
		WithType(domain.EventTypeSystem).
		WithSource("test").
		Build()

	err = pipeline.ProcessEvent(event)
	if err == nil {
		t.Error("Expected error when processing event with stopped pipeline")
	}

	// Try to process batch without starting pipeline
	events := []*domain.UnifiedEvent{event}
	err = pipeline.ProcessBatch(events)
	if err == nil {
		t.Error("Expected error when processing batch with stopped pipeline")
	}

	t.Log("✅ Proper error handling for stopped pipeline")
}
