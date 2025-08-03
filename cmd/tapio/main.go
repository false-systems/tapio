package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yairfalse/tapio/pkg/collectors/kubeapi"
	"github.com/yairfalse/tapio/pkg/collectors/pipeline"
	"github.com/yairfalse/tapio/pkg/config"
	"go.uber.org/zap"
)

var (
	natsURL = flag.String("nats", "", "NATS server URL (overrides config)")
)

func main() {
	flag.Parse()

	// Create logger
	logger, _ := zap.NewProduction()
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
		BufferSize: 10000,
		Workers:    4,
	}

	// Create EventPipeline
	eventPipeline, err := pipeline.New(logger, pipelineConfig)
	if err != nil {
		log.Fatalf("Failed to create event pipeline: %v", err)
	}

	// Create kubeapi collector
	kubeapiConfig := kubeapi.DefaultConfig()
	kubeapiCollector, err := kubeapi.New(logger, kubeapiConfig)
	if err != nil {
		log.Fatalf("Failed to create kubeapi collector: %v", err)
	}

	// Register collector with pipeline
	if err := eventPipeline.RegisterCollector("kubeapi", kubeapiCollector); err != nil {
		log.Fatalf("Failed to register kubeapi collector: %v", err)
	}

	// Start pipeline
	if err := eventPipeline.Start(ctx); err != nil {
		log.Fatalf("Failed to start event pipeline: %v", err)
	}

	logger.Info("Tapio started successfully",
		zap.String("nats_url", natsConfig.URL),
		zap.String("collector", "kubeapi"))

	// Wait for shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down...")
	eventPipeline.Stop()
}
