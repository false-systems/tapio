package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/kernel"
	"github.com/yairfalse/tapio/pkg/collectors/kubelet"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"go.uber.org/zap"
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

	// Track enabled collectors
	enabledCollectors := []string{}

	// Register collectors based on YAML config
	if err := registerCollectors(collectorOrchestrator, config, logger, &enabledCollectors); err != nil {
		log.Fatalf("Failed to register collectors: %v", err)
	}

	// Validate we have at least one collector
	if len(enabledCollectors) == 0 {
		log.Fatal("No collectors enabled. Enable at least one collector in the config file.")
	}

	// Start orchestrator
	if err := collectorOrchestrator.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector orchestrator: %v", err)
	}

	logger.Info("Tapio collectors started successfully",
		zap.String("config_file", *configFile),
		zap.Strings("collectors", enabledCollectors),
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

// registerCollectors registers all enabled collectors from config
func registerCollectors(orch *orchestrator.CollectorOrchestrator, config *orchestrator.YAMLConfig, logger *zap.Logger, enabled *[]string) error {
	// Kernel collector
	if config.IsCollectorEnabled("kernel") {
		cfg, _ := config.GetCollectorConfig("kernel")
		kernelConfig := &kernel.Config{
			Name:       "kernel",
			BufferSize: getIntOrDefault(cfg.BufferSize, 10000),
			EnableEBPF: cfg.EnableEBPF,
		}

		collector, err := kernel.NewCollector("kernel", kernelConfig)
		if err != nil {
			logger.Error("Failed to create kernel collector", zap.Error(err))
		} else {
			if err := orch.RegisterCollector("kernel", collector); err != nil {
				logger.Error("Failed to register kernel collector", zap.Error(err))
			} else {
				*enabled = append(*enabled, "kernel")
			}
		}
	}

	// Kubelet collector
	if config.IsCollectorEnabled("kubelet") {
		cfg, _ := config.GetCollectorConfig("kubelet")
		kubeletConfig := kubelet.DefaultConfig()

		if cfg.Address != "" {
			kubeletConfig.Address = cfg.Address
		}
		kubeletConfig.Insecure = cfg.Insecure
		kubeletConfig.Logger = logger

		collector, err := kubelet.NewCollector("kubelet", kubeletConfig)
		if err != nil {
			logger.Error("Failed to create kubelet collector", zap.Error(err))
		} else {
			if err := orch.RegisterCollector("kubelet", collector); err != nil {
				logger.Error("Failed to register kubelet collector", zap.Error(err))
			} else {
				*enabled = append(*enabled, "kubelet")
			}
		}
	}

	// Add more collectors here following the same pattern...
	// DNS, Network, CRI-eBPF, etc.

	return nil
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

// Helper function for default values
func getIntOrDefault(value, defaultVal int) int {
	if value == 0 {
		return defaultVal
	}
	return value
}
