package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yairfalse/tapio/pkg/integrations/loader"
	"go.uber.org/zap"
)

func main() {
	// Create logger
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Configuration
	config := &loader.Config{
		NATSURL:       "nats://localhost:4222",
		Neo4jURL:      "bolt://localhost:7687",
		Neo4jUser:     "neo4j",
		Neo4jPassword: "password",
		BatchSize:     10,
		BatchTimeout:  "5s",
		WorkerCount:   2,
		Subjects: []string{
			"observations.kernel",
			"observations.kubeapi",
			"observations.dns",
			"observations.etcd",
		},
	}

	// Validate config
	if err := config.Validate(); err != nil {
		log.Fatalf("Invalid config: %v", err)
	}

	// Create loader
	ldr, err := loader.New(logger, config)
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
		zap.String("nats", config.NATSURL),
		zap.String("neo4j", config.Neo4jURL),
		zap.Int("workers", config.WorkerCount),
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
