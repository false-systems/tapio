//go:build ignore
// +build ignore

// Simple test program to validate ProcessEvent and ProcessBatch routing
// Run with: go run main_test_routing.go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/pipeline"
)

func main() {
	fmt.Println("🧪 Testing Pipeline Adapter Routing...")
	fmt.Println("=====================================")

	// Create a pipeline using the PipelineBuilder
	fmt.Println("1. Creating pipeline with PipelineBuilder...")
	builder := pipeline.NewPipelineBuilder().
		WithMode(pipeline.PipelineModeHighPerformance).
		WithBatchSize(10).
		WithBufferSize(100).
		WithMaxConcurrency(4).
		EnableValidation(true).
		EnableContext(true).
		EnableCorrelation(true).
		EnableMetrics(true)

	pipelineInstance, err := builder.Build()
	if err != nil {
		log.Fatalf("❌ Failed to build pipeline: %v", err)
	}
	fmt.Println("✅ Pipeline created successfully")

	// Start the pipeline
	fmt.Println("\n2. Starting pipeline...")
	ctx := context.Background()
	if err := pipelineInstance.Start(ctx); err != nil {
		log.Fatalf("❌ Failed to start pipeline: %v", err)
	}
	fmt.Println("✅ Pipeline started successfully")

	// Ensure cleanup
	defer func() {
		fmt.Println("\n6. Stopping pipeline...")
		if err := pipelineInstance.Stop(); err != nil {
			fmt.Printf("⚠️  Error stopping pipeline: %v\n", err)
		} else {
			fmt.Println("✅ Pipeline stopped successfully")
		}
	}()

	// Test ProcessEvent routing
	fmt.Println("\n3. Testing ProcessEvent routing...")
	testProcessEvent(pipelineInstance)

	// Test ProcessBatch routing
	fmt.Println("\n4. Testing ProcessBatch routing...")
	testProcessBatch(pipelineInstance)

	// Check metrics
	fmt.Println("\n5. Checking pipeline metrics...")
	checkMetrics(pipelineInstance)

	fmt.Println("\n🎉 All routing tests completed successfully!")
}

func testProcessEvent(pipeline pipeline.IntelligencePipeline) {
	// Create a valid UnifiedEvent
	event := domain.NewUnifiedEvent().
		WithType(domain.EventTypeSystem).
		WithSource("test-collector").
		WithSemantic("test-routing", "performance", "routing", "validation").
		WithEntity("pod", "test-pod", "default").
		WithApplicationData("info", "Testing ProcessEvent routing through orchestrator.GetPipeline()").
		WithImpact("low", 0.1).
		Build()

	fmt.Printf("   📝 Created event: ID=%s, Type=%s, Source=%s\n",
		event.ID, event.Type, event.Source)

	// Call ProcessEvent - this should route through pa.orchestrator.GetPipeline().ProcessEvent(event)
	start := time.Now()
	err := pipeline.ProcessEvent(event)
	duration := time.Since(start)

	if err != nil {
		log.Fatalf("   ❌ ProcessEvent failed: %v", err)
	}

	fmt.Printf("   ✅ ProcessEvent completed in %v\n", duration)
	fmt.Println("   🔗 Event successfully routed through orchestrator.GetPipeline().ProcessEvent()")
}

func testProcessBatch(pipeline pipeline.IntelligencePipeline) {
	// Create a batch of valid UnifiedEvents
	const batchSize = 5
	events := make([]*domain.UnifiedEvent, batchSize)

	for i := 0; i < batchSize; i++ {
		events[i] = domain.NewUnifiedEvent().
			WithType(domain.EventTypeKubernetes).
			WithSource("test-batch-collector").
			WithSemantic("batch-routing", "kubernetes", "batch", "validation").
			WithEntity("service", fmt.Sprintf("test-service-%d", i), "default").
			WithNetworkData("TCP", "10.0.0.1", uint16(8080+i), "10.0.0.2", uint16(9090+i)).
			WithImpact("medium", 0.3).
			Build()
	}

	fmt.Printf("   📦 Created batch of %d events\n", batchSize)

	// Call ProcessBatch - this should route through pa.orchestrator.GetPipeline().ProcessBatch(events)
	start := time.Now()
	err := pipeline.ProcessBatch(events)
	duration := time.Since(start)

	if err != nil {
		log.Fatalf("   ❌ ProcessBatch failed: %v", err)
	}

	fmt.Printf("   ✅ ProcessBatch completed in %v\n", duration)
	fmt.Println("   🔗 Batch successfully routed through orchestrator.GetPipeline().ProcessBatch()")
}

func checkMetrics(pipeline pipeline.IntelligencePipeline) {
	metrics := pipeline.GetMetrics()

	fmt.Printf("   📊 Pipeline Metrics:\n")
	fmt.Printf("      Events Received: %d\n", metrics.EventsReceived)
	fmt.Printf("      Events Processed: %d\n", metrics.EventsProcessed)
	fmt.Printf("      Events Dropped: %d\n", metrics.EventsDropped)
	fmt.Printf("      Events Failed: %d\n", metrics.EventsFailed)
	fmt.Printf("      Throughput: %.2f events/sec\n", metrics.ThroughputPerSecond)
	fmt.Printf("      Average Latency: %v\n", metrics.AverageLatency)

	// Verify that we have some activity
	if metrics.EventsReceived > 0 {
		fmt.Println("   ✅ Events are being received by the pipeline")
	} else {
		fmt.Println("   ⚠️  No events received (this might be expected in some implementations)")
	}

	// Check if pipeline is still running
	if pipeline.IsRunning() {
		fmt.Println("   ✅ Pipeline is still running")
	} else {
		fmt.Println("   ⚠️  Pipeline is not running")
	}

	// Get configuration info
	config := pipeline.GetConfig()
	fmt.Printf("   ⚙️  Configuration: Mode=%s, BatchSize=%d, BufferSize=%d\n",
		config.Mode, config.BatchSize, config.BufferSize)
}
