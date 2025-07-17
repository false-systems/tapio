package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/journald/core"
	"github.com/yairfalse/tapio/pkg/collectors/journald/internal"
	"github.com/yairfalse/tapio/pkg/domain"
)

func main() {
	var (
		configFile = flag.String("config", "", "Configuration file path")
		units      = flag.String("units", "", "Comma-separated list of systemd units to monitor")
		follow     = flag.Bool("follow", false, "Follow mode (real-time)")
		priority   = flag.String("priority", "info", "Minimum priority level (debug,info,notice,warning,error,critical,alert,emergency)")
		maxEvents  = flag.Int("max-events", 100, "Maximum events to collect before stopping")
		timeout    = flag.Duration("timeout", 30*time.Second, "Timeout for collection")
		verbose    = flag.Bool("verbose", false, "Verbose output")
		health     = flag.Bool("health", false, "Show health status and exit")
		version    = flag.Bool("version", false, "Show version and exit")
	)
	flag.Parse()

	if *version {
		fmt.Println("journald-collector v1.0.0")
		return
	}

	// Create configuration
	config := createConfig(*configFile, *units, *follow, *priority, *maxEvents)
	
	if *verbose {
		configJSON, _ := json.MarshalIndent(config, "", "  ")
		fmt.Printf("Configuration:\n%s\n", configJSON)
	}

	// Create collector
	collector, err := internal.NewCollector(config)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}

	if *health {
		showHealth(collector)
		return
	}

	// Setup context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start collector
	if err := collector.Start(ctx); err != nil {
		log.Printf("Failed to start collector: %v", err)
		// Continue to show what we can
	}

	fmt.Printf("journald collector started (max_events=%d, timeout=%v)\n", *maxEvents, *timeout)
	fmt.Println("Collecting events... Press Ctrl+C to stop")

	// Event collection loop
	eventCount := 0
	eventChan := collector.Events()

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("\nTimeout reached. Collected %d events.\n", eventCount)
			goto cleanup

		case <-sigChan:
			fmt.Printf("\nSignal received. Collected %d events.\n", eventCount)
			goto cleanup

		case event, ok := <-eventChan:
			if !ok {
				fmt.Printf("\nEvent channel closed. Collected %d events.\n", eventCount)
				goto cleanup
			}

			printEvent(event, *verbose)
			eventCount++

			if eventCount >= *maxEvents {
				fmt.Printf("\nMax events (%d) reached.\n", eventCount)
				goto cleanup
			}

		case <-time.After(5 * time.Second):
			// Periodic health check
			if *verbose {
				health := collector.Health()
				fmt.Printf("[HEALTH] Status: %s, Events: %d, Errors: %d\n",
					health.Status, health.EventsProcessed, health.ErrorCount)
			}
		}
	}

cleanup:
	fmt.Println("Stopping collector...")
	if err := collector.Stop(); err != nil {
		log.Printf("Error stopping collector: %v", err)
	}

	// Show final statistics
	showStatistics(collector, *verbose)
}

func createConfig(configFile, units string, follow bool, priority string, maxEvents int) core.Config {
	config := core.Config{
		Name:            "journald-collector-cli",
		Enabled:         true,
		EventBufferSize: 1000,
		FollowMode:      follow,
		SeekToEnd:       follow,
		MaxEntries:      maxEvents,
		ReadTimeout:     10 * time.Second,
		BatchSize:       100,
		FlushInterval:   5 * time.Second,
	}

	// Parse priority
	priorityMap := map[string]core.Priority{
		"debug":     core.PriorityDebug,
		"info":      core.PriorityInfo,
		"notice":    core.PriorityNotice,
		"warning":   core.PriorityWarning,
		"error":     core.PriorityError,
		"critical":  core.PriorityCritical,
		"alert":     core.PriorityAlert,
		"emergency": core.PriorityEmergency,
	}

	if prio, ok := priorityMap[priority]; ok {
		// Include all priorities from the specified level and above
		config.Priorities = make([]core.Priority, 0)
		for i := core.PriorityEmergency; i <= prio; i++ {
			config.Priorities = append(config.Priorities, i)
		}
	}

	// Parse units
	if units != "" {
		config.Units = parseUnits(units)
	}

	// Load from config file if specified
	if configFile != "" {
		if err := loadConfigFromFile(&config, configFile); err != nil {
			log.Printf("Warning: Failed to load config file %s: %v", configFile, err)
		}
	}

	return config
}

func parseUnits(units string) []string {
	// Simple comma-separated parsing
	var result []string
	for _, unit := range strings.Split(units, ",") {
		unit = strings.TrimSpace(unit)
		if unit != "" {
			result = append(result, unit)
		}
	}
	return result
}

func loadConfigFromFile(config *core.Config, filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, config)
}

func printEvent(event domain.Event, verbose bool) {
	if verbose {
		eventJSON, _ := json.MarshalIndent(event, "", "  ")
		fmt.Printf("EVENT: %s\n", eventJSON)
	} else {
		// Simple format
		fmt.Printf("[%s] %s %s: %s\n",
			event.Timestamp.Format("15:04:05"),
			event.Severity,
			event.Source,
			getEventMessage(event))
	}
}

func getEventMessage(event domain.Event) string {
	if payload, ok := event.Payload.(domain.LogEventPayload); ok {
		return payload.Message
	}
	return "Unknown message"
}

func showHealth(collector core.Collector) {
	health := collector.Health()
	
	fmt.Printf("Health Status: %s\n", health.Status)
	fmt.Printf("Message: %s\n", health.Message)
	fmt.Printf("Journal Open: %t\n", health.JournalOpen)
	fmt.Printf("Events Processed: %d\n", health.EventsProcessed)
	fmt.Printf("Events Dropped: %d\n", health.EventsDropped)
	fmt.Printf("Error Count: %d\n", health.ErrorCount)
	fmt.Printf("Current Cursor: %s\n", health.CurrentCursor)
	fmt.Printf("Boot ID: %s\n", health.BootID)
	fmt.Printf("Machine ID: %s\n", health.MachineID)
	
	if len(health.Metrics) > 0 {
		fmt.Println("Metrics:")
		for key, value := range health.Metrics {
			fmt.Printf("  %s: %.2f\n", key, value)
		}
	}
}

func showStatistics(collector core.Collector, verbose bool) {
	stats := collector.Statistics()
	
	fmt.Printf("\nCollection Statistics:\n")
	fmt.Printf("  Start Time: %s\n", stats.StartTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Uptime: %v\n", time.Since(stats.StartTime))
	fmt.Printf("  Events Collected: %d\n", stats.EventsCollected)
	fmt.Printf("  Events Dropped: %d\n", stats.EventsDropped)
	fmt.Printf("  Bytes Read: %d\n", stats.BytesRead)
	fmt.Printf("  Entries Read: %d\n", stats.EntriesRead)
	fmt.Printf("  Read Errors: %d\n", stats.ReadErrors)
	
	if verbose && len(stats.Custom) > 0 {
		fmt.Println("  Custom Metrics:")
		for key, value := range stats.Custom {
			fmt.Printf("    %s: %v\n", key, value)
		}
	}
}