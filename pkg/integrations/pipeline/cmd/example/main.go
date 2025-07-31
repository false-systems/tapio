package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/integrations/pipeline"
	"go.uber.org/zap"
)

func main() {
	// Create logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Create collector manager
	managerConfig := collectors.DefaultManagerConfig()
	managerConfig.EventBufferSize = 10000
	manager := collectors.NewManager(managerConfig)

	// Create pipeline
	pipelineConfig := pipeline.DefaultConfig()
	pipelineConfig.EnrichmentEnabled = true
	pipelineConfig.BatchSize = 10
	pipelineConfig.BatchTimeout = 2 * time.Second

	collectorPipeline, err := pipeline.NewCollectorIntelligencePipeline(manager, logger, pipelineConfig)
	if err != nil {
		log.Fatalf("Failed to create pipeline: %v", err)
	}

	// Register collectors (simplified for example)
	if err := registerExampleCollectors(manager); err != nil {
		log.Fatalf("Failed to register collectors: %v", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start collector manager
	logger.Info("Starting collector manager...")
	if err := manager.Start(ctx); err != nil {
		log.Fatalf("Failed to start manager: %v", err)
	}

	// Start pipeline
	logger.Info("Starting collector intelligence pipeline...")
	if err := collectorPipeline.Start(); err != nil {
		log.Fatalf("Failed to start pipeline: %v", err)
	}

	// Monitor pipeline in a separate goroutine
	go monitorPipeline(collectorPipeline, logger)

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Received shutdown signal, stopping...")

	// Stop pipeline first
	if err := collectorPipeline.Stop(); err != nil {
		logger.Error("Error stopping pipeline", zap.Error(err))
	}

	// Stop manager
	if err := manager.Stop(); err != nil {
		logger.Error("Error stopping manager", zap.Error(err))
	}

	logger.Info("Example pipeline stopped successfully")
}

func registerExampleCollectors(manager *collectors.Manager) error {
	// In a real deployment, you would register actual collectors here
	// For this example, we'll create a mock collector that generates events

	mockCollector := NewMockCollector("mock-collector")
	if err := manager.Register("mock", mockCollector); err != nil {
		return fmt.Errorf("failed to register mock collector: %w", err)
	}

	return nil
}

func monitorPipeline(pipeline *pipeline.CollectorIntelligencePipeline, logger *zap.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		stats := pipeline.GetStatistics()
		logger.Info("Pipeline statistics", zap.Any("stats", stats))

		// Check for findings
		findings := pipeline.GetLatestFindings()
		if findings != nil {
			logger.Info("Found correlations!", zap.Any("findings", findings))
		}

		// Check semantic groups
		groups := pipeline.GetSemanticGroups()
		if groups != nil {
			logger.Info("Semantic groups", zap.Any("groups", groups))
		}
	}
}
