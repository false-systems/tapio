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
	"github.com/yairfalse/tapio/pkg/collectors/cni"
)

func main() {
	fmt.Println("🚀 Starting CNI eBPF Collector Test")
	fmt.Println("===================================")

	// Create collector config
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"test": "minikube",
			"env":  "development",
		},
	}

	// Create CNI collector
	collector, err := cni.NewCollector(config)
	if err != nil {
		log.Fatalf("Failed to create CNI collector: %v", err)
	}

	fmt.Printf("✅ CNI Collector created: %s\n", collector.Name())
	fmt.Printf("✅ Health status: %v\n", collector.IsHealthy())

	// Start collector
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := collector.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	fmt.Println("✅ Collector started successfully")
	fmt.Println("📊 Monitoring CNI events...")
	fmt.Println("   - execve syscalls for CNI binaries")
	fmt.Println("   - Network namespace creation/changes")
	fmt.Println("   - Network policy enforcement")
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
