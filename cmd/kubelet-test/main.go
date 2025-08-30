package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yairfalse/tapio/pkg/collectors/kubelet"
	"github.com/yairfalse/tapio/pkg/collectors/orchestrator"
	"github.com/yairfalse/tapio/pkg/config"
	"go.uber.org/zap"
)

var (
	kubeletAddress = flag.String("kubelet-address", "localhost:10250", "Kubelet address")
	natsURL        = flag.String("nats", "nats://localhost:4222", "NATS server URL")
	logLevel       = flag.String("log-level", "debug", "Log level (debug, info, warn, error)")
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

	logger.Info("Starting Kubelet-only collector test",
		zap.String("kubelet_address", *kubeletAddress),
		zap.String("nats_url", *natsURL))

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create NATS config
	natsConfig := config.DefaultNATSConfig()
	natsConfig.URL = *natsURL

	// Create orchestrator
	orchestratorConfig := orchestrator.Config{
		NATSConfig: natsConfig,
		BufferSize: 1000,
		Workers:    2,
	}

	collectorOrchestrator, err := orchestrator.New(logger, orchestratorConfig)
	if err != nil {
		log.Fatalf("Failed to create orchestrator: %v", err)
	}

	// Create Kubelet collector ONLY (no eBPF)
	logger.Info("Creating Kubelet collector...")
	kubeletConfig := kubelet.DefaultConfig()
	kubeletConfig.Address = *kubeletAddress
	kubeletConfig.Insecure = true
	kubeletConfig.Logger = logger

	kubeletCollector, err := kubelet.NewCollector("kubelet", kubeletConfig)
	if err != nil {
		log.Fatalf("Failed to create kubelet collector: %v", err)
	}

	// Register with orchestrator
	logger.Info("Registering Kubelet collector...")
	if err := collectorOrchestrator.RegisterCollector("kubelet", kubeletCollector); err != nil {
		log.Fatalf("Failed to register kubelet collector: %v", err)
	}

	// Start orchestrator
	logger.Info("Starting orchestrator...")
	if err := collectorOrchestrator.Start(ctx); err != nil {
		log.Fatalf("Failed to start orchestrator: %v", err)
	}

	logger.Info("Kubelet collector started successfully")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down...")
	collectorOrchestrator.Stop()
	logger.Info("Shutdown complete")
}