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
	"github.com/yairfalse/tapio/pkg/domain"
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
		for event := range collector.Events() {
			eventCount++
			fmt.Printf("\n[Event #%d] %s\n", eventCount, time.Now().Format(time.RFC3339))
			fmt.Printf("  ID: %s\n", event.ID)
			fmt.Printf("  Type: %s\n", event.Type)
			fmt.Printf("  Source: %s\n", event.Source)
			fmt.Printf("  Severity: %s\n", event.Severity)
			
			// Print systemd-specific payload information
			if servicePayload, ok := event.Payload.(domain.ServiceEventPayload); ok {
				fmt.Printf("  Service: %s\n", servicePayload.ServiceName)
				fmt.Printf("  Event Type: %s\n", servicePayload.EventType)
				if servicePayload.OldState != "" {
					fmt.Printf("  State Change: %s -> %s\n", servicePayload.OldState, servicePayload.NewState)
				} else {
					fmt.Printf("  New State: %s\n", servicePayload.NewState)
				}
				
				if servicePayload.ExitCode != nil {
					fmt.Printf("  Exit Code: %d\n", *servicePayload.ExitCode)
				}
				
				if servicePayload.Signal != nil {
					fmt.Printf("  Signal: %d\n", *servicePayload.Signal)
				}
				
				// Print important properties
				for key, value := range servicePayload.Properties {
					if key == "result" || key == "sub_state" || key == "main_pid" {
						fmt.Printf("  %s: %s\n", strings.Title(strings.ReplaceAll(key, "_", " ")), value)
					}
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