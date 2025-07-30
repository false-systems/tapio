package pipeline

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
	"github.com/yairfalse/tapio/pkg/collectors/k8s"
)

// ExampleMinimalCollectorSetup demonstrates how to set up collectors with the new pipeline
func ExampleMinimalCollectorSetup() {
	ctx := context.Background()

	// 1. Create collector registry
	registry := collectors.NewRegistry()

	// 2. Create and register collectors
	// eBPF collector - minimal, only memory tracking
	ebpfCollector, err := ebpf.NewSimpleCollector(collectors.DefaultCollectorConfig())
	if err != nil {
		log.Fatalf("Failed to create eBPF collector: %v", err)
	}
	registry.Register("ebpf", ebpfCollector)

	// K8s collector - minimal, just raw events
	k8sCollector, err := k8s.NewSimpleK8sCollector(collectors.DefaultCollectorConfig())
	if err != nil {
		log.Fatalf("Failed to create K8s collector: %v", err)
	}
	registry.Register("k8s", k8sCollector)

	// 3. Create bridge with intelligence pipeline
	bridgeConfig := BridgeConfig{
		CollectorBufferSize:    10000,
		CollectorWorkers:       4,
		EnableK8sEnrichment:    true, // K8s context added in pipeline, not collector
		EnableTracing:          true,
		KubeConfig:             "", // Use in-cluster config
		IntelligenceMode:       "high-performance",
		IntelligenceWorkers:    8,
		IntelligenceBufferSize: 50000,
	}

	bridge, err := CreateDefaultBridge(bridgeConfig)
	if err != nil {
		log.Fatalf("Failed to create bridge: %v", err)
	}

	// 4. Start everything
	if err := registry.Start(ctx); err != nil {
		log.Fatalf("Failed to start collectors: %v", err)
	}

	if err := bridge.Start(ctx); err != nil {
		log.Fatalf("Failed to start bridge: %v", err)
	}

	// 5. Forward events from collectors to bridge
	go func() {
		for event := range registry.Events() {
			if err := bridge.ProcessRawEvent(ctx, event); err != nil {
				log.Printf("Failed to process event: %v", err)
			}
		}
	}()

	// 6. Monitor metrics
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			metrics := bridge.GetMetrics()
			fmt.Printf("Bridge metrics: %+v\n", metrics)

		case <-ctx.Done():
			// Graceful shutdown
			registry.Stop()
			bridge.Stop()
			return
		}
	}
}

// ExampleCustomCollector shows how to add a custom collector
func ExampleCustomCollector() {
	// Example of how a minimal custom etcd collector would look:
	// 1. Implement the Collector interface
	// 2. Emit raw events with Type="etcd"
	// 3. Create a converter that handles "etcd" events
	// 4. Register both with the system

	// The key is that the collector only watches etcd and emits raw data
	// All parsing, enrichment, and intelligence happens in the pipeline

	fmt.Println("See code comments for custom collector example")
}

// ExampleProductionSetup shows a production-ready setup
func ExampleProductionSetup() {
	// This demonstrates the complete flow:
	// 1. Minimal collectors emit raw bytes
	// 2. Pipeline converts raw bytes to UnifiedEvents
	// 3. Pipeline enriches with K8s context
	// 4. Intelligence pipeline does correlation and analysis

	// The key benefits:
	// - Collectors are tiny (single BPF program for eBPF)
	// - No business logic in collectors
	// - K8s context added centrally in pipeline
	// - Easy to add new collectors (etcd, prometheus, etc)
	// - All intelligence in the intelligence package

	fmt.Println("Production setup example - see code for details")
}
