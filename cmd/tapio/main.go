package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/yairfalse/tapio/pkg/collectors/manager"
	"github.com/yairfalse/tapio/pkg/config"

	// Import collectors to trigger init() registration
	_ "github.com/yairfalse/tapio/pkg/collectors/cni"
	// _ "github.com/yairfalse/tapio/pkg/collectors/ebpf" // Needs eBPF bindings generated
	_ "github.com/yairfalse/tapio/pkg/collectors/etcd"
	_ "github.com/yairfalse/tapio/pkg/collectors/k8s"
	// _ "github.com/yairfalse/tapio/pkg/collectors/systemd" // Needs eBPF bindings generated
)

var (
	configFile    = flag.String("config", "", "Path to configuration file")
	collectorList = flag.String("collectors", "cni,etcd,k8s", "Comma-separated list of collectors to enable")
	healthAddr    = flag.String("health", ":8080", "Address for health endpoint")
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

	// Create collector manager
	mgr := manager.NewManager(cfg)

	// Create root context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start health endpoint
	if *healthAddr != "" {
		log.Printf("Starting health endpoint on %s", *healthAddr)
		if err := mgr.StartHealthEndpoint(*healthAddr); err != nil {
			log.Printf("Failed to start health endpoint: %v", err)
		}
	}

	// Start collector manager
	log.Printf("Pipeline endpoint: %s", cfg.Pipeline.Endpoint)
	log.Printf("Enabled collectors: %v", cfg.Collectors.Enabled)

	if err := mgr.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector manager: %v", err)
	}

	// Process events from all collectors
	go processManagerEvents(ctx, mgr)

	log.Printf("Collector manager started successfully")

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down...")

	// Stop manager
	if err := mgr.Stop(); err != nil {
		log.Printf("Error stopping manager: %v", err)
	}

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

func processManagerEvents(ctx context.Context, mgr *manager.CollectorManager) {
	events := mgr.Events()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			// For now, just log events
			// TODO: Send to pipeline service
			log.Printf("Event: %s at %v", event.Type, event.Timestamp)
		case <-ctx.Done():
			return
		}
	}
}
