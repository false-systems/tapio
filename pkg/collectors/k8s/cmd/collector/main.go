package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/k8s"
	"github.com/yairfalse/tapio/pkg/domain"
)

func main() {
	var (
		kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig file")
		namespace  = flag.String("namespace", "", "Namespace to watch (empty for all)")
		inCluster  = flag.Bool("in-cluster", false, "Use in-cluster authentication")
	)
	flag.Parse()

	// Create configuration
	config := k8s.DefaultConfig()
	config.Name = "k8s-collector-standalone"

	// Override with command line flags
	if *kubeconfig != "" {
		config.KubeConfig = *kubeconfig
		config.InCluster = false
	} else if *inCluster {
		config.InCluster = true
		config.KubeConfig = ""
	}

	if *namespace != "" {
		config.Namespace = *namespace
	}

	// Create collector
	collector, err := k8s.NewCollector(config)
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
	fmt.Println("Kubernetes collector started successfully")

	// Get initial health
	health := collector.Health()
	if health.Connected {
		fmt.Printf("Connected to cluster: %s (version: %s)\n",
			health.ClusterInfo.Name, health.ClusterInfo.Version)
	}

	// Start event processor
	go func() {
		eventCount := 0
		for event := range collector.Events() {
			eventCount++
			fmt.Printf("\n[Event #%d] %s\n", eventCount, time.Now().Format(time.RFC3339))
			fmt.Printf("  ID: %s\n", event.ID)
			fmt.Printf("  Type: %s\n", event.Type)
			fmt.Printf("  Source: %s\n", event.Source)
			fmt.Printf("  Severity: %s\n", event.Severity)

			// Print K8s-specific payload information
			if k8sPayload, ok := event.Data.(domain.KubernetesEventPayload); ok {
				fmt.Printf("  Resource: %s/%s in namespace %s\n",
					k8sPayload.Resource.Kind,
					k8sPayload.Resource.Name,
					k8sPayload.Resource.Namespace)
				fmt.Printf("  Event Type: %s\n", k8sPayload.EventType)
				if k8sPayload.Reason != "" {
					fmt.Printf("  Reason: %s\n", k8sPayload.Reason)
				}
				if k8sPayload.Message != "" {
					fmt.Printf("  Message: %s\n", k8sPayload.Message)
				}
			}
		}
	}()

	// Start health monitor
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				health := collector.Health()
				stats := collector.Statistics()

				fmt.Printf("\n=== Health Report ===\n")
				fmt.Printf("Status: %s - %s\n", health.Status, health.Message)
				fmt.Printf("Connected: %v\n", health.Connected)
				fmt.Printf("Events: Collected=%d, Dropped=%d\n",
					health.EventsProcessed, health.EventsDropped)
				fmt.Printf("API: Calls=%d, Errors=%d\n",
					stats.APICallsTotal, stats.APIErrors)
				fmt.Printf("Watchers Active: %d\n", stats.WatchersActive)
				fmt.Printf("Resources Watched: %v\n", stats.ResourcesWatched)
				fmt.Printf("Reconnect Count: %d\n", stats.ReconnectCount)

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

	// Get final statistics
	stats := collector.Statistics()
	fmt.Printf("\nFinal Statistics:\n")
	fmt.Printf("Total Events Collected: %d\n", stats.EventsCollected)
	fmt.Printf("Total Events Dropped: %d\n", stats.EventsDropped)
	fmt.Printf("Total API Calls: %d\n", stats.APICallsTotal)
	fmt.Printf("Total API Errors: %d\n", stats.APIErrors)

	fmt.Println("Collector stopped successfully")
}
