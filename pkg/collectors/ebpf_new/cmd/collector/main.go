package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	ebpf "github.com/yairfalse/tapio/pkg/collectors/ebpf_new"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

var (
	configFile   = flag.String("config", "", "Path to configuration file")
	profile      = flag.String("profile", "minimal", "Configuration profile: minimal, syscall, network, process, memory, fileio")
	outputFormat = flag.String("output", "json", "Output format: json, text")
	verbose      = flag.Bool("verbose", false, "Enable verbose output")
	testMode     = flag.Bool("test", false, "Run in test mode (collect for 10 seconds and exit)")
)

func main() {
	flag.Parse()

	// Setup logging
	if !*verbose {
		log.SetOutput(os.Stderr)
	}

	log.Println("Starting eBPF collector...")

	// Load configuration
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create collector
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}
	defer collector.Close()

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start collector
	log.Println("Starting event collection...")
	if err := collector.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}

	// Subscribe to events
	criteria := domain.QueryCriteria{
		TimeWindow: domain.TimeWindow{
			Start: time.Now(),
			End:   time.Now().Add(24 * time.Hour),
		},
	}

	options := domain.SubscriptionOptions{
		BufferSize: 1000,
		Filters:    make(map[string]interface{}),
	}

	eventChan, err := collector.Subscribe(ctx, criteria, options)
	if err != nil {
		log.Fatalf("Failed to subscribe to events: %v", err)
	}

	// Event processing
	go func() {
		eventCount := 0
		for event := range eventChan {
			eventCount++
			if err := outputEvent(event, eventCount); err != nil {
				log.Printf("Error outputting event: %v", err)
			}
		}
	}()

	// Health monitoring
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				health := collector.GetHealth()
				stats, _ := collector.GetStats()
				log.Printf("Health: %s - Programs: %d/%d - Events: %d collected, %d dropped",
					health.Status,
					health.ProgramsHealthy,
					health.ProgramsLoaded,
					stats.EventsCollected,
					stats.EventsDropped)
			}
		}
	}()

	// Test mode timeout
	if *testMode {
		go func() {
			time.Sleep(10 * time.Second)
			log.Println("Test mode: shutting down after 10 seconds")
			cancel()
		}()
	}

	// Wait for shutdown signal
	select {
	case <-sigChan:
		log.Println("Received shutdown signal")
	case <-ctx.Done():
		log.Println("Context cancelled")
	}

	// Graceful shutdown
	log.Println("Stopping collector...")
	if err := collector.Stop(); err != nil {
		log.Printf("Error stopping collector: %v", err)
	}

	// Print final statistics
	stats, err := collector.GetStats()
	if err == nil {
		log.Printf("Final statistics:")
		log.Printf("  Events collected: %d", stats.EventsCollected)
		log.Printf("  Events dropped: %d", stats.EventsDropped)
		log.Printf("  Events filtered: %d", stats.EventsFiltered)
		log.Printf("  Bytes processed: %d", stats.BytesProcessed)
		log.Printf("  Collection errors: %d", stats.CollectionErrors)
	}

	log.Println("Collector stopped")
}

func loadConfig() (core.Config, error) {
	// If config file is provided, load from file
	if *configFile != "" {
		data, err := os.ReadFile(*configFile)
		if err != nil {
			return core.Config{}, fmt.Errorf("failed to read config file: %w", err)
		}

		var config core.Config
		if err := json.Unmarshal(data, &config); err != nil {
			return core.Config{}, fmt.Errorf("failed to parse config file: %w", err)
		}

		return config, nil
	}

	// Otherwise, use profile
	switch *profile {
	case "minimal":
		return core.MinimalConfig(), nil
	case "syscall":
		return core.SyscallMonitorConfig(), nil
	case "network":
		return core.NetworkMonitorConfig(), nil
	case "process":
		return core.ProcessMonitorConfig(), nil
	case "memory":
		return core.MemoryMonitorConfig(), nil
	case "fileio":
		return core.FileIOMonitorConfig(), nil
	default:
		return core.Config{}, fmt.Errorf("unknown profile: %s", *profile)
	}
}

func outputEvent(event domain.Event, count int) error {
	switch *outputFormat {
	case "json":
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		fmt.Println(string(data))

	case "text":
		fmt.Printf("[%d] %s | %s | %s | %s\n",
			count,
			event.Timestamp.Format("15:04:05.000"),
			event.Type,
			event.Severity,
			getEventMessage(event))

		if *verbose {
			fmt.Printf("  Source: %s/%s\n", event.Source.Component, event.Source.Instance)
			fmt.Printf("  Context: PID=%d, Container=%s, Namespace=%s\n",
				event.Context.ProcessInfo.PID,
				event.Context.ContainerID,
				event.Context.Namespace)
		}

	default:
		return fmt.Errorf("unsupported output format: %s", *outputFormat)
	}

	return nil
}

func getEventMessage(event domain.Event) string {
	switch payload := event.Payload.(type) {
	case domain.SystemEventPayload:
		return payload.Message
	case domain.ServiceEventPayload:
		return payload.Message
	case domain.KubernetesEventPayload:
		return payload.Message
	case domain.LogEventPayload:
		return payload.Message
	default:
		return "Unknown event type"
	}
}