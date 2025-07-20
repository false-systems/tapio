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

	"github.com/yairfalse/tapio/pkg/collectors/systemd"
	"github.com/yairfalse/tapio/pkg/collectors/systemd/internal"
	"github.com/yairfalse/tapio/pkg/domain"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func main() {
	var (
		configType       = flag.String("config", "default", "Configuration type: default, critical, all")
		serviceFilter    = flag.String("services", "", "Comma-separated list of services to monitor")
		excludeServices  = flag.String("exclude", "", "Comma-separated list of services to exclude")
		unitTypes        = flag.String("unit-types", "service", "Comma-separated list of unit types to watch")
		pollInterval     = flag.Duration("poll-interval", 30*time.Second, "Polling interval for service scanning")
		bufferSize       = flag.Int("buffer-size", 1000, "Event buffer size")
		watchAllServices = flag.Bool("watch-all", false, "Watch all services")
		serverAddr       = flag.String("server", "", "Tapio server address (e.g., localhost:50051)")
		standalone       = flag.Bool("standalone", false, "Run in standalone mode without connecting to Tapio server")
	)
	flag.Parse()

	// Create configuration based on type
	var config systemd.Config
	switch *configType {
	case "critical":
		config = systemd.CriticalServicesConfig()
	case "all":
		config = systemd.AllServicesConfig()
	default:
		config = systemd.DefaultConfig()
	}

	// Override with command line flags
	config.Name = "systemd-collector-standalone"
	config.EventBufferSize = *bufferSize
	config.PollInterval = *pollInterval
	config.WatchAllServices = *watchAllServices

	if *serviceFilter != "" {
		config.ServiceFilter = strings.Split(*serviceFilter, ",")
		// Trim whitespace
		for i := range config.ServiceFilter {
			config.ServiceFilter[i] = strings.TrimSpace(config.ServiceFilter[i])
		}
	}

	if *excludeServices != "" {
		config.ServiceExclude = strings.Split(*excludeServices, ",")
		// Trim whitespace
		for i := range config.ServiceExclude {
			config.ServiceExclude[i] = strings.TrimSpace(config.ServiceExclude[i])
		}
	}

	if *unitTypes != "" {
		config.UnitTypes = strings.Split(*unitTypes, ",")
		// Trim whitespace
		for i := range config.UnitTypes {
			config.UnitTypes[i] = strings.TrimSpace(config.UnitTypes[i])
		}
	}

	// Create collector
	collector, err := systemd.NewCollector(config)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize gRPC client if server address is provided
	var grpcClient *internal.GRPCClient
	if *serverAddr != "" && !*standalone {
		grpcClient, err = internal.NewGRPCClient(*serverAddr)
		if err != nil {
			log.Fatalf("Failed to create gRPC client: %v", err)
		}

		// Connect to server
		if err := grpcClient.Connect(ctx); err != nil {
			log.Fatalf("Failed to connect to Tapio server: %v", err)
		}
		fmt.Printf("Connected to Tapio server at %s\n", *serverAddr)
		defer grpcClient.Close()
	}

	// Start collector
	if err := collector.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}
	fmt.Println("systemd collector started successfully")

	// Get initial health
	health := collector.Health()
	if health.DBusConnected {
		fmt.Printf("Connected to D-Bus, systemd version: %s\n", health.SystemdVersion)
	}

	// Print configuration
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Watch all services: %v\n", config.WatchAllServices)
	fmt.Printf("  Service filter: %v\n", config.ServiceFilter)
	fmt.Printf("  Service exclude: %v\n", config.ServiceExclude)
	fmt.Printf("  Unit types: %v\n", config.UnitTypes)
	fmt.Printf("  Poll interval: %v\n", config.PollInterval)

	// Start event processor
	go func() {
		eventCount := 0
		for unifiedEvent := range collector.Events() {
			eventCount++

			// Convert UnifiedEvent to legacy Event for grpcClient compatibility
			legacyEvent := convertUnifiedEventToLegacy(unifiedEvent)

			// Send event to Tapio server if connected
			if grpcClient != nil && grpcClient.IsConnected() {
				if err := grpcClient.SendEvent(ctx, legacyEvent); err != nil {
					fmt.Printf("Failed to send event to server: %v\n", err)
				}
			}

			// Print event details in standalone mode or verbose output
			if *standalone || grpcClient == nil {
				fmt.Printf("\n[Event #%d] %s\n", eventCount, time.Now().Format(time.RFC3339))
				fmt.Printf("  ID: %s\n", unifiedEvent.ID)
				fmt.Printf("  Type: %s\n", unifiedEvent.Type)
				fmt.Printf("  Source: %s\n", unifiedEvent.Source)
				fmt.Printf("  Severity: %s\n", unifiedEvent.GetSeverity())

				// Print systemd-specific payload information from Application context
				if unifiedEvent.Application != nil && unifiedEvent.Application.Custom != nil {
					eventData := unifiedEvent.Application.Custom
					if unitName, ok := eventData["unit_name"].(string); ok {
						fmt.Printf("  Service: %s\n", unitName)
					}
					if unitType, ok := eventData["unit_type"].(string); ok {
						fmt.Printf("  Unit Type: %s\n", unitType)
					}

					oldState, hasOld := eventData["old_state"].(string)
					newState, hasNew := eventData["new_state"].(string)
					if hasOld && hasNew && oldState != "" {
						fmt.Printf("  State Change: %s -> %s\n", oldState, newState)
					} else if hasNew {
						fmt.Printf("  New State: %s\n", newState)
					}

					if exitCode, ok := eventData["exit_code"].(int); ok {
						fmt.Printf("  Exit Code: %d\n", exitCode)
					}

					if exitStatus, ok := eventData["exit_status"]; ok {
						fmt.Printf("  Exit Status: %v\n", exitStatus)
					}

					// Print important properties
					if properties, ok := eventData["properties"].(map[string]interface{}); ok {
						for key, value := range properties {
							if key == "result" || key == "sub_state" || key == "main_pid" {
								caser := cases.Title(language.English)
								fmt.Printf("  %s: %v\n", caser.String(strings.ReplaceAll(key, "_", " ")), value)
							}
						}
					}
				}
			} else if grpcClient != nil {
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

				fmt.Printf("\n=== Health Report ===\n")
				fmt.Printf("Status: %s - %s\n", health.Status, health.Message)
				fmt.Printf("D-Bus Connected: %v\n", health.DBusConnected)
				fmt.Printf("systemd Version: %s\n", health.SystemdVersion)
				fmt.Printf("Events: Collected=%d, Dropped=%d\n",
					health.EventsProcessed, health.EventsDropped)
				fmt.Printf("Services: Monitored=%d, Active=%d, Failed=%d\n",
					stats.ServicesMonitored, stats.ActiveServices, stats.FailedServices)
				fmt.Printf("D-Bus: Calls=%d, Errors=%d\n",
					stats.DBusCallsTotal, stats.DBusErrors)
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
	fmt.Printf("Total D-Bus Calls: %d\n", stats.DBusCallsTotal)
	fmt.Printf("Total D-Bus Errors: %d\n", stats.DBusErrors)
	fmt.Printf("Services Monitored: %d\n", stats.ServicesMonitored)

	fmt.Println("Collector stopped successfully")
}

// convertUnifiedEventToLegacy converts a UnifiedEvent to the legacy Event format for grpcClient compatibility
func convertUnifiedEventToLegacy(unified domain.UnifiedEvent) domain.Event {
	// Create data map from Application custom data
	data := make(map[string]interface{})
	if unified.Application != nil && unified.Application.Custom != nil {
		for k, v := range unified.Application.Custom {
			data[k] = v
		}
	}

	// Create legacy event context
	context := domain.EventContext{
		Service:   "systemd",
		Component: "systemd-collector",
		Host:      getHostname(),
		Labels:    make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	// Copy entity context to labels if available
	if unified.Entity != nil {
		if unified.Entity.Name != "" {
			context.Component = unified.Entity.Name
			context.Labels["unit_name"] = unified.Entity.Name
		}
		if unified.Entity.Namespace != "" {
			context.Labels["unit_type"] = unified.Entity.Namespace
		}
		for k, v := range unified.Entity.Labels {
			context.Labels[k] = v
		}
	}

	// Add semantic context to metadata
	if unified.Semantic != nil {
		context.Metadata["semantic_intent"] = unified.Semantic.Intent
		context.Metadata["semantic_category"] = unified.Semantic.Category
		context.Metadata["semantic_tags"] = unified.Semantic.Tags
		context.Metadata["semantic_confidence"] = unified.Semantic.Confidence
	}

	return domain.Event{
		ID:        domain.EventID(unified.ID),
		Timestamp: unified.Timestamp,
		Type:      unified.Type,
		Source:    domain.SourceType(unified.Source),
		Data:      data,
		Severity:  domain.EventSeverity(unified.GetSeverity()),
		Context:   context,
		Message:   func() string {
			if unified.Application != nil && unified.Application.Message != "" {
				return unified.Application.Message
			}
			return ""
		}(),
		Confidence: func() float64 {
			if unified.Semantic != nil {
				return unified.Semantic.Confidence
			}
			return 1.0
		}(),
	}
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "localhost"
	}
	return hostname
}
