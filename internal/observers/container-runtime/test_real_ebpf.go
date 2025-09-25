//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	cr "github.com/yairfalse/tapio/internal/observers/container-runtime"
	"github.com/yairfalse/tapio/pkg/domain"
)

func main() {
	fmt.Println("=== Starting Real eBPF Observer Test ===")

	config := cr.NewDefaultConfig("real-test")
	config.EnableOOMKill = true
	config.EnableMemoryPressure = true
	config.EnableProcessExit = true
	config.EnableProcessFork = true

	observer, err := cr.NewObserver("real-test", config)
	if err != nil {
		log.Fatalf("Failed to create observer: %v", err)
	}

	ctx := context.Background()
	if err := observer.Start(ctx); err != nil {
		log.Fatalf("Failed to start observer: %v", err)
	}

	fmt.Println("✓ eBPF programs attached successfully!")

	// Listen for events
	eventCount := 0
	events := observer.Events()
	go func() {
		for event := range events {
			eventCount++
			fmt.Printf("\n🔔 EVENT #%d CAPTURED!\n", eventCount)
			fmt.Printf("   Type: %s\n", event.Type)
			fmt.Printf("   Source: %s\n", event.Source)
			fmt.Printf("   Severity: %s\n", event.Severity)
			fmt.Printf("   Event ID: %s\n", event.EventID)

			if event.CorrelationHints != nil {
				if event.CorrelationHints.ContainerID != "" {
					fmt.Printf("   Container ID: %s\n", event.CorrelationHints.ContainerID)
				}
				if event.CorrelationHints.CgroupPath != "" {
					fmt.Printf("   Cgroup Path: %s\n", event.CorrelationHints.CgroupPath)
				}
			}

			if containerData, ok := event.EventData.(domain.EventDataContainer); ok {
				if containerData.Process != nil {
					fmt.Printf("   PID: %d\n", containerData.Process.PID)
					if containerData.Process.Command != "" {
						fmt.Printf("   Command: %s\n", containerData.Process.Command)
					}
				}
				if containerData.Container != nil && containerData.Container.ExitCode != nil {
					fmt.Printf("   Exit Code: %d\n", *containerData.Container.ExitCode)
				}
			}
			fmt.Println("   ---")
		}
	}()

	fmt.Println("\nObserver is running. Waiting for events...")
	fmt.Println("\nTo test, run these commands in another terminal:")
	fmt.Println("  1. Simple process: sleep 1 & kill $!")
	fmt.Println("  2. Docker: docker run --rm alpine echo 'test'")
	fmt.Println("  3. Memory limit: docker run --rm -m 10m alpine sh -c 'dd if=/dev/zero of=/dev/null bs=1M'")
	fmt.Println("  4. Force kill: sleep 100 & PID=$!; sleep 0.5; kill -9 $PID")
	fmt.Println("\nPress Ctrl+C to stop...\n")

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Periodic stats
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			stats := observer.Statistics()
			fmt.Printf("\n📊 Stats: %d events processed, %d dropped, %d errors\n",
				stats.EventsProcessed, stats.EventsDropped, stats.ErrorCount)

		case <-sigChan:
			fmt.Println("\n\nShutting down...")

			stats := observer.Statistics()
			fmt.Printf("\n=== Final Statistics ===\n")
			fmt.Printf("Total events captured: %d\n", eventCount)
			fmt.Printf("Events processed: %d\n", stats.EventsProcessed)
			fmt.Printf("Events dropped: %d\n", stats.EventsDropped)
			fmt.Printf("Errors: %d\n", stats.ErrorCount)

			if err := observer.Stop(); err != nil {
				log.Printf("Error stopping observer: %v", err)
			}

			fmt.Println("✓ Observer stopped successfully")

			if eventCount == 0 {
				fmt.Println("\n⚠️  No events were captured!")
				fmt.Println("Possible reasons:")
				fmt.Println("  1. No container processes are running")
				fmt.Println("  2. The cgroup filtering might be too restrictive")
				fmt.Println("  3. The eBPF programs might not be triggering")
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
}
