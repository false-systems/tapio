package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/internal/integrations/loader"
	"go.uber.org/zap"
)

func main() {
	// Create logger
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Configuration
	config := loader.DefaultConfig()

	// Override with custom values
	config.NATS.URL = "nats://localhost:4222"
	config.Neo4j.URI = "bolt://localhost:7687"
	config.Neo4j.Username = "neo4j"
	config.Neo4j.Password = "password"
	config.BatchSize = 10
	config.BatchTimeout = 5 * time.Second

	// Validate config
	if err := config.Validate(); err != nil {
		log.Fatalf("Invalid config: %v", err)
	}

	// Create loader
	ldr, err := loader.NewLoader(logger, config)
	if err != nil {
		log.Fatalf("Failed to create loader: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("Received shutdown signal")
		cancel()
	}()

	// Start the loader
	logger.Info("Starting observation loader...",
		zap.String("nats", config.NATS.URL),
		zap.String("neo4j", config.Neo4j.URI),
		zap.Int("batch_size", config.BatchSize),
	)

	if err := ldr.Start(ctx); err != nil {
		log.Fatalf("Failed to start loader: %v", err)
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	logger.Info("Shutting down loader...")
	if err := ldr.Stop(); err != nil {
		logger.Error("Error during shutdown", zap.Error(err))
	}

	logger.Info("Loader stopped successfully")
}
