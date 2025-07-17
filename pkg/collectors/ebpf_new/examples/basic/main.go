package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	ebpf "github.com/yairfalse/tapio/pkg/collectors/ebpf_new"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

func main() {
	// Check if running on Linux with proper permissions
	if os.Geteuid() != 0 {
		log.Fatal("This example requires root privileges. Please run with sudo.")
	}

	// Create a simple configuration
	config := core.Config{
		Name:               "example-collector",
		Enabled:            true,
		EventBufferSize:    1000,
		RingBufferSize:     16384, // 16KB
		BatchSize:          10,
		CollectionInterval: 100 * time.Millisecond,
		MaxEventsPerSecond: 1000,
		Timeout:            30 * time.Second,
		Programs: []core.ProgramSpec{
			{
				Name:         "sync_monitor",
				Type:         core.ProgramTypeKprobe,
				AttachTarget: "sys_sync", // Monitor sync system call
				Maps: []core.MapSpec{
					{
						Name:       "sync_events",
						Type:       core.MapTypeRingBuf,
						KeySize:    0,
						ValueSize:  0,
						MaxEntries: 4096,
					},
				},
			},
		},
		Filter: core.Filter{
			// No filtering - collect all events
		},
	}

	// Create the collector
	fmt.Println("Creating eBPF collector...")
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}
	defer func() {
		fmt.Println("\nClosing collector...")
		collector.Close()
	}()

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start the collector
	fmt.Println("Starting event collection...")
	if err := collector.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}

	// Subscribe to events
	criteria := domain.QueryCriteria{
		TimeWindow: domain.TimeWindow{
			Start: time.Now(),
			End:   time.Now().Add(24 * time.Hour),
		},
	}

	options := domain.SubscriptionOptions{
		BufferSize: 100,
	}

	eventChan, err := collector.Subscribe(ctx, criteria, options)
	if err != nil {
		log.Fatalf("Failed to subscribe to events: %v", err)
	}

	fmt.Println("Collector is running. Monitoring sync() system calls...")
	fmt.Println("Run 'sync' command in another terminal to generate events.")
	fmt.Println("Press Ctrl+C to stop.\n")

	// Process events
	eventCount := 0
	go func() {
		for event := range eventChan {
			eventCount++
			fmt.Printf("[%d] Event received:\n", eventCount)
			fmt.Printf("  ID: %s\n", event.ID)
			fmt.Printf("  Type: %s\n", event.Type)
			fmt.Printf("  Timestamp: %s\n", event.Timestamp.Format("15:04:05.000"))
			fmt.Printf("  Severity: %s\n", event.Severity)
			
			// Type assert to get payload details
			if payload, ok := event.Payload.(domain.SystemEventPayload); ok {
				fmt.Printf("  Component: %s\n", payload.Component)
				fmt.Printf("  Operation: %s\n", payload.Operation)
				fmt.Printf("  Message: %s\n", payload.Message)
			}
			
			processName := "unknown"
			if event.Context.Labels != nil && event.Context.Labels["process_name"] != "" {
				processName = event.Context.Labels["process_name"]
			}
			pid := int32(-1)
			if event.Context.PID != nil {
				pid = *event.Context.PID
			}
			fmt.Printf("  Process: %s (PID: %d)\n", processName, pid)
			fmt.Println()
		}
	}()

	// Periodically show health and stats
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				health := collector.GetHealth()
				stats, _ := collector.GetStats()
				
				fmt.Println("\n--- Health Check ---")
				fmt.Printf("Status: %s - %s\n", health.Status, health.Message)
				fmt.Printf("Events collected: %d, dropped: %d\n",
					stats.EventsCollected, stats.EventsDropped)
				fmt.Println()
				
			case <-ctx.Done():
				return
			}
		}
	}()

	// Generate a test event
	go func() {
		time.Sleep(2 * time.Second)
		fmt.Println("Generating test event by calling sync()...")
		syscall.Sync()
	}()

	// Wait for shutdown signal
	<-sigChan
	fmt.Println("\nReceived shutdown signal...")

	// Stop the collector
	if err := collector.Stop(); err != nil {
		log.Printf("Error stopping collector: %v", err)
	}

	// Print final statistics
	stats, err := collector.GetStats()
	if err == nil {
		fmt.Println("\nFinal Statistics:")
		fmt.Printf("  Total events collected: %d\n", stats.EventsCollected)
		fmt.Printf("  Events dropped: %d\n", stats.EventsDropped)
		fmt.Printf("  Events filtered: %d\n", stats.EventsFiltered)
		fmt.Printf("  Collection errors: %d\n", stats.CollectionErrors)
		fmt.Printf("  Runtime: %s\n", time.Since(stats.StartTime).Round(time.Second))
	}
}