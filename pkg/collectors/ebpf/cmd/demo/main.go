// Demo program showing container correlation in action
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
	if len(os.Args) < 2 {
		fmt.Println("Usage: demo <command>")
		fmt.Println("Commands:")
		fmt.Println("  show    - Show correlation demo")
		fmt.Println("  live    - Live event monitoring (requires root)")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "show":
		showCorrelationDemo()
	case "live":
		runLiveMonitoring()
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func showCorrelationDemo() {
	fmt.Println("=== K8s Correlation Demo: From Raw Events to Stories ===")
	fmt.Println()
	fmt.Println("Imagine these events happening in your K8s cluster:")
	fmt.Println()

	// Simulate timeline
	events := []struct {
		time   string
		source string
		event  string
	}{
		{"10:00:00", "KubeAPI", "Pod 'frontend-abc123' created in namespace 'prod'"},
		{"10:00:01", "Runtime", "Container 'nginx-def456' started with PID 12345"},
		{"10:00:02", "eBPF", "Process 12345 started in cgroup 567890"},
		{"10:00:02", "Tapio", "Correlation: PID 12345 → Container nginx-def456 → Pod frontend-abc123"},
		{"10:00:10", "KubeAPI", "Service 'backend-api' endpoint added: 10.244.1.15:8080"},
		{"10:00:15", "eBPF", "Process 12345 connected to 10.244.1.15:8080"},
		{"10:00:15", "Tapio", "Story: Frontend pod connected to backend-api service"},
		{"10:00:18", "eBPF", "Process 12345 opened /etc/config/app.yaml"},
		{"10:00:18", "Tapio", "Story: Frontend pod loaded ConfigMap 'app-config'"},
		{"10:00:20", "eBPF", "Process 12345 allocated 100MB memory"},
		{"10:00:20", "Tapio", "Story: Container nginx in pod frontend-abc123 is using high memory"},
		{"10:00:25", "eBPF", "Process 12345 read /etc/secrets/api-key"},
		{"10:00:25", "Tapio", "Security: Frontend pod accessed Secret 'api-credentials'"},
		{"10:00:30", "eBPF", "Process 12345 killed (OOM)"},
		{"10:00:30", "KubeAPI", "Pod 'frontend-abc123' container restarting"},
		{"10:00:31", "Tapio", "Narrative: Frontend pod crashed due to memory pressure after loading config and connecting to backend"},
	}

	for _, e := range events {
		fmt.Printf("[%s] %-8s | %s\n", e.time, e.source, e.event)
		time.Sleep(400 * time.Millisecond)
	}

	fmt.Println()
	fmt.Println("=== The Power of Correlation ===")
	fmt.Println("✓ Process PID ↔ Container ID (implemented)")
	fmt.Println("✓ Cgroup ID ↔ Pod UID (implemented)")
	fmt.Println("✓ Network connections ↔ Service endpoints (implemented)")
	fmt.Println("✓ File operations ↔ ConfigMaps/Secrets (implemented)")
	fmt.Println()
	fmt.Println("This enables intelligent narratives instead of raw events!")
}

func runLiveMonitoring() {
	if os.Getuid() != 0 {
		log.Fatal("Live monitoring requires root privileges")
	}

	fmt.Println("=== Live eBPF Monitoring with Container Correlation ===")
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	// Create collector
	collector, err := ebpf.NewCollector("live-demo")
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}

	// Start collector
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := collector.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Add some test correlations
	fmt.Println("Adding test correlations...")

	// Current process as example
	myPID := uint32(os.Getpid())
	collector.UpdateContainerInfo(myPID, "demo-container", "demo-pod", "tapio/demo:latest")
	collector.UpdatePodInfo(uint64(myPID), "demo-pod", "default", "demo")

	fmt.Printf("Added correlation for current process (PID %d)\n", myPID)
	fmt.Println()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Monitor events
	eventCh := collector.Events()
	eventCount := 0

	fmt.Println("Monitoring events...")
	for {
		select {
		case <-sigCh:
			fmt.Printf("\nReceived %d events\n", eventCount)
			return

		case event := <-eventCh:
			eventCount++

			// Show event with correlation
			fmt.Printf("[%s] %s event from PID %s",
				event.Timestamp.Format("15:04:05"),
				event.Type,
				event.Metadata["pid"])

			// Show correlation if available
			if containerID := event.Metadata["container_id"]; containerID != "" {
				fmt.Printf(" → Container: %s", containerID)
			}
			if podUID := event.Metadata["pod_uid"]; podUID != "" && podUID != "unknown" {
				fmt.Printf(" → Pod: %s", podUID)
			}

			fmt.Println()

			// Show narrative for interesting events
			if event.Type == "memory_alloc" && event.Metadata["size"] != "0" {
				comm := event.Metadata["comm"]
				size := event.Metadata["size"]
				if containerID := event.Metadata["container_id"]; containerID != "" {
					fmt.Printf("  → Story: Process '%s' in container '%s' allocated %s bytes\n",
						comm, containerID, size)
				}
			}
		}
	}
}
