package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/dns"
	"github.com/yairfalse/tapio/pkg/collectors/kernel"
	"github.com/yairfalse/tapio/pkg/collectors/kubeapi"
	"github.com/yairfalse/tapio/pkg/collectors/kubelet"
	"github.com/yairfalse/tapio/pkg/collectors/network"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"github.com/yairfalse/tapio/pkg/collectors/otel"
	runtime_signals "github.com/yairfalse/tapio/pkg/collectors/runtime-signals"
	"github.com/yairfalse/tapio/pkg/collectors/systemd"
	"github.com/yairfalse/tapio/pkg/config"
	"go.uber.org/zap"
)

var (
	natsURL        = flag.String("nats", "", "NATS server URL (overrides config)")
	enableKubeAPI  = flag.Bool("enable-kubeapi", true, "Enable KubeAPI collector")
	enableEBPF     = flag.Bool("enable-ebpf", true, "Enable eBPF collector")
	enableDNS      = flag.Bool("enable-dns", true, "Enable intelligent DNS collector")
	enableSystemd  = flag.Bool("enable-systemd", true, "Enable systemd collector")
	enableRuntime  = flag.Bool("enable-runtime", true, "Enable runtime signals collector")
	enableKubelet  = flag.Bool("enable-kubelet", true, "Enable kubelet collector")
	enableNetwork  = flag.Bool("enable-network", true, "Enable network collector")
	enableOTEL     = flag.Bool("enable-otel", true, "Enable OTEL collector")
	kubeletAddress = flag.String("kubelet-address", "localhost:10250", "Kubelet address")
	logLevel       = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	workerCount    = flag.Int("workers", 4, "Number of orchestrator workers")
	bufferSize     = flag.Int("buffer-size", 10000, "Event buffer size")
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

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create NATS config
	natsConfig := config.DefaultNATSConfig()
	if *natsURL != "" {
		natsConfig.URL = *natsURL
	}

	// Create CollectorOrchestrator config
	orchestratorConfig := orchestrator.Config{
		NATSConfig: natsConfig,
		BufferSize: *bufferSize,
		Workers:    *workerCount,
	}

	// Create CollectorOrchestrator
	collectorOrchestrator, err := orchestrator.New(logger, orchestratorConfig)
	if err != nil {
		log.Fatalf("Failed to create collector orchestrator: %v", err)
	}

	// Track enabled collectors
	enabledCollectors := []string{}

	// Create and register collectors based on flags
	if *enableKubeAPI {
		kubeapiConfig := kubeapi.DefaultConfig()
		kubeapiCollector, err := kubeapi.New(logger, kubeapiConfig)
		if err != nil {
			logger.Error("Failed to create kubeapi collector", zap.Error(err))
		} else {
			if err := collectorOrchestrator.RegisterCollector("kubeapi", kubeapiCollector); err != nil {
				logger.Error("Failed to register kubeapi collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "kubeapi")
			}
		}
	}

	if *enableEBPF {
		// Create kernel collector
		kernelConfig := &kernel.Config{
			Name:       "kernel",
			BufferSize: 10000,
			EnableEBPF: true,
		}
		kernelCollector, err := kernel.NewCollector("kernel", kernelConfig)
		if err != nil {
			logger.Error("Failed to create kernel collector", zap.Error(err))
		} else {
			if err := collectorOrchestrator.RegisterCollector("kernel", kernelCollector); err != nil {
				logger.Error("Failed to register kernel collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "kernel")
			}
		}
	}

	if *enableSystemd {
		systemdConfig := systemd.DefaultConfig()
		systemdConfig.EnableJournal = true // Enable journald collection
		systemdCollector, err := systemd.NewCollector("systemd", systemdConfig, logger)
		if err != nil {
			logger.Error("Failed to create systemd collector", zap.Error(err))
		} else {
			if err := collectorOrchestrator.RegisterCollector("systemd", systemdCollector); err != nil {
				logger.Error("Failed to register systemd collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "systemd")
			}
		}
	}

	if *enableRuntime {
		// Create runtime signals collector (transformed from namespace collector)
		runtimeCollector, err := runtime_signals.NewCollector("runtime-signals")
		if err != nil {
			logger.Error("Failed to create runtime signals collector", zap.Error(err))
		} else {
			if err := collectorOrchestrator.RegisterCollector("runtime-signals", runtimeCollector); err != nil {
				logger.Error("Failed to register runtime signals collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "runtime-signals")
			}
		}
	}

	if *enableKubelet {
		kubeletConfig := kubelet.DefaultConfig()
		kubeletConfig.Address = *kubeletAddress
		kubeletConfig.Logger = logger
		// For local testing, might need insecure mode
		if *kubeletAddress == "localhost:10250" {
			kubeletConfig.Insecure = true
		}

		kubeletCollector, err := kubelet.NewCollector("kubelet", kubeletConfig)
		if err != nil {
			logger.Error("Failed to create kubelet collector", zap.Error(err))
		} else {
			if err := collectorOrchestrator.RegisterCollector("kubelet", kubeletCollector); err != nil {
				logger.Error("Failed to register kubelet collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "kubelet")
			}
		}
	}

	if *enableNetwork {
		networkConfig := network.DefaultIntelligenceConfig()
		networkCollector, err := network.NewIntelligenceCollector("network", networkConfig, logger)
		if err != nil {
			logger.Error("Failed to create network collector", zap.Error(err))
		} else {
			if err := collectorOrchestrator.RegisterCollector("network", networkCollector); err != nil {
				logger.Error("Failed to register network collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "network")
			}
		}
	}

	if *enableDNS {
		dnsConfig := dns.DefaultConfig()
		// Enable intelligence features by default
		dnsConfig.EnableIntelligence = true
		dnsConfig.ContainerIDExtraction = true
		dnsConfig.ParseAnswers = true

		dnsCollector, err := dns.NewCollector("dns", dnsConfig)
		if err != nil {
			logger.Error("Failed to create DNS collector", zap.Error(err))
		} else {
			if err := collectorOrchestrator.RegisterCollector("dns", dnsCollector); err != nil {
				logger.Error("Failed to register DNS collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "dns")
				logger.Info("DNS collector registered with intelligent features",
					zap.String("filtering_mode", dnsConfig.SmartFilterConfig.Mode.String()),
					zap.Bool("learning_enabled", dnsConfig.LearningConfig.Enabled),
					zap.Bool("circuit_breaker_enabled", dnsConfig.CircuitBreakerConfig.Enabled))
			}
		}
	}

	if *enableOTEL {
		otelConfig := otel.DefaultConfig()
		otelCollector, err := otel.NewCollector("otel", otelConfig)
		if err != nil {
			logger.Error("Failed to create OTEL collector", zap.Error(err))
		} else {
			if err := collectorOrchestrator.RegisterCollector("otel", otelCollector); err != nil {
				logger.Error("Failed to register OTEL collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "otel")
			}
		}
	}

	// Validate we have at least one collector
	if len(enabledCollectors) == 0 {
		log.Fatal("No collectors enabled. Enable at least one collector.")
	}

	// Start pipeline
	if err := collectorOrchestrator.Start(ctx); err != nil {
		log.Fatalf("Failed to start collector orchestrator: %v", err)
	}

	logger.Info("Tapio collectors started successfully",
		zap.String("nats_url", natsConfig.URL),
		zap.Strings("collectors", enabledCollectors),
		zap.Int("workers", *workerCount),
		zap.Int("buffer_size", *bufferSize))

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
			// Get health status from all collectors
			healthStatus := orchestrator.GetHealthStatus()

			// Log health summary
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

// Helper function to get collector status
func getCollectorStatus(c collectors.Collector) string {
	if c.IsHealthy() {
		return "healthy"
	}
	return "unhealthy"
}
