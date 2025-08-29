package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"

	// Import ALL collectors - they all register via init()
	// Tapio is a Linux-native observability platform using eBPF
	_ "github.com/yairfalse/tapio/pkg/collectors/cri"
	_ "github.com/yairfalse/tapio/pkg/collectors/cri-ebpf"
	_ "github.com/yairfalse/tapio/pkg/collectors/dns"
	_ "github.com/yairfalse/tapio/pkg/collectors/etcd-api"
	_ "github.com/yairfalse/tapio/pkg/collectors/etcd-ebpf"
	_ "github.com/yairfalse/tapio/pkg/collectors/etcd-metrics"
	_ "github.com/yairfalse/tapio/pkg/collectors/kernel"
	_ "github.com/yairfalse/tapio/pkg/collectors/kubeapi"
	_ "github.com/yairfalse/tapio/pkg/collectors/kubelet"
	_ "github.com/yairfalse/tapio/pkg/collectors/memory-leak-hunter"
	_ "github.com/yairfalse/tapio/pkg/collectors/network"
	_ "github.com/yairfalse/tapio/pkg/collectors/otel"
	_ "github.com/yairfalse/tapio/pkg/collectors/runtime-signals"
	_ "github.com/yairfalse/tapio/pkg/collectors/storage-io"
	_ "github.com/yairfalse/tapio/pkg/collectors/syscall-errors"
	_ "github.com/yairfalse/tapio/pkg/collectors/systemd"
	_ "github.com/yairfalse/tapio/pkg/collectors/systemd-api"
)

var (
	configFile = flag.String("config", "", "Path to YAML configuration file")
	logLevel   = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
)

func main() {
	flag.Parse()

	// Create logger
	var logger *zap.Logger
	var err error
	switch *logLevel {
	case "debug":
		logger, err = zap.NewDevelopment()
	default:
		logger, err = zap.NewProduction()
	}
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Load configuration
	var config *orchestrator.YAMLConfig
	if *configFile != "" {
		logger.Info("Loading configuration from file", zap.String("file", *configFile))
		config, err = orchestrator.LoadYAMLConfig(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config file: %v", err)
		}

		// Validate configuration
		if err := orchestrator.ValidateYAMLConfig(config); err != nil {
			log.Fatalf("Invalid configuration: %v", err)
		}

		// Override log level from config
		if config.Orchestrator.LogLevel != "" {
			*logLevel = config.Orchestrator.LogLevel
		}
	} else {
		log.Fatal("Configuration file is required. Use -config flag")
	}

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create orchestrator
	orchestratorConfig := config.ToOrchestratorConfig()
	collectorOrchestrator, err := orchestrator.New(logger, orchestratorConfig)
	if err != nil {
		log.Fatalf("Failed to create collector orchestrator: %v", err)
	}

	// Register collectors automatically from YAML config
	if err := collectorOrchestrator.RegisterCollectorsFromYAML(config, logger); err != nil {
		log.Fatalf("Failed to register collectors from YAML: %v", err)
	}

	// Start orchestrator
	if err := collectorOrchestrator.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector orchestrator: %v", err)
	}

	logger.Info("Tapio collectors started successfully",
		zap.String("config_file", *configFile),
		zap.Int("workers", orchestratorConfig.Workers),
		zap.Int("buffer_size", orchestratorConfig.BufferSize))

	// Start health monitoring
	go monitorCollectorHealth(ctx, collectorOrchestrator, logger)

	// Wait for shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down collectors...")
	collectorOrchestrator.Stop()
}

// monitorCollectorHealth periodically checks collector health
func monitorCollectorHealth(ctx context.Context, orchestrator *orchestrator.CollectorOrchestrator, logger *zap.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			healthStatus := orchestrator.GetHealthStatus()

			healthy := 0
			unhealthy := 0
			for name, status := range healthStatus {
				if status.Healthy {
					healthy++
				} else {
					unhealthy++
					logger.Warn("Collector unhealthy",
						zap.String("collector", name),
						zap.String("error", status.Error),
						zap.Time("last_event", status.LastEvent))
				}
			}

			logger.Info("Collector health check",
				zap.Int("healthy", healthy),
				zap.Int("unhealthy", unhealthy),
				zap.Int("total", len(healthStatus)))
		}
	}
}
