package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/systemd"
)

func main() {
	fmt.Println("Systemd Collector Test")
	fmt.Println("=====================")

	// Create config
	config := systemd.DefaultConfig()
	config.BufferSize = 100

	// Create collector
	collector, err := systemd.New(config)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start collector
	fmt.Println("Starting collector...")
	if err := collector.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}
	fmt.Println("Collector started successfully")

	// Monitor events
	go func() {
		eventCount := 0
		for event := range collector.Events() {
			eventCount++
			fmt.Printf("\n[Event #%d] %s\n", eventCount, time.Now().Format(time.RFC3339))
			fmt.Printf("  Type: %s\n", event.Type)
			fmt.Printf("  Metadata: %v\n", event.Metadata)
			if eventCount <= 5 {
				// Print full data for first 5 events
				fmt.Printf("  Data: %s\n", string(event.Data))
			} else if eventCount%10 == 0 {
				fmt.Printf("  (Received %d events total)\n", eventCount)
			}
		}
	}()

	// Wait for signal
	fmt.Println("\nMonitoring systemd events. Press Ctrl+C to stop...")
	<-sigChan

	fmt.Println("\nStopping collector...")
	if err := collector.Stop(); err != nil {
		log.Printf("Error stopping collector: %v", err)
	}
	fmt.Println("Collector stopped")
}
