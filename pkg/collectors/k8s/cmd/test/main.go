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
	"github.com/yairfalse/tapio/pkg/collectors/k8s"
)

func main() {
	// Create collector config with eBPF enabled
	config := collectors.CollectorConfig{
		BufferSize: 1000,
		Labels: map[string]string{
			"enable_ebpf": "true",
			"environment": "test",
		},
	}

	// Create K8s collector
	collector, err := k8s.NewMinimalK8sCollector(config)
	if err != nil {
		log.Fatalf("Failed to create K8s collector: %v", err)
	}

	// Start collector
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("Starting K8s collector with eBPF monitoring...")
	if err := collector.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Process events
	go func() {
		eventCount := 0
		for event := range collector.Events() {
			eventCount++
			fmt.Printf("\n[Event #%d] Type: %s, Time: %s\n",
				eventCount, event.Type, event.Timestamp.Format(time.RFC3339))

			// Print metadata
			if len(event.Metadata) > 0 {
				fmt.Println("Metadata:")
				for k, v := range event.Metadata {
					fmt.Printf("  %s: %s\n", k, v)
				}
			}

			// Special handling for K8s syscall events
			if event.Type == "k8s_syscall" {
				fmt.Println("K8s Syscall Event Detected!")
			}
		}
	}()

	fmt.Println("K8s eBPF monitor is running. Press Ctrl+C to stop.")
	fmt.Println("\nMonitoring for:")
	fmt.Println("- Pod creation/deletion")
	fmt.Println("- Volume mounts")
	fmt.Println("- Container exec")
	fmt.Println("- Image pulls")
	fmt.Println("- Service connections")
	fmt.Println("- DNS queries")

	// Wait for interrupt
	<-sigChan
	fmt.Println("\nShutting down...")

	// Stop collector
	if err := collector.Stop(); err != nil {
		log.Printf("Error stopping collector: %v", err)
	}

	fmt.Println("Done!")
}
