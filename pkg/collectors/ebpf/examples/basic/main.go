package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
)

func main() {
	// Create configuration
	config := ebpf.DefaultConfig()
	config.Name = "example-ebpf-collector"

	// Create collector
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start collector
	if err := collector.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}
	fmt.Println("eBPF collector started successfully")

	// Start event processor
	go func() {
		for event := range collector.Events() {
			fmt.Printf("Event: ID=%s Type=%s Source=%s Severity=%s\n",
				event.ID, event.Type, event.Source, event.GetSeverity())

			// Print kernel details if available
			if event.Kernel != nil {
				fmt.Printf("  Kernel: PID=%d Comm=%s\n", event.Kernel.PID, event.Kernel.Comm)
			}
			// Print semantic context if available
			if event.Semantic != nil {
				fmt.Printf("  Semantic: Intent=%s Category=%s\n", event.Semantic.Intent, event.Semantic.Category)
			}
		}
	}()

	// Start health monitor
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				health := collector.Health()
				stats := collector.Statistics()

				fmt.Printf("\nHealth Status: %s - %s\n", health.Status, health.Message)
				fmt.Printf("Events: Collected=%d, Dropped=%d, Errors=%d\n",
					health.EventsProcessed, health.EventsDropped, health.ErrorCount)
				fmt.Printf("Programs Loaded: %d, Events/sec: %.2f\n",
					stats.ProgramsLoaded,
					stats.Custom["events_per_second"].(float64))

			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for signal
	<-sigChan
	fmt.Println("\nShutting down...")

	// Stop collector
	if err := collector.Stop(); err != nil {
		log.Printf("Error stopping collector: %v", err)
	}

	fmt.Println("Collector stopped successfully")
}
