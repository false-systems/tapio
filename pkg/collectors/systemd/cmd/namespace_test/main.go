package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/systemd"
)

func main() {
	fmt.Println("Systemd Namespace Monitoring Test")
	fmt.Println("================================")
	fmt.Println("This will monitor systemd events related to namespace operations")
	fmt.Println()

	// Create config
	config := systemd.DefaultConfig()
	config.BufferSize = 1000

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
	fmt.Println()

	// Track namespace-related events
	namespaceEvents := 0
	dockerEvents := 0
	mountEvents := 0

	// Monitor events
	go func() {
		for event := range collector.Events() {
			unit := event.Metadata["unit"]
			unitType := event.Metadata["unit_type"]
			eventType := event.Metadata["event_type"]

			// Filter for interesting events
			isInteresting := false
			eventCategory := ""

			// Docker/containerd related
			if strings.Contains(unit, "docker") || strings.Contains(unit, "containerd") {
				isInteresting = true
				eventCategory = "DOCKER"
				dockerEvents++
			}

			// Namespace mount points
			if strings.Contains(unit, "netns") || strings.Contains(unit, "run-docker") {
				isInteresting = true
				eventCategory = "NAMESPACE"
				namespaceEvents++
			}

			// Mount operations
			if unitType == "mount" && strings.Contains(unit, "docker") {
				isInteresting = true
				eventCategory = "MOUNT"
				mountEvents++
			}

			// Service state changes
			if unitType == "service" && (unit == "docker.service" || unit == "containerd.service") {
				isInteresting = true
				eventCategory = "SERVICE"
			}

			if isInteresting {
				fmt.Printf("\n[%s] %s - %s\n", time.Now().Format("15:04:05"), eventCategory, eventType)
				fmt.Printf("  Unit: %s (%s)\n", unit, unitType)
				fmt.Printf("  State: %s / %s\n", event.Metadata["active_state"], event.Metadata["sub_state"])

				// Parse and show mount details
				if unitType == "mount" {
					var data map[string]interface{}
					if err := json.Unmarshal(event.Data, &data); err == nil {
						fmt.Printf("  Mount Path: %s\n", extractMountPath(unit))
					}
				}
			}
		}
	}()

	fmt.Println("Monitoring namespace and Docker-related events...")
	fmt.Println()
	fmt.Println("Try these commands to generate events:")
	fmt.Println("  docker run --rm alpine echo 'Hello from container'")
	fmt.Println("  docker run --rm --network host alpine echo 'Hello from host network'")
	fmt.Println("  docker run --rm --pid host alpine ps aux")
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop...")

	// Periodic summary
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			fmt.Printf("\n--- Summary: Namespace: %d, Docker: %d, Mount: %d ---\n",
				namespaceEvents, dockerEvents, mountEvents)
		}
	}()

	// Wait for signal
	<-sigChan
	ticker.Stop()

	fmt.Println("\nStopping collector...")
	if err := collector.Stop(); err != nil {
		log.Printf("Error stopping collector: %v", err)
	}

	// Final summary
	fmt.Printf("\nFinal Summary:\n")
	fmt.Printf("  Namespace Events: %d\n", namespaceEvents)
	fmt.Printf("  Docker Events: %d\n", dockerEvents)
	fmt.Printf("  Mount Events: %d\n", mountEvents)
}

// extractMountPath extracts the mount path from unit name
func extractMountPath(unit string) string {
	// Remove .mount suffix
	path := strings.TrimSuffix(unit, ".mount")
	// Replace systemd escaping
	path = strings.ReplaceAll(path, "\\x2d", "-")
	path = strings.ReplaceAll(path, "\\x2f", "/")
	return path
}
