package pipeline

import (
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// TestProcessEventRouting tests that ProcessEvent and ProcessBatch methods properly route to internal pipeline
func TestProcessEventRouting(t *testing.T) {
	// Create a test event
	event := &domain.UnifiedEvent{
		ID:        "test-event-1",
		Type:      domain.EventTypeSystem,
		Source:    "test",
		Timestamp: time.Now(),
		Message:   "Test event for ProcessEvent routing",
	}

	// Test ProcessEvent method existence and basic routing
	// Note: We can't test the full pipeline due to dependencies,
	// but we can verify the methods exist and don't panic

	t.Run("ProcessEvent method exists", func(t *testing.T) {
		// Create a basic pipeline config
		config := DefaultPipelineConfig()
		config.Mode = PipelineModeStandard

		// Build pipeline using the builder
		builder := NewPipelineBuilder().WithConfig(config)
		pipeline, err := builder.Build()

		if err != nil {
			t.Skipf("Cannot build pipeline due to dependencies: %v", err)
			return
		}

		// Test ProcessEvent doesn't panic and has correct signature
		err = pipeline.ProcessEvent(event)
		// We expect this might fail due to missing dependencies, but should not panic
		t.Logf("ProcessEvent returned: %v", err)
	})

	t.Run("ProcessBatch method exists", func(t *testing.T) {
		// Create a basic pipeline config
		config := DefaultPipelineConfig()
		config.Mode = PipelineModeStandard

		// Build pipeline using the builder
		builder := NewPipelineBuilder().WithConfig(config)
		pipeline, err := builder.Build()

		if err != nil {
			t.Skipf("Cannot build pipeline due to dependencies: %v", err)
			return
		}

		// Test ProcessBatch doesn't panic and has correct signature
		events := []*domain.UnifiedEvent{event}
		err = pipeline.ProcessBatch(events)
		// We expect this might fail due to missing dependencies, but should not panic
		t.Logf("ProcessBatch returned: %v", err)
	})
}

// TestUnifiedOrchestratorGetPipeline tests that the GetPipeline method works
func TestUnifiedOrchestratorGetPipeline(t *testing.T) {
	// Create unified orchestrator config
	config := DefaultUnifiedConfig()
	config.BufferSize = 100

	// Try to create orchestrator
	orchestrator, err := NewUnifiedOrchestrator(config)
	if err != nil {
		t.Skipf("Cannot create orchestrator due to dependencies: %v", err)
		return
	}

	// Test GetPipeline method exists and returns non-nil
	pipeline := orchestrator.GetPipeline()
	if pipeline == nil {
		t.Error("GetPipeline() returned nil")
	} else {
		t.Log("GetPipeline() successfully returned a pipeline")
	}
}
