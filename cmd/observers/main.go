package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/internal/observers/orchestrator"
	"go.uber.org/zap"

	// Import ALL observers - they all register via init()
	// Tapio is a Linux-native observability platform using eBPF
	_ "github.com/yairfalse/tapio/internal/observers/container-runtime"
	_ "github.com/yairfalse/tapio/internal/observers/dns"
	_ "github.com/yairfalse/tapio/internal/observers/health"
	_ "github.com/yairfalse/tapio/internal/observers/kernel"
	_ "github.com/yairfalse/tapio/internal/observers/lifecycle"
	_ "github.com/yairfalse/tapio/internal/observers/link"
	_ "github.com/yairfalse/tapio/internal/observers/memory"
	_ "github.com/yairfalse/tapio/internal/observers/network"
	_ "github.com/yairfalse/tapio/internal/observers/node-runtime"
	_ "github.com/yairfalse/tapio/internal/observers/otel"
	_ "github.com/yairfalse/tapio/internal/observers/process-signals"
	_ "github.com/yairfalse/tapio/internal/observers/scheduler"
	_ "github.com/yairfalse/tapio/internal/observers/services"
	_ "github.com/yairfalse/tapio/internal/observers/status"
	_ "github.com/yairfalse/tapio/internal/observers/storage-io"
	_ "github.com/yairfalse/tapio/internal/observers/systemd"
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
	observerOrchestrator, err := orchestrator.New(logger, orchestratorConfig)
	if err != nil {
		log.Fatalf("Failed to create observer orchestrator: %v", err)
	}

	// Register observers automatically from YAML config
	if err := observerOrchestrator.RegisterObserversFromYAML(config, logger); err != nil {
		log.Fatalf("Failed to register observers from YAML: %v", err)
	}

	// Start orchestrator
	if err := observerOrchestrator.Start(ctx); err != nil {
		log.Fatalf("Failed to start observer orchestrator: %v", err)
	}

	logger.Info("Tapio observers started successfully",
		zap.String("config_file", *configFile),
		zap.Int("workers", orchestratorConfig.Workers),
		zap.Int("buffer_size", orchestratorConfig.BufferSize))

	// Start health monitoring
	go monitorObserverHealth(ctx, observerOrchestrator, logger)

	// Wait for shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down observers...")
	observerOrchestrator.Stop()
}

// monitorObserverHealth periodically checks observer health
func monitorObserverHealth(ctx context.Context, orchestrator *orchestrator.ObserverOrchestrator, logger *zap.Logger) {
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
					logger.Warn("Observer unhealthy",
						zap.String("observer", name),
						zap.String("error", status.Error),
						zap.Time("last_event", status.LastEvent))
				}
			}

			logger.Info("Observer health check",
				zap.Int("healthy", healthy),
				zap.Int("unhealthy", unhealthy),
				zap.Int("total", len(healthStatus)))
		}
	}
}
