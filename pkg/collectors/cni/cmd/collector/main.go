package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/cni"
	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
)

func main() {
	var (
		configType       = flag.String("config", "default", "Configuration type: default, production, development")
		cniBinPath       = flag.String("cni-bin-path", "/opt/cni/bin", "Path to CNI binary directory")
		cniConfPath      = flag.String("cni-conf-path", "/etc/cni/net.d", "Path to CNI configuration directory")
		bufferSize       = flag.Int("buffer-size", 1000, "Event buffer size")
		pollInterval     = flag.Int("poll-interval", 5000, "Polling interval in milliseconds")
		eventRateLimit   = flag.Int("rate-limit", 100, "Event rate limit per second")
		enableLogMon     = flag.Bool("enable-log-monitoring", true, "Enable CNI log monitoring")
		enableProcMon    = flag.Bool("enable-process-monitoring", false, "Enable CNI process monitoring")
		enableEventMon   = flag.Bool("enable-event-monitoring", true, "Enable Kubernetes event monitoring")
		enableFileMon    = flag.Bool("enable-file-monitoring", false, "Enable CNI configuration file monitoring")
		inCluster        = flag.Bool("in-cluster", true, "Running in Kubernetes cluster")
		serverAddr       = flag.String("server", "", "Tapio server address (e.g., localhost:50051)")
		standalone       = flag.Bool("standalone", false, "Run in standalone mode without connecting to Tapio server")
		verbose          = flag.Bool("verbose", false, "Enable verbose output")
	)
	flag.Parse()

	// Create configuration based on type
	var config core.Config
	switch *configType {
	case "production":
		config = cni.ProductionConfig()
	case "development":
		config = cni.DevelopmentConfig()
	default:
		config = cni.DefaultConfig()
	}

	// Override with command line flags
	config.Name = "cni-collector-" + *configType
	config.EventBufferSize = *bufferSize
	config.CNIBinPath = *cniBinPath
	config.CNIConfPath = *cniConfPath
	config.PollInterval = time.Duration(*pollInterval) * time.Millisecond
	config.EventRateLimit = *eventRateLimit
	config.EnableLogMonitoring = *enableLogMon
	config.EnableProcessMonitoring = *enableProcMon
	config.EnableEventMonitoring = *enableEventMon
	config.EnableFileMonitoring = *enableFileMon
	config.InCluster = *inCluster

	// Create collector
	collector, err := cni.NewCNICollector(config)
	if err != nil {
		log.Fatalf("Failed to create CNI collector: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize Tapio gRPC client if server address is provided
	var tapioClient *cni.TapioGRPCClient
	if *serverAddr != "" && !*standalone {
		tapioClient, err = cni.NewTapioGRPCClient(*serverAddr)
		if err != nil {
			log.Fatalf("Failed to create Tapio gRPC client: %v", err)
		}
		fmt.Printf("Connected to Tapio server at %s\n", *serverAddr)
		defer tapioClient.Close()
	}

	// Start collector
	if err := collector.Start(ctx); err != nil {
		log.Fatalf("Failed to start CNI collector: %v", err)
	}
	fmt.Println("CNI collector started successfully")

	// Get initial health
	health := collector.Health()
	fmt.Printf("Collector Status: %s - %s\n", health.Status, health.Message)
	fmt.Printf("Active Monitors: %d\n", health.ActiveMonitors)
	if len(health.CNIPluginsDetected) > 0 {
		fmt.Printf("CNI Plugins Detected: %v\n", health.CNIPluginsDetected)
	}

	// Print configuration
	fmt.Printf("Configuration:\n")
	fmt.Printf("  CNI Binary Path: %s\n", config.CNIBinPath)
	fmt.Printf("  CNI Config Path: %s\n", config.CNIConfPath)
	fmt.Printf("  Log monitoring: %v\n", config.EnableLogMonitoring)
	fmt.Printf("  Process monitoring: %v\n", config.EnableProcessMonitoring)
	fmt.Printf("  Event monitoring: %v\n", config.EnableEventMonitoring)
	fmt.Printf("  File monitoring: %v\n", config.EnableFileMonitoring)
	fmt.Printf("  In-cluster: %v\n", config.InCluster)
	fmt.Printf("  Event rate limit: %d/sec\n", config.EventRateLimit)

	// Start event processor
	go func() {
		eventCount := 0
		for unifiedEvent := range collector.Events() {
			eventCount++

			// Send event to Tapio server if connected
			if tapioClient != nil {
				if err := tapioClient.SendEvent(ctx, &unifiedEvent); err != nil {
					if *verbose {
						fmt.Printf("Failed to send event to server: %v\n", err)
					}
				}
			}

			// Print event details in standalone mode or verbose output
			if *standalone || tapioClient == nil || *verbose {
				fmt.Printf("\n[Event #%d] %s\n", eventCount, time.Now().Format(time.RFC3339))
				fmt.Printf("  ID: %s\n", unifiedEvent.ID)
				fmt.Printf("  Type: %s\n", unifiedEvent.Type)
				fmt.Printf("  Source: %s\n", unifiedEvent.Source)
				fmt.Printf("  Severity: %s\n", unifiedEvent.GetSeverity())

				// Print network-specific information
				if unifiedEvent.Network != nil {
					fmt.Printf("  Network:\n")
					if unifiedEvent.Network.Protocol != "" {
						fmt.Printf("    Protocol: %s\n", unifiedEvent.Network.Protocol)
					}
					if unifiedEvent.Network.SourceIP != "" {
						fmt.Printf("    Source IP: %s\n", unifiedEvent.Network.SourceIP)
					}
					if unifiedEvent.Network.DestIP != "" {
						fmt.Printf("    Dest IP: %s\n", unifiedEvent.Network.DestIP)
					}
					if unifiedEvent.Network.SourcePort != 0 {
						fmt.Printf("    Source Port: %d\n", unifiedEvent.Network.SourcePort)
					}
					if unifiedEvent.Network.DestPort != 0 {
						fmt.Printf("    Dest Port: %d\n", unifiedEvent.Network.DestPort)
					}
					if unifiedEvent.Network.Direction != "" {
						fmt.Printf("    Direction: %s\n", unifiedEvent.Network.Direction)
					}
				}

				// Print Kubernetes context if available
				if unifiedEvent.Kubernetes != nil {
					fmt.Printf("  Kubernetes:\n")
					if unifiedEvent.Kubernetes.Object != "" {
						fmt.Printf("    Object: %s\n", unifiedEvent.Kubernetes.Object)
					}
					if unifiedEvent.Kubernetes.Reason != "" {
						fmt.Printf("    Reason: %s\n", unifiedEvent.Kubernetes.Reason)
					}
					if unifiedEvent.Kubernetes.EventType != "" {
						fmt.Printf("    Event Type: %s\n", unifiedEvent.Kubernetes.EventType)
					}
				}

				// Print entity information
				if unifiedEvent.Entity != nil {
					fmt.Printf("  Entity:\n")
					fmt.Printf("    Name: %s\n", unifiedEvent.Entity.Name)
					if unifiedEvent.Entity.Namespace != "" {
						fmt.Printf("    Namespace: %s\n", unifiedEvent.Entity.Namespace)
					}
					if unifiedEvent.Entity.Type != "" {
						fmt.Printf("    Type: %s\n", unifiedEvent.Entity.Type)
					}
					// Print important labels
					if len(unifiedEvent.Entity.Labels) > 0 {
						fmt.Printf("    Labels:\n")
						for key, value := range unifiedEvent.Entity.Labels {
							if strings.HasPrefix(key, "k8s.") || key == "pod" || key == "namespace" || key == "node" {
								fmt.Printf("      %s: %s\n", key, value)
							}
						}
					}
				}

				// Print semantic information
				if unifiedEvent.Semantic != nil && unifiedEvent.Semantic.Intent != "" {
					fmt.Printf("  Semantic:\n")
					fmt.Printf("    Intent: %s\n", unifiedEvent.Semantic.Intent)
					fmt.Printf("    Category: %s\n", unifiedEvent.Semantic.Category)
					if len(unifiedEvent.Semantic.Tags) > 0 {
						fmt.Printf("    Tags: %v\n", unifiedEvent.Semantic.Tags)
					}
					fmt.Printf("    Confidence: %.2f\n", unifiedEvent.Semantic.Confidence)
				}
			} else if tapioClient != nil {
				// In server mode, just show brief status
				if eventCount%10 == 0 {
					fmt.Printf("\rEvents sent to server: %d", eventCount)
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

				if *verbose || *standalone {
					fmt.Printf("\n=== Health Report ===\n")
					fmt.Printf("Status: %s - %s\n", health.Status, health.Message)
					fmt.Printf("Active Monitors: %d\n", health.ActiveMonitors)
					fmt.Printf("Error Count: %d\n", health.ErrorCount)
					fmt.Printf("CNI Operations: Total=%d, Failed=%d\n",
						stats.CNIOperationsTotal, stats.CNIOperationsFailed)
					fmt.Printf("IP Allocations: Total=%d, Deallocations=%d\n",
						stats.IPAllocationsTotal, stats.IPDeallocationsTotal)
					fmt.Printf("Events: Collected=%d, Dropped=%d\n",
						stats.EventsCollected, stats.EventsDropped)
					if stats.K8sEventsProcessed > 0 {
						fmt.Printf("K8s Events Processed: %d\n", stats.K8sEventsProcessed)
					}

					// Show plugin execution times
					if len(stats.PluginExecutionTime) > 0 {
						fmt.Printf("Plugin Execution Times:\n")
						for plugin, duration := range stats.PluginExecutionTime {
							fmt.Printf("  %s: %v\n", plugin, duration)
						}
					}

					// Show Tapio client statistics if connected
					if tapioClient != nil {
						tapioStats := tapioClient.GetStatistics()
						fmt.Printf("Tapio Client:\n")
						fmt.Printf("  Connected: %v\n", tapioStats["connected"])
						fmt.Printf("  Events Sent: %d\n", tapioStats["events_sent"])
						fmt.Printf("  Events Dropped: %d\n", tapioStats["events_dropped"])
						fmt.Printf("  Buffer Usage: %d/%d\n",
							tapioStats["buffer_size"], tapioStats["buffer_capacity"])
					}
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
	fmt.Printf("Total CNI Operations: %d\n", stats.CNIOperationsTotal)
	fmt.Printf("Failed CNI Operations: %d\n", stats.CNIOperationsFailed)
	fmt.Printf("Total IP Allocations: %d\n", stats.IPAllocationsTotal)
	fmt.Printf("Total IP Deallocations: %d\n", stats.IPDeallocationsTotal)
	fmt.Printf("Total Events Collected: %d\n", stats.EventsCollected)
	fmt.Printf("Total Events Dropped: %d\n", stats.EventsDropped)
	fmt.Printf("Total K8s Events Processed: %d\n", stats.K8sEventsProcessed)
	fmt.Printf("Monitoring Errors: %d\n", stats.MonitoringErrors)

	if len(stats.PluginExecutionTime) > 0 {
		fmt.Printf("Plugin Execution Times:\n")
		for plugin, duration := range stats.PluginExecutionTime {
			fmt.Printf("  %s: %v\n", plugin, duration)
		}
	}

	fmt.Println("CNI collector stopped successfully")
}