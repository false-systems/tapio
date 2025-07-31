package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
	"github.com/yairfalse/tapio/pkg/config"

	// Import collectors to trigger init() registration
	_ "github.com/yairfalse/tapio/pkg/collectors/cni"
	// _ "github.com/yairfalse/tapio/pkg/collectors/ebpf" // Needs eBPF bindings generated
	_ "github.com/yairfalse/tapio/pkg/collectors/etcd"
	_ "github.com/yairfalse/tapio/pkg/collectors/k8s"
	// _ "github.com/yairfalse/tapio/pkg/collectors/systemd" // Needs eBPF bindings generated
)

var (
	configFile      = flag.String("config", "", "Path to configuration file")
	collectorList   = flag.String("collectors", "cni,etcd,k8s", "Comma-separated list of collectors to enable")
)

func main() {
	flag.Parse()

	log.Println("Starting Tapio observability platform...")

	// Load configuration
	var cfg *config.Config
	var err error
	
	if *configFile != "" {
		cfg, err = config.LoadConfig(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		log.Printf("Loaded configuration from %s", *configFile)
	} else {
		// Use defaults if no config file
		cfg = &config.Config{}
		cfg.Pipeline.Endpoint = "localhost:50051"
		cfg.Pipeline.Timeout = 30
		cfg.Pipeline.Retries = 3
		cfg.Collectors.BufferSize = 1000
		cfg.Collectors.Enabled = parseCollectorList(*collectorList)
		cfg.Collectors.Labels = make(map[string]string)
	}

	// Create root context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Get enabled collectors from config
	enabledCollectors := cfg.Collectors.Enabled
	log.Printf("Pipeline endpoint: %s", cfg.Pipeline.Endpoint)
	log.Printf("Enabled collectors: %v", enabledCollectors)
	
	// Start collectors
	var wg sync.WaitGroup
	activeCollectors := make([]collectors.Collector, 0)

	for _, name := range enabledCollectors {
		log.Printf("Starting %s collector...", name)
		
		// Create collector from registry with unified config
		collectorConfig := cfg.Collectors.ToCollectorConfig()
		collector, err := registry.CreateCollector(name, collectorConfig)
		if err != nil {
			log.Printf("Failed to create %s collector: %v", name, err)
			continue
		}

		// Start collector
		if err := collector.Start(ctx); err != nil {
			log.Printf("Failed to start %s collector: %v", name, err)
			continue
		}

		activeCollectors = append(activeCollectors, collector)

		// Process events from collector
		wg.Add(1)
		go func(c collectors.Collector) {
			defer wg.Done()
			processEvents(ctx, c)
		}(collector)
	}

	log.Printf("Started %d collectors", len(activeCollectors))

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down...")

	// Cancel context to stop all collectors
	cancel()

	// Stop all collectors
	for _, c := range activeCollectors {
		if err := c.Stop(); err != nil {
			log.Printf("Error stopping %s: %v", c.Name(), err)
		}
	}

	// Wait for all goroutines to finish
	wg.Wait()
	log.Println("Shutdown complete")
}

func parseCollectorList(list string) []string {
	result := []string{}
	for _, c := range strings.Split(list, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			result = append(result, c)
		}
	}
	return result
}

func processEvents(ctx context.Context, collector collectors.Collector) {
	events := collector.Events()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			// For now, just log events
			log.Printf("[%s] Event: %s at %v", collector.Name(), event.Type, event.Timestamp)
		case <-ctx.Done():
			return
		}
	}
}