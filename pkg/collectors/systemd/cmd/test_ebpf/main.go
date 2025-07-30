package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/systemd"
)

func main() {
	fmt.Println("Systemd Collector with eBPF Enhancement Test")
	fmt.Println("===========================================")
	fmt.Println("This will monitor K8s service syscalls including namespace operations")
	fmt.Println()

	// Create config with eBPF enabled
	config := systemd.DefaultConfig()
	config.BufferSize = 1000
	config.Labels["enable_ebpf"] = "true"

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

	// Event counters
	eventCounts := make(map[string]int)

	// Monitor events
	go func() {
		for event := range collector.Events() {
			// Parse event data
			var data map[string]interface{}
			if err := json.Unmarshal(event.Data, &data); err != nil {
				continue
			}

			eventType := event.Metadata["event_type"]
			eventCounts[eventType]++

			// Special handling for namespace operations
			if eventType == "syscall_namespace" || eventType == "state_change" {
				fmt.Printf("\n[%s] %s\n", time.Now().Format("15:04:05"), eventType)
				fmt.Printf("  Source: %s\n", event.Metadata["source"])

				if service := event.Metadata["service_name"]; service != "" {
					fmt.Printf("  Service: %s\n", service)
				}

				if k8sRelated := event.Metadata["k8s_related"]; k8sRelated == "true" {
					fmt.Printf("  K8s Component: YES\n")
				}

				// Show namespace details
				if props, ok := data["properties"].(map[string]interface{}); ok {
					if details, ok := props["details"].(map[string]interface{}); ok {
						if flags, ok := details["flags"].(float64); ok {
							fmt.Printf("  Namespace Flags: %s\n", parseNamespaceFlags(uint32(flags)))
						}
						if nsPath, ok := details["namespace_path"].(string); ok && nsPath != "" {
							fmt.Printf("  Namespace Path: %s\n", nsPath)
						}
					}
				}

				fmt.Printf("  Full Data: %s\n", string(event.Data))
			}

			// Print summary every 100 events
			total := 0
			for _, count := range eventCounts {
				total += count
			}
			if total%100 == 0 {
				fmt.Printf("\nEvent Summary (Total: %d):\n", total)
				for eventType, count := range eventCounts {
					fmt.Printf("  %s: %d\n", eventType, count)
				}
			}
		}
	}()

	fmt.Println("Monitoring K8s service syscalls...")
	fmt.Println("Try these actions to generate events:")
	fmt.Println("  - kubectl create/delete pod")
	fmt.Println("  - kubectl exec into a pod")
	fmt.Println("  - docker/containerd operations")
	fmt.Println("  - systemctl restart kubelet")
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop...")

	// Wait for signal
	<-sigChan

	fmt.Println("\nStopping collector...")
	if err := collector.Stop(); err != nil {
		log.Printf("Error stopping collector: %v", err)
	}

	// Final summary
	fmt.Println("\nFinal Event Summary:")
	for eventType, count := range eventCounts {
		fmt.Printf("  %s: %d\n", eventType, count)
	}
}

// parseNamespaceFlags converts namespace flags to human-readable format
func parseNamespaceFlags(flags uint32) string {
	const (
		CLONE_NEWNS     = 0x00020000
		CLONE_NEWCGROUP = 0x02000000
		CLONE_NEWUTS    = 0x04000000
		CLONE_NEWIPC    = 0x08000000
		CLONE_NEWUSER   = 0x10000000
		CLONE_NEWPID    = 0x20000000
		CLONE_NEWNET    = 0x40000000
	)

	var namespaces []string
	if flags&CLONE_NEWNS != 0 {
		namespaces = append(namespaces, "MOUNT")
	}
	if flags&CLONE_NEWCGROUP != 0 {
		namespaces = append(namespaces, "CGROUP")
	}
	if flags&CLONE_NEWUTS != 0 {
		namespaces = append(namespaces, "UTS")
	}
	if flags&CLONE_NEWIPC != 0 {
		namespaces = append(namespaces, "IPC")
	}
	if flags&CLONE_NEWUSER != 0 {
		namespaces = append(namespaces, "USER")
	}
	if flags&CLONE_NEWPID != 0 {
		namespaces = append(namespaces, "PID")
	}
	if flags&CLONE_NEWNET != 0 {
		namespaces = append(namespaces, "NET")
	}

	if len(namespaces) == 0 {
		return fmt.Sprintf("0x%x", flags)
	}
	return fmt.Sprintf("%v (0x%x)", namespaces, flags)
}
