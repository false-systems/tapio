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
	"github.com/yairfalse/tapio/pkg/collectors/etcd"
	etcdBPF "github.com/yairfalse/tapio/pkg/collectors/etcd/bpf"
	"github.com/yairfalse/tapio/pkg/collectors/kernel"
	"github.com/yairfalse/tapio/pkg/collectors/kubeapi"
	"github.com/yairfalse/tapio/pkg/collectors/kubelet"
	namespace_collector "github.com/yairfalse/tapio/pkg/collectors/namespace-collector"
	namespaceBPF "github.com/yairfalse/tapio/pkg/collectors/namespace-collector/bpf"
	"github.com/yairfalse/tapio/pkg/collectors/network/pkg/collectors/network"
	"github.com/yairfalse/tapio/pkg/collectors/pipeline"
	"github.com/yairfalse/tapio/pkg/collectors/systemd"
	"github.com/yairfalse/tapio/pkg/config"
	"go.uber.org/zap"
)

var (
	natsURL         = flag.String("nats", "", "NATS server URL (overrides config)")
	enableKubeAPI   = flag.Bool("enable-kubeapi", true, "Enable KubeAPI collector")
	enableEBPF      = flag.Bool("enable-ebpf", true, "Enable eBPF collector")
	enableSystemd   = flag.Bool("enable-systemd", true, "Enable systemd collector")
	enableEtcd      = flag.Bool("enable-etcd", true, "Enable etcd collector")
	enableNamespace = flag.Bool("enable-namespace", true, "Enable namespace collector")
	enableKubelet   = flag.Bool("enable-kubelet", true, "Enable kubelet collector")
	enableNetwork   = flag.Bool("enable-network", true, "Enable network collector")
	kubeletAddress  = flag.String("kubelet-address", "localhost:10250", "Kubelet address")
	etcdEndpoints   = flag.String("etcd-endpoints", "localhost:2379", "Etcd endpoints (comma-separated)")
	logLevel        = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	workerCount     = flag.Int("workers", 4, "Number of pipeline workers")
	bufferSize      = flag.Int("buffer-size", 10000, "Event buffer size")
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

	// Create EventPipeline config
	pipelineConfig := pipeline.Config{
		NATSConfig: natsConfig,
		BufferSize: *bufferSize,
		Workers:    *workerCount,
	}

	// Create EventPipeline
	eventPipeline, err := pipeline.New(logger, pipelineConfig)
	if err != nil {
		log.Fatalf("Failed to create event pipeline: %v", err)
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
			if err := eventPipeline.RegisterCollector("kubeapi", kubeapiCollector); err != nil {
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
			if err := eventPipeline.RegisterCollector("kernel", kernelCollector); err != nil {
				logger.Error("Failed to register kernel collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "kernel")
			}
		}
	}

	if *enableSystemd {
		systemdConfig := systemd.DefaultConfig()
		systemdConfig.EnableJournal = true // Enable journald collection
		systemdCollector, err := systemd.NewCollector("systemd", systemdConfig)
		if err != nil {
			logger.Error("Failed to create systemd collector", zap.Error(err))
		} else {
			if err := eventPipeline.RegisterCollector("systemd", systemdCollector); err != nil {
				logger.Error("Failed to register systemd collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "systemd")
			}
		}
	}

	if *enableEtcd {
		etcdConfig := etcd.Config{
			Endpoints: []string{*etcdEndpoints},
			// Add auth if needed
		}

		// Check if etcd eBPF is available
		if etcdBPF.IsSupported() {
			etcdConfig.EnableEBPF = true
		}

		etcdCollector, err := etcd.NewCollector("etcd", etcdConfig)
		if err != nil {
			logger.Error("Failed to create etcd collector", zap.Error(err))
		} else {
			if err := eventPipeline.RegisterCollector("etcd", etcdCollector); err != nil {
				logger.Error("Failed to register etcd collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "etcd")
			}
		}
	}

	if *enableNamespace {
		namespaceConfig := namespace_collector.DefaultConfig()

		// Check if namespace eBPF is available
		if namespaceBPF.IsSupported() {
			namespaceConfig.EnableEBPF = true
		}

		namespaceCollector, err := namespace_collector.NewCollector("namespace")
		if err != nil {
			logger.Error("Failed to create namespace collector", zap.Error(err))
		} else {
			if err := eventPipeline.RegisterCollector("namespace", namespaceCollector); err != nil {
				logger.Error("Failed to register namespace collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "namespace")
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
			if err := eventPipeline.RegisterCollector("kubelet", kubeletCollector); err != nil {
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
			if err := eventPipeline.RegisterCollector("network", networkCollector); err != nil {
				logger.Error("Failed to register network collector", zap.Error(err))
			} else {
				enabledCollectors = append(enabledCollectors, "network")
			}
		}
	}

	// Validate we have at least one collector
	if len(enabledCollectors) == 0 {
		log.Fatal("No collectors enabled. Enable at least one collector.")
	}

	// Start pipeline
	if err := eventPipeline.Start(ctx); err != nil {
		log.Fatalf("Failed to start event pipeline: %v", err)
	}

	logger.Info("Tapio collectors started successfully",
		zap.String("nats_url", natsConfig.URL),
		zap.Strings("collectors", enabledCollectors),
		zap.Int("workers", *workerCount),
		zap.Int("buffer_size", *bufferSize))

	// Start health monitoring
	go monitorCollectorHealth(ctx, eventPipeline, logger)

	// Wait for shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down collectors...")
	eventPipeline.Stop()
}

// monitorCollectorHealth periodically checks collector health
func monitorCollectorHealth(ctx context.Context, eventPipeline *pipeline.EventPipeline, logger *zap.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Get health status from all collectors
			healthStatus := eventPipeline.GetHealthStatus()

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
