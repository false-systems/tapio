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
)

func main() {
	var (
		kubeconfig  = flag.String("kubeconfig", "", "Path to kubeconfig file")
		namespace   = flag.String("namespace", "", "Namespace to watch (empty for all)")
		inCluster   = flag.Bool("in-cluster", false, "Use in-cluster authentication")
		serverAddr  = flag.String("server", "localhost:8080", "Tapio server address")
		enableTapio = flag.Bool("enable-tapio", true, "Send events to Tapio server")
		verbose     = flag.Bool("verbose", false, "Enable verbose logging")
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

	// Initialize Tapio client if enabled
	var tapioClient *k8s.TapioGRPCClient
	if *enableTapio {
		client, err := k8s.NewTapioGRPCClient(*serverAddr)
		if err != nil {
			log.Printf("Failed to create Tapio client: %v", err)
			fmt.Println("Continuing without Tapio integration...")
		} else {
			tapioClient = client
			fmt.Printf("Connected to Tapio server at %s\n", *serverAddr)
			defer func() {
				if err := tapioClient.Close(); err != nil {
					log.Printf("Error closing Tapio client: %v", err)
				}
			}()
		}
	}

	// Start event processor
	go func() {
		eventCount := 0
		for event := range collector.Events() {
			eventCount++

			// Send to Tapio if client is available
			if tapioClient != nil {
				if err := tapioClient.SendEvent(ctx, &event); err != nil {
					log.Printf("Failed to send event to Tapio: %v", err)
					// Continue processing even if Tapio send fails
				}
			}

			// Print events if verbose mode is enabled
			if *verbose {
				fmt.Printf("\n[Event #%d] %s\n", eventCount, time.Now().Format(time.RFC3339))
				fmt.Printf("  ID: %s\n", event.ID)
				fmt.Printf("  Type: %s\n", event.Type)
				fmt.Printf("  Source: %s\n", event.Source)
				fmt.Printf("  Severity: %s\n", event.GetSeverity())

				// Print semantic context
				if event.Semantic != nil {
					fmt.Printf("  Intent: %s\n", event.Semantic.Intent)
					fmt.Printf("  Category: %s\n", event.Semantic.Category)
					fmt.Printf("  Narrative: %s\n", event.Semantic.Narrative)
				}

				// Print entity information
				if event.Entity != nil {
					fmt.Printf("  Entity: %s/%s", event.Entity.Type, event.Entity.Name)
					if event.Entity.Namespace != "" {
						fmt.Printf(" in namespace %s", event.Entity.Namespace)
					}
					fmt.Printf("\n")
				}

				// Print K8s-specific data
				if event.Kubernetes != nil {
					fmt.Printf("  K8s Action: %s\n", event.Kubernetes.Action)
					fmt.Printf("  K8s Reason: %s\n", event.Kubernetes.Reason)
					fmt.Printf("  K8s Message: %s\n", event.Kubernetes.Message)
				}

				// Print impact information
				if event.Impact != nil {
					fmt.Printf("  Impact: %s (%.2f business impact)\n",
						event.Impact.Severity, event.Impact.BusinessImpact)
					if len(event.Impact.AffectedServices) > 0 {
						fmt.Printf("  Affected Services: %v\n", event.Impact.AffectedServices)
					}
				}
			} else if eventCount%100 == 0 {
				// Print progress every 100 events
				fmt.Printf("Processed %d events (sent to Tapio: %t)\n", eventCount, tapioClient != nil)
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

				// Include Tapio client statistics if available
				if tapioClient != nil {
					tapioStats := tapioClient.GetStatistics()
					fmt.Printf("Tapio Connection: %v\n", tapioStats["connected"])
					fmt.Printf("Events Sent to Tapio: %d\n", tapioStats["events_sent"])
					fmt.Printf("Events Dropped (Tapio): %d\n", tapioStats["events_dropped"])
					fmt.Printf("Tapio Reconnects: %d\n", tapioStats["reconnects"])
					fmt.Printf("Buffer Utilization: %d/%d\n",
						tapioStats["buffer_size"], tapioStats["buffer_capacity"])
				}

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
