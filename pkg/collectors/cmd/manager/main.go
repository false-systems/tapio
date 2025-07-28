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
	ebpfcore "github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
	k8score "github.com/yairfalse/tapio/pkg/collectors/k8s/core"
	systemdcore "github.com/yairfalse/tapio/pkg/collectors/systemd/core"
)

func main() {
	// Create manager with default configuration
	managerConfig := collectors.DefaultManagerConfig()
	manager := collectors.NewManager(managerConfig)

	// Configure and register collectors based on environment
	if err := registerCollectors(manager); err != nil {
		log.Fatalf("Failed to register collectors: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start the manager
	log.Println("Starting collector manager...")
	if err := manager.Start(ctx); err != nil {
		log.Fatalf("Failed to start manager: %v", err)
	}

	// Process events in a separate goroutine
	go processEvents(manager)

	// Monitor health in a separate goroutine
	go monitorHealth(manager)

	// Wait for shutdown signal
	<-sigChan
	log.Println("Received shutdown signal, stopping...")

	// Stop the manager
	if err := manager.Stop(); err != nil {
		log.Printf("Error stopping manager: %v", err)
	}

	log.Println("Collector manager stopped successfully")
}

func registerCollectors(manager *collectors.Manager) error {
	// K8s Collector
	if os.Getenv("ENABLE_K8S_COLLECTOR") != "false" {
		k8sConfig := k8score.Config{
			Name:             "k8s-main",
			Enabled:          true,
			EventBufferSize:  10000,
			InCluster:        os.Getenv("KUBERNETES_SERVICE_HOST") != "",
			WatchPods:        true,
			WatchNodes:       true,
			WatchServices:    true,
			WatchDeployments: true,
			WatchEvents:      true,
			ResyncPeriod:     30 * time.Second,
			EventRateLimit:   1000,
		}

		k8sCollector, err := collectors.CreateK8sCollector("k8s", k8sConfig)
		if err != nil {
			log.Printf("Failed to create K8s collector: %v (skipping)", err)
		} else {
			if err := manager.Register("k8s", k8sCollector); err != nil {
				return fmt.Errorf("failed to register k8s collector: %w", err)
			}
			log.Println("Registered K8s collector")
		}
	}

	// eBPF Collector (Linux only)
	if os.Getenv("ENABLE_EBPF_COLLECTOR") == "true" {
		ebpfConfig := ebpfcore.Config{
			Name:            "ebpf-main",
			Enabled:         true,
			EventBufferSize: 10000,
			Programs: []ebpfcore.ProgramSpec{
				{
					Name:       "network_monitor",
					Type:       ebpfcore.ProgramTypeKprobe,
					AttachType: ebpfcore.AttachTypeEntry,
				},
				{
					Name:       "memory_tracker",
					Type:       ebpfcore.ProgramTypeKprobe,
					AttachType: ebpfcore.AttachTypeEntry,
				},
			},
			EnableNetwork: true,
			EnableMemory:  true,
		}

		ebpfCollector, err := collectors.CreateEBPFCollector("ebpf", ebpfConfig)
		if err != nil {
			log.Printf("Failed to create eBPF collector: %v (skipping)", err)
		} else {
			if err := manager.Register("ebpf", ebpfCollector); err != nil {
				return fmt.Errorf("failed to register ebpf collector: %w", err)
			}
			log.Println("Registered eBPF collector")
		}
	}

	// Systemd Collector (Linux only)
	if os.Getenv("ENABLE_SYSTEMD_COLLECTOR") == "true" {
		systemdConfig := systemdcore.Config{
			Name:            "systemd-main",
			Enabled:         true,
			EventBufferSize: 5000,
			ServiceFilter: []string{
				"docker.service",
				"kubelet.service",
				"containerd.service",
			},
			WatchServiceStates:   true,
			WatchServiceFailures: true,
			UnitTypes:            []string{"service"},
		}

		systemdCollector, err := collectors.CreateSystemdCollector("systemd", systemdConfig)
		if err != nil {
			log.Printf("Failed to create systemd collector: %v (skipping)", err)
		} else {
			if err := manager.Register("systemd", systemdCollector); err != nil {
				return fmt.Errorf("failed to register systemd collector: %w", err)
			}
			log.Println("Registered systemd collector")
		}
	}

	// Ensure at least one collector is registered
	if manager.CollectorCount() == 0 {
		return fmt.Errorf("no collectors were registered")
	}

	log.Printf("Registered %d collectors", manager.CollectorCount())
	return nil
}

func processEvents(manager *collectors.Manager) {
	events := manager.Events()
	eventCount := 0
	startTime := time.Now()

	for event := range events {
		eventCount++

		// Log sample events
		if eventCount%1000 == 0 {
			duration := time.Since(startTime)
			rate := float64(eventCount) / duration.Seconds()
			log.Printf("Processed %d events (%.2f events/sec)", eventCount, rate)
		}

		// In a real deployment, events would be sent to:
		// 1. Intelligence pipeline for correlation
		// 2. Storage backend for persistence
		// 3. Real-time streaming for monitoring

		// For now, just log interesting events
		if event.Severity == "error" || event.Severity == "critical" {
			log.Printf("[%s] %s event from %s: %s",
				event.Severity, event.Type, event.Source, event.Message)
		}
	}
}

func monitorHealth(manager *collectors.Manager) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		health := manager.Health()

		// Log health summary
		unhealthy := 0
		for name, h := range health {
			if h.Status == collectors.HealthStatusUnhealthy {
				log.Printf("[HEALTH] %s is unhealthy: %s", name, h.Message)
				unhealthy++
			}
		}

		if unhealthy == 0 {
			log.Printf("[HEALTH] All %d collectors are healthy", len(health)-1) // -1 for manager itself
		}

		// Log statistics
		stats := manager.Statistics()
		if managerStats, ok := stats["manager"]; ok {
			log.Printf("[STATS] Total events: %d, Dropped: %d, Uptime: %.0fs",
				managerStats.EventsCollected,
				managerStats.EventsDropped,
				managerStats.Custom["uptime_seconds"])
		}
	}
}
