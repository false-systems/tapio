package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	namespace_collector "github.com/yairfalse/tapio/pkg/collectors/namespace-collector"
)

func main() {
	fmt.Println("🚀 Starting Namespace eBPF Collector Test")
	fmt.Println("===================================")

	// Create collector config
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"test": "minikube",
			"env":  "development",
		},
	}

	// Create namespace collector
	collector, err := namespace_collector.NewCollector(config)
	if err != nil {
		log.Fatalf("Failed to create namespace collector: %v", err)
	}

	fmt.Printf("✅ Namespace Collector created: %s\n", collector.Name())
	fmt.Printf("✅ Health status: %v\n", collector.IsHealthy())

	// Start collector
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := collector.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	fmt.Println("✅ Collector started successfully")
	fmt.Println("📊 Monitoring namespace events...")
	fmt.Println("   - Network namespace creation (unshare)")
	fmt.Println("   - Network namespace entry (setns)")
	fmt.Println("   - Network namespace changes")
	fmt.Println("")

	// Handle shutdown gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Process events
	eventCount := 0
	go func() {
		for event := range collector.Events() {
			eventCount++
			fmt.Printf("🔍 Event #%d [%s] %s - %s\n",
				eventCount,
				event.Timestamp.Format("15:04:05"),
				event.Type,
				event.Metadata["source"])

			// Print event details every 10 events
			if eventCount%10 == 0 {
				fmt.Printf("   📈 Total events received: %d\n", eventCount)
			}
		}
	}()

	// Wait for shutdown or timeout
	select {
	case <-sigCh:
		fmt.Println("\n🛑 Shutdown signal received")
	case <-time.After(2 * time.Minute):
		fmt.Println("\n⏰ Test timeout reached")
	}

	cancel()
	fmt.Printf("📊 Final stats: %d total events collected\n", eventCount)
	fmt.Println("✅ Test completed")
}
